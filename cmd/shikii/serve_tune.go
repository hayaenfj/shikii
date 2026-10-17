package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// 画面と MCP から tune を回し、結果を読み、人が「意味は同じ」と確かめた時だけ採用する。
//
//	GET  /v1/tune?spec=id        → {running, error, latest: tuneResult}
//	POST /v1/tune/run {spec, propose} → 202。裏で runTune。提案役は serve の --proposer
//	POST /v1/tune/sync {spec, propose} → その場で runTune して結果(MCP が使う)
//	POST /v1/tune/adopt {spec, run, meaning_same, note} → 採用 / 却下。meaning_same 無しは 400、採用済みは 409
//
// 採用の道具は MCP には出さない。文言の意味が変わっていないかは人にしか分からない。

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

// runTuneCaptured は stdout を捕まえて runTune を回す(calibrate と同じ鍵で 1 つずつ)。
func (s *server) runTuneCaptured(o tuneOpts) (*tuneResult, string, error) {
	s.calibMu.Lock()
	defer s.calibMu.Unlock()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	var res *tuneResult
	err := func() (err error) {
		defer func() {
			stdout = old
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
		writeErr(w, 404, fmt.Errorf("unknown spec %q", id))
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
	writeJSON(w, 200, out)
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
		writeErr(w, 404, fmt.Errorf("unknown spec %q", req.Spec))
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
		_, report, err := s.runTuneCaptured(s.tuneOptsFor(ref, propose))
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
	}()
	writeJSON(w, 202, map[string]any{"spec": ref.id, "running": true})
}

func errString(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
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
	// 読んでから書くまで 1 つの鍵の中で(同じ run への同時の採用を 409 にする)
	s.calibMu.Lock()
	defer s.calibMu.Unlock()
	res, err := loadTuneResult(s.o.wikiDir, req.Run)
	if err != nil {
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
		old := stdout
		stdout = &buf
		defer func() {
			stdout = old
			if r := recover(); r != nil {
				err = fmt.Errorf("adopt panicked: %v", r)
			}
		}()
		return adoptWording(res, s.o.ef, s.o.storeDir, "")
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
