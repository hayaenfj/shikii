// shikii は決定モデルの運用 CLI。
//
//	shikii decide    <spec.yaml> -i k=v ... [--model M] [--hosted URL] [--local URL] [--tainted] [--store DIR]
//	shikii label     <trace_id> <truth> [--spec-id ID] [--weight W] [--source ask|undo|manual] [--store DIR]
//	shikii calibrate <spec.yaml> [--labeled file] [--model M] [--store DIR] [--write] [--force]
//	shikii hash      <spec.yaml>
//
// ホスト版の鍵は TYPESAFE_API_KEY から読む。鍵の取り出しはこのプロセスでは行わない。
// 判定は既定で ./.shikii/ に記録され、label がそこから正解ラベルを作り、calibrate がそれを使う。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

var stdout io.Writer = os.Stdout

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "decide":
		err = cmdDecide(os.Args[2:])
	case "label":
		err = cmdLabel(os.Args[2:])
	case "calibrate":
		err = cmdCalibrate(os.Args[2:])
	case "hash":
		err = cmdHash(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "drift":
		err = cmdDrift(os.Args[2:])
	case "bias":
		err = cmdBias(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "gc":
		err = cmdGC(os.Args[2:])
	case "wiki":
		err = cmdWiki(os.Args[2:])
	case "init":
		err = cmdInit(os.Args[2:])
	case "import":
		err = cmdImport(os.Args[2:])
	case "judge":
		err = cmdJudge(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "new":
		err = cmdNew(os.Args[2:])
	case "up":
		err = cmdUp(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "tune":
		err = cmdTune(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "shikii:", err)
		os.Exit(1)
	}
}

func usage() { fmt.Fprint(os.Stderr, usageText()) }

func usageText() string {
	return `usage (flags may come before or after the positional arguments):
  shikii init      実行する場所に decisions/ wiki/ .shikii/ を作り、.mcp.json に shikii を足し、skill を ~/.claude/skills/shikii に写す(オプション無し、2 回目も安全)
  shikii new       <id> [--type choice|noul|score] [--question ...] [--option id=desc ...] [--input name ...] [--pii true|false] [--send-to local|hosted] [--from spec.yaml]
                   質問に答えるだけで decisions/<id>.yaml を作る
  shikii up        [--hf repo:quant | --gguf file --alias name] [--port 8080]
                   手元のモデルを起こす(llama-server を探し、普通の GGUF なら shikii-readout も前に立てる)
  shikii serve     [--port 8095] [--specs DIR] [--store DIR] [--local URL] [--hosted URL] [--proposer URL] [--auto-calibrate=false]
                   HTTP の口 + 答え合わせと聞き方の画面。wiki は ./wiki(基本構成: 実行する場所に decisions/ wiki/ .shikii/)
  shikii mcp       [--specs DIR] [--store DIR] [--local URL] [--hosted URL] [--proposer URL]
                   同じ中身を stdio の MCP サーバーとして出す(道具: shikii_specs / shikii_decide / shikii_label / shikii_status / shikii_tune。採用の道具は無い)
  shikii doctor    [--specs DIR] [--store DIR] [--local URL] [--hosted URL]
  shikii decide    <spec.yaml> -i key=value ... [--model M] [--hosted URL] [--local URL] [--tainted] [--store DIR] [--no-record] [--json]
                   spec に state.context があれば、迷った時(ask。多択では discard も)だけ文脈の入力を足して聞き直す(別のハッシュ・別の線)。そこで確信に届いた答えは最小の質問の正解として残る
  shikii label     <trace_id> <truth> [--spec-id ID] [--weight W] [--source ask|outcome|manual] [--store DIR]
  shikii label     --last --spec-id ID <truth>
  shikii calibrate <spec.yaml> [--labeled file] [--model M] [--local URL] [--hosted URL] [--store DIR] [--bins 10] [--write] [--force]
                   正解で線を引く。今の (hash, model) に無い正解は本文で聞き直す。state.context があれば文脈の段の線も別に引く
                   --labeled の行に raw(noul は P(yes) の数字、choice / score は {id: p})があればモデルを呼ばずその確率で測る(別の道具の確率を同じ物差しで)
  shikii status    [spec.yaml...] [--store DIR]      記録と正解の数、段ごとの数(1000 判定あたりの人の判断)、線の状態
  shikii tune      <spec.yaml> [--labeled file] [--local URL] [--store DIR] [--folds 5] [--propose N --proposer URL [--proposer-model M]] [--write]
                   spec の variants:(聞き方の候補)を正解の貯金で採点。holdout が現状を上回り、人が意味を確かめた候補だけ新しい版に。結果は ./wiki/runs/
  shikii import    <past.csv> --spec <spec.yaml> [--out truths.json]
                   手元の過去の表(入力の列 + truth [+ weight])を正解の貯金(JSON)にする。線を引く前の 50 件を業務の表で埋める
  shikii judge     <spec.yaml> --local <judge URL> [--model M] [--store DIR]
                   審判: 別の(大きい)モデルに同じ spec を聞く。人の正解との一致率が act に届けば、正解の無い記録に重み付きの正解(source judge)を付ける。審判も迷えば人へ
  shikii wiki verify [pattern.md...] [--local URL] [--labeled file] [--store DIR]
                   wiki/patterns の主張(前書き: claim / applies_to / evidence / retest)の再検証を今のモデルで回し、証拠を書き足す。状態を変えるのは人
  shikii check     <spec.yaml>... [--store DIR]      CI: 文言が変わった線、手書きの線、精度不足の線を落とす
  shikii drift     <spec.yaml> --model M [--window N] [--store DIR]
  shikii bias      <spec.yaml> [--rows K] [--permutations N] [--store DIR] [--labeled file] [--local URL] [--hosted URL]
                   選択肢の並びで答えが変わるかを測る。本文は calibrate と同じ正解の貯金(store、または --labeled)から
  shikii gc        [spec.yaml...] [--older-than 30d] [--apply] [--store DIR]
  shikii hash      <spec.yaml>
`
}

// endpointFlags は decide / calibrate 共通のエンドポイント指定。
type endpointFlags struct {
	model, hosted, local string
	tainted              bool
}

func (f *endpointFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.model, "model", "jev-latest", "モデル名(リクエストに入れる。calibrate では較正の鍵にもなる)")
	fs.StringVar(&f.hosted, "hosted", "https://api.typesafe.ai", "ホスト版の URL。パス付きならそのまま使う(例 https://openrouter.ai/api/alpha/decisions)。鍵は DECISIONS_API_KEY か TYPESAFE_API_KEY")
	fs.StringVar(&f.local, "local", "", "ローカル(llama.cpp 等)のベース URL")
	fs.BoolVar(&f.tainted, "tainted", false, "本番データを読んだセッション: hosted に送らない")
}

// hostedKey は hosted 用の鍵。DECISIONS_API_KEY(汎用)か TYPESAFE_API_KEY。鍵の取り出しはこのプロセスでは行わない。
func hostedKey() string {
	if k := os.Getenv("DECISIONS_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("TYPESAFE_API_KEY")
}

func (f *endpointFlags) endpoints() []decide.Endpoint {
	var eps []decide.Endpoint
	if key := hostedKey(); key != "" && f.hosted != "" {
		eps = append(eps, decide.Endpoint{Kind: "hosted", Client: systemone.New(f.hosted, key, f.model)})
	}
	if f.local != "" {
		eps = append(eps, decide.Endpoint{Kind: "local", Client: systemone.New(f.local, "", f.model)})
	}
	return eps
}

func bindStore(fs *flag.FlagSet) *string {
	return fs.String("store", ".shikii", "記録とラベルの置き場")
}

type kvFlag map[string]string

func (k kvFlag) String() string { return "" }
func (k kvFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("want key=value, got %q", v)
	}
	k[v[:i]] = v[i+1:]
	return nil
}

// ---------------------------------------------------------------- decide

func cmdDecide(args []string) error {
	fs := flag.NewFlagSet("decide", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	noRecord := fs.Bool("no-record", false, "記録しない")
	inputs := kvFlag{}
	fs.Var(inputs, "i", "入力 key=value(複数可)")
	asJSON := fs.Bool("json", false, "Decision を JSON で出す")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{Tainted: ef.tainted}, ef.endpoints())
	if err != nil {
		return err
	}
	attachReceipt(sp, store.Store{Dir: *storeDir}, d)
	if !*noRecord && d.Band != decide.Abstain {
		if err := recordDecision(store.Store{Dir: *storeDir}, sp, d, inputs); err != nil {
			return err
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s endpoint=%s trace=%s\n", d.SpecID, d.SpecHash, d.Model, d.Endpoint, d.TraceID)
	fmt.Fprintf(stdout, "band=%s top=%s p=%.3f calibrated=%v", d.Band, d.Top, d.P, d.IsCalibrated)
	if sp.Type == spec.Score {
		fmt.Fprintf(stdout, " score=%.2f", d.Score)
	}
	fmt.Fprintln(stdout)
	if d.Reason != "" {
		fmt.Fprintln(stdout, "reason:", d.Reason)
	}
	if d.Minimal != nil {
		fmt.Fprintf(stdout, "rung=context (the minimal question was unsure: top=%s p=%.3f band=%s; asked again with state.context)\n", d.Minimal.Top, d.Minimal.P, d.Minimal.Band)
	}
	if r := d.Receipt; r != nil {
		fmt.Fprintf(stdout, "receipt: line %s fitted %s on %d truths (%d human); human-checked act decisions %d, precision %.3f\n", r.Method, r.FittedAt, r.N, r.NHuman, r.HumanCheckedAct, r.HumanPrecision)
	}
	if len(d.Raw) > 0 {
		fmt.Fprintln(stdout, "raw:", fmtDist(d.Raw))
		if d.IsCalibrated {
			fmt.Fprintln(stdout, "calibrated:", fmtDist(d.Calibrated))
		}
	}
	return nil
}

// recordOf は Decision を記録の形にする。本文は常に残す。
// copyInputs は本文の写し(記録とラベルに残す)。
func copyInputs(inputs map[string]string) map[string]string {
	if len(inputs) == 0 {
		return nil
	}
	out := make(map[string]string, len(inputs))
	for k, v := range inputs {
		out[k] = v
	}
	return out
}

func recordOf(sp *spec.Spec, d *decide.Decision, inputs map[string]string) store.Record {
	return store.Record{
		TraceID: d.TraceID, SpecID: d.SpecID, SpecHash: d.SpecHash, Type: d.Type,
		Model: d.Model, Endpoint: d.Endpoint,
		Inputs: copyInputs(inputs), InputsHash: d.InputsHash,
		Raw: d.Raw, Calibrated: d.Calibrated, IsCalibrated: d.IsCalibrated,
		Top: d.Top, P: d.P, Band: string(d.Band), Tainted: d.Tainted, Rung: d.Rung, Minimal: d.Minimal, At: d.At,
	}
}

// recordDecision は判定を記録する。文脈を足した段で確信(act)に届いたら、その答えを最小の質問の正解として残す:
// 上の段の答えが下の段の正解になる(重みは較正済みの確率、出どころは context)。人が後で答えればそちらが新しいので勝つ。
func recordDecision(st store.Store, sp *spec.Spec, d *decide.Decision, inputs map[string]string) error {
	return recordDecisionRec(st, sp, d, recordOf(sp, d, inputs))
}

// recordDecisionRec は rec(recordOf に印を足したもの。やり直しなら RedoOf)を書く本体。判定の記録の作法はここ 1 か所(監査 6 の 5)。
func recordDecisionRec(st store.Store, sp *spec.Spec, d *decide.Decision, rec store.Record) error {
	if err := st.AppendRecord(rec); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	if d.Rung != "context" || d.Band != decide.Act || d.Minimal == nil {
		return nil
	}
	if sp.Type == spec.Score && sp.Thresholds.Level > 0 {
		return nil // P は P(level ≥ k)、Top は一番高い段階。別の事象なので 1 つの正解にはできない(監査 3 回目の 8)
	}
	inputsHash := d.Minimal.InputsHash
	if inputsHash == "" {
		inputsHash = rec.InputsHash
	}
	l := store.Label{
		TraceID: rec.TraceID, SpecID: rec.SpecID, SpecHash: d.Minimal.SpecHash, Type: rec.Type, Model: rec.Model,
		InputsHash: inputsHash, Inputs: rec.Inputs, Raw: d.Minimal.Raw,
		Truth: d.Top, Weight: d.P, Source: "context", Tainted: rec.Tainted, Rung: "minimal", At: rec.At,
	}
	if err := st.AppendLabel(l); err != nil {
		return fmt.Errorf("record context truth: %w", err)
	}
	return nil
}

// attachReceipt は act の判定に領収書を付ける: その段の線(方法・日・正解の数)と、人が確かめた act の判定の数と精度(store の人の正解だけ)。
func attachReceipt(sp *spec.Spec, st store.Store, d *decide.Decision) {
	if d.Band != decide.Act {
		return
	}
	rs := sp
	if d.Rung == "context" && sp.ContextSpec() != nil {
		rs = sp.ContextSpec()
	}
	c := rs.CalibrationFor(d.Model)
	if c == nil {
		return
	}
	r := &decide.Receipt{SpecHash: c.SpecHash, Model: c.Model, Method: c.Method, FittedAt: c.FittedAt, N: c.N, NHuman: c.NHuman, PrecisionAtFit: c.PrecisionAt}
	if labels, err := st.Labels(sp.ID, c.SpecHash, d.Model); err == nil {
		var human []store.Label
		for _, l := range store.LatestPerInput(labels) {
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		if len(human) > 0 {
			r.HumanPrecision, _, r.HumanCheckedAct = evaluateCalibration(rs, *c, human)
		}
	}
	d.Receipt = r
}

// ---------------------------------------------------------------- label

func cmdLabel(args []string) error {
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	storeDir := bindStore(fs)
	specID := fs.String("spec-id", "", "記録を探す spec(省略時は全部見る)")
	weight := fs.Float64("weight", 1, "ラベルの重み(弱いラベルは 1 未満)")
	source := fs.String("source", "manual", "ラベルの出どころ: ask | outcome | manual | ui | mcp など(replay と file は calibrate が付ける)")
	last := fs.Bool("last", false, "trace_id の代わりに、--spec-id の直近の記録に付ける")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	var rec *store.Record
	var truth string
	switch {
	case *last:
		if len(pos) != 1 || *specID == "" {
			return fmt.Errorf("usage: shikii label --last --spec-id ID <truth>")
		}
		r, err := st.LastRecord(*specID)
		if err != nil {
			return err
		}
		rec, truth = r, pos[0]
	default:
		if len(pos) != 2 {
			return fmt.Errorf("usage: shikii label <trace_id> <truth>  |  shikii label --last --spec-id ID <truth>")
		}
		r, err := st.FindRecord(*specID, pos[0])
		if err != nil {
			return err
		}
		rec, truth = r, pos[1]
	}
	labels, err := store.LabelsFor(rec, truth, *weight, *source)
	if err != nil {
		return err
	}
	if err := st.AppendLabels(labels); err != nil { // 2 段分を 1 回で書く(片方だけ残らない)
		return err
	}
	for _, l := range labels {
		fmt.Fprintf(stdout, "labeled %s (%s, %s, %s rung) truth=%s weight=%.2f source=%s\n", l.TraceID, l.SpecID, l.Model, l.Rung, l.Truth, l.Weight, l.Source)
	}
	return nil
}

// ---------------------------------------------------------------- calibrate

// labeled はラベル付きデータ 1 行。Truth は noul なら true/false か "yes"/"no"、choice/score なら id/番号。
// Raw(noul の生の確率)があればモデルを呼ばない。
type labeled struct {
	Inputs map[string]string `json:"inputs"`
	Truth  json.RawMessage   `json:"truth"`
	Weight float64           `json:"weight,omitempty"`
	// Raw は確率の持ち込み(モデルを呼ばない行)。noul は P(yes) の数字 1 つ、choice / score は {id: p} の object。
	// 比較ベンチの LoRA の腕のように、別の道具が出した確率を同じ物差しで測るのに使う。
	Raw json.RawMessage `json:"raw,omitempty"`
}

// rawFromRow は行の raw を分布にする。無ければ nil。鍵が spec の選択肢に無ければ誤り(黙って捨てない)。
func rawFromRow(sp *spec.Spec, raw json.RawMessage) (map[string]float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var p float64
	if err := json.Unmarshal(raw, &p); err == nil {
		if sp.Type != spec.Noul {
			return nil, fmt.Errorf("raw: a single number is only for noul; give {id: probability} for %s", sp.Type)
		}
		if p < 0 || p > 1 {
			return nil, fmt.Errorf("raw: P(yes) must be in 0..1, got %g", p)
		}
		return map[string]float64{"yes": p, "no": 1 - p}, nil
	}
	var dist map[string]float64
	if err := json.Unmarshal(raw, &dist); err != nil {
		return nil, fmt.Errorf("raw: want a number (noul) or {id: probability}: %w", err)
	}
	// 鍵は全部揃っていて(noul は yes が要る。no は補う)、各値は 0..1、和は 1 に近いこと。欠け・範囲外は黙って 0 にしない(監査 4 回目の 9)
	if sp.Type == spec.Noul {
		if _, ok := dist["yes"]; !ok {
			return nil, errors.New("raw: noul needs \"yes\"")
		}
		if _, ok := dist["no"]; !ok {
			dist["no"] = 1 - dist["yes"]
		}
	}
	sum := 0.0
	for k, v := range dist {
		if !sp.HasTruthKey(k) {
			return nil, fmt.Errorf("raw: %q is not an option of this spec", k)
		}
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("raw: %q must be in 0..1, got %g", k, v)
		}
		sum += v
	}
	for _, id := range sp.TruthKeys() {
		if _, ok := dist[id]; !ok {
			return nil, fmt.Errorf("raw: missing %q (give every option's probability)", id)
		}
	}
	if sum < 0.98 || sum > 1.02 {
		return nil, fmt.Errorf("raw: probabilities sum to %.3f, want 1", sum)
	}
	return dist, nil
}

func parseTruth(t spec.Type, raw json.RawMessage) (string, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if t != spec.Noul {
			return "", fmt.Errorf("%s truth must be a string, got %s", t, raw)
		}
		if b {
			return "yes", nil
		}
		return "no", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("truth must be bool or string, got %s", raw)
	}
	return s, nil
}

func cmdCalibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	labeledPath := fs.String("labeled", "", "ラベル付きデータ(省略時は spec の eval.labeled。無ければ store のラベルだけ。none なら spec の既定も使わない)")
	bins := fs.Int("bins", 10, "信頼度図の bin 数")
	write := fs.Bool("write", false, "較正を spec に書き戻す")
	force := fs.Bool("force", false, "逆転・精度不足でも書き戻す")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	return runCalibrate(path, calibrateOpts{ef: ef, storeDir: *storeDir, labeled: *labeledPath, bins: *bins, write: *write, force: *force})
}

// calibrateOpts は calibrate の指定。CLI と serve(自動の較正)が同じ中身を使う。
type calibrateOpts struct {
	ef       endpointFlags
	storeDir string
	labeled  string // "" = spec の eval.labeled、"none" = ファイルを使わない
	bins     int
	write    bool
	force    bool
	rung     string // "" = 両方の段、"minimal" | "context" = その段だけ引く(serve の自動較正は正解が揃った段だけ)
}

// runCalibrate は calibrate の本体。報告は stdout に出す。
func runCalibrate(path string, o calibrateOpts) error {
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	wrote := false
	// 最小の質問の段。ここに線が引けないのは誤り(今までと同じ)。
	if o.rung != "context" {
		cal, ok, err := calibrateRung(sp, o)
		if err != nil {
			return err
		}
		if o.write && !ok {
			return fmt.Errorf("calibrate: refusing to write: fewer human truths than eval.min_n=%d (see WARNING above). Collect more truths (shikii serve, shikii label, shikii import), or for a trial lower eval.min_n in the spec to the truths you have; --force writes anyway but check will flag the line", sp.Eval.MinN)
		}
		if o.write {
			sp.PutCalibration(cal)
			wrote = true
		}
	}
	// 文脈の段(あれば)。正解は、文脈を足して決まった判定に人が答えた分だけ。まだ無ければ知らせて進む。
	if csp := sp.ContextSpec(); csp != nil && o.rung != "minimal" {
		fmt.Fprintf(stdout, "\ncontext rung: the same question asked with state.context (hash=%s)\n", csp.Hash())
		co := o
		co.labeled = "none" // ファイルの行は最小の質問のもの
		ccal, cok, err := calibrateRung(csp, co)
		switch {
		case errors.Is(err, errNoLabels):
			fmt.Fprintln(stdout, "note: no truths on the context rung yet (they come from people answering decisions that went through it)")
		case err != nil:
			return err
		case o.write && cok:
			sp.PutCalibration(ccal)
			wrote = true
		case o.write && o.rung == "context":
			return fmt.Errorf("calibrate: refusing to write the context rung (see WARNING above)")
		case o.write:
			fmt.Fprintln(stdout, "note: context rung not written (see WARNING above)")
		}
	}
	if wrote {
		if err := sp.Save(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "wrote calibration to", sp.Path())
	}
	return nil
}

var errNoLabels = errors.New("no labels")

// calibrateRung は 1 つの段(spec のハッシュ 1 つ)の線を引く。報告は常に出し、書いてよいか(ok)を返す。線そのものは書かない(呼ぶ側がまとめて書く)。
func calibrateRung(sp *spec.Spec, o calibrateOpts) (spec.Calibration, bool, error) {
	ef, storeDir, labeledPath, bins, force := o.ef, &o.storeDir, &o.labeled, &o.bins, &o.force
	var cal spec.Calibration
	if *bins <= 0 {
		*bins = 10
	}
	if *labeledPath == "none" {
		*labeledPath = "" // store のラベルと聞き直しだけで進む
	} else if *labeledPath == "" {
		// spec 既定のファイルは無くてもよい(store のラベルだけで進む)。--labeled で明示したら無ければ誤り。
		if _, err := os.Stat(sp.Eval.Labeled); sp.Eval.Labeled != "" && err == nil {
			*labeledPath = sp.Eval.Labeled
		} else if sp.Eval.Labeled != "" {
			fmt.Fprintf(stdout, "note: eval.labeled %s not found; using store labels only\n", sp.Eval.Labeled)
		}
	}

	// 1. ラベル付きファイル(あれば)。Raw の無い行は、store に同じ入力のラベルがあればそれを使い、無ければモデルに聞く。
	st := store.Store{Dir: *storeDir}
	labels, model, err := labelsFromFile(sp, *labeledPath, ef, st)
	if err != nil {
		return cal, false, err
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if l.InputsHash != "" {
			seen[l.InputsHash] = true
		}
	}
	// 2. 聞き直し: 古い文言・別のモデルで付いた正解を、本文が残っている記録から今の (hash, model) で聞き直す。
	//    正解は人が決めたもので文言にもモデルにも依らない。確率だけ取り直す。
	replayed, replayModel, err := replayTruths(sp, st, ef, model, seen)
	if err != nil {
		return cal, false, err
	}
	if model == "" {
		model = replayModel
	}
	if model == "" {
		model = ef.model
	}
	// 3. store に溜まったラベル。(hash, model) が一致するもの全部。ファイル側と同じ入力があっても落とさない:
	//    後で人が付けた訂正の方が新しければ、下の LatestPerInput でそちらが勝つ(監査 C2)。
	stored, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return cal, false, err
	}
	replayedSet, err := storedInputs(st, sp, model)
	if err != nil {
		return cal, false, err
	}
	for _, l := range replayed {
		if replayedSet[l.InputsHash] {
			continue
		}
		replayedSet[l.InputsHash] = true
		if err := st.AppendLabel(l); err != nil {
			return cal, false, fmt.Errorf("persist replayed label: %w", err)
		}
		stored = append(stored, l)
	}
	// 4. モデルに聞いて得たファイル行のラベルは store に残す。以後 check / drift がモデル無しで検証できる。
	persisted := 0
	storedSet, err := storedInputs(st, sp, model)
	if err != nil {
		return cal, false, err
	}
	for _, l := range labels {
		if l.Model == "" || l.InputsHash == "" {
			continue // raw 付きの行(モデルを呼んでいない)は残さない
		}
		if storedSet[l.InputsHash] {
			continue
		}
		storedSet[l.InputsHash] = true
		if err := st.AppendLabel(l); err != nil {
			return cal, false, fmt.Errorf("persist label: %w", err)
		}
		persisted++
	}
	if persisted > 0 {
		fmt.Fprintf(stdout, "persisted %d queried labels to %s\n", persisted, *storeDir)
	}
	fileN, storedN := len(labels), len(stored)
	labels = append(labels, stored...)
	// 入力ごとに最新の正解 1 つ(check / drift と同じ数え方)。訂正は置き換え、同じ入力の重複は 1 件
	if before := len(labels); before > 0 {
		labels = store.LatestPerInput(labels)
		if len(labels) < before {
			fmt.Fprintf(stdout, "deduplicated: %d labels on %d distinct inputs (latest truth per input wins)\n", before, len(labels))
		}
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s n=%d (file=%d store=%d replay=%d)\n", sp.ID, sp.Hash(), model, len(labels), fileN, storedN, len(replayed))
	if len(labels) == 0 {
		return cal, false, fmt.Errorf("calibrate: %w for (hash=%s, model=%s)", errNoLabels, sp.Hash(), model)
	}
	// 件数不足でも報告は出す。書き戻しだけ止める。数えるのは人(または結果)が決めた正解: 機械の正解(上の段の答え)だけで線は引かない(監査 3 回目の 2)
	nHuman := 0
	for _, l := range labels {
		if store.HumanSource(l.Source) {
			nHuman++
		}
	}
	underMin := nHuman < sp.Eval.MinN
	if underMin {
		fmt.Fprintf(stdout, "WARNING: %d human truths (%d samples in all) < eval.min_n=%d; report only, will not write (collect more, or lower eval.min_n in the spec for a trial)\n", nHuman, len(labels), sp.Eval.MinN)
	}

	// 報告は常に出す。書き戻してよいか(ok)は別に返し、--write の時だけ効く。
	var ok bool
	switch sp.Type {
	case spec.Noul:
		cal, ok, err = calibrateNoul(sp, labels, *bins)
	default:
		cal, ok, err = verifyTop(sp, labels, *bins)
	}
	if err != nil {
		return cal, false, err
	}
	cal.Model, cal.SpecHash, cal.N, cal.NHuman = model, sp.Hash(), len(labels), nHuman
	cal.FittedOn, cal.FittedAt = *labeledPath, time.Now().Format("2006-01-02")
	return cal, (ok && !underMin) || *force, nil
}

// labelsFromFile はラベル付きファイルを store.Label の形に揃える。モデルに聞いた行があれば、その実モデル名も返す。
// Raw の無い行は、store に同じ入力(inputs_hash)のラベルが --model の鍵で残っていればそれを使う(モデル無しで再較正できる)。
func labelsFromFile(sp *spec.Spec, path string, ef endpointFlags, st store.Store) ([]store.Label, string, error) {
	if path == "" {
		return nil, "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var rows []labeled
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	eps := ef.endpoints()
	sess := decide.Session{Tainted: ef.tainted}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 同じ入力の以前のラベル(raw と時刻の再利用に使う)。複数あれば一番古いもの = 最初に聞いた時のもの。
	// 後から人が付けた訂正の時刻を借りると、ファイルの正解がその訂正と同時刻になり LatestPerInput で勝ってしまう(監査 C2)。
	stored := map[string]store.Label{}
	if prior, err := st.Labels(sp.ID, sp.Hash(), ef.model); err == nil {
		for _, l := range prior {
			if cur, ok := stored[l.InputsHash]; !ok || l.At.Before(cur.At) {
				stored[l.InputsHash] = l
			}
		}
	}
	// ファイルの行の時刻は「その入力を最初に取り込んだ時刻」(モデルを問わず、今の文言で一番古いラベル)。
	// 毎回「今」にすると、人が後から付けた訂正より新しくなって勝ってしまう。
	firstSeen := map[string]time.Time{}
	if _, all, err := st.Counts(sp.ID); err == nil {
		for _, l := range all {
			if l.SpecHash != sp.Hash() || l.InputsHash == "" {
				continue
			}
			if cur, ok := firstSeen[l.InputsHash]; !ok || l.At.Before(cur) {
				firstSeen[l.InputsHash] = l.At
			}
		}
	}
	reused := 0

	model := ""
	out := make([]store.Label, 0, len(rows))
	for i, r := range rows {
		truth, err := parseTruth(sp.Type, r.Truth)
		if err != nil {
			return nil, "", fmt.Errorf("row %d: %w", i, err)
		}
		l := store.Label{SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Truth: truth, Weight: r.Weight, Source: "file", At: time.Now()}
		given, err := rawFromRow(sp, r.Raw)
		if err != nil {
			return nil, "", fmt.Errorf("row %d: %w", i, err)
		}
		switch {
		case given != nil:
			l.Raw = given // 持ち込みの確率: モデルは呼ばない
		case func() bool { _, ok := stored[store.HashInputs(r.Inputs)]; return ok }():
			prior := stored[store.HashInputs(r.Inputs)]
			l.Raw, l.Model, l.TraceID, l.InputsHash = prior.Raw, prior.Model, prior.TraceID, prior.InputsHash
			l.Inputs = copyInputs(r.Inputs)
			if !prior.At.IsZero() {
				l.At = prior.At
			}
			if model == "" {
				model = prior.Model
			}
			reused++
		default:
			d, err := decide.Decide(ctx, sp.WithoutContext(), r.Inputs, sess, eps) // ファイルの正解は最小の質問の段のもの
			if err != nil {
				return nil, "", fmt.Errorf("row %d: %w", i, err)
			}
			if d.Band == decide.Abstain {
				return nil, "", fmt.Errorf("row %d: abstained: %s", i, d.Reason)
			}
			l.Raw, l.Model, l.TraceID, l.InputsHash = d.Raw, d.Model, d.TraceID, d.InputsHash
			l.Inputs = copyInputs(r.Inputs) // 正解と一緒に本文も残す(聞き直しの材料)
			if model == "" {
				model = d.Model
			} else if model != d.Model {
				return nil, "", fmt.Errorf("row %d: model changed mid-run (%s → %s)", i, model, d.Model)
			}
		}
		if l.Weight <= 0 {
			l.Weight = 1
		}
		if first, ok := firstSeen[l.InputsHash]; ok && l.InputsHash != "" {
			l.At = first
		}
		out = append(out, l)
	}
	if reused > 0 {
		fmt.Fprintf(stdout, "reused %d stored labels for file rows (model=%s); queried %d\n", reused, ef.model, len(out)-reused)
	}
	return out, model, nil
}

// alreadyStored は同じ (hash, model, inputs_hash) のラベルが store にあるか。
func storedInputs(st store.Store, sp *spec.Spec, model string) (map[string]bool, error) {
	existing, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(existing))
	for _, e := range existing {
		out[e.InputsHash] = true
	}
	return out, nil
}

// replayTruths は「正解の貯金」を今の (spec_hash, model) に持ち込む。
// store にある spec のラベル全部(古いハッシュ・別モデルのものを含む)から、入力ごとに最新の正解を取り、
// 本文が残っている記録のものだけを、今の文言で今のモデルに聞き直してラベルにする。
// seen に入っている入力(ファイル側で扱ったもの)は飛ばす。model が空なら、最初の答えのモデル名を返す。
// 聞き直せない正解(本文が無い)は数えて理由を出す。黙って 0 件にしない。
func replayTruths(sp *spec.Spec, st store.Store, ef endpointFlags, model string, seen map[string]bool) ([]store.Label, string, error) {
	_, all, err := st.Counts(sp.ID)
	if err != nil {
		return nil, model, err
	}
	// 入力ごとに最新の正解(同じ入力に複数付いていれば新しい方)
	type truth struct {
		truth  string
		weight float64
		at     time.Time
	}
	bank := map[string]truth{}
	var human []store.Label
	for _, l := range all {
		if store.HumanSource(l.Source) { // 機械の正解(上の段の答え)は聞き直さない: 別の段で聞き直しても同じモデルの答えで、人の正解に化けてしまう(監査 3 回目の 1)
			human = append(human, l)
		}
	}
	for _, l := range store.LatestPerInput(human) {
		if l.InputsHash == "" || seen[l.InputsHash] {
			continue
		}
		bank[l.InputsHash] = truth{l.Truth, l.Weight, l.At}
	}
	if len(bank) == 0 {
		return nil, model, nil
	}
	// 今の (hash, model) に既にあるものは聞き直さない。model が未定なら、最初の答えで確定してから見る。
	current := map[string]bool{}
	loadCurrent := func(m string) error {
		have, err := st.Labels(sp.ID, sp.Hash(), m)
		if err != nil {
			return err
		}
		for _, l := range have {
			current[l.InputsHash] = true
		}
		return nil
	}
	// model が未定でも --model の値(既定あり)で一度見ておく。実際のモデル名が違えば最初の答えで見直す。
	guess := model
	if guess == "" {
		guess = ef.model
	}
	if err := loadCurrent(guess); err != nil {
		return nil, model, err
	}
	texts, err := st.InputsByHash(sp.ID)
	if err != nil {
		return nil, model, err
	}
	eps := ef.endpoints()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var out []store.Label
	noText, noEndpoint, noContext, invalid := 0, 0, 0, 0
	rs := sp.WithoutContext() // この段(このハッシュ)だけで聞き直す。段を上がると別のハッシュの答えになってしまう
	hashes := make([]string, 0, len(bank))
	for h := range bank {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	for _, h := range hashes {
		if current[h] {
			continue
		}
		stored, ok := texts[h]
		if !ok {
			noText++
			continue
		}
		inputs := stored.Inputs
		if len(eps) == 0 {
			noEndpoint++
			continue
		}
		// tainted で記録された本文は、今のセッションが tainted でなくても hosted に送らない(監査 S1)
		sess := decide.Session{Tainted: ef.tainted || stored.Tainted}
		d, err := decide.Decide(ctx, rs, inputs, sess, eps)
		if err != nil {
			return nil, model, fmt.Errorf("replay %s: %w", h, err)
		}
		if d.Band == decide.Abstain {
			if strings.HasPrefix(d.Reason, "missing required input") {
				noContext++ // 文脈の段の聞き直し: この本文には足す文脈が記録されていない
			} else {
				noEndpoint++
			}
			continue
		}
		if model == "" {
			model = d.Model
			if err := loadCurrent(model); err != nil {
				return nil, model, err
			}
			if current[h] {
				continue
			}
		} else if d.Model != model {
			return nil, model, fmt.Errorf("replay: model changed mid-run (%s → %s)", model, d.Model)
		}
		rec := recordOf(sp, d, inputs)
		t := bank[h]
		l, err := store.LabelFor(&rec, t.truth, t.weight, "replay")
		if err != nil {
			invalid++ // 正解が今の spec に無い選択肢など。黙って捨てずに数える
			continue
		}
		out = append(out, l)
		current[h] = true
	}
	if len(out) > 0 {
		fmt.Fprintf(stdout, "replayed %d earlier truths against the current wording and model (hash=%s, model=%s); no human labels needed\n", len(out), sp.Hash(), model)
	}
	if noText > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) cannot be replayed: no text stored for them (recorded before text was kept)\n", noText)
	}
	if noEndpoint > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) not replayed: no endpoint to ask (pass --local or a key for --hosted)\n", noEndpoint)
	}
	if noContext > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) not replayed on this rung: their text has no context inputs (%s)\n", noContext, strings.Join(sp.State.Required, ", "))
	}
	if invalid > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) skipped: their answer no longer exists in the spec\n", invalid)
	}
	return out, model, nil
}

