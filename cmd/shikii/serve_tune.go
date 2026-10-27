package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/spec"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Runs tune from the page and MCP, reads the results, and adopts only when a person has confirmed "the meaning is the same".
//
//	GET  /v1/tune?spec=id        → {running, error, latest: tuneResult}
//	POST /v1/tune/run {spec, propose} → 202. runTune in the background. The proposer is serve's --proposer
//	POST /v1/tune/sync {spec, propose} → runs runTune on the spot and returns the result (used by MCP)
//	POST /v1/tune/adopt {spec, run, meaning_same, note} → adopt / reject. Without meaning_same 400; a run already adopted or reverted 409
//	POST /v1/tune/review {spec, run, keep} → for wording an agent checked and adopted automatically, the person says "keep as is" or "revert" (0.9.2)
//
// The adopt tool is not exposed over MCP. The agent checks the meaning with hook variants check (with a Japanese translation); when it cannot decide, and for reverting, the person uses the page.

func (s *server) tuneOptsFor(sp specRef, propose int) tuneOpts {
	return tuneOpts{specPath: sp.path, ef: s.o.ef, storeDir: s.o.storeDir, folds: 5,
		propose: propose, proposer: s.o.proposer, proposerModel: s.o.proposerModel, wikiDir: s.o.wikiDir}
}

type specRef struct{ id, path string }

func (s *server) specRef(id string) (specRef, error) {
	sp, err := s.spec(id)
	if err != nil {
		return specRef{}, err
	}
	return specRef{id: sp.ID, path: sp.Path()}, nil
}

// runTuneCaptured runs runTune with its own report destination. Only changes to specs of the same work are serialized.
func (s *server) runTuneCaptured(o tuneOpts) (*tuneResult, string, error) {
	defer s.lockCalibration()()
	var buf bytes.Buffer
	o.output = &buf
	var res *tuneResult
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("tune panicked: %v", r)
			}
		}()
		var e error
		res, e = runTune(o)
		return e
	}()
	return res, strings.TrimSpace(buf.String()), err
}

func (s *server) handleTuneGet(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	if _, err := s.specRef(id); errors.Is(err, errNotFound) {
		writeUnknownSpec(w, id)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.mu.Lock()
	running, lastErr := s.tuning[id], s.tuneErr[id]
	s.mu.Unlock()
	out := map[string]any{"spec": id, "running": running, "error": lastErr, "proposer": s.o.proposer != ""}
	if res, err := latestTuneResult(s.o.wikiDir, id); err == nil {
		out["latest"] = res
	}
	// The current candidates (written by a person on the page or in YAML, or added by the proposer). The next tune (manual or the hook's automatic one) scores them
	variants := []map[string]any{}
	if sp, err := s.spec(id); err == nil {
		for _, v := range sp.Variants {
			variants = append(variants, map[string]any{"name": v.Name, "question": v.Question, "options": v.Options})
		}
	}
	out["variants"] = variants
	writeJSON(w, 200, out)
}

// variantNameRe is the shape of a candidate name (it goes into wiki/runs file names and tables, so no line breaks or long sentences). variantTextMax is the length limit (bytes) for the question and descriptions.
var variantNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

const variantTextMax = 2000

// handleTuneVariant adds one candidate wording that a person wrote on the page to the spec's variants (POST /v1/tune/variant: spec, name, question, options{id: description}).
// It writes only variants (the same path as the proposer: reread the file and add, so Save does not touch other fields). The candidate is checked as a spec before writing (e.g. a choice id that does not exist is 400).
// It only adds; it neither scores nor adopts: the next tune scores, and a person adopts.
func (s *server) handleTuneVariant(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec     string            `json:"spec"`
		Name     string            `json:"name"`
		Question string            `json:"question"`
		Options  map[string]string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	ref, err := s.specRef(req.Spec)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, req.Spec)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	v := spec.Variant{Name: strings.TrimSpace(req.Name), Question: strings.TrimSpace(req.Question)}
	for id, d := range req.Options {
		id, d = strings.TrimSpace(id), strings.TrimSpace(d)
		if id == "" || d == "" { // ignore empty lines (blank lines in the page's textarea)
			continue
		}
		if v.Options == nil {
			v.Options = map[string]string{}
		}
		v.Options[id] = d
	}
	if !variantNameRe.MatchString(v.Name) {
		writeErr(w, 400, errors.New("name is required: a short label for this rewording, 1 to 40 letters, digits, dashes or underscores"))
		return
	}
	if len(v.Question) > variantTextMax {
		writeErr(w, 400, fmt.Errorf("the question is too long (%d bytes; at most %d)", len(v.Question), variantTextMax))
		return
	}
	for id, d := range v.Options {
		if len(d) > variantTextMax {
			writeErr(w, 400, fmt.Errorf("the description of %q is too long (%d bytes; at most %d)", id, len(d), variantTextMax))
			return
		}
	}
	if v.Question == "" && len(v.Options) == 0 {
		writeErr(w, 400, errors.New("a rewording needs a question or at least one option description"))
		return
	}
	defer s.lockCalibration()() // read and write under one lock (do not write at the same time as tune --write or adopt)
	fresh, err := spec.Load(ref.path)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	for _, have := range fresh.Variants {
		if have.Name == v.Name {
			writeErr(w, 409, fmt.Errorf("a rewording named %q already exists; pick another name", v.Name))
			return
		}
	}
	fresh.Variants = append(fresh.Variants, v)
	if err := fresh.Validate(); err != nil {
		writeErr(w, 400, fmt.Errorf("this rewording is not valid for the spec: %w", err))
		return
	}
	if err := fresh.Save(); err != nil {
		writeErr(w, 500, err)
		return
	}
	names := make([]string, 0, len(fresh.Variants))
	for _, x := range fresh.Variants {
		names = append(names, x.Name)
	}
	log.Printf("tune %s: rewording %q added by a person (%d candidates)", ref.id, v.Name, len(names))
	writeJSON(w, 200, map[string]any{"spec": ref.id, "added": v.Name, "variants": names})
}

