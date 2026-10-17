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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
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
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var o serveOpts
	o.ef.bind(fs)
	fs.IntVar(&o.port, "port", 8095, "待ち受けるポート(127.0.0.1 のみ)")
	fs.StringVar(&o.specsDir, "specs", "decisions", "spec(*.yaml)のフォルダ")
	fs.StringVar(&o.storeDir, "store", ".shikii", "記録とラベルの置き場")
	fs.BoolVar(&o.autoCalibrate, "auto-calibrate", true, "正解が eval.min_n に届いたら自動で較正して spec に書き戻す")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := loadSpecs(o.specsDir); err != nil {
		return err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", o.port)
	log.Printf("shikii serve: specs=%s store=%s local=%q hosted=%q auto-calibrate=%v  →  http://%s/", o.specsDir, o.storeDir, o.ef.local, o.ef.hosted, o.autoCalibrate, addr)
	return http.ListenAndServe(addr, newServer(o))
}

type server struct {
	o       serveOpts
	st      store.Store
	calibMu sync.Mutex        // calibrate は stdout を差し替えて報告を捕まえるので 1 つずつ
	lastCal map[string]string // spec id → 最後の自動較正の報告
	mu      sync.Mutex
}

func newServer(o serveOpts) http.Handler {
	s := &server{o: o, st: store.Store{Dir: o.storeDir}, lastCal: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleUI)
	mux.HandleFunc("/v1/specs", s.handleSpecs)
	mux.HandleFunc("/v1/decide", s.handleDecide)
	mux.HandleFunc("/v1/label", s.handleLabel)
	mux.HandleFunc("/v1/queue", s.handleQueue)
	mux.HandleFunc("/v1/status", s.handleStatus)
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
	m := map[string]any{"id": sp.ID, "type": string(sp.Type), "path": sp.Path(), "hash": sp.Hash(), "question": sp.Question,
		"min_n": sp.Eval.MinN, "act": sp.Thresholds.Act, "ask": sp.Thresholds.Ask, "choices": truthChoices(sp)}
	return m
}

// truthChoices は画面のボタンになる正解の候補。choice は選択肢 id、noul は yes/no、score は "0".."n-1"。
func truthChoices(sp *spec.Spec) []map[string]string {
	var out []map[string]string
	switch sp.Type {
	case spec.Noul:
		out = append(out, map[string]string{"truth": "yes", "label": "yes"}, map[string]string{"truth": "no", "label": "no"})
	case spec.Choice:
		for _, o := range sp.Options {
			out = append(out, map[string]string{"truth": o.ID, "label": o.ID, "description": o.Description})
		}
	case spec.Score:
		for i, lv := range sp.Levels {
			out = append(out, map[string]string{"truth": fmt.Sprint(i), "label": fmt.Sprintf("%d: %s", i, lv)})
		}
	}
	return out
}

func (s *server) handleDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, errors.New("POST only"))
		return
	}
	var req struct {
		Spec    string            `json:"spec"`
		Inputs  map[string]string `json:"inputs"`
		Tainted bool              `json:"tainted"`
		Record  *bool             `json:"record"`
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
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, req.Inputs, decide.Session{Tainted: req.Tainted || s.o.ef.tainted}, s.o.ef.endpoints())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	if (req.Record == nil || *req.Record) && d.Band != decide.Abstain {
		if err := s.st.AppendRecord(recordOf(sp, d, req.Inputs)); err != nil {
			writeErr(w, 500, fmt.Errorf("record: %w", err))
			return
		}
	}
	writeJSON(w, 200, d)
}