// calibrateNoul は Platt を当てて指標を出す(設計 §7)。正負が逆転していれば ok=false。
func calibrateNoul(sp *spec.Spec, labels []store.Label, bins int) (spec.Calibration, bool, error) {
	samples := make([]calib.Sample, len(labels))
	rawProbs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	for i, l := range labels {
		p := l.Raw["yes"]
		samples[i] = calib.Sample{Delta: calib.Logit(p), Truth: l.Truth == "yes", Weight: l.Weight}
		rawProbs[i], truths[i], weights[i] = p, l.Truth == "yes", l.Weight
	}
	sep := calib.CheckSeparation(samples)
	pl, err := calib.Fit(samples)
	if err != nil {
		return spec.Calibration{}, false, fmt.Errorf("calibrate: %w", err)
	}
	calProbs := make([]float64, len(samples))
	for i, s := range samples {
		calProbs[i] = pl.Apply(s.Delta)
	}
	fmt.Fprintf(stdout, "platt: bias=%.4f temperature=%.4f\n", pl.Bias, pl.Temperature)
	fmt.Fprintf(stdout, "brier: raw=%.4f calibrated=%.4f\n", calib.Brier(rawProbs, truths, weights), calib.Brier(calProbs, truths, weights))
	fmt.Fprintf(stdout, "ece:   raw=%.4f calibrated=%.4f (bins=%d)\n", calib.ECE(rawProbs, truths, weights, bins), calib.ECE(calProbs, truths, weights, bins), bins)
	fmt.Fprintf(stdout, "separation(Δ, weight>=1): pos_min=%.3f neg_max=%.3f overlap=%d inverted=%v (weak labels excluded: %d)\n", sep.PosMin, sep.NegMax, sep.Overlap, sep.Inverted, sep.Weak)
	if sep.Overlap > 0 {
		// どの例が重なっているかを出す。本文は出さない。inputs_hash でラベルを辿れる。
		fmt.Fprintln(stdout, "overlapping (truth raw weight inputs_hash):")
		for i, l := range labels {
			if l.Weight > 0 && l.Weight < 1 {
				continue
			}
			d := samples[i].Delta
			if (l.Truth == "yes" && d < sep.NegMax) || (l.Truth == "no" && d > sep.PosMin) {
				fmt.Fprintf(stdout, "  %-3s %.3f %.1f %s\n", l.Truth, l.Raw["yes"], l.Weight, l.InputsHash)
			}
		}
	}
	fmt.Fprintf(stdout, "boundaries after calibration: neg_max→%.3f (ask=%.2f) pos_min→%.3f (act=%.2f)\n",
		pl.Apply(sep.NegMax), sp.Thresholds.Ask, pl.Apply(sep.PosMin), sp.Thresholds.Act)
	printThresholdReport(sp.Thresholds, calProbs, truths, weights)
	printReliability("calibrated", calProbs, truths, weights, bins)
	if sep.Inverted {
		fmt.Fprintln(stdout, "WARNING: positives and negatives overlap on Δ; a monotone calibration cannot fix this. Change the question or the model.")
		return spec.Calibration{Method: "platt", Bias: pl.Bias, Temperature: pl.Temperature,
			Brier: calib.Brier(calProbs, truths, weights), ECE: calib.ECE(calProbs, truths, weights, bins)}, false, nil
	}

	// identity と platt を比べる。platt は in-sample で甘いので 5-fold CV の Brier と precision@act で判定。
	// 生の確率が既に較正されているモデル(Jev、Kev の noul 等)では platt は改善しないので identity を選ぶ。
	rawPrec := precisionAt(sp.Thresholds.Act, rawProbs, truths, weights)
	rawBrier := calib.Brier(rawProbs, truths, weights)
	cvPrec, cvBrier := crossValidatePlatt(sp, samples, 5)
	fmt.Fprintf(stdout, "platt 5-fold CV: precision@act=%.3f brier=%.4f (raw: precision@act=%.3f brier=%.4f)\n", cvPrec, cvBrier, rawPrec, rawBrier)
	identityOK, plattOK := rawPrec >= sp.Thresholds.Act, cvPrec >= sp.Thresholds.Act
	identity := spec.Calibration{Method: "identity", PrecisionAt: rawPrec, Brier: rawBrier, ECE: calib.ECE(rawProbs, truths, weights, bins)}
	// spec に書く Brier は CV の値(in-sample は楽観側で、drift の基準線として甘い。監査 C6)
	platt := spec.Calibration{Method: "platt", Bias: pl.Bias, Temperature: pl.Temperature, PrecisionAt: cvPrec,
		Brier: cvBrier, ECE: calib.ECE(calProbs, truths, weights, bins)}
	switch {
	case identityOK && plattOK && cvBrier < rawBrier:
		fmt.Fprintf(stdout, "choose platt: both satisfy act; CV brier %.4f < raw %.4f\n", cvBrier, rawBrier)
		return platt, true, nil
	case identityOK:
		fmt.Fprintf(stdout, "choose identity: raw precision at act %.3f >= act and platt does not improve brier (CV %.4f vs raw %.4f)\n", rawPrec, cvBrier, rawBrier)
		return identity, true, nil
	case plattOK:
		fmt.Fprintf(stdout, "choose platt: CV precision at act %.3f >= act (raw was %.3f)\n", cvPrec, rawPrec)
		return platt, true, nil
	default:
		fmt.Fprintf(stdout, "WARNING: neither raw (%.3f) nor platt-calibrated (CV %.3f) precision at act reaches thresholds.act (%.2f): act must not be allowed for this model.\n", rawPrec, cvPrec, sp.Thresholds.Act)
		return platt, false, nil
	}
}