func (s *server) decodeTuneReq(w http.ResponseWriter, r *http.Request) (specRef, int, bool) {
	if !acceptJSONPost(w, r) {
		return specRef{}, 0, false
	}
	var req struct {
		Spec    string `json:"spec"`
		Propose int    `json:"propose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return specRef{}, 0, false
	}
	ref, err := s.specRef(req.Spec)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, req.Spec)
		return specRef{}, 0, false
	} else if err != nil {
		writeErr(w, 500, err)
		return specRef{}, 0, false
	}
	if req.Propose > 0 && s.o.proposer == "" {
		writeErr(w, 400, errors.New("no proposer configured (start serve with --proposer URL) ; run without propose to score the spec's own variants"))
		return specRef{}, 0, false
	}
	return ref, req.Propose, true
}

// tuneFinished, when set (tests), is called by a background tune after its state (running, error) is updated, so a test waits for that
// one moment instead of polling GET /v1/tune.
var tuneFinished func(spec string)

func (s *server) handleTuneRun(w http.ResponseWriter, r *http.Request) {
	ref, propose, ok := s.decodeTuneReq(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	if s.tuning[ref.id] {
		s.mu.Unlock()
		writeErr(w, 409, errors.New("a tune is already running for this spec"))
		return
	}
	s.tuning[ref.id] = true
	s.tuneErr[ref.id] = ""
	s.mu.Unlock()
	go func() {
		_, report, err := s.runTuneCaptured(s.tuneOptsFor(ref, propose)) // no reply waits: the route answered 202
		s.mu.Lock()
		s.tuning[ref.id] = false
		if err != nil {
			s.tuneErr[ref.id] = err.Error()
		}
		s.mu.Unlock()
		if err != nil {
			log.Printf("tune %s: FAILED: %v", ref.id, err)
		} else {
			log.Printf("tune %s: %s", ref.id, lastLine(report))
		}
		if tuneFinished != nil {
			tuneFinished(ref.id)
		}
	}()
	writeJSON(w, 202, map[string]any{"spec": ref.id, "running": true})
}

func (s *server) handleTuneSync(w http.ResponseWriter, r *http.Request) {
	ref, propose, ok := s.decodeTuneReq(w, r)
	if !ok {
		return
	}
	res, report, err := s.runTuneCaptured(s.tuneOptsFor(ref, propose))
	if err != nil {
		writeErr(w, 422, fmt.Errorf("%v\n%s", err, report))
		return
	}
	writeJSON(w, 200, res)
}

func (s *server) handleTuneAdopt(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec        string `json:"spec"`
		Run         string `json:"run"`
		MeaningSame *bool  `json:"meaning_same"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	if req.MeaningSame == nil {
		writeErr(w, 400, errors.New("meaning_same (true/false) is required: a person must say whether the new wording still asks the same thing"))
		return
	}
	if req.Run == "" {
		writeErr(w, 400, errors.New("run is required (from GET /v1/tune)"))
		return
	}
	// read and write under one lock (so concurrent adopts of the same run get 409); the run's own lock is shared with the agent's adoption (hook variants check)
	defer s.lockCalibration()()
	res, err := loadTuneResult(s.o.wikiDir, req.Run)
	if err != nil {
		writeErr(w, 404, fmt.Errorf("unknown run %q", req.Run))
		return
	}
	unlock, err := lockTuneRun(s.o.wikiDir, res.Run)
	if err != nil {
		writeErr(w, 409, err)
		return
	}
	defer unlock()
	if res, err = loadTuneResult(s.o.wikiDir, req.Run); err != nil {
		writeErr(w, 404, fmt.Errorf("unknown run %q", req.Run))
		return
	}
	if req.Spec != "" && res.SpecID != req.Spec {
		writeErr(w, 400, fmt.Errorf("run %s belongs to spec %s", req.Run, res.SpecID))
		return
	}
	if res.Adopted {
		writeErr(w, 409, fmt.Errorf("already adopted as %s", res.AdoptedPath))
		return
	}
	if res.Reverted {
		writeErr(w, 409, errors.New("this wording was adopted and then undone by a person; run tune again"))
		return
	}
	if !res.Adoptable {
		writeErr(w, 409, errors.New("this run has nothing to adopt (the current wording was kept)"))
		return
	}
	recordMeaningCheck(res, *req.MeaningSame, req.Note, "page")
	if !*req.MeaningSame {
		writeJSON(w, 200, map[string]any{"adopted": false, "run": res.Run, "reason": "meaning judged to have changed; recorded"})
		return
	}
	var buf bytes.Buffer
	newPath, err := func() (p string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("adopt panicked: %v", r)
			}
		}()
		return adoptWording(res, s.o.ef, s.o.storeDir, "", &buf)
	}()
	saveTuneResult(res)
	if err != nil {
		appendRunNote(res.RunPage, fmt.Sprintf("**Not adopted:** %s", strings.SplitN(err.Error(), "\n", 2)[0]))
		writeErr(w, 422, fmt.Errorf("%v\n%s", err, strings.TrimSpace(buf.String())))
		return
	}
	appendRunNote(res.RunPage, fmt.Sprintf("**Adopted** as %s via the page after a person confirmed the meaning.", newPath))
	writeJSON(w, 200, map[string]any{"adopted": true, "run": res.Run, "path": newPath, "report": strings.TrimSpace(buf.String())})
}

