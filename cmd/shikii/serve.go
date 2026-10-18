package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	shikii "github.com/hayaenfj/shikii"
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
//	GET  /                                                          → 画面(web/dist を埋め込んだ Svelte の画面。外部の読み込み無し)
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
	fs.StringVar(&o.specsDir, "specs", "", "spec(*.yaml)のフォルダ(既定は <store>/decisions)")
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
	if o.specsDir == "" {
		o.specsDir = specsDirOf(o.storeDir)
	}
	if o.wikiDir == "" {
		o.wikiDir = wikiDirOf(o.storeDir)
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
		writeUnknownSpec(w, req.Spec)
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
		writeUnknownSpec(w, id)
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
	superseded := map[string]bool{} // やり直した元の記録は回答済みにも出さない(新しい記録に答えが写っている。件数の水増しと、元への訂正が新しい記録に届かない経路を防ぐ)
	for _, rec := range recs {
		if rec.RedoOf != "" {
			superseded[rec.RedoOf] = true
		}
	}
	items := []map[string]any{}
	for _, rec := range recs {
		l, ok := labeled[rec.TraceID]
		if !ok || !store.HumanSource(l.Source) || superseded[rec.TraceID] {
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
		writeUnknownSpec(w, req.Spec)
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
	if err := recordDecisionRec(s.st, sp, d, newRec); err != nil { // /v1/decide と同じ作法(文脈の段で act なら最小の段の機械の正解も)
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
		writeUnknownSpec(w, id)
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
		// 回答済みの数 = 人が答えた本文(inputs_hash)の数。やり直しで写した答えや、同じ記録への答えの直しは数えない
		answered := map[string]bool{}
		for _, l := range labeled {
			if store.HumanSource(l.Source) {
				answered[l.InputsHash] = true
			}
		}
		funnel, _ := s.st.Funnel(sp.ID, decide.DiscardUnsure(sp))
		s.mu.Lock()
		last := s.lastCal[sp.ID]
		s.mu.Unlock()
		out = append(out, map[string]any{"id": sp.ID, "type": string(sp.Type), "hash": sp.Hash(), "titles": titles, "questions": questions, "question": strings.TrimSpace(sp.Question), "records": nRec, "labels": len(answered), "label_rows": len(labs),
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

// handleUI は画面(web/ を Svelte で組んだもの)。ビルド結果 web/dist は実行ファイルに埋め込まれている(shikii.WebFS)ので外部の読み込みは無い。
// "/" が index.html、"/assets/..." がその JS と CSS。それ以外の知らないパスは 404(/v1/... は mux が先に取る)。
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r) // フォルダの一覧(/assets/)は出さない。"/" 以外に HTML のパスは無い
		return
	}
	w.Header().Set("Cache-Control", "no-cache") // dist は版ごとに同じ名前(assets/index.js)なので、古い版を抱えない
	webFiles.ServeHTTP(w, r)
}

var webFiles = http.FileServer(http.FS(mustSub(shikii.WebFS, "web/dist")))

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// writeUnknownSpec は spec の id が解決できない時の返事。id が空なら「spec が要る」と言う(初めての利用者が /v1/redo に trace_id だけ送った時、
// `unknown spec ""` では何を足せばよいか分からなかった)。
func writeUnknownSpec(w http.ResponseWriter, id string) {
	if id == "" {
		writeErr(w, 400, errors.New(`spec is required: the decision's spec id (the file name in decisions/ without .yaml, as in /v1/queue?spec=<id>)`))
		return
	}
	writeErr(w, 404, fmt.Errorf("unknown spec %q", id))
}