// crossValidatePlatt は k-fold で Platt を当て、外した fold での precision@act と Brier を合算する。
func crossValidatePlatt(sp *spec.Spec, samples []calib.Sample, k int) (precision, brier float64) {
	if k < 2 || len(samples) < k {
		return 0, 1
	}
	var probs []float64
	var truths []bool
	var weights []float64
	for fold := 0; fold < k; fold++ {
		var train, test []calib.Sample
		for i, s := range samples {
			if i%k == fold {
				test = append(test, s)
			} else {
				train = append(train, s)
			}
		}
		pl, err := calib.Fit(train)
		if err != nil {
			continue
		}
		for _, s := range test {
			probs = append(probs, pl.Apply(s.Delta))
			truths = append(truths, s.Truth)
			weights = append(weights, s.Weight)
		}
	}
	if len(probs) == 0 {
		return 0, 1
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), calib.Brier(probs, truths, weights)
}

// verifyTop は choice / score の検証と較正(設計 §7.1)。
//  1. 生の分布で、最上位の確率に対する正答率と act 閾値での precision を測る
//  2. precision が act 以上なら identity(= 生のまま act を許す印)で ok
//  3. 足りなければ vector scaling を当てて測り直す。較正後に act 以上なら vector で ok、
//     それでも足りなければ vector を返すが ok=false(書き戻しは --force が要る)
func verifyTop(sp *spec.Spec, labels []store.Label, bins int) (spec.Calibration, bool, error) {
	// 報告は全ラベルで、当てる(FitVector / CV)のは正解の選択肢が分かっているラベルだけ。
	// 部分ラベル("!edit" = edit ではなかった)は precision の評価には効くが、分布の学習には使えない。
	ms := make([]calib.MultiSample, len(labels))
	var fit []calib.MultiSample
	partial := 0
	for i, l := range labels {
		ms[i] = calib.MultiSample{Raw: l.Raw, Truth: l.Truth, Weight: l.Weight}
		if store.Partial(l.Truth) {
			partial++
		} else {
			fit = append(fit, ms[i])
		}
	}
	if partial > 0 {
		fmt.Fprintf(stdout, "labels: %d full, %d partial (\"!option\"; used for precision, not for fitting)\n", len(fit), partial)
	}
	rawRep := topReport(sp, ms, nil)
	fmt.Fprintf(stdout, "raw:        top-1 accuracy=%.3f brier(top)=%.4f ece(top)=%.4f\n", rawRep.acc, rawRep.brier, rawRep.ece)
	printThresholdReport(sp.Thresholds, rawRep.pTop, rawRep.correct, rawRep.weights)
	printReliability("top, raw", rawRep.pTop, rawRep.correct, rawRep.weights, bins)
	printConfusion(rawRep.confusion)
	identityOK := rawRep.precAct >= sp.Thresholds.Act
	identity := spec.Calibration{Method: "identity", PrecisionAt: rawRep.precAct, Brier: rawRep.brier, ECE: rawRep.ece}

	// vector は in-sample だと甘く見えるので、採否は k-fold CV の指標で決める。
	v, err := calib.FitVector(fit)
	if err != nil {
		if identityOK {
			fmt.Fprintf(stdout, "identity: raw precision at act %.3f >= act (vector scaling could not be fitted: %v)\n", rawRep.precAct, err)
			return identity, true, nil
		}
		return spec.Calibration{}, false, fmt.Errorf("vector scaling: %w", err)
	}
	calRep := topReport(sp, ms, &v)
	cvPrec, cvRec := crossValidateVector(sp, ms, 5) // 採点は全ラベル(部分ラベル込み)。当てるのは完全ラベルだけ(関数の中で分ける)
	fmt.Fprintf(stdout, "vector:     temperature=%.4f biases=%s\n", v.Temperature, fmtDist(v.Biases))
	fmt.Fprintf(stdout, "calibrated: top-1 accuracy=%.3f brier(top)=%.4f ece(top)=%.4f (in-sample)\n", calRep.acc, calRep.brier, calRep.ece)
	printThresholdReport(sp.Thresholds, calRep.pTop, calRep.correct, calRep.weights)
	printReliability("top, calibrated", calRep.pTop, calRep.correct, calRep.weights, bins)
	printConfusion(calRep.confusion)
	fmt.Fprintf(stdout, "vector 5-fold CV at act: precision=%.3f recall=%.3f\n", cvPrec, cvRec)
	vectorOK := cvPrec >= sp.Thresholds.Act
	vector := spec.Calibration{
		Method: "vector", Temperature: v.Temperature, Biases: v.Biases,
		PrecisionAt: cvPrec, Brier: calRep.brier, ECE: calRep.ece,
	}

	// 選ぶ: precision@act を満たす候補のうち recall@act が高い方。同点なら単純な identity。
	switch {
	case identityOK && vectorOK && cvRec > rawRep.recAct:
		fmt.Fprintf(stdout, "choose vector: both satisfy act; vector recall %.3f > identity recall %.3f\n", cvRec, rawRep.recAct)
		return vector, true, nil
	case identityOK:
		fmt.Fprintf(stdout, "choose identity: raw precision at act %.3f >= act (recall %.3f)\n", rawRep.precAct, rawRep.recAct)
		return identity, true, nil
	case vectorOK:
		fmt.Fprintf(stdout, "choose vector: CV precision at act %.3f >= act (identity was %.3f)\n", cvPrec, rawRep.precAct)
		return vector, true, nil
	default:
		fmt.Fprintf(stdout, "WARNING: neither raw (%.3f) nor vector-calibrated (CV %.3f) precision at act reaches thresholds.act (%.2f): act must not be allowed for this model.\n", rawRep.precAct, cvPrec, sp.Thresholds.Act)
		return vector, false, nil
	}
}

