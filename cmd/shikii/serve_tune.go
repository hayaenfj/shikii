package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/spec"
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
	// 今ある候補(人が画面か YAML で書いたもの、提案役が足したもの)。次の tune(手でも、hook の自動でも)が採点する
	variants := []map[string]any{}
	if sp, err := s.spec(id); err == nil {
		for _, v := range sp.Variants {
			variants = append(variants, map[string]any{"name": v.Name, "question": v.Question, "options": v.Options})
		}
	}
	out["variants"] = variants
	writeJSON(w, 200, out)
}

// handleTuneVariant は人が画面で書いた候補の言い方を spec の variants に 1 つ足す(POST /v1/tune/variant: spec, name, question, options{id: 説明})。
// 書くのは variants だけ(提案役と同じ経路: ファイルを読み直して足し、Save が他の項目を触らない)。候補は spec として検証してから書く(無い選択肢の id などは 400)。
// 足すだけで採点も採用もしない: 採点は次の tune、採用は人。
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
		if id == "" || d == "" { // 空の行は無視(画面の textarea の空行)
			continue
		}
		if v.Options == nil {
			v.Options = map[string]string{}
		}
		v.Options[id] = d
	}
	if v.Name == "" {
		writeErr(w, 400, errors.New("name is required: a short label for this rewording (letters, digits, dashes)"))
		return
	}
	if v.Question == "" && len(v.Options) == 0 {
		writeErr(w, 400, errors.New("a rewording needs a question or at least one option description"))
		return
	}
	s.calibMu.Lock() // 読んでから書くまで 1 つの鍵の中で(tune --write や採用と同時に書かない)
	defer s.calibMu.Unlock()
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
