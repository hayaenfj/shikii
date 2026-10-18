package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// shikii serve: 1 プロセスで、プログラム向けの HTTP の口と、人向けの画面を出す。
//
//	POST /v1/decide  {"spec":"id","inputs":{...},"tainted":false}   → Decision(decide --json と同じ形)。記録に残る
//	POST /v1/label   {"trace_id":"…","truth":"…","weight":1,"source":"ui"} → 付いたラベル。min_n に届けば自動で較正して書き戻す
//	GET  /v1/queue?spec=id&limit=50                                 → 正解の付いていない記録(ask 帯を先に)。本文は残っていれば付く
//	GET  /v1/status                                                 → spec ごとの記録・正解・(model, hash) の状態
//	GET  /v1/specs                                                  → 読み込んでいる spec
//	GET  /                                                          → 画面(1 枚の HTML。外部の読み込み無し)
//
// spec はフォルダから毎回読み直すので、ファイルを直せばすぐ効く。推論は持たない(--local / --hosted の先に任せる)。

type serveOpts struct {
	port          int
	specsDir      string
	storeDir      string
	ef            endpointFlags
	autoCalibrate bool
	wikiDir       string // 聞き方の wiki。空なら ./wiki。tune の結果を置き、画面と MCP が読む
	proposer      string // 候補を作る提案役(チャット用 LLM)の URL。空なら spec の variants だけ
	proposerModel string
}

// bindHost は待ち受け先。ホストでは 127.0.0.1 だけ(LAN に出さない)。容れ物(Docker、/.dockerenv がある)の中では全ての口で待ち、
// 外に出すかはホスト側の port の公開が決める(compose は 127.0.0.1:8095 に絞っている)。選ぶ設定は無い。
func bindHost() string { return systemone.BindHost() }

func bindHostFor(marker string) string { return systemone.BindHostFor([]string{marker}, "") }

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var o serveOpts
	o.ef.bind(fs)
	fs.IntVar(&o.port, "port", 8095, "待ち受けるポート(127.0.0.1 のみ)")
	fs.StringVar(&o.specsDir, "specs", "decisions", "spec(*.yaml)のフォルダ")
	fs.StringVar(&o.storeDir, "store", ".shikii", "記録とラベルの置き場")
	fs.BoolVar(&o.autoCalibrate, "auto-calibrate", true, "正解が eval.min_n に届いたら自動で較正して spec に書き戻す")
	fs.StringVar(&o.proposer, "proposer", "", "聞き方の候補を作る提案役(チャット用 LLM)の URL。空なら spec の variants だけを測る")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "提案役のモデル名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := loadSpecs(o.specsDir); err != nil {
		return err
	}
	if err := waitForLocalModel(o.ef.local, systemone.WaitReady); err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", bindHost(), o.port)
	log.Printf("shikii serve: specs=%s store=%s local=%q hosted=%q auto-calibrate=%v  →  http://%s/", o.specsDir, o.storeDir, systemone.RedactURL(o.ef.local), systemone.RedactURL(o.ef.hosted), o.autoCalibrate, addr)
	return http.ListenAndServe(addr, newServer(o))
}

// waitForLocalModel は手元のモデルの口が立つのを自分で待つ(Docker で先にモデルを起こす時。curl / wget に頼らない)。
// --local が無い(hosted だけ)なら待たない: hosted は外のサービスで、起動を待つ対象ではない。最長 15 分。URL が違う時は最初の行で分かるようにする。
func waitForLocalModel(local string, wait func(context.Context, string, time.Duration, func(string, ...any)) error) error {
	if local == "" {
		return nil
	}
	log.Printf("waiting for the local model at %s (if the URL is wrong, stop now and fix --local; serve does not open its port until the model answers)", systemone.RedactURL(local))
	wctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return wait(wctx, local, 3*time.Second, log.Printf)
}

type server struct {
	wikiMu  sync.Mutex // 画面からの wiki の操作を直列にする(tune の updateClaims とはプロセスが違えば別。書き込みは原子的に行う)
	o       serveOpts
	st      store.Store
	calibMu sync.Mutex        // calibrate / tune は stdout を差し替えて報告を捕まえるので 1 つずつ
	lastCal map[string]string // spec id → 最後の自動較正の報告
	mu      sync.Mutex
	tuning  map[string]bool   // spec id → 裏で tune が走っている
	tuneErr map[string]string // spec id → 最後の tune の失敗
}

func newServer(o serveOpts) http.Handler {
	if o.wikiDir == "" {
		o.wikiDir = wikiRoot
	}
	s := &server{o: o, st: store.Store{Dir: o.storeDir}, lastCal: map[string]string{}, tuning: map[string]bool{}, tuneErr: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleUI)
	mux.HandleFunc("/v1/specs", s.handleSpecs)
	mux.HandleFunc("/v1/decide", s.handleDecide)
	mux.HandleFunc("/v1/label", s.handleLabel)
	mux.HandleFunc("/v1/queue", s.handleQueue)
	mux.HandleFunc("/v1/answered", s.handleAnswered)
	mux.HandleFunc("/v1/redo", s.handleRedo)
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/tune", s.handleTuneGet)
	mux.HandleFunc("/v1/tune/run", s.handleTuneRun)
	mux.HandleFunc("/v1/tune/sync", s.handleTuneSync)
	mux.HandleFunc("/v1/tune/adopt", s.handleTuneAdopt)
	mux.HandleFunc("/v1/wiki", s.handleWikiList)
	mux.HandleFunc("/v1/wiki/claim", s.handleWikiClaim)
	return mux
}

// loadSpecs はフォルダの *.yaml を id で引けるようにする。毎回読む(小さいファイルなので)。
func loadSpecs(dir string) (map[string]*spec.Spec, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no spec (*.yaml) in %s", dir)
	}
	out := map[string]*spec.Spec{}
	seen := map[string]map[int]string{} // id → version → path
	for _, f := range files {
		sp, err := spec.Load(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		// 同じ id は version の大きい方(文言 v1 → v2 を同じ id で持つ運用)。同じ id・同じ version が 2 つあれば決められないので誤り
		if prev, ok := seen[sp.ID][sp.Version]; ok {
			return nil, fmt.Errorf("duplicate spec id %q with the same version %d: %s and %s", sp.ID, sp.Version, prev, f)
		}
		if seen[sp.ID] == nil {
			seen[sp.ID] = map[int]string{}
		}
		seen[sp.ID][sp.Version] = f
		if cur, ok := out[sp.ID]; !ok || sp.Version > cur.Version {
			out[sp.ID] = sp
		}
	}
	return out, nil
}

func (s *server) spec(id string) (*spec.Spec, error) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		return nil, err
	}
	sp, ok := specs[id]
	if !ok {
		return nil, errNotFound
	}
	return sp, nil
}