// crossValidateVector は k-fold で vector を当て、外した fold での act 閾値の precision / recall を合算する。
func crossValidateVector(sp *spec.Spec, ms []calib.MultiSample, k int) (precision, recall float64) {
	// 採点は identity / check と同じ集合(部分ラベル "!x" を含む全ラベル)と同じ事象(judge)で行う。
	// 当てる(FitVector)のは正解の選択肢が分かっている完全ラベルだけ。監査 C1 / C4。
	if k < 2 || len(ms) < k {
		return 0, 0
	}
	var tp, fp, fn float64
	for fold := 0; fold < k; fold++ {
		var train, test []calib.MultiSample
		for i, s := range ms {
			switch {
			case i%k == fold:
				test = append(test, s)
			case !store.Partial(s.Truth):
				train = append(train, s)
			}
		}
		v, err := calib.FitVector(train)
		if err != nil {
			continue
		}
		for _, s := range test {
			_, p, correct := judge(sp, v.Apply(s.Raw), s.Truth)
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			switch {
			case p >= sp.Thresholds.Act && correct:
				tp += w
			case p >= sp.Thresholds.Act && !correct:
				fp += w
			case correct:
				fn += w
			}
		}
	}
	if tp+fp > 0 {
		precision = tp / (tp + fp)
	}
	if tp+fn > 0 {
		recall = tp / (tp + fn)
	}
	return precision, recall
}