// handleTuneReview is the person's answer to wording an agent confirmed has the same meaning and adopted automatically (0.9.2).
// keep: true means "keep as is" (kept as the person's confirmation). false means "revert": the adopted version is renamed to <file>.reverted (not deleted),
// and the hook and serve read the previous version. That run can no longer be adopted. The rejection also stays on the spec's page, and the next proposer reads it.
func (s *server) handleTuneReview(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec string `json:"spec"`
		Run  string `json:"run"`
		Keep *bool  `json:"keep"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	if req.Keep == nil || req.Run == "" {
		writeErr(w, 400, errors.New("run and keep (true: keep the new wording, false: undo it) are required"))
		return
	}
	defer s.lockCalibration()()
	res, err := loadTuneResult(s.o.wikiDir, req.Run)
	if err != nil {
		writeErr(w, 404, fmt.Errorf("unknown run %q", req.Run))
		return
	}
	unlock, err := lockTuneRun(s.o.wikiDir, res.Run)
	if err != nil {
		writeErr(w, 409, err)
		return
	}
	defer unlock()
	if res, err = loadTuneResult(s.o.wikiDir, req.Run); err != nil {
		writeErr(w, 404, fmt.Errorf("unknown run %q", req.Run))
		return
	}
	if req.Spec != "" && res.SpecID != req.Spec {
		writeErr(w, 400, fmt.Errorf("run %s belongs to spec %s", req.Run, res.SpecID))
		return
	}
	if !res.AutoAdopted || !res.Adopted || res.Reverted {
		writeErr(w, 409, errors.New("this run is not a wording the agent adopted that is still in use"))
		return
	}
	if *req.Keep {
		recordMeaningCheck(res, true, "kept by a person after the agent adopted it", "page")
		writeJSON(w, 200, map[string]any{"reverted": false, "run": res.Run})
		return
	}
	if err := os.Rename(res.AdoptedPath, res.AdoptedPath+".reverted"); err != nil {
		writeErr(w, 500, err)
		return
	}
	res.Adopted, res.Reverted = false, true
	recordMeaningCheck(res, false, "undone by a person after the agent adopted it", "page")
	appendRunNote(res.RunPage, fmt.Sprintf("**Undone** by a person: %s was renamed to %s.reverted; the previous wording is used again.", res.AdoptedPath, filepath.Base(res.AdoptedPath)))
	writeJSON(w, 200, map[string]any{"reverted": true, "run": res.Run})
}