var errNotFound = errors.New("not found")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *server) handleSpecs(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := []map[string]any{}
	for _, sp := range specs {
		out = append(out, specSummary(sp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	writeJSON(w, 200, map[string]any{"specs": out})
}

func specSummary(sp *spec.Spec) map[string]any {
	titles, questions := map[string]string{}, map[string]string{}
	for lang, d := range sp.Display {
		if d.Title != "" {
			titles[lang] = d.Title
		}
		if d.Question != "" {
			questions[lang] = d.Question
		}
	}
	return map[string]any{"id": sp.ID, "type": string(sp.Type), "path": sp.Path(), "hash": sp.Hash(), "question": sp.Question,
		"titles": titles, "questions": questions,
		"min_n": sp.Eval.MinN, "act": sp.Thresholds.Act, "ask": sp.Thresholds.Ask, "choices": truthChoices(sp)}
}

// truthChoices は画面のボタンになる正解の候補。choice は選択肢 id、noul は yes/no、score は "0".."n-1"。
func truthChoices(sp *spec.Spec) []map[string]any {
	// labels は言語ごとの表示名(display から)。無い言語は画面側で label(英語/id)に落ちる
	labelsFor := func(id string, level int) map[string]string {
		out := map[string]string{}
		for lang, d := range sp.Display {
			if level >= 0 {
				if level < len(d.Levels) {
					out[lang] = fmt.Sprintf("%d: %s", level, d.Levels[level])
				}
			} else if v, ok := d.Options[id]; ok {
				out[lang] = v
			}
		}
		return out
	}
	var out []map[string]any
	switch sp.Type {
	case spec.Noul:
		out = append(out, map[string]any{"truth": "yes", "label": "yes", "labels": labelsFor("yes", -1)},
			map[string]any{"truth": "no", "label": "no", "labels": labelsFor("no", -1)})
	case spec.Choice:
		for _, o := range sp.Options {
			out = append(out, map[string]any{"truth": o.ID, "label": o.ID, "description": o.Description, "labels": labelsFor(o.ID, -1)})
		}
	case spec.Score:
		for i, lv := range sp.Levels {
			out = append(out, map[string]any{"truth": fmt.Sprint(i), "label": fmt.Sprintf("%d: %s", i, lv), "labels": labelsFor("", i)})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

// maxBody は POST 本文の上限。store の 1 行の上限(4 MiB)より小さく。
const maxBody = 1 << 20

// acceptJSONPost はブラウザ上の他サイトからの書き込みを弾く(監査 S3)。
//   - Origin があれば、この口(127.0.0.1 / localhost)のものだけ通す
//   - Content-Type は application/json だけ(text/plain の「単純リクエスト」は preflight 無しで届くため)
//   - 本文は maxBody まで(超えると 413)
func acceptJSONPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeErr(w, 405, errors.New("POST only"))
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "[::1]" && u.Hostname() != "::1") {
			writeErr(w, 403, fmt.Errorf("cross-origin request from %q is not allowed", origin))
			return false
		}
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeErr(w, 415, errors.New("Content-Type must be application/json"))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	return true
}

func (s *server) handleDecide(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec    string            `json:"spec"`
		Inputs  map[string]string `json:"inputs"`
		Tainted bool              `json:"tainted"`
		Record  *bool             `json:"record"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, 413, fmt.Errorf("body larger than %d bytes", maxBody))
			return
		}
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	sp, err := s.spec(req.Spec)
	if errors.Is(err, errNotFound) {
		writeErr(w, 404, fmt.Errorf("unknown spec %q", req.Spec))
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, req.Inputs, decide.Session{Tainted: req.Tainted || s.o.ef.tainted}, s.o.ef.endpoints())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	attachReceipt(sp, s.st, d)
	if (req.Record == nil || *req.Record) && d.Band != decide.Abstain {
		if err := recordDecision(s.st, sp, d, req.Inputs); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	writeJSON(w, 200, d)
}

// handleAnswered は人が答えた判定(新しい順)。答えを直す(同じ trace にもう一度 /v1/label: 新しい答えが勝つ)と、AI にもう一度聞く(/v1/redo)の材料。
func (s *server) handleAnswered(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit < 1 || limit > 1000 {
		limit = 50
	}
	sp, err := s.spec(id)
	if errors.Is(err, errNotFound) {
		writeErr(w, 404, fmt.Errorf("unknown spec %q", id))
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	recs, err := s.st.Records(id, 0)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	labeled, err := s.st.LabeledTraces(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	items := []map[string]any{}
	for _, rec := range recs {
		l, ok := labeled[rec.TraceID]
		if !ok || !store.HumanSource(l.Source) {
			continue
		}
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "stale": !sp.LiveHash(rec.SpecHash), "rung": rec.RungOf(), "redo_of": rec.RedoOf,
			"answerable": len(rec.Inputs) > 0, "choices": truthChoices(sp),
			"truth": l.Truth, "truth_at": l.At, "source": l.Source, "correct": store.Correct(rec.Top, l.Truth),
		})
		if len(items) >= limit {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items})
}

// handleRedo は過去の判定の本文を、今の聞き方(spec)と線でもう一度判定する。新しい記録(redo_of = 元の trace)を残し、元の記録は列から消える。
// 人が既に答えていれば、その答えは同じ本文の正解なので新しい記録にも写す(もう一度答えさせない)。前と今を並べて返す。
func (s *server) handleRedo(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec    string `json:"spec"`
		TraceID string `json:"trace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	sp, err := s.spec(req.Spec)
	if errors.Is(err, errNotFound) {
		writeErr(w, 404, fmt.Errorf("unknown spec %q", req.Spec))
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	rec, err := s.st.FindRecord(req.Spec, req.TraceID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if len(rec.Inputs) == 0 {
		writeErr(w, 409, errors.New("this decision kept no text, so it cannot be asked again"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, rec.Inputs, decide.Session{Tainted: rec.Tainted || s.o.ef.tainted}, s.o.ef.endpoints())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	attachReceipt(sp, s.st, d)
	resp := map[string]any{"before": map[string]any{"trace_id": rec.TraceID, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model, "stale": !sp.LiveHash(rec.SpecHash)}, "after": d, "recorded": false}
	if d.Band == decide.Abstain {
		writeJSON(w, 200, resp)
		return
	}
	newRec := recordOf(sp, d, rec.Inputs)
	newRec.RedoOf = rec.TraceID
	if err := s.st.AppendRecord(newRec); err != nil {
		writeErr(w, 500, err)
		return
	}
	resp["recorded"] = true
	labeled, err := s.st.LabeledTraces(req.Spec)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if l, ok := labeled[rec.TraceID]; ok && store.HumanSource(l.Source) {
		copied, err := store.LabelsFor(&newRec, l.Truth, l.Weight, l.Source)
		if err == nil {
			err = s.st.AppendLabels(copied)
		}
		if err != nil {
			writeErr(w, 500, fmt.Errorf("carry the person's answer to the new decision: %w", err))
			return
		}
		resp["truth"] = l.Truth
		resp["correct_before"] = store.Correct(rec.Top, l.Truth)
		resp["correct_after"] = store.Correct(d.Top, l.Truth)
	}
	writeJSON(w, 200, resp)
}

func (s *server) handleLabel(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		TraceID string  `json:"trace_id"`
		Spec    string  `json:"spec"`
		Truth   string  `json:"truth"`
		Weight  float64 `json:"weight"`
		Source  string  `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	if req.TraceID == "" || req.Truth == "" {
		writeErr(w, 400, errors.New("trace_id and truth are required"))
		return
	}
	rec, err := s.st.FindRecord(req.Spec, req.TraceID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if req.Source == "" {
		req.Source = "ui"
	}
	labels, err := store.LabelsFor(rec, req.Truth, req.Weight, req.Source)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.st.AppendLabels(labels); err != nil { // 2 段分を 1 回で書く(片方だけ残らない)
		writeErr(w, 500, err)
		return
	}
	l := labels[0]
	// 文脈を足して決まった判定への答えは、最小の質問にも付く(rungs = 2)。線は段ごとに引かれる
	resp := map[string]any{"trace_id": l.TraceID, "spec": l.SpecID, "model": l.Model, "truth": l.Truth, "weight": l.Weight, "source": l.Source, "rungs": len(labels), "calibrated": false}
	if s.o.autoCalibrate {
		if report, did := s.maybeCalibrate(rec.SpecID, rec.SpecHash, rec.Model); did {
			resp["calibrated"] = true
			resp["calibration"] = report
		}
	}
	writeJSON(w, 200, resp)
}

// maybeCalibrate は (hash, model) の正解が min_n に届き、まだ線が無い(か古い)時だけ較正して書き戻す。
// 失敗しても label の処理は成功のまま(報告に残すだけ)。
func (s *server) maybeCalibrate(specID, hash, model string) (string, bool) {
	sp, err := s.spec(specID)
	if err != nil || !sp.LiveHash(hash) || model == "" {
		return "", false
	}
	rs, rung := sp, "minimal" // この記録の段。線を引くのはこの段だけ(他の段の線は触らない)
	if hash == sp.ContextHash() {
		rs, rung = sp.ContextSpec(), "context"
	}
	if c := rs.CalibrationFor(model); c != nil && c.SpecHash == hash {
		return "", false // もう線がある。引き直しは drift が悪化を言った時に(ここでは触らない)
	}
	labels, err := s.st.Labels(specID, hash, model)
	if err != nil || len(labels) < sp.Eval.MinN {
		return "", false
	}
	s.calibMu.Lock()
	defer s.calibMu.Unlock()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	ef := s.o.ef
	ef.model = model
	err = func() (err error) {
		defer func() {
			stdout = old // panic しても stdout を差し替えたままにしない(MCP の通信路を守る)
			if r := recover(); r != nil {
				err = fmt.Errorf("calibrate panicked: %v", r)
			}
		}()
		return runCalibrate(sp.Path(), calibrateOpts{ef: ef, storeDir: s.o.storeDir, labeled: "none", bins: 10, write: true, rung: rung})
	}()
	report := strings.TrimSpace(buf.String())
	if err != nil {
		report += "\nERROR: " + err.Error()
	}
	s.mu.Lock()
	s.lastCal[specID] = time.Now().Format("2006-01-02 15:04:05") + "\n" + report
	s.mu.Unlock()
	log.Printf("auto-calibrate %s (%s): %s", specID, model, lastLine(report))
	return report, err == nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// handleQueue は正解の付いていない記録。ask 帯を先に、その中では新しい順。
func (s *server) handleQueue(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit < 1 || limit > 1000 {
		limit = 50
	}
	sp, err := s.spec(id)
	if errors.Is(err, errNotFound) {
		writeErr(w, 404, fmt.Errorf("unknown spec %q", id))
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	recs, err := s.st.Records(id, 0)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	labeled, err := s.st.LabeledTraces(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	rank := map[string]int{"ask": 0, "audit": 1, "act": 2, "discard": 3} // 迷い → 抜き取り(act の 20 件に 1 件) → 残り
	superseded := map[string]bool{}                                      // やり直した元の記録は列に出さない(新しい判定が代わりに出る)
	for _, rec := range recs {
		if rec.RedoOf != "" {
			superseded[rec.RedoOf] = true
		}
	}
	var items []map[string]any
	for _, rec := range recs {
		if _, ok := labeled[rec.TraceID]; ok {
			continue
		}
		if superseded[rec.TraceID] {
			continue
		}
		// 本文が無い(古い)記録は人には答えられない。ボタンの材料は出さず、answerable=false で言う(結果を知るプログラムからの正解は別経路)
		answerable := len(rec.Inputs) > 0
		choices := []map[string]any{}
		if answerable {
			choices = truthChoices(sp)
		}
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "raw": rec.Raw, "stale": !sp.LiveHash(rec.SpecHash),
			"answerable": answerable, "choices": choices, "rung": rec.RungOf(),
			"audit": rec.Band == "act" && store.AuditSample(rec.TraceID),
		})
	}
	order := func(it map[string]any) int {
		if it["audit"] == true {
			return rank["audit"]
		}
		return rank[it["band"].(string)]
	}
	sort.SliceStable(items, func(i, j int) bool { return order(items[i]) < order(items[j]) })
	if len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items})
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := []map[string]any{}
	for _, sp := range specs {
		titles := map[string]string{}
		questions := map[string]string{} // 画面の「何を聞いているか」(人の言葉)
		for lang, d := range sp.Display {
			if d.Title != "" {
				titles[lang] = d.Title
			}
			if d.Question != "" {
				questions[lang] = d.Question
			}
		}
		nRec, labs, err := s.st.Counts(sp.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		byKey := map[string]int{}
		for _, l := range labs {
			byKey[l.Model+"\x00"+l.SpecHash]++
		}
		keys := []map[string]any{} // 無くても [] (null だと画面が落ちる)
		for k, n := range byKey {
			parts := strings.SplitN(k, "\x00", 2)
			model, hash := parts[0], parts[1]
			state := "collecting"
			method, rung := "", "minimal"
			rs := sp // このハッシュの段
			if hash == sp.ContextHash() {
				rs, rung = sp.ContextSpec(), "context"
			}
			switch {
			case !sp.LiveHash(hash):
				state = "stale"
			case rs.CalibrationFor(model) != nil && rs.CalibrationFor(model).SpecHash == hash:
				state, method = "calibrated", rs.CalibrationFor(model).Method
			case n >= sp.Eval.MinN:
				state = "ready"
			}
			keys = append(keys, map[string]any{"model": model, "hash": hash, "n": n, "state": state, "method": method, "rung": rung})
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i]["model"] != keys[j]["model"] {
				return keys[i]["model"].(string) < keys[j]["model"].(string)
			}
			return keys[i]["rung"].(string) > keys[j]["rung"].(string) // minimal を先に
		})
		labeled, _ := s.st.LabeledTraces(sp.ID)
		funnel, _ := s.st.Funnel(sp.ID, decide.DiscardUnsure(sp))
		s.mu.Lock()
		last := s.lastCal[sp.ID]
		s.mu.Unlock()
		out = append(out, map[string]any{"id": sp.ID, "type": string(sp.Type), "hash": sp.Hash(), "titles": titles, "questions": questions, "question": strings.TrimSpace(sp.Question), "records": nRec, "labels": len(labs),
			"unlabeled": nRec - countLabeledRecords(labeled, nRec), "min_n": sp.Eval.MinN, "keys": keys, "last_calibration": last,
			"funnel": map[string]any{"minimal": funnel.Minimal, "context": funnel.Context, "human": funnel.Human, "human_per_thousand": funnel.HumanPerThousand()}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	writeJSON(w, 200, map[string]any{"specs": out, "auto_calibrate": s.o.autoCalibrate, "store": s.o.storeDir})
}

func countLabeledRecords(labeled map[string]store.Label, nRec int) int {
	if len(labeled) > nRec {
		return nRec
	}
	return len(labeled)
}

func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(uiHTML))
}

// uiHTML は 1 枚の画面。外部の読み込み無し(オフラインでも開ける)。言語は右上で選ぶ(辞書 T に足せば増える)。
const uiHTML = `<!doctype html>
<meta charset="utf-8">
<title>shikii</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root { color-scheme: light dark; --fg:#1b1f2a; --bg:#fff; --muted:#6b7280; --line:#e5e7eb; --amber:#b45309; --amberbg:#fef3c7; --ok:#166534; --okbg:#dcfce7; }
  @media (prefers-color-scheme: dark) { :root { --fg:#e5e7eb; --bg:#0b1020; --muted:#9ca3af; --line:#1f2937; --amber:#fbbf24; --amberbg:#3a2a05; --ok:#86efac; --okbg:#0f2f1a; } }
  body { font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; color: var(--fg); background: var(--bg); margin: 0; padding: 20px; max-width: 980px; margin: 0 auto; }
  header { display:flex; align-items:baseline; gap:12px; flex-wrap:wrap; }
  h1 { font-size: 20px; margin: 0 0 4px; } h1 small { color: var(--muted); font-weight: 400; font-size: 13px; margin-left: 8px; }
  .adv { display: none !important; } body.advanced .adv { display: revert !important; }
  #help { margin-left:auto; font: inherit; font-size: 13px; width: 30px; border-radius: 999px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); cursor: pointer; }
  .howto { border: 1px solid var(--amber); background: var(--amberbg); color: var(--fg); border-radius: 10px; padding: 12px 16px; margin: 8px 0 14px; font-size: 14px; } .howto ol { margin: 6px 0 8px 20px; padding: 0; } .howto li { margin: 3px 0; }
  .redo-out { font-size: 13px; color: var(--amber); margin-top: 6px; min-height: 0; }
  .todo { font-size: 14px; color: var(--fg); margin: 8px 0 6px; } .num { display:inline-block; min-width: 1.4em; color: var(--muted); font-size: 12px; }
  #mode { font: inherit; font-size: 13px; padding: 3px 10px; border-radius: 6px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); cursor: pointer; }
  .status { margin: 6px 0 10px; font-size: 14px; } .bar { height: 8px; border-radius: 4px; background: var(--line); overflow: hidden; margin-top: 4px; } .bar > div { height: 100%; background: var(--ok); }
  #tabs { display: flex; gap: 6px; flex-wrap: wrap; margin: 6px 0 14px; } #tabs button { font-size: 14px; padding: 6px 12px; } #tabs button.active { border-color: var(--fg); font-weight: 600; } .badge { background: var(--amberbg); color: var(--amber); border-radius: 999px; padding: 0 7px; font-size: 12px; }
  .chips { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; margin: 6px 0 10px; } .chips button { font-size: 13px; padding: 4px 10px; } .chips button.active { border-color: var(--fg); font-weight: 600; } .chips .sep { flex: 1; }
  .item.card { padding: 18px 20px; } .item.card .text { font-size: 16px; }
  .q { font-size: 15px; margin: 4px 0 8px; } .guess { font-size: 14px; margin: 6px 0; } .keys { color: var(--muted); font-size: 12px; margin: 4px 0 10px; }
  body:not(.advanced) .btns button { font-size: 15px; padding: 8px 14px; }
  #lang { margin-left:8px; font: inherit; font-size: 13px; padding: 3px 6px; border-radius: 6px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); }
  h2 { font-size: 16px; margin: 28px 0 8px; display:flex; gap:10px; align-items:baseline; flex-wrap: wrap; }
  .pill { font-size: 12px; padding: 2px 8px; border-radius: 999px; border: 1px solid var(--line); color: var(--muted); }
  .pill.calibrated { background: var(--okbg); color: var(--ok); border-color: transparent; }
  .pill.ready { background: var(--amberbg); color: var(--amber); border-color: transparent; }
  .item { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; margin: 8px 0; }
  .item.ask { border-color: var(--amber); }
  .meta { color: var(--muted); font-size: 12px; margin-bottom: 6px; display:flex; gap:12px; flex-wrap:wrap; }
  .meta .audit { font-weight: 600; }
  .text { white-space: pre-wrap; word-break: break-word; margin: 4px 0 10px; }
  .text .k { color: var(--muted); font-size: 12px; }
  .btns { display:flex; gap:8px; flex-wrap:wrap; }
  button { font: inherit; padding: 6px 12px; border-radius: 8px; border: 1px solid var(--line); background: transparent; color: var(--fg); cursor: pointer; }
  button:hover { border-color: var(--fg); } button.top { border-color: var(--amber); }
  button.weak { color: var(--muted); font-size: 13px; }
  .empty { color: var(--muted); padding: 10px 0; }
  pre { font-size: 12px; color: var(--muted); white-space: pre-wrap; margin: 6px 0 0; }
  #toast { position: fixed; bottom: 16px; left: 50%; transform: translateX(-50%); background: var(--fg); color: var(--bg); padding: 8px 14px; border-radius: 8px; display: none; max-width: 90vw; white-space: pre-wrap; }
  details.wording { border: 1px dashed var(--line); border-radius: 10px; padding: 8px 14px; margin: 12px 0; }
  details.wording summary { cursor: pointer; }
  .wbody { margin-top: 8px; } .wbody .k { color: var(--muted); font-size: 12px; margin: 4px 0; }
  table.cands { border-collapse: collapse; font-size: 13px; margin: 6px 0; } table.cands th, table.cands td { text-align: left; padding: 2px 10px 2px 0; border-bottom: 1px solid var(--line); }
  table.cands tr.win td { color: var(--amber); }
  .diff { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin: 8px 0; } @media (max-width: 700px) { .diff { grid-template-columns: 1fr; } }
  .opts div { font-size: 13px; margin: 2px 0; } .opts div.changed { background: var(--amberbg); border-radius: 4px; padding: 1px 4px; }
</style>
<header>
  <h1>shikii <small id="tagline"></small></h1>
  <button id="help" type="button" title="how to use">?</button>
  <button id="mode" type="button"></button>
  <select id="lang" aria-label="language"><option value="en">English</option><option value="ja">日本語</option></select>
</header>
<div id="root"><div class="empty">…</div></div>
<div id="toast"></div>
<script>
// 辞書。言語を足すときはここに 1 つ増やし、<select> に option を足す。
const T = {
  en: { tagline: 'tell it what was actually true; that is all it needs to learn when to trust itself', loading: 'loading…', noSpecs: 'no decisions set up yet', empty: 'nothing waiting for an answer. When the AI decides something, it shows up here.',
        modeSimple: 'simple view', modeAdvanced: 'details', remaining: n => n + ' waiting', keysHint: 'Keys: 1-9 choose an answer, Enter = same as the AI, → = skip',
        tabKnowledge: 'Knowledge', viewOne: 'one at a time', viewList: 'list', skip: 'skip (later)', position: (i, n) => i + ' / ' + n, more: m => m + ' more waiting', showMore: m => 'show more (' + m + ')', unskip: n => 'bring back ' + n + ' skipped', emptyFiltered: 'nothing in this group',
        chip_all: 'all', chip_ask: 'AI unsure', chip_audit: 'spot check', chip_other: 'other', auditShort: 'spot check',
        viewTodo: 'to answer', viewDone: 'answered', yourAnswer: (name, when) => 'Your answer: ' + name + ' (' + when + ')', changeAnswer: 'Press another button to change your answer. The newest answer counts.', redo: 'ask the AI again', redoHint: 'asks the AI again with today\'s wording and line, keeps your answer', redoResult: (b, a) => 'before: ' + b + '  →  now: ' + a, redoAgree: 'now matches your answer', redoDisagree: 'still differs from your answer', noneAnswered: 'nothing answered yet', wasRedone: 'asked again',
        plainAuto: 'The AI may act on its own here (enough answers were checked).', plainReady: 'Enough answers: the line can be drawn.', plainLearning: (n, m) => 'Still learning: ' + n + ' of ' + m + ' answers checked before the AI may act on its own.', plainStale: 'The question changed; it is learning again.',
        aiGuess: (name, p) => 'AI guess: ' + name + ' (' + p + '% sure)', bandAsk: 'The AI was unsure.', bandAct: 'The AI acted on its own.', bandDiscard: 'The AI let it pass.', sameAsAI: 'same as the AI',
        records: n => n + ' decisions', answered: n => n + ' checked', calibrated: m => 'may act on its own (' + m + ')', stale: 'question changed, learning again', collecting: (n, m) => 'learning ' + n + '/' + m, ready: (n, m) => 'ready to draw the line ' + n + '/' + m,
        howToTitle: 'How to use this page', howTo: ['Pick a decision in the tabs at the top.', 'Read the text on the card. The AI already guessed; its guess is marked "same as the AI".', 'Press the answer that was actually true (keys 1-9, or Enter for "same as the AI"). The next card appears.', 'Not sure? Skip it with → and come back later. When enough answers are checked, the AI is allowed to act on its own.'], gotIt: 'got it', todo: 'Read the text, then press what was actually true.',
        withContext: 'asked again with context', contextRung: 'context rung', toPerson: (h, k) => 'to a person ' + h + ' (' + k + ' per 1000)',
        audit: 'audit sample: the machine acted on this; 1 in 20 is shown here so its precision is measured on what people checked',
        knowledge: 'Knowledge (claims)', claimsCount: (n, p) => n + ' claims, ' + p + ' prose pages', noClaims: 'no claims yet (tune drafts one when a candidate wins)',
        promote: 'make it a claim (promote)', deleteDraft: 'delete draft', confirm: 'confirm', reject: 'reject', confirmable: 'two independent models support it: you may confirm',
        promptClaim: 'The generalized claim (not tied to this spec):', promptSlug: 'File name (lowercase, digits, dashes):', promptModels: 'Which models does it apply to?', claimDone: 'recorded',
        noText: h => 'No text was kept for this decision (recorded before text was kept), so nobody can tell what was true here. Only a program that knows the outcome can answer it. Hash: ' + h, model: p => 'model ' + p + '%', notThat: t => 'not ' + t + ' (that is all I know)', changed: 'wording changed since',
        done: t => 'answered: ' + t, calibratedToast: 'enough answers: calibration written', error: m => 'error: ' + m, last: 'last auto-calibration',
        wording: 'Wording (rewordings)', current: 'current', noRun: 'not measured yet', running: 'measuring rewordings…', runTune: 'measure the rewordings', runPropose: n => 'ask the AI for ' + n + ' rewordings and measure',
        holdoutLine: (h, c) => 'tried on a held-back part: ' + h + ' (keeping current: ' + c + ')', winnerLine: w => 'rewording ' + w + ' may be adopted', keptLine: 'current wording kept',
        adoptedLine: p => 'adopted as ' + p, sameMeaning: 'same meaning → adopt', changedMeaning: 'meaning changed → reject', notePrompt: 'What changed in the meaning? (optional)',
        checkHint: 'Does the new wording still ask what you mean? Numbers are checked by the machine; the meaning is yours to check.', rejected: 'recorded: meaning changed, not adopted', adoptedToast: p => 'adopted: ' + p },
  ja: { tagline: 'AI の答えが本当に合っていたかを教えてください。それだけで、AI はいつ自分を信じてよいかを覚えます', loading: '読み込み中…', noSpecs: 'まだ判定が登録されていません', empty: '答え合わせ待ちはありません。AI が何かを判定すると、ここに並びます。',
        modeSimple: 'かんたん表示', modeAdvanced: 'くわしく', remaining: n => '残り ' + n + ' 件', keysHint: 'キー操作: 1〜9 で答えを選ぶ、Enter で「AI と同じ」、→ でスキップ',
        tabKnowledge: '知識', viewOne: '1 件ずつ', viewList: '一覧', skip: 'スキップ(あとで)', position: (i, n) => i + ' / ' + n + ' 件', more: m => 'ほかに ' + m + ' 件待っています', showMore: m => 'さらに表示(' + m + ' 件)', unskip: n => 'スキップした ' + n + ' 件を戻す', emptyFiltered: 'この種類の待ちはありません',
        chip_all: '全部', chip_ask: 'AI が迷った', chip_audit: '抜き取り', chip_other: 'その他', auditShort: '抜き取り',
        viewTodo: '未回答', viewDone: '回答済み', yourAnswer: (name, when) => 'あなたの答え: ' + name + '(' + when + ')', changeAnswer: '別のボタンを押すと答えを直せます。いちばん新しい答えが使われます。', redo: 'AI にもう一度聞く', redoHint: '今の聞き方と線で同じ本文をもう一度判定します。あなたの答えはそのまま残ります', redoResult: (b, a) => '前: ' + b + '  →  今: ' + a, redoAgree: 'あなたの答えと一致しました', redoDisagree: 'まだあなたの答えと違います', noneAnswered: '回答済みのものはまだありません', wasRedone: 'もう一度聞いた結果',
        plainAuto: 'この判定は AI が自分で動いてよい状態です(答え合わせが十分に揃いました)。', plainReady: '答えが揃いました。AI の自信の数字を直す線を引けます。', plainLearning: (n, m) => 'まだ学習中: 答え合わせ ' + n + ' 件。AI が自分で動けるようになるまで ' + m + ' 件。', plainStale: '質問の文が変わったので、学び直しています。',
        aiGuess: (name, p) => 'AI の予想: ' + name + '(自信 ' + p + '%)', bandAsk: 'AI は迷いました。', bandAct: 'AI は自分で動きました。', bandDiscard: 'AI は見送りました。', sameAsAI: 'AI と同じ',
        records: n => '判定 ' + n + ' 件', answered: n => '答え合わせ済み ' + n + ' 件', calibrated: m => '自分で動けます (' + m + ')', stale: '質問の文が変わったので学び直し', collecting: (n, m) => '学習中 ' + n + '/' + m, ready: (n, m) => '線を引けます ' + n + '/' + m,
        howToTitle: 'この画面の使い方', howTo: ['上のタブで判定の種類を選びます。', 'カードの本文を読みます。AI の予想は「AI と同じ」と書いたボタンです。', '本当はどれだったかのボタンを押します(キー 1〜9、AI と同じなら Enter)。押すと次のカードが出ます。', '分からなければ → でスキップして、あとで戻れます。答え合わせが揃うと、AI が自分で動けるようになります。'], gotIt: 'わかった', todo: '本文を読んで、本当はどれだったかを押してください。',
        withContext: '文脈を足して聞き直した', contextRung: '文脈の段', toPerson: (h, k) => '人へ ' + h + '(1000 判定あたり ' + k + ')',
        audit: '抜き取り検査: 機械が自動で動いた判定。20 件に 1 件をここに出し、人が確かめた分で精度を測る',
        knowledge: '知識(主張)', claimsCount: (n, p) => '主張 ' + n + '、文章のページ ' + p, noClaims: 'まだ主張はありません(tune で勝った候補があれば下書きができます)',
        promote: '主張にする', deleteDraft: '下書きを消す', confirm: 'confirmed にする', reject: '却下', confirmable: '独立したモデル 2 つが支持: confirmed にできます',
        promptClaim: '一般化した主張の文(この spec に限らない言い方で):', promptSlug: 'ファイル名(英小文字・数字・ハイフン):', promptModels: '効くモデルの範囲:', claimDone: '記録しました',
        noText: h => 'この判定は本文が残っていないので(本文を残す前の記録)、人には正解が分かりません。結果を知っているプログラムだけが答えられます。ハッシュ: ' + h, model: p => 'AI の自信 ' + p + '%', notThat: t => t + ' ではない(それしか分からない)', changed: 'この後に質問の文が変わりました',
        done: t => '答えました: ' + t, calibratedToast: '答えが揃いました。線を引いて書き込みました', error: m => 'エラー: ' + m, last: '最後の自動較正',
        wording: '聞き方(言い換えの案)', current: '今の文言', noRun: 'まだ測っていません', running: '案を測っています…', runTune: '案を測る', runPropose: n => 'AI に案を ' + n + ' 個作らせて測る',
        holdoutLine: (h, c) => '別の分で試した正解率 ' + h + '(今のまま: ' + c + ')', winnerLine: w => '言い換えの案 ' + w + ' を採用できます', keptLine: '今の文言のまま',
        adoptedLine: p => '採用済み: ' + p, sameMeaning: '意味は同じ → 採用', changedMeaning: '意味が違う → 却下', notePrompt: '意味のどこが変わりましたか？(任意)',
        checkHint: '言い換えても、まだあなたの聞きたいことを聞いていますか？数字は機械が確かめました。意味はあなたが確かめてください。', rejected: '記録しました: 意味が違うので採用しない', adoptedToast: p => '採用しました: ' + p },
};
const $ = (s, el=document) => el.querySelector(s);
const esc = s => String(s).replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
let lang = 'en';
try { lang = localStorage.getItem('shikii.lang') || (navigator.language || 'en').slice(0,2); } catch (e) {}
if (!T[lang]) lang = 'en';
const t = k => T[lang][k];
$('#lang').value = lang;
$('#lang').addEventListener('change', e => { lang = e.target.value; try { localStorage.setItem('shikii.lang', lang); } catch (x) {} render(); });
// かんたん表示が既定。「くわしく」で主張・言い換えの案・ハッシュ・モデル名などを出す
let advanced = false, showHelp = true;
try { advanced = localStorage.getItem('shikii.mode') === 'advanced'; showHelp = localStorage.getItem('shikii.help') !== 'hidden'; } catch (e) {}
$('#help').addEventListener('click', () => { showHelp = !showHelp; remember('shikii.help', showHelp ? 'shown' : 'hidden'); render(); });
document.body.classList.toggle('advanced', advanced);
$('#mode').addEventListener('click', () => { advanced = !advanced; document.body.classList.toggle('advanced', advanced); try { localStorage.setItem('shikii.mode', advanced ? 'advanced' : 'simple'); } catch (x) {} render(); });
// キー操作: 1〜9 で先頭の判定の答えを選ぶ、Enter で AI と同じ
document.addEventListener('keydown', ev => {
  if (ev.target && /INPUT|SELECT|TEXTAREA/.test(ev.target.tagName)) return;
  const first = document.querySelector('.item'); if (!first) return;
  if (/^[1-9]$/.test(ev.key)) { const b = first.querySelectorAll('.btns button')[Number(ev.key) - 1]; if (b) { ev.preventDefault(); b.click(); } }
  else if (ev.key === 'Enter') { const b = first.querySelector('.btns button.top'); if (b) { ev.preventDefault(); b.click(); } }
  else if (ev.key === 'ArrowRight' || ev.key === 's') { const b = first.querySelector('.btns button.skip'); if (b) { ev.preventDefault(); b.click(); } }
});
let toastTimer;
function toast(msg, ms=4000) { const el = $('#toast'); el.textContent = msg; el.style.display = 'block'; clearTimeout(toastTimer); toastTimer = setTimeout(() => el.style.display = 'none', ms); }
async function j(url, opts) { const r = await fetch(url, opts); const b = await r.json(); if (!r.ok) throw new Error(b.error || r.status); return b; }
let state = null;
async function load() {
  const st = await j('/v1/status');
  for (const sp of st.specs) {
    sp.queue = await j('/v1/queue?spec=' + encodeURIComponent(sp.id) + '&limit=200');
    try { sp.tune = await j('/v1/tune?spec=' + encodeURIComponent(sp.id)); } catch (e) { sp.tune = null; }
  }
  try { st.wiki = await j('/v1/wiki'); } catch (e) { st.wiki = null; }
  state = st; render();
}
// 画面の状態: 開いているタブ(判定ごと。「くわしく」なら知識のタブも)、見せ方(1 件ずつ / 一覧)、絞り込み(全部 / 迷い / 抜き取り / その他)、あとで答える(スキップ)
let tab = null, view = 'one', filter = 'all', listSize = 30, done = false;
const skipped = new Set();
try { tab = localStorage.getItem('shikii.tab'); view = localStorage.getItem('shikii.view') || 'one'; } catch (e) {}
function remember(k, v) { try { localStorage.setItem(k, v); } catch (e) {} }
function itemCard(sp, it, pos) {
  const el = document.createElement('div'); el.className = 'item ' + it.band + (view === 'one' ? ' card' : '');
  const text = it.answerable ? Object.entries(it.inputs).map(([k,v]) => '<div><span class="k">' + esc(k) + '</span><br>' + esc(v) + '</div>').join('') : '<span class="k">' + esc(t('noText')(it.inputs_hash)) + '</span>';
  const name = c => (c.labels && c.labels[lang]) || c.label;
  const topName = it.top ? name(it.choices.find(c => c.truth === it.top) || {label: it.top}) : '';
  const btns = it.choices.map((c, i) => '<button class="' + (c.truth === it.top ? 'top' : '') + (c.truth === it.truth ? ' active' : '') + '" data-truth="' + esc(c.truth) + '" title="' + esc(c.description || '') + '"><span class="num">' + (i + 1) + '</span>' + esc(name(c)) + (c.truth === it.top ? ' · ' + esc(t('sameAsAI')) : '') + '</button>').join('');
  const not = (it.answerable && it.top) ? '<button class="weak" data-truth="!' + esc(it.top) + '">' + esc(t('notThat')(topName)) + '</button>' : '';
  const skip = it.truth ? '' : '<button class="weak skip" data-skip="1">' + esc(t('skip')) + '</button>';
  const redo = it.answerable ? '<button class="weak adv" data-redo="1" title="' + esc(t('redoHint')) + '">' + esc(t('redo')) + '</button>' : '';
  const yours = it.truth ? '<div class="guess">' + esc(t('yourAnswer')(name(it.choices.find(c => c.truth === it.truth) || {label: it.truth}), new Date(it.truth_at).toLocaleString())) + (it.redo_of ? ' <span class="k">· ' + esc(t('wasRedone')) + '</span>' : '') + '</div><div class="k">' + esc(t('changeAnswer')) + '</div>' : '';
  const bandWord = it.band === 'ask' ? t('bandAsk') : it.band === 'act' ? t('bandAct') : t('bandDiscard');
  const guess = it.top ? '<div class="guess">' + esc(bandWord) + ' ' + esc(t('aiGuess')(topName, (it.p*100).toFixed(0))) + (it.audit ? ' <span class="audit">' + esc(t('auditShort')) + '</span>' : '') + '</div>' : '';
  el.innerHTML = (pos ? '<div class="k">' + esc(pos) + '</div>' : '') + yours + guess + '<div class="meta adv"><span>' + esc(it.band) + '</span><span>' + esc(new Date(it.at).toLocaleString()) + '</span><span>' + esc(it.model || '') + '</span>' + (it.rung === 'context' ? '<span>' + esc(t('withContext')) + '</span>' : '') + (it.audit ? '<span class="audit">' + esc(t('audit')) + '</span>' : '') + (it.stale ? '<span>' + esc(t('changed')) + '</span>' : '') + '</div><div class="text">' + text + '</div><div class="btns">' + btns + not + skip + redo + '</div><div class="redo-out"></div>';
  el.addEventListener('click', async ev => {
    const b = ev.target.closest('button'); if (!b) return;
    if (b.dataset.skip) { skipped.add(it.trace_id); render(); return; }
    for (const x of el.querySelectorAll('button')) x.disabled = true;
    if (b.dataset.redo) {
      try {
        const res = await j('/v1/redo', { method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({ trace_id: it.trace_id, spec: sp.id }) });
        const word = x => (x.band === 'ask' ? t('bandAsk') : x.band === 'act' ? t('bandAct') : x.band === 'abstain' ? 'abstain' : t('bandDiscard')) + ' ' + name(it.choices.find(c => c.truth === x.top) || {label: x.top || '-'}) + ' ' + Math.round((x.p || 0) * 100) + '%';
        let line = t('redoResult')(word(res.before), word(res.after));
        if (res.truth) line += ' · ' + (res.correct_after ? t('redoAgree') : t('redoDisagree'));
        el.querySelector('.redo-out').textContent = line;
        if (res.recorded) { sp.answered = null; load(); }
      } catch (e) { toast(t('error')(e.message)); }
      for (const x of el.querySelectorAll('button')) x.disabled = false;
      return;
    }
    try {
      const res = await j('/v1/label', { method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({ trace_id: it.trace_id, spec: sp.id, truth: b.dataset.truth, source: it.audit ? 'audit' : 'ui' }) });
      if (it.truth) { sp.answered = null; } else { sp.queue.items = sp.queue.items.filter(x => x.trace_id !== it.trace_id); sp.unlabeled = Math.max(0, (sp.unlabeled || 1) - 1); sp.labels++; }
      if (res.calibrated) { toast(t('calibratedToast') + '\n' + (res.calibration || '').split('\n').slice(-2).join('\n'), 8000); load(); } else { toast(t('done')(res.truth)); render(); }
    } catch (e) { toast(t('error')(e.message)); for (const x of el.querySelectorAll('button')) x.disabled = false; }
  });
  return el;
}
function render() {
  $('#tagline').textContent = t('tagline');
  $('#mode').textContent = t(advanced ? 'modeSimple' : 'modeAdvanced');
  document.documentElement.lang = lang;
  const root = $('#root'); root.innerHTML = '';
  if (!state) { root.innerHTML = '<div class="empty">' + esc(t('loading')) + '</div>'; return; }
  if (!state.specs.length) { root.innerHTML = '<div class="empty">' + esc(t('noSpecs')) + '</div>'; return; }
  if (showHelp) {
    const h = document.createElement('div'); h.className = 'howto';
    h.innerHTML = '<b>' + esc(t('howToTitle')) + '</b><ol>' + t('howTo').map(x => '<li>' + esc(x) + '</li>').join('') + '</ol><button class="weak" data-gotit="1">' + esc(t('gotIt')) + '</button>';
    h.addEventListener('click', ev => { if (ev.target.closest('[data-gotit]')) { showHelp = false; remember('shikii.help', 'hidden'); render(); } });
    root.appendChild(h);
  }
  // タブ: 判定ごとに 1 つ(待ち件数の印付き)。知識のタブは「くわしく」の時だけ
  const ids = state.specs.map(s => s.id);
  if (tab === 'wiki' && !(advanced && state.wiki)) tab = null;
  if (tab !== 'wiki' && !ids.includes(tab)) tab = ids[0];
  const nav = document.createElement('nav'); nav.id = 'tabs';
  nav.innerHTML = state.specs.map(sp => '<button data-tab="' + esc(sp.id) + '" class="' + (tab === sp.id ? 'active' : '') + '">' + esc((sp.titles && sp.titles[lang]) || sp.id) + (sp.unlabeled ? ' <span class="badge">' + esc(sp.unlabeled) + '</span>' : '') + '</button>').join('')
    + (advanced && state.wiki ? '<button data-tab="wiki" class="adv ' + (tab === 'wiki' ? 'active' : '') + '">' + esc(t('tabKnowledge')) + '</button>' : '');
  nav.addEventListener('click', ev => { const b = ev.target.closest('button'); if (!b) return; tab = b.dataset.tab; remember('shikii.tab', tab); render(); });
  root.appendChild(nav);
  if (tab === 'wiki') { root.appendChild(knowledgeBlock(state.wiki)); return; }
  const sp = state.specs.find(s => s.id === tab);
  const sec = document.createElement('section');
  const pills = sp.keys.map(k => '<span class="pill ' + k.state + '">' + esc(k.model) + (k.rung === 'context' ? ' · ' + esc(t('contextRung')) : '') + ' · ' + esc(k.state === 'calibrated' ? t('calibrated')(k.method) : k.state === 'stale' ? t('stale') : t(k.state)(k.n, sp.min_n)) + '</span>').join(' ');
  const title = (sp.titles && sp.titles[lang]) ? esc(sp.titles[lang]) + ' <span class="k adv" style="color:var(--muted);font-size:12px;font-weight:400">' + esc(sp.id) + '</span>' : esc(sp.id);
  const funnel = (sp.funnel && sp.records > 0) ? ' <span class="pill">' + esc(t('toPerson')(sp.funnel.human, sp.funnel.human_per_thousand)) + '</span>' : '';
  sec.innerHTML = '<h2>' + title + ' <span class="adv"><span class="pill">' + esc(sp.type) + '</span> ' + pills + ' <span class="pill">' + esc(t('records')(sp.records)) + ' · ' + esc(t('answered')(sp.labels)) + '</span>' + funnel + '</span></h2>';
  // 人の言葉の状態と進み具合(かんたん表示の中心)
  const live = sp.keys.filter(k => k.state !== 'stale');
  const n = live.reduce((m, k) => Math.max(m, k.n), 0);
  let plain = t('plainLearning')(n, sp.min_n), pct = Math.min(100, Math.round(100 * n / Math.max(1, sp.min_n)));
  if (live.some(k => k.state === 'calibrated')) { plain = t('plainAuto'); pct = 100; }
  else if (live.some(k => k.state === 'ready')) { plain = t('plainReady'); pct = 100; }
  else if (sp.keys.length && !live.length) plain = t('plainStale');
  const q = (sp.questions && sp.questions[lang]) || sp.question || '';
  sec.innerHTML += (q ? '<div class="q">' + esc(q) + '</div>' : '') + '<div class="status">' + esc(plain) + '<div class="bar"><div style="width:' + pct + '%"></div></div></div>';
  // 件数が増えた時の見せ方: 種類ごとの数(押すと絞り込み)、1 件ずつ / 一覧、スキップ、ほかに待っている数
  const all = sp.queue.items;
  const kind = it => it.band === 'ask' ? 'ask' : it.audit ? 'audit' : 'other';
  const counts = { all: all.length, ask: all.filter(i => kind(i) === 'ask').length, audit: all.filter(i => kind(i) === 'audit').length, other: all.filter(i => kind(i) === 'other').length };
  const chips = document.createElement('div'); chips.className = 'chips';
  chips.innerHTML = ['all', 'ask', 'audit', 'other'].map(k => '<button data-filter="' + k + '" class="' + (filter === k ? 'active' : '') + '">' + esc(t('chip_' + k)) + ' ' + counts[k] + '</button>').join('')
    + '<span class="sep"></span><button data-view="one" class="' + (view === 'one' ? 'active' : '') + '">' + esc(t('viewOne')) + '</button><button data-view="list" class="' + (view === 'list' ? 'active' : '') + '">' + esc(t('viewList')) + '</button>'
    + '<span class="sep"></span><button data-done="0" class="' + (!done ? 'active' : '') + '">' + esc(t('viewTodo')) + '</button><button data-done="1" class="' + (done ? 'active' : '') + '">' + esc(t('viewDone')) + ' ' + (sp.labels || 0) + '</button>';
  chips.addEventListener('click', ev => { const b = ev.target.closest('button'); if (!b) return; if (b.dataset.filter) filter = b.dataset.filter; if (b.dataset.view) { view = b.dataset.view; remember('shikii.view', view); } if (b.dataset.done !== undefined) done = b.dataset.done === '1'; render(); });
  sec.appendChild(chips);
  if (done) {
    // 回答済み: 新しい順。答えを直す(別のボタン)、AI にもう一度聞く(くわしく)
    if (!sp.answered) {
      sec.innerHTML += '<div class="empty">' + esc(t('loading')) + '</div>'; root.appendChild(sec);
      j('/v1/answered?spec=' + encodeURIComponent(sp.id) + '&limit=200').then(r => { sp.answered = r.items; render(); }).catch(e => toast(t('error')(e.message)));
      return;
    }
    if (!sp.answered.length) sec.innerHTML += '<div class="empty">' + esc(t('noneAnswered')) + '</div>';
    for (const it of sp.answered.slice(0, listSize)) sec.appendChild(itemCard(sp, it, ''));
    if (sp.answered.length > listSize) { const b = document.createElement('button'); b.className = 'weak'; b.textContent = t('showMore')(sp.answered.length - listSize); b.addEventListener('click', () => { listSize += 30; render(); }); sec.appendChild(b); }
    root.appendChild(sec);
    return;
  }
  const matching = all.filter(it => filter === 'all' || kind(it) === filter);
  const visible = matching.filter(it => !skipped.has(it.trace_id));
  const extra = Math.max(0, (sp.unlabeled || 0) - all.length);
  if (!visible.length) {
    const d = document.createElement('div'); d.className = 'empty';
    d.innerHTML = esc(all.length ? t('emptyFiltered') : t('empty')) + (matching.length ? ' <button class="weak" data-unskip="1">' + esc(t('unskip')(matching.length)) + '</button>' : '') + (extra ? '<div>' + esc(t('more')(extra)) + '</div>' : '');
    d.addEventListener('click', ev => { if (ev.target.closest('[data-unskip]')) { skipped.clear(); render(); } });
    sec.appendChild(d);
  } else if (view === 'one') {
    sec.innerHTML += '<div class="todo">' + esc(t('todo')) + '</div><div class="keys">' + esc(t('keysHint')) + '</div>';
    sec.appendChild(itemCard(sp, visible[0], t('position')(1, visible.length) + (extra ? ' · ' + t('more')(extra) : '')));
  } else {
    sec.innerHTML += '<div class="keys">' + esc(t('keysHint')) + '</div>';
    for (const it of visible.slice(0, listSize)) sec.appendChild(itemCard(sp, it, ''));
    if (visible.length > listSize || extra) {
      const b = document.createElement('button'); b.className = 'weak'; b.textContent = t('showMore')(Math.max(0, visible.length - listSize) + extra);
      b.addEventListener('click', () => { listSize += 30; render(); }); sec.appendChild(b);
    }
  }
  if (sp.last_calibration) sec.innerHTML += '<pre class="adv">' + esc(t('last')) + '\n' + esc(sp.last_calibration) + '</pre>';
  sec.appendChild(wordingBlock(sp));
  root.appendChild(sec);
}
// 知識の欄: 主張と下書きの一覧(証拠、支持するモデルの数、confirmable の印)。人がすること: 下書きを一般化して主張にする(promote)、confirmed / rejected にする、下書きを消す。
function knowledgeBlock(wk) {
  const sec = document.createElement('section'); sec.className = 'adv';
  const claims = (wk.claims || []).filter(c => c.is_claim);
  const prose = (wk.claims || []).filter(c => !c.is_claim).length;
  sec.innerHTML = '<h2>' + esc(t('knowledge')) + ' <span class="pill">' + esc(t('claimsCount')(claims.length, prose)) + '</span></h2>';
  if (!claims.length) sec.innerHTML += '<div class="empty">' + esc(t('noClaims')) + '</div>';
  for (const c of claims) {
    const el = document.createElement('div'); el.className = 'item';
    const ev = (c.evidence || []).map(e => '<div class="k">' + esc(e.date + ' · ' + e.model + ' · ' + e.result + ' → ' + e.verdict) + '</div>').join('');
    let btns = '';
    if (c.status === 'draft') btns += '<button data-act="promote">' + esc(t('promote')) + '</button><button class="weak" data-act="delete">' + esc(t('deleteDraft')) + '</button>';
    else {
      if (c.status !== 'confirmed') btns += '<button data-act="status" data-status="confirmed">' + esc(t('confirm')) + '</button>';
      if (c.status !== 'rejected') btns += '<button class="weak" data-act="status" data-status="rejected">' + esc(t('reject')) + '</button>';
    }
    el.innerHTML = '<div class="meta"><span class="pill ' + (c.status === 'confirmed' ? 'calibrated' : c.status === 'draft' ? 'ready' : '') + '">' + esc(c.status) + '</span><span>' + esc(c.path) + '</span>' +
      (c.confirmable ? '<span class="audit">' + esc(t('confirmable')) + '</span>' : '') + '</div><div class="text"><b>' + esc(c.claim) + '</b>' +
      (c.applies_to ? '<div class="k">' + esc(Object.entries(c.applies_to).map(([k, v]) => k + ': ' + v).join(' · ')) + '</div>' : '') + ev + '</div><div class="btns">' + btns + '</div>';
    el.addEventListener('click', async ev => {
      const b = ev.target.closest('button'); if (!b) return;
      const body = { path: c.path, action: b.dataset.act, status: b.dataset.status };
      if (b.dataset.act === 'promote') {
        body.claim = prompt(t('promptClaim'), c.claim); if (!body.claim) return;
        body.slug = prompt(t('promptSlug'), c.path.split('/').pop().replace(/\.md$/, '')); if (!body.slug) return;
        const models = prompt(t('promptModels'), (c.applies_to && c.applies_to.models) || ''); if (models === null) return;
        body.applies_to = Object.assign({}, c.applies_to || {}, { models });
      }
      for (const x of el.querySelectorAll('button')) x.disabled = true;
      try { await j('/v1/wiki/claim', { method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify(body) }); toast(t('claimDone')); load(); }
      catch (e) { toast(t('error')(e.message)); for (const x of el.querySelectorAll('button')) x.disabled = false; }
    });
    sec.appendChild(el);
  }
  return sec;
}
// 聞き方の欄: 最新の tune の結果、勝った候補の文言、人の「意味は同じ / 違う」、候補を測るボタン
function wordingBlock(sp) {
  const el = document.createElement('details'); el.className = 'wording adv';
  const tu = sp.tune; const latest = tu && tu.latest;
  let head = '<summary><b>' + esc(t('wording')) + '</b>';
  if (tu && tu.running) head += ' <span class="pill ready">' + esc(t('running')) + '</span>';
  else if (latest && latest.adopted) head += ' <span class="pill calibrated">' + esc(t('adoptedLine')(latest.adopted_path.split('/').pop())) + '</span>';
  else if (latest && latest.adoptable) head += ' <span class="pill ready">' + esc(t('winnerLine')(latest.winner)) + '</span>';
  else if (latest) head += ' <span class="pill">' + esc(t('keptLine')) + '</span>';
  else head += ' <span class="pill">' + esc(t('noRun')) + '</span>';
  head += '</summary>';
  let body = '';
  if (latest) {
    body += '<div class="meta"><span>' + esc(latest.model) + '</span><span>' + esc(latest.truths) + ' truths</span><span>' + esc(latest.at.slice(0, 16).replace('T', ' ')) + '</span></div>';
    body += '<table class="cands"><tr><th>candidate</th><th>top-1</th><th>brier</th><th>prec@act</th><th>rec@act</th><th></th></tr>';
    for (const c of latest.candidates) body += '<tr' + (c.name === latest.winner && latest.winner !== 'current' ? ' class="win"' : '') + '><td>' + esc(c.name) + '</td><td>' + c.top1.toFixed(3) + '</td><td>' + c.brier.toFixed(4) + '</td><td>' + c.prec_at_act.toFixed(3) + '</td><td>' + c.rec_at_act.toFixed(3) + '</td><td class="k">' + esc(c.change) + '</td></tr>';
    body += '</table>';
    if (latest.have_holdout) body += '<div class="k">' + esc(t('holdoutLine')(latest.holdout.toFixed(3), latest.candidates[0].top1.toFixed(3))) + '</div>';
    body += '<div class="k">' + esc(latest.decision) + '</div>';
    if (latest.adoptable && !latest.adopted && latest.winner_wording) {
      const w = latest.winner_wording, cur = latest.current_wording || {};
      body += '<div class="diff"><div><div class="k">' + esc(t('current')) + '</div><div class="text">' + esc(cur.question || '') + '</div>' + optionsHTML(cur.options) + '</div>';
      body += '<div><div class="k">' + esc(latest.winner) + '</div><div class="text">' + esc(w.question || '') + '</div>' + optionsHTML(w.options, cur.options) + stepsHTML(w.steps) + '</div></div>';
      body += '<div class="k">' + esc(t('checkHint')) + '</div>';
      body += '<div class="btns"><button class="top" data-act="adopt">' + esc(t('sameMeaning')) + '</button><button class="weak" data-act="reject">' + esc(t('changedMeaning')) + '</button></div>';
    }
  }
  if (tu && tu.error) body += '<div class="k">' + esc(t('error')(tu.error)) + '</div>';
  if (tu && !tu.running) {
    body += '<div class="btns"><button data-act="run" data-propose="0">' + esc(t('runTune')) + '</button>' + (tu.proposer ? '<button data-act="run" data-propose="3">' + esc(t('runPropose')(3)) + '</button>' : '') + '</div>';
  }
  el.innerHTML = head + '<div class="wbody">' + body + '</div>';
  if (latest && (latest.adoptable && !latest.adopted)) el.open = true;
  el.addEventListener('click', async ev => {
    const b = ev.target.closest('button'); if (!b) return;
    ev.preventDefault();
    try {
      if (b.dataset.act === 'run') {
        await j('/v1/tune/run', { method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({ spec: sp.id, propose: Number(b.dataset.propose) }) });
        toast(t('running')); load();
      } else if (b.dataset.act === 'adopt' || b.dataset.act === 'reject') {
        const same = b.dataset.act === 'adopt';
        const note = same ? '' : (prompt(t('notePrompt')) || '');
        const res = await j('/v1/tune/adopt', { method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({ spec: sp.id, run: latest.run, meaning_same: same, note }) });
        toast(same ? t('adoptedToast')(res.path.split('/').pop()) : t('rejected'), 6000); load();
      }
    } catch (e) { toast(t('error')(e.message)); }
  });
  return el;
}
function stepsHTML(steps) {
  if (!steps || !steps.length) return '';
  return '<div class="opts">' + steps.map((s, i) => '<div class="changed"><span class="k">' + (i + 1) + '.</span> ' + esc(s.ask) + (s.yes ? ' <span class="k">yes→</span>' + esc(s.yes) : '') + (s.no ? ' <span class="k">no→</span>' + esc(s.no) : '') + '</div>').join('') + '</div>';
}
function optionsHTML(opts, base) {
  if (!opts) return '';
  return '<div class="opts">' + Object.entries(opts).map(([id, d]) => '<div' + (base && base[id] !== d ? ' class="changed"' : '') + '><span class="k">' + esc(id) + '</span> ' + esc(d) + '</div>').join('') + '</div>';
}
render();
load().catch(e => { $('#root').innerHTML = '<div class="empty">' + esc(t('error')(e.message)) + '</div>'; });
setInterval(() => { if (!document.hidden) load().catch(()=>{}); }, 15000);
</script>
`