// report は最上位の確率に対する指標の束。
type report struct {
	pTop      []float64
	correct   []bool
	weights   []float64
	acc       float64
	brier     float64
	ece       float64
	precAct   float64
	recAct    float64
	confusion map[string]map[string]float64
}

// topReport は(較正があれば当ててから)最上位の確率・正誤・混同表を集める。
func topReport(sp *spec.Spec, ms []calib.MultiSample, v *calib.Vector) report {
	r := report{
		pTop: make([]float64, len(ms)), correct: make([]bool, len(ms)), weights: make([]float64, len(ms)),
		confusion: map[string]map[string]float64{},
	}
	var hit, total float64
	for i, s := range ms {
		dist := s.Raw
		if v != nil {
			dist = v.Apply(s.Raw)
		}
		top, p, correct := judge(sp, dist, s.Truth)
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		r.pTop[i], r.correct[i], r.weights[i] = p, correct, w
		if r.confusion[s.Truth] == nil {
			r.confusion[s.Truth] = map[string]float64{}
		}
		r.confusion[s.Truth][top] += w
		total += w
		if r.correct[i] {
			hit += w
		}
	}
	if total > 0 {
		r.acc = hit / total
	}
	r.brier = calib.Brier(r.pTop, r.correct, r.weights)
	r.ece = calib.ECE(r.pTop, r.correct, r.weights, 10)
	r.precAct = precisionAt(sp.Thresholds.Act, r.pTop, r.correct, r.weights)
	r.recAct = recallAt(sp.Thresholds.Act, r.pTop, r.correct, r.weights)
	return r
}