func (s *server) handleLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, errors.New("POST only"))
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
	l, err := store.LabelFor(rec, req.Truth, req.Weight, req.Source)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.st.AppendLabel(l); err != nil {
		writeErr(w, 500, err)
		return
	}
	resp := map[string]any{"trace_id": l.TraceID, "spec": l.SpecID, "model": l.Model, "truth": l.Truth, "weight": l.Weight, "source": l.Source, "calibrated": false}
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
	if err != nil || sp.Hash() != hash || model == "" {
		return "", false
	}
	if c := sp.CalibrationFor(model); c != nil && c.SpecHash == sp.Hash() {
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
	err = runCalibrate(sp.Path(), calibrateOpts{ef: ef, storeDir: s.o.storeDir, labeled: "none", bins: 10, write: true})
	stdout = old
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
	rank := map[string]int{"ask": 0, "act": 1, "discard": 2}
	var items []map[string]any
	for _, rec := range recs {
		if _, ok := labeled[rec.TraceID]; ok {
			continue
		}
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "raw": rec.Raw, "stale": rec.SpecHash != sp.Hash(),
			"choices": truthChoices(sp),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return rank[items[i]["band"].(string)] < rank[items[j]["band"].(string)] })
	if len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items, "text_stored": sp.Policy.PII == "never" || sp.Policy.PII == "local"})
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := []map[string]any{}
	for _, sp := range specs {
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
			method := ""
			switch {
			case hash != sp.Hash():
				state = "stale"
			case sp.CalibrationFor(model) != nil && sp.CalibrationFor(model).SpecHash == sp.Hash():
				state, method = "calibrated", sp.CalibrationFor(model).Method
			case n >= sp.Eval.MinN:
				state = "ready"
			}
			keys = append(keys, map[string]any{"model": model, "hash": hash, "n": n, "state": state, "method": method})
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i]["model"].(string) < keys[j]["model"].(string) })
		labeled, _ := s.st.LabeledTraces(sp.ID)
		s.mu.Lock()
		last := s.lastCal[sp.ID]
		s.mu.Unlock()
		out = append(out, map[string]any{"id": sp.ID, "type": string(sp.Type), "hash": sp.Hash(), "records": nRec, "labels": len(labs),
			"unlabeled": nRec - countLabeledRecords(labeled, nRec), "min_n": sp.Eval.MinN, "keys": keys, "last_calibration": last})
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
  #lang { margin-left:auto; font: inherit; font-size: 13px; padding: 3px 6px; border-radius: 6px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); }
  h2 { font-size: 16px; margin: 28px 0 8px; display:flex; gap:10px; align-items:baseline; flex-wrap: wrap; }
  .pill { font-size: 12px; padding: 2px 8px; border-radius: 999px; border: 1px solid var(--line); color: var(--muted); }
  .pill.calibrated { background: var(--okbg); color: var(--ok); border-color: transparent; }
  .pill.ready { background: var(--amberbg); color: var(--amber); border-color: transparent; }
  .item { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; margin: 8px 0; }
  .item.ask { border-color: var(--amber); }
  .meta { color: var(--muted); font-size: 12px; margin-bottom: 6px; display:flex; gap:12px; flex-wrap:wrap; }
  .text { white-space: pre-wrap; word-break: break-word; margin: 4px 0 10px; }
  .text .k { color: var(--muted); font-size: 12px; }
  .btns { display:flex; gap:8px; flex-wrap:wrap; }
  button { font: inherit; padding: 6px 12px; border-radius: 8px; border: 1px solid var(--line); background: transparent; color: var(--fg); cursor: pointer; }
  button:hover { border-color: var(--fg); } button.top { border-color: var(--amber); }
  button.weak { color: var(--muted); font-size: 13px; }
  .empty { color: var(--muted); padding: 10px 0; }
  pre { font-size: 12px; color: var(--muted); white-space: pre-wrap; margin: 6px 0 0; }
  #toast { position: fixed; bottom: 16px; left: 50%; transform: translateX(-50%); background: var(--fg); color: var(--bg); padding: 8px 14px; border-radius: 8px; display: none; max-width: 90vw; white-space: pre-wrap; }
</style>
<header>
  <h1>shikii <small id="tagline"></small></h1>
  <select id="lang" aria-label="language"><option value="en">English</option><option value="ja">日本語</option></select>