func recallAt(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tp, fn float64
	for i, p := range probs {
		if !truths[i] {
			continue
		}
		if p >= th {
			tp += weights[i]
		} else {
			fn += weights[i]
		}
	}
	if tp+fn == 0 {
		return 0
	}
	return tp / (tp + fn)
}

func precisionAt(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tp, fp float64
	for i, p := range probs {
		if p < th {
			continue
		}
		if truths[i] {
			tp += weights[i]
		} else {
			fp += weights[i]
		}
	}
	if tp+fp == 0 {
		return 0
	}
	return tp / (tp + fp)
}

func printConfusion(confusion map[string]map[string]float64) {
	fmt.Fprintln(stdout, "confusion (truth → predicted):")
	truths := make([]string, 0, len(confusion))
	for t := range confusion {
		truths = append(truths, t)
	}
	sort.Strings(truths)
	for _, t := range truths {
		fmt.Fprintf(stdout, "  %s: %s\n", t, fmtDist(confusion[t]))
	}
}

// printThresholdReport は act / ask の閾値での precision / recall を出し、act での precision を返す。
func printThresholdReport(t spec.Thresholds, probs []float64, truths []bool, weights []float64) float64 {
	report := func(name string, th float64) float64 {
		var tp, fp, fn float64
		for i, p := range probs {
			w := weights[i]
			switch {
			case p >= th && truths[i]:
				tp += w
			case p >= th && !truths[i]:
				fp += w
			case p < th && truths[i]:
				fn += w
			}
		}
		prec, rec := 0.0, 0.0
		if tp+fp > 0 {
			prec = tp / (tp + fp)
		}
		if tp+fn > 0 {
			rec = tp / (tp + fn)
		}
		fmt.Fprintf(stdout, "at %s>=%.2f: precision=%.3f recall=%.3f (tp=%.0f fp=%.0f fn=%.0f)\n", name, th, prec, rec, tp, fp, fn)
		return prec
	}
	precAct := report("act", t.Act)
	report("ask", t.Ask)
	return precAct
}

func printReliability(name string, probs []float64, truths []bool, weights []float64, bins int) {
	fmt.Fprintf(stdout, "reliability (%s):\n", name)
	for _, b := range calib.Reliability(probs, truths, weights, bins) {
		if b.N > 0 {
			fmt.Fprintf(stdout, "  [%.1f,%.1f) n=%4.0f conf=%.3f acc=%.3f\n", b.Lo, b.Hi, b.N, b.Confidence, b.Accuracy)
		}
	}
}

func argmax(m map[string]float64) (string, float64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bp := "", -1.0
	for _, k := range keys {
		if m[k] > bp {
			best, bp = k, m[k]
		}
	}
	return best, bp
}

// ---------------------------------------------------------------- hash / helpers

func cmdHash(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shikii hash <spec.yaml>\n  prints the spec's hash (question, options, template; not display/variants/calibration)")
	}
	sp, err := spec.Load(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, sp.Hash())
	return nil
}

// ---------------------------------------------------------------- check(契約の検査、設計 §8)

// cmdCheck は spec の較正ブロックが今も有効かを調べる。CI で回す前提で、違反があれば exit 1。
//   - 較正の spec_hash が現在のハッシュと一致する(文言を変えたのに定数を据え置いていない)
//   - 較正は手で書かれていない(method が既知で、鍵が揃っている)
//   - ラベル(store + eval.labeled の raw 行)で、較正後の act 閾値での precision が act 以上
//   - noul は正負が逆転していない
func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	storeDir := bindStore(fs)
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("usage: shikii check <spec.yaml>...")
	}
	var violations int
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			fmt.Fprintf(stdout, "FAIL %s: %v\n", path, err)
			violations++
			continue
		}
		for _, v := range checkSpec(sp, store.Store{Dir: *storeDir}) {
			fmt.Fprintf(stdout, "FAIL %s: %s\n", sp.ID, v)
			violations++
		}
		fmt.Fprintf(stdout, "ok   %s hash=%s calibrations=%d\n", sp.ID, sp.Hash(), len(sp.Calibration))
	}
	if violations > 0 {
		return fmt.Errorf("check: %d violation(s)", violations)
	}
	return nil
}

// checkSpec は 1 spec の違反を列挙する。
func checkSpec(sp *spec.Spec, st store.Store) []string {
	var out []string
	h := sp.Hash()
	base := sp
	for _, c := range sp.Calibration {
		key := fmt.Sprintf("calibration(model=%s)", c.Model)
		sp := base // この線の段の spec(文脈の段の線は、文脈の段のハッシュと正解で検証する)
		if c.SpecHash == base.ContextHash() {
			sp = base.ContextSpec()
			key = fmt.Sprintf("calibration(model=%s, context rung)", c.Model)
		} else if c.SpecHash != h {
			out = append(out, fmt.Sprintf("%s: stale spec_hash %s (current %s): the question/template/options changed after calibration; re-run calibrate", key, c.SpecHash, h))
			continue
		}
		switch c.Method {
		case "identity":
		case "platt":
			if sp.Type != spec.Noul || c.Temperature <= 0 {
				out = append(out, key+": platt requires noul and temperature > 0")
				continue
			}
		case "vector":
			if sp.Type == spec.Noul || c.Temperature <= 0 {
				out = append(out, key+": vector requires choice/score and temperature > 0")
				continue
			}
		default:
			out = append(out, fmt.Sprintf("%s: unknown method %q (calibration blocks are generated, not hand-written)", key, c.Method))
			continue
		}
		// 手書きのブロックを弾く: calibrate が書かないもの(min_n 未満の n、日付無し)は通さない(監査 S5)
		if c.N < sp.Eval.MinN {
			out = append(out, fmt.Sprintf("%s: n=%d < eval.min_n=%d (calibrate would not have written this block; --force or hand-written)", key, c.N, sp.Eval.MinN))
			continue
		}
		if strings.TrimSpace(c.FittedAt) == "" {
			out = append(out, key+": fitted_at is empty (calibration blocks are generated, not hand-written)")
			continue
		}
		labels, err := collectLabels(sp, st, c.Model, c.FittedOn)
		if err != nil {
			out = append(out, key+": "+err.Error())
			continue
		}
		if len(labels) == 0 {
			out = append(out, key+": no labels to verify against (store or eval.labeled)")
			continue
		}
		prec, inverted, _ := evaluateCalibration(sp, c, labels)
		if prec < sp.Thresholds.Act {
			out = append(out, fmt.Sprintf("%s: precision at act is %.3f < thresholds.act %.2f on %d labels", key, prec, sp.Thresholds.Act, len(labels)))
		}
		// 人が決めた正解だけでも測る。機械の正解(context: 上の段の答え)は同じモデルと同じ方向に間違いうるので、act の精度の保証は人の分で取る。
		// 抜き取り(act の 20 件に 1 件が答え合わせの列に出る)が母数を作る。min_n に届くまでは数だけ言い、届いたら act を割れば落とす。
		var human []store.Label
		for _, l := range labels {
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		hPrec, _, hAct := evaluateCalibration(sp, c, human)
		fmt.Fprintf(stdout, "note %s: %s: human-checked act decisions: %d (precision %.3f)\n", sp.ID, key, hAct, hPrec)
		if hAct >= sp.Eval.MinN && hPrec < sp.Thresholds.Act {
			out = append(out, fmt.Sprintf("%s: precision at act on human-checked decisions is %.3f < thresholds.act %.2f on %d act decisions (machine truths made it look like %.3f)", key, hPrec, sp.Thresholds.Act, hAct, prec))
		}
		if inverted {
			out = append(out, key+": positives and negatives overlap on Δ; a monotone calibration cannot separate them")
		}
	}
	return out
}

// collectLabels は store のラベルと、eval.labeled の raw 付き行(noul)を合わせる。モデルは呼ばない。
// raw 付き行はどのモデルの確率か分からないので、その較正が同じファイルで当てられた(fitted_on が一致する)時だけ使う。
func collectLabels(sp *spec.Spec, st store.Store, model, fittedOn string) ([]store.Label, error) {
	all, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return nil, err
	}
	labels := store.LatestPerInput(all) // calibrate と同じ数え方(入力ごとに最新の正解 1 つ)
	if sp.Eval.Labeled == "" || sp.Eval.Labeled != fittedOn {
		return labels, nil
	}
	b, err := os.ReadFile(sp.Eval.Labeled)
	if err != nil {
		if os.IsNotExist(err) {
			return labels, nil
		}
		return nil, err
	}
	var rows []labeled
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w", sp.Eval.Labeled, err)
	}
	for _, r := range rows {
		given, err := rawFromRow(sp, r.Raw)
		if err != nil {
			return nil, err
		}
		if given == nil {
			continue // モデルを呼ばないと分からない行は check では使わない
		}
		truth, err := parseTruth(sp.Type, r.Truth)
		if err != nil {
			return nil, err
		}
		w := r.Weight
		if w <= 0 {
			w = 1
		}
		labels = append(labels, store.Label{SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: model,
			Raw: given, Truth: truth, Weight: w, Source: "file"})
	}
	return labels, nil
}

// evaluateCalibration は較正を当てた上で act 閾値での precision を出す。noul は逆転も見る。
func evaluateCalibration(sp *spec.Spec, c spec.Calibration, labels []store.Label) (precision float64, inverted bool, nAct int) {
	probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	switch sp.Type {
	case spec.Noul:
		pl := calib.Platt{Bias: c.Bias, Temperature: c.Temperature}
		samples := make([]calib.Sample, len(labels))
		for i, l := range labels {
			d := calib.Logit(l.Raw["yes"])
			samples[i] = calib.Sample{Delta: d, Truth: l.Truth == "yes", Weight: l.Weight}
			p := l.Raw["yes"]
			if c.Method == "platt" {
				p = pl.Apply(d)
			}
			probs[i], truths[i], weights[i] = p, l.Truth == "yes", l.Weight
		}
		inverted = calib.CheckSeparation(samples).Inverted
	default:
		for i, l := range labels {
			dist := l.Raw
			if c.Method == "vector" {
				dist = calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(l.Raw)
			}
			_, p, correct := judge(sp, dist, l.Truth)
			probs[i], truths[i], weights[i] = p, correct, l.Weight
		}
	}
	for _, p := range probs {
		if p >= sp.Thresholds.Act {
			nAct++ // 線を掛けた後に act 帯に入る判定の数(人の正解だけで精度を測る時の母数)
		}
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), inverted, nAct
}

// judge は 1 ラベルの「帯に使う確率」と「その答えが正しかったか」。
//   - choice: 最上位の確率、正解は top == truth("!x" は x 以外)
//   - score で thresholds.level あり: P(level ≥ k)、正解は truth のレベル ≥ k(事象が起きたか)
//   - score で level なし: choice と同じ
func judge(sp *spec.Spec, dist map[string]float64, truth string) (top string, p float64, correct bool) {
	top, p = argmax(dist)
	if sp.Type == spec.Score && sp.Thresholds.Level > 0 && !store.Partial(truth) {
		p = decide.MassAtOrAbove(dist, sp.Thresholds.Level)
		lvl, err := strconv.Atoi(truth)
		return top, p, err == nil && lvl >= sp.Thresholds.Level
	}
	return top, p, store.Correct(top, truth)
}

// ---------------------------------------------------------------- drift(劣化の監視、設計 §9)