</header>
<div id="root"><div class="empty">…</div></div>
<div id="toast"></div>
<script>
// 辞書。言語を足すときはここに 1 つ増やし、<select> に option を足す。
const T = {
  en: { tagline: 'answer what was true; the line draws itself', loading: 'loading…', noSpecs: 'no specs', empty: 'nothing waiting for an answer',
        records: n => n + ' records', answered: n => n + ' answered', calibrated: m => 'calibrated (' + m + ')', stale: 'stale wording', collecting: (n, m) => 'collecting ' + n + '/' + m, ready: (n, m) => 'ready ' + n + '/' + m,
        noText: h => '(text not stored: policy.pii is possible; only the hash ' + h + ')', model: p => 'model ' + p + '%', notThat: t => 'not ' + t + ' (that is all I know)', changed: 'wording changed since',
        done: t => 'answered: ' + t, calibratedToast: 'enough answers: calibration written', error: m => 'error: ' + m, last: 'last auto-calibration' },
  ja: { tagline: '本当はどうだったかを答える。線は自分で引かれる', loading: '読み込み中…', noSpecs: 'spec がありません', empty: '答え合わせ待ちはありません',
        records: n => '記録 ' + n, answered: n => '答え済み ' + n, calibrated: m => '線あり (' + m + ')', stale: '文言が古い', collecting: (n, m) => '集めている ' + n + '/' + m, ready: (n, m) => '線を引ける ' + n + '/' + m,
        noText: h => '(本文は残していません: policy.pii が possible。ハッシュ ' + h + ' だけ)', model: p => 'AI は ' + p + '%', notThat: t => t + ' ではない(それしか分からない)', changed: 'この後に文言が変わった',
        done: t => '答えました: ' + t, calibratedToast: '答えが揃いました。線を引いて書き込みました', error: m => 'エラー: ' + m, last: '最後の自動較正' },
};
const $ = (s, el=document) => el.querySelector(s);
const esc = s => String(s).replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
let lang = 'en';
try { lang = localStorage.getItem('shikii.lang') || (navigator.language || 'en').slice(0,2); } catch (e) {}
if (!T[lang]) lang = 'en';
const t = k => T[lang][k];
$('#lang').value = lang;
$('#lang').addEventListener('change', e => { lang = e.target.value; try { localStorage.setItem('shikii.lang', lang); } catch (x) {} render(); });
let toastTimer;
function toast(msg, ms=4000) { const el = $('#toast'); el.textContent = msg; el.style.display = 'block'; clearTimeout(toastTimer); toastTimer = setTimeout(() => el.style.display = 'none', ms); }
async function j(url, opts) { const r = await fetch(url, opts); const b = await r.json(); if (!r.ok) throw new Error(b.error || r.status); return b; }
let state = null;
async function load() {
  const st = await j('/v1/status');
  for (const sp of st.specs) sp.queue = await j('/v1/queue?spec=' + encodeURIComponent(sp.id) + '&limit=30');
  state = st; render();
}
function render() {
  $('#tagline').textContent = t('tagline');
  document.documentElement.lang = lang;
  const root = $('#root'); root.innerHTML = '';
  if (!state) { root.innerHTML = '<div class="empty">' + esc(t('loading')) + '</div>'; return; }
  if (!state.specs.length) { root.innerHTML = '<div class="empty">' + esc(t('noSpecs')) + '</div>'; return; }
  for (const sp of state.specs) {
    const sec = document.createElement('section');
    const pills = sp.keys.map(k => '<span class="pill ' + k.state + '">' + esc(k.model) + ' · ' + esc(k.state === 'calibrated' ? t('calibrated')(k.method) : k.state === 'stale' ? t('stale') : t(k.state)(k.n, sp.min_n)) + '</span>').join(' ');
    sec.innerHTML = '<h2>' + esc(sp.id) + ' <span class="pill">' + esc(sp.type) + '</span> ' + pills + ' <span class="pill">' + esc(t('records')(sp.records)) + ' · ' + esc(t('answered')(sp.labels)) + '</span></h2>';
    if (!sp.queue.items.length) { sec.innerHTML += '<div class="empty">' + esc(t('empty')) + '</div>'; }
    for (const it of sp.queue.items) {
      const el = document.createElement('div'); el.className = 'item ' + it.band;
      const text = it.inputs ? Object.entries(it.inputs).map(([k,v]) => '<div><span class="k">' + esc(k) + '</span><br>' + esc(v) + '</div>').join('') : '<span class="k">' + esc(t('noText')(it.inputs_hash)) + '</span>';
      const btns = it.choices.map(c => '<button class="' + (c.truth === it.top ? 'top' : '') + '" data-truth="' + esc(c.truth) + '" title="' + esc(c.description || '') + '">' + esc(c.label) + (c.truth === it.top ? ' · ' + esc(t('model')((it.p*100).toFixed(0))) : '') + '</button>').join('');
      const not = it.top ? '<button class="weak" data-truth="!' + esc(it.top) + '">' + esc(t('notThat')(it.top)) + '</button>' : '';
      el.innerHTML = '<div class="meta"><span>' + esc(it.band) + '</span><span>' + esc(new Date(it.at).toLocaleString()) + '</span><span>' + esc(it.model || '') + '</span>' + (it.stale ? '<span>' + esc(t('changed')) + '</span>' : '') + '</div><div class="text">' + text + '</div><div class="btns">' + btns + not + '</div>';
      el.addEventListener('click', async ev => {
        const b = ev.target.closest('button'); if (!b) return;
        for (const x of el.querySelectorAll('button')) x.disabled = true;
        try {
          const res = await j('/v1/label', { method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({ trace_id: it.trace_id, spec: sp.id, truth: b.dataset.truth, source: 'ui' }) });
          el.remove();
          if (res.calibrated) { toast(t('calibratedToast') + '\n' + (res.calibration || '').split('\n').slice(-2).join('\n'), 8000); load(); } else { toast(t('done')(res.truth)); }
        } catch (e) { toast(t('error')(e.message)); for (const x of el.querySelectorAll('button')) x.disabled = false; }
      });
      sec.appendChild(el);
    }
    if (sp.last_calibration) sec.innerHTML += '<pre>' + esc(t('last')) + '\n' + esc(sp.last_calibration) + '</pre>';
    root.appendChild(sec);
  }
}
render();
load().catch(e => { $('#root').innerHTML = '<div class="empty">' + esc(t('error')(e.message)) + '</div>'; });
setInterval(() => { if (!document.hidden) load().catch(()=>{}); }, 15000);
</script>
`