// cmdDrift は直近 N 件のラベルで、較正時の Brier と act での precision がどれだけ悪化したかを見る。
// 較正時の Brier から +0.05 以上悪化、または precision が act を割ったら警告して exit 1。
func cmdDrift(args []string) error {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	storeDir := bindStore(fs)
	model := fs.String("model", "", "較正の鍵になっているモデル名(必須)")
	window := fs.Int("window", 50, "直近何件を見るか")
	tol := fs.Float64("tolerance", 0.05, "Brier の悪化をどこまで許すか")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	if *model == "" {
		return fmt.Errorf("drift: --model is required")
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	c := sp.CalibrationFor(*model)
	if c == nil {
		return fmt.Errorf("drift: no current calibration for model %s (hash %s); nothing to drift from", *model, sp.Hash())
	}
	if (c.Method == "platt" || c.Method == "vector") && !(c.Temperature > 0) {
		return fmt.Errorf("drift: calibration for %s has temperature %g (not > 0); the block is broken or hand-written, run check", *model, c.Temperature)
	}
	all, err := (store.Store{Dir: *storeDir}).Labels(sp.ID, sp.Hash(), *model)
	if err != nil {
		return err
	}
	labels := store.LatestPerInput(all)
	if len(labels) == 0 {
		return fmt.Errorf("drift: no labels in store for (hash=%s, model=%s)", sp.Hash(), *model)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].At.Before(labels[j].At) })
	if len(labels) > *window {
		labels = labels[len(labels)-*window:]
	}
	prec, brier, agree, askN := driftMetrics(sp, *c, labels)
	fmt.Fprintf(stdout, "spec=%s model=%s window=%d (calibrated %s on n=%d)\n", sp.ID, *model, len(labels), c.FittedAt, c.N)
	fmt.Fprintf(stdout, "brier:            at calibration=%.4f recent=%.4f\n", c.Brier, brier)
	fmt.Fprintf(stdout, "precision at act: recent=%.3f (act=%.2f)\n", prec, sp.Thresholds.Act)
	if askN > 0 {
		fmt.Fprintf(stdout, "ask agreement:    %.3f (share of ask-sourced labels where the user picked the model's top)\n", agree)
	} else {
		fmt.Fprintln(stdout, "ask agreement:    n/a (no ask-sourced labels in this window)")
	}
	var warn []string
	if brier > c.Brier+*tol {
		warn = append(warn, fmt.Sprintf("brier worsened by %.4f (> %.2f)", brier-c.Brier, *tol))
	}
	if prec < sp.Thresholds.Act {
		warn = append(warn, fmt.Sprintf("precision at act %.3f < %.2f", prec, sp.Thresholds.Act))
	}
	if len(warn) > 0 {
		fmt.Fprintln(stdout, "WARNING: "+strings.Join(warn, "; ")+". Re-run calibrate, or drop act to ask for this model.")
		return fmt.Errorf("drift: %d warning(s)", len(warn))
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

// driftMetrics は較正を当てた直近ラベルの precision@act・Brier・ask での一致率。
func driftMetrics(sp *spec.Spec, c spec.Calibration, labels []store.Label) (precision, brier, askAgreement, askN float64) {
	probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	var askHit float64
	for i, l := range labels {
		var p float64
		var correct bool
		if sp.Type == spec.Noul {
			p = l.Raw["yes"]
			if c.Method == "platt" {
				p = calib.Platt{Bias: c.Bias, Temperature: c.Temperature}.Apply(calib.Logit(p))
			}
			correct = l.Truth == "yes"
		} else {
			dist := l.Raw
			if c.Method == "vector" {
				dist = calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(l.Raw)
			}
			_, p, correct = judge(sp, dist, l.Truth)
		}
		probs[i], truths[i], weights[i] = p, correct, l.Weight
		if l.Source == "ask" {
			askN += l.Weight
			if correct {
				askHit += l.Weight
			}
		}
	}
	if askN > 0 {
		askAgreement = askHit / askN
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), calib.Brier(probs, truths, weights), askAgreement, askN
}

// ---------------------------------------------------------------- bias(順序バイアスの測定)

// cmdBias は choice の選択肢の並びを入れ替えて同じ state を聞き直し、答えが並びでどれだけ動くかを測る。
//   - flip rate: 並びを変えたら最上位が変わった割合
//   - mean |Δp|: 選択肢ごとの確率の動き
//   - position mass: 並びの位置ごとの平均確率(位置に偏りがあれば「先頭に寄る」等が見える)
//
// 記録も較正もしない。生成しないモデルなので順列を回すのは安い。
func cmdBias(args []string) error {
	fs := flag.NewFlagSet("bias", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	labeledPath := fs.String("labeled", "", "state を取る元(省略時は spec の eval.labeled。無ければ store の正解の本文)")
	storeDir := bindStore(fs)
	rows := fs.Int("rows", 20, "使う行数の上限")
	perms := fs.Int("permutations", 5, "行ごとに回す順列の数")
	seed := fs.Int64("seed", 1, "順列の乱数の種")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	if sp.Type != spec.Choice {
		return fmt.Errorf("bias: only choice has an option order (spec type is %s)", sp.Type)
	}
	if *labeledPath == "" {
		*labeledPath = sp.Eval.Labeled
	}
	// state は calibrate / tune と同じ貯金から: --labeled のファイルと store の正解の本文(入力ごとに最新)
	bank, _, err := truthBank(sp, store.Store{Dir: *storeDir}, *labeledPath)
	if err != nil {
		return err
	}
	if len(bank) == 0 {
		return fmt.Errorf("bias: no states to ask: no truths in %s and no --labeled file (shikii label / shikii import first)", *storeDir)
	}
	lab := make([]labeled, 0, len(bank))
	for _, row := range bank {
		truth, _ := json.Marshal(row.truth)
		lab = append(lab, labeled{Inputs: row.inputs, Truth: truth, Weight: row.weight})
	}
	if len(lab) > *rows {
		lab = lab[:*rows]
	}
	ep := decide.PickEndpoint(sp, decide.Session{Tainted: ef.tainted}, ef.endpoints())
	if ep == nil {
		return fmt.Errorf("bias: no permitted endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	rep, model, err := measureBias(ctx, sp, lab, ep.Client, *perms, *seed)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s rows=%d permutations=%d queries=%d\n", sp.ID, sp.Hash(), model, rep.rows, *perms, rep.queries)
	fmt.Fprintf(stdout, "flip rate (top changes with order): %.3f (%d/%d)\n", rep.flipRate, rep.flips, rep.compared)
	fmt.Fprintln(stdout, "mean |Δp| by option:")
	for _, o := range sp.Options {
		fmt.Fprintf(stdout, "  %-16s %.3f\n", o.ID, rep.meanAbsDelta[o.ID])
	}
	fmt.Fprintln(stdout, "mean Δp by position vs spec order (1 = listed first; + means that slot gets extra probability):")
	for i, m := range rep.positionMass {
		fmt.Fprintf(stdout, "  pos %d: %+.3f\n", i+1, m)
	}
	fmt.Fprintf(stdout, "position shift spread (max−min): %.3f\n", rep.positionSpread)
	if rep.flipRate > 0.10 || rep.positionSpread > 0.10 {
		fmt.Fprintln(stdout, "WARNING: the answer depends on option order. Shuffle options at query time or average over permutations before trusting act.")
		return fmt.Errorf("bias: order sensitivity above tolerance")
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

type biasReport struct {
	rows, queries, flips, compared int
	flipRate                       float64
	meanAbsDelta                   map[string]float64
	positionMass                   []float64
	positionSpread                 float64
}

// measureBias は行ごとに spec の並び + N 個の順列で聞き、並びによる揺れを集計する。
func measureBias(ctx context.Context, sp *spec.Spec, lab []labeled, c *systemone.Client, perms int, seed int64) (biasReport, string, error) {
	K := len(sp.Options)
	rep := biasReport{meanAbsDelta: map[string]float64{}, positionMass: make([]float64, K)}
	posN := make([]float64, K)
	sumAbs := map[string]float64{}
	var nAbs float64
	rng := rand.New(rand.NewSource(seed))
	model := ""
	canonical := make([]int, K)
	for i := range canonical {
		canonical[i] = i
	}
	// 位置の偏りは「その位置に置かれた選択肢の確率が、spec の並びで聞いた時からどれだけ動いたか」で測る。
	// 位置ごとの平均確率そのものだと、どの選択肢が偶然その位置に来たかに引きずられる。
	addPos := func(order []int, dist, base map[string]float64) {
		for pos, idx := range order {
			id := sp.Options[idx].ID
			rep.positionMass[pos] += dist[id] - base[id]
			posN[pos]++
		}
	}
	for _, r := range lab {
		state, missing := sp.Render(r.Inputs)
		if len(missing) > 0 {
			continue
		}
		base, m, err := decide.AskOrdered(ctx, sp, state, canonical, c)
		if err != nil {
			return rep, "", err
		}
		model = m
		rep.rows++
		rep.queries++
		baseTop, _ := argmax(base)
		for p := 0; p < perms; p++ {
			order := rng.Perm(K)
			dist, _, err := decide.AskOrdered(ctx, sp, state, order, c)
			if err != nil {
				return rep, "", err
			}
			rep.queries++
			rep.compared++
			addPos(order, dist, base)
			if top, _ := argmax(dist); top != baseTop {
				rep.flips++
			}
			for _, o := range sp.Options {
				sumAbs[o.ID] += math.Abs(dist[o.ID] - base[o.ID])
			}
			nAbs++
		}
	}
	if rep.compared > 0 {
		rep.flipRate = float64(rep.flips) / float64(rep.compared)
	}
	for _, o := range sp.Options {
		if nAbs > 0 {
			rep.meanAbsDelta[o.ID] = sumAbs[o.ID] / nAbs
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range rep.positionMass {
		if posN[i] > 0 {
			rep.positionMass[i] /= posN[i]
		}
		lo, hi = math.Min(lo, rep.positionMass[i]), math.Max(hi, rep.positionMass[i])
	}
	if K > 0 {
		rep.positionSpread = hi - lo
	}
	return rep, model, nil
}

// ---------------------------------------------------------------- status(店の棚卸し)

// cmdStatus は store と spec を突き合わせて、判定ごとの記録・ラベルの数、ラベルの出どころと中身の偏り、
// 較正の有無と鍵の一致を 1 画面に出す。運用で最初に見る画面。モデルも外部も呼ばない。
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	storeDir := bindStore(fs)
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	specs := map[string]*spec.Spec{}
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			return err
		}
		specs[sp.ID] = sp
	}
	ids, err := st.SpecIDs()
	if err != nil {
		return err
	}
	for id := range specs {
		if !containsString(ids, id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "no records, no labels, no specs")
		return nil
	}
	for _, id := range ids {
		recs, labs, err := st.Counts(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: records=%d labels=%d\n", id, recs, len(labs))
		byKey := map[string]int{}
		bySource := map[string]int{}
		byTruth := map[string]int{}
		var last time.Time
		for _, l := range labs {
			byKey[l.Model+" @ "+l.SpecHash]++
			bySource[l.Source]++
			byTruth[l.Truth]++
			if l.At.After(last) {
				last = l.At
			}
		}
		if len(labs) > 0 {
			fmt.Fprintf(stdout, "  last label: %s\n", last.Format("2006-01-02 15:04"))
			fmt.Fprintf(stdout, "  by source: %s\n", fmtCounts(bySource))
			fmt.Fprintf(stdout, "  by truth:  %s\n", fmtCounts(byTruth))
		}
		sp := specs[id]
		if f, err := st.Funnel(id, sp != nil && decide.DiscardUnsure(sp)); err == nil && f.Records > 0 {
			// 段ごとの数。進み具合は 1000 判定あたりの人の判断(ask のまま人に回った数)
			fmt.Fprintf(stdout, "  rungs: settled by the minimal question %d, by added context %d, left to a person %d (%.0f per 1000 decisions)\n", f.Minimal, f.Context, f.Human, f.HumanPerThousand())
		}
		if len(labs) > 0 {
			audited := map[string]bool{} // 判定(trace)で数える。文脈を通った判定の答えは 2 段に付くが、人の確認は 1 回
			for _, l := range labs {
				if l.Source == "audit" {
					audited[l.TraceID] = true
				}
			}
			// receipt の「human-checked act decisions」とは別の数: こちらは抜き取りに人が答えた判定の数。receipt は人の正解のうち、この線なら act に入るもの全部
			fmt.Fprintf(stdout, "  audit: 1 in 20 act decisions is shown to a person; audit answers so far: %d\n", len(audited))
		}
		for key, n := range byKey {
			note := ""
			if sp != nil {
				parts := strings.SplitN(key, " @ ", 2)
				rs := sp // このハッシュの段
				if parts[1] == sp.ContextHash() {
					rs = sp.ContextSpec()
					note = "  (context rung)"
				}
				switch {
				case !sp.LiveHash(parts[1]):
					note = "  (stale hash: spec wording changed since; not used)"
				case rs.CalibrationFor(parts[0]) != nil:
					note += "  calibrated: " + rs.CalibrationFor(parts[0]).Method
				case n >= sp.Eval.MinN:
					note += fmt.Sprintf("  ready to calibrate (>= min_n %d)", sp.Eval.MinN)
				default:
					note += fmt.Sprintf("  collecting (%d/%d)", n, sp.Eval.MinN)
				}
			}
			fmt.Fprintf(stdout, "  %-50s %4d%s\n", key, n, note)
		}
		if sp == nil {
			fmt.Fprintln(stdout, "  (spec not given; pass its path to see calibration state)")
		}
	}
	return nil
}

// cmdGC は「古くてラベルの無い記録」を店じまいする。ラベル付きの記録とラベルは年齢に関係なく残す(較正の材料)。
// 既定は表示だけ。--apply を付けた時だけ書き換える。
func cmdGC(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	storeDir := bindStore(fs)
	olderThan := fs.String("older-than", "30d", "この年齢より古く、ラベルの無い記録を消す(例 30d, 12h, 1w。0 なら年齢を問わない)")
	apply := fs.Bool("apply", false, "実際に消す。無ければ数えるだけ")
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	age, err := parseAge(*olderThan)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	var ids []string
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			return err
		}
		ids = append(ids, sp.ID)
	}
	if len(ids) == 0 {
		if ids, err = st.SpecIDs(); err != nil {
			return err
		}
	}
	sort.Strings(ids)
	cutoff := time.Now().Add(-age)
	total := 0
	for _, id := range ids {
		res, err := st.GC(id, cutoff, *apply)
		if err != nil {
			return fmt.Errorf("gc %s: %w", id, err)
		}
		verb := "would remove"
		if *apply {
			verb = "removed"
		}
		fmt.Fprintf(stdout, "%s: %s %d unlabeled record(s) older than %s; keep %d labeled, %d recent, %d unparsable\n",
			id, verb, res.Removed, *olderThan, res.KeptLabeled, res.KeptRecent, res.KeptUnparsed)
		total += res.Removed
	}
	if !*apply && total > 0 {
		fmt.Fprintln(stdout, "dry run: nothing was changed. Re-run with --apply to remove them.")
	}
	return nil
}

// parseAge は "30d" "1w" "12h" のような年齢。time.ParseDuration は d と w を知らないので先に換える。負は拒む。
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("--older-than needs a value such as 30d, 12h or 0")
	}
	if s == "0" {
		return 0, nil
	}
	var d time.Duration
	var err error
	switch {
	case strings.HasSuffix(s, "d"):
		d, err = scaledDuration(strings.TrimSuffix(s, "d"), 24*time.Hour)
	case strings.HasSuffix(s, "w"):
		d, err = scaledDuration(strings.TrimSuffix(s, "w"), 7*24*time.Hour)
	default:
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("--older-than %q: use a duration such as 30d, 1w, 12h or 0", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("--older-than %q: must not be negative", s)
	}
	return d, nil
}

func scaledDuration(num string, unit time.Duration) (time.Duration, error) {
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(n * float64(unit)), nil
}

func fmtCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// parseSpecPath は「<spec.yaml> フラグ…」と「フラグ… <spec.yaml>」の両方を受ける。
func parseSpecPath(fs *flag.FlagSet, args []string) (string, error) {
	pos, err := parseMixed(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", fmt.Errorf("spec path is required")
	}
	return pos[0], nil
}

// parseMixed はフラグを位置引数の前にも後ろにも書けるようにする(`shikii status spec.yaml --store DIR` と
// `shikii status --store DIR spec.yaml` が同じ)。Go の flag は最初の位置引数で止まるので、残りを続けて読む。
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func fmtDist(m map[string]float64) string {
	b, _ := json.Marshal(m)
	return string(b)
}
