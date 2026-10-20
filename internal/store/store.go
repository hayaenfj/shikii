// Package store は判定の記録とラベルの保管。JSONL を spec ごとに 1 ファイル。
//
//	<dir>/records/<spec_id>.jsonl  … 全判定(生の確率付き)
//	<dir>/labels/<spec_id>.jsonl   … 正解ラベル(利用者の操作から拾う)
//
// 本文(inputs)は常に手元の記録に残す(人が後から正解を付け、文言やモデルを変えた時に聞き直すため)。外に送るかは spec の send_to と pii で決まる。
package store

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Record は 1 回の判定。decide.Decision に来歴を足したもの。
type Record struct {
	TraceID       string             `json:"trace_id"`
	SpecID        string             `json:"spec_id"`
	SpecHash      string             `json:"spec_hash"`
	Type          string             `json:"type"`
	Model         string             `json:"model,omitempty"`
	Endpoint      string             `json:"endpoint,omitempty"`
	Inputs        map[string]string  `json:"inputs,omitempty"` // 本文(古い記録には無いことがある)
	InputsHash    string             `json:"inputs_hash"`
	Raw           map[string]float64 `json:"raw,omitempty"`
	Calibrated    map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated  bool               `json:"is_calibrated"`
	Top           string             `json:"top,omitempty"`
	P             float64            `json:"p"`
	Band          string             `json:"band"`
	Tainted       bool               `json:"tainted,omitempty"`        // 本番データを読んだセッションの判定。この本文は聞き直しでも hosted に送らない
	Rung          string             `json:"rung,omitempty"`           // どの段で決まったか: minimal(最小の質問。古い記録は空) | context(文脈を足して聞き直した)
	Minimal       *Pass              `json:"minimal,omitempty"`        // 文脈を足した時の、最小の質問の答え(その段のハッシュと raw。人の正解はこちらにも付く)
	Display       *RecordDisplay     `json:"display,omitempty"`        // 設定の言語での併記(hook が書く。英語なら無い): 何と言われて何をしたかを後から辿るため
	Project       string             `json:"project,omitempty"`        // どの仕事の判定か(hook が書く: cwd の repo のルートの名、git の外なら cwd の名)。入力でもハッシュの鍵でもなく、画面と status で分けて見るための印
	RedoOf        string             `json:"redo_of,omitempty"`        // 過去の判定(trace_id)の本文を、今の聞き方と線でもう一度判定したもの。元の記録は列から消える
	ReviewPending bool               `json:"review_pending,omitempty"` // 会話の続きを受けて AI が仮採点する。人への全件採点待ちにしない
	At            time.Time          `json:"at"`
}

// Pass は 1 つの段の答え。文脈を足して聞き直した記録が、最小の質問の答えを持っておくのに使う。
// RecordDisplay は判定の記録に添える、設定の言語での訳(spec の display.<lang> から)。モデルには送らず、較正の鍵にも入らない。
type RecordDisplay struct {
	Lang     string `json:"lang"`
	Question string `json:"question,omitempty"` // 人の言葉の質問
	Top      string `json:"top,omitempty"`      // モデルの答え(選択肢)の人の言葉
	Message  string `json:"message,omitempty"`  // hook が人かエージェントに出した文(英語と併記)。出さなかったら無い
}

type Pass struct {
	SpecHash     string             `json:"spec_hash"`
	InputsHash   string             `json:"inputs_hash,omitempty"` // この段の template が使う入力だけのハッシュ(文脈の入力が違っても最小の質問では同じ入力)
	Raw          map[string]float64 `json:"raw,omitempty"`
	Calibrated   map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated bool               `json:"is_calibrated"`
	Top          string             `json:"top,omitempty"`
	P            float64            `json:"p"`
	Band         string             `json:"band"`
}

// RungOf は記録の段の名前。古い記録(rung 無し)は最小の質問。
func (r Record) RungOf() string {
	if r.Rung == "" {
		return "minimal"
	}
	return r.Rung
}

// Label は正解 1 件。Truth は型ごとの表現: noul は "yes"/"no"、choice は選択肢 id、score はレベル番号。
type Label struct {
	TraceID    string             `json:"trace_id"`
	SpecID     string             `json:"spec_id"`
	SpecHash   string             `json:"spec_hash"`
	Type       string             `json:"type"`
	Model      string             `json:"model"`
	InputsHash string             `json:"inputs_hash"`
	Inputs     map[string]string  `json:"inputs,omitempty"` // 正解と本文を一緒に持つ(聞き直しの材料)
	Raw        map[string]float64 `json:"raw"`
	Truth      string             `json:"truth"`
	Weight     float64            `json:"weight"`
	Source     string             `json:"source"` // ask | outcome | manual | ui | mcp | file | replay | context | …
	Tainted    bool               `json:"tainted,omitempty"`
	Rung       string             `json:"rung,omitempty"` // この正解が付いた段(minimal | context)。raw と spec_hash はその段のもの
	At         time.Time          `json:"at"`
}

// MaxLineBytes は記録・ラベル 1 行の上限。書く側で弾く(読む側の上限より小さい)。
const MaxLineBytes = 4 << 20

// StoredInput は本文と、その判定が tainted だったか。
type StoredInput struct {
	Inputs  map[string]string
	Tainted bool
}

// Store はディレクトリ 1 つ。
type Store struct{ Dir string }

// NewTraceID は 16 桁の乱数。
func NewTraceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// HashInputs は入力の本文を残さずに同一性だけ比べるためのハッシュ。
func HashInputs(inputs map[string]string) string {
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(inputs[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (s Store) recordsPath(specID string) string {
	return filepath.Join(s.Dir, "records", specID+".jsonl")
}

func (s Store) labelsPath(specID string) string {
	return filepath.Join(s.Dir, "labels", specID+".jsonl")
}

// AppendRecord は判定を追記する。
func (s Store) AppendRecord(r Record) error {
	if r.TraceID == "" || r.SpecID == "" {
		return errors.New("record needs trace_id and spec_id")
	}
	return appendJSONL(s.recordsPath(r.SpecID), r)
}

// FindRecord は trace_id で判定を探す。spec が分かっていれば specID で絞る(空なら全部見る)。
func (s Store) FindRecord(specID, traceID string) (*Record, error) {
	var files []string
	if specID != "" {
		files = []string{s.recordsPath(specID)}
	} else {
		var err error
		files, err = filepath.Glob(filepath.Join(s.Dir, "records", "*.jsonl"))
		if err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		var found *Record
		err := scanJSONL(f, func(line []byte) error {
			var r Record
			if err := json.Unmarshal(line, &r); err != nil {
				return nil // 壊れた行は飛ばす
			}
			if r.TraceID == traceID {
				found = &r
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, fmt.Errorf("no record with trace_id %s", traceID)
}

// LabelFor は判定の記録と利用者の答えからラベルを作る。答えは型ごとの表現に合っていなければならない。
func LabelFor(rec *Record, truth string, weight float64, source string) (Label, error) {
	truth = strings.TrimSpace(truth)
	switch rec.Type {
	case "noul":
		if truth != "yes" && truth != "no" {
			return Label{}, fmt.Errorf("noul truth must be yes or no, got %q", truth)
		}
	case "choice", "score":
		// "!<id>" は部分ラベル(その選択肢ではなかった、だけが分かっている)。結果から拾う時に使う。
		if _, ok := rec.Raw[strings.TrimPrefix(truth, "!")]; !ok || truth == "!" {
			keys := make([]string, 0, len(rec.Raw))
			for k := range rec.Raw {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return Label{}, fmt.Errorf("%s truth must be one of %v, got %q", rec.Type, keys, truth)
		}
	default:
		return Label{}, fmt.Errorf("record has unknown type %q", rec.Type)
	}
	if weight <= 0 {
		weight = 1
	}
	if weight > 1 {
		return Label{}, fmt.Errorf("weight must be in (0, 1], got %g (1 = a firm truth; less = a weak one)", weight)
	}
	if source == "" {
		source = "manual"
	}
	return Label{
		TraceID: rec.TraceID, SpecID: rec.SpecID, SpecHash: rec.SpecHash, Type: rec.Type,
		Model: rec.Model, InputsHash: rec.InputsHash, Inputs: rec.Inputs, Raw: rec.Raw,
		Truth: truth, Weight: weight, Source: source, Tainted: rec.Tainted, At: time.Now(),
	}, nil
}

// LabelsFor は 1 つの正解を、その記録が通った全部の段に付ける。
// 最小の質問で決まった記録なら 1 件。文脈を足して決まった記録なら 2 件: 最終の答え(文脈のハッシュ・raw)と、最小の質問(そのハッシュ・raw)。
// 人の答えは聞き方に依らないので、下の段もそのまま同じ正解で学べる(上の段の答えが下の段の正解になる、の人の側)。
func LabelsFor(rec *Record, truth string, weight float64, source string) ([]Label, error) {
	l, err := LabelFor(rec, truth, weight, source)
	if err != nil {
		return nil, err
	}
	l.Rung = rec.RungOf()
	out := []Label{l}
	if rec.Minimal != nil {
		m := l
		m.SpecHash, m.Raw, m.Rung = rec.Minimal.SpecHash, rec.Minimal.Raw, "minimal"
		if rec.Minimal.InputsHash != "" {
			m.InputsHash = rec.Minimal.InputsHash
		}
		if _, ok := m.Raw[strings.TrimPrefix(truth, "!")]; !ok && rec.Type != "noul" {
			return nil, fmt.Errorf("truth %q is not an option of the minimal pass", truth)
		}
		out = append(out, m)
	}
	return out, nil
}

// Funnel は記録がどの段で決まったかの数。進み具合は「1000 判定あたりの人の判断(ask のまま人に回った数)」で見る。
type Funnel struct {
	Records int `json:"records"`
	Minimal int `json:"minimal"` // 最小の質問で決まった(act / discard)
	Context int `json:"context"` // 文脈を足して決まった(act / discard)
	Human   int `json:"human"`   // どの段でも決まらず人に回った(ask)
}

// HumanPerThousand は 1000 判定あたりの人の判断。記録が無ければ 0。
func (f Funnel) HumanPerThousand() float64 {
	if f.Records == 0 {
		return 0
	}
	return math.Round(1000 * float64(f.Human) / float64(f.Records))
}

// Funnel は spec の記録を段と帯で数える。読めない行は数えない。
// discardUnsure は decide の「迷い」と同じ規則: 一番高い選択肢で帯を決める型(choice、level 無しの score)では discard も人へ。
func (s Store) Funnel(specID string, discardUnsure bool) (Funnel, error) {
	var f Funnel
	err := scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) != nil {
			return nil
		}
		f.Records++
		switch {
		case r.Band == "ask" || (discardUnsure && r.Band == "discard"):
			f.Human++
		case r.RungOf() == "context":
			f.Context++
		default:
			f.Minimal++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return f, err
	}
	return f, nil
}

// AuditSample は「この判定を抜き取りで人に見せるか」。act で自動で動いた判定は人が見ないので、act の精度は人の正解で測れない。
// そこで 20 件に 1 件を trace_id で決めて(何度見ても同じ)答え合わせの列に出す。空の trace は出さない。
func AuditSample(traceID string) bool {
	if traceID == "" {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte(traceID))
	return h.Sum32()%20 == 0
}

// HumanSource は、その出どころの正解を人(または結果)が決めたか。機械が決めた正解は上の段の答え(context)と審判(judge: 別のモデルの答え)。
// 聞き直し(replay)とファイル(file)は確率を機械が取り直しただけで、正解そのものは人が付けたもの。
func HumanSource(source string) bool {
	return source != "context" && source != "judge"
}

// Partial は "!<id>" 形式の部分ラベルか。
func Partial(truth string) bool { return strings.HasPrefix(truth, "!") && len(truth) > 1 }

// Correct はモデルの最上位 top が正解 truth と合うか。部分ラベル "!x" は「x でなければ正しい」。
func Correct(top, truth string) bool {
	if Partial(truth) {
		return top != truth[1:]
	}
	return top == truth
}

// AppendLabel はラベルを追記する。
func (s Store) AppendLabel(l Label) error {
	return s.AppendLabels([]Label{l})
}

// AppendLabels は複数のラベル(同じ spec)を 1 回の書き込みで追記する。1 つの答えが 2 つの段に付く時、片方だけ残ることがない。
func (s Store) AppendLabels(ls []Label) error {
	if len(ls) == 0 {
		return nil
	}
	var buf []byte
	for _, l := range ls {
		if l.TraceID == "" || l.SpecID == "" || l.SpecHash == "" || l.Model == "" {
			return errors.New("label needs trace_id, spec_id, spec_hash and model")
		}
		if l.SpecID != ls[0].SpecID {
			return errors.New("labels in one append must belong to one spec")
		}
		b, err := json.Marshal(l)
		if err != nil {
			return err
		}
		if len(b) > MaxLineBytes {
			return fmt.Errorf("record too large: %d bytes > %d (inputs must stay under the line limit)", len(b), MaxLineBytes)
		}
		buf = append(append(buf, b...), '\n')
	}
	path := s.labelsPath(ls[0].SpecID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(buf)
	return err
}

// Labels は (spec_hash, model) に一致するラベルだけを返す。古い文言・別モデルのラベルは較正に使わない。
func (s Store) Labels(specID, specHash, model string) ([]Label, error) {
	var out []Label
	err := scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if err := json.Unmarshal(line, &l); err != nil {
			return nil
		}
		if l.SpecHash == specHash && l.Model == model {
			out = append(out, l)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// SpecIDs は記録かラベルのある spec の id を返す。
func (s Store) SpecIDs() ([]string, error) {
	seen := map[string]bool{}
	for _, sub := range []string{"records", "labels"} {
		files, err := filepath.Glob(filepath.Join(s.Dir, sub, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			seen[strings.TrimSuffix(filepath.Base(f), ".jsonl")] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Counts は spec の記録数と、全ラベル(鍵で絞らない)を返す。棚卸し用。
func (s Store) Counts(specID string) (records int, labels []Label, err error) {
	err = scanJSONL(s.recordsPath(specID), func([]byte) error { records++; return nil })
	if err != nil && !os.IsNotExist(err) {
		return 0, nil, err
	}
	err = scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil {
			labels = append(labels, l)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, nil, err
	}
	return records, labels, nil
}

// InputsByHash は本文が残っている記録から、入力のハッシュ → 本文 の対応を作る。
// 文言やモデルを変えた後に、同じ入力を新しい (spec_hash, model) で聞き直すための材料。
func (s Store) InputsByHash(specID string) (map[string]StoredInput, error) {
	out := map[string]StoredInput{}
	take := func(hash string, inputs map[string]string, tainted bool) {
		if hash == "" || len(inputs) == 0 {
			return
		}
		cur, ok := out[hash]
		if !ok {
			out[hash] = StoredInput{Inputs: inputs, Tainted: tainted}
			return
		}
		if tainted && !cur.Tainted { // 一度でも tainted で記録された本文は、以後も外に出さない
			cur.Tainted = true
			out[hash] = cur
		}
	}
	err := scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			take(r.InputsHash, r.Inputs, r.Tainted)
			if r.Minimal != nil {
				take(r.Minimal.InputsHash, r.Inputs, r.Tainted) // 最小の段のハッシュからも同じ本文に届く(Render は余分な入力を無視する)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// ファイルから較正した時のラベルは記録を持たないので、ラベル側の本文も見る
	err = scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil {
			take(l.InputsHash, l.Inputs, l.Tainted)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// LatestPerInput は入力(inputs_hash)ごとに最新のラベル 1 つだけを残す。
// 同じ入力に複数の正解(ファイルの行、後から人が付けた訂正、同じ trace への 2 回目)があれば新しい方が勝つ。
// calibrate / check / drift は全部これを通す(数え方を揃えるため)。inputs_hash の無いラベルはそのまま残す。
func LatestPerInput(labels []Label) []Label {
	latest := map[string]Label{}
	order := []string{}
	var out []Label
	for _, l := range labels {
		if l.InputsHash == "" {
			out = append(out, l)
			continue
		}
		cur, ok := latest[l.InputsHash]
		if !ok {
			order = append(order, l.InputsHash)
			latest[l.InputsHash] = l
			continue
		}
		// 人の正解は機械の正解(context / judge)より常に強い。同じ種類どうしは新しい方(監査 4 回目の 2)
		switch {
		case HumanSource(l.Source) && !HumanSource(cur.Source):
			latest[l.InputsHash] = l
		case !HumanSource(l.Source) && HumanSource(cur.Source):
		case l.At.After(cur.At):
			latest[l.InputsHash] = l
		}
	}
	for _, h := range order {
		out = append(out, latest[h])
	}
	return out
}

// Records は spec の記録を新しい順に limit 件(limit <= 0 なら全部)。待ち行列の表示用。
func (s Store) Records(specID string, limit int) ([]Record, error) {
	var all []Record
	err := scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			all = append(all, r)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.After(all[j].At) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// LabeledTraces は trace_id → ラベル(複数なら最新)。記録に正解が付いたかを見るため。
func (s Store) LabeledTraces(specID string) (map[string]Label, error) {
	out := map[string]Label{}
	err := scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) != nil || l.TraceID == "" {
			return nil
		}
		if cur, ok := out[l.TraceID]; !ok || (HumanSource(l.Source) && !HumanSource(cur.Source)) || (HumanSource(l.Source) == HumanSource(cur.Source) && l.At.After(cur.At)) {
			out[l.TraceID] = l
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// LastRecord は spec の最後に追記された記録。手でラベルを付けるときに trace_id を控えなくてよいようにする。
func (s Store) LastRecord(specID string) (*Record, error) {
	var last *Record
	err := scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			last = &r
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if last == nil {
		return nil, fmt.Errorf("no records for spec %s", specID)
	}
	return last, nil
}

// GCResult は GC が何を残し、何を消した(消す)かの数。
type GCResult struct {
	Removed      int // 古くてラベルの無い記録(apply なら消した数、そうでなければ消す予定の数)
	KeptLabeled  int // ラベルが付いているので年齢に関係なく残した
	KeptRecent   int // cutoff より新しいので残した
	KeptUnparsed int // JSON として読めない行。読めないものは捨てない
}

// GC は spec の記録のうち「at が before より古く、ラベルが 1 つも付いていない」ものを取り除く。
// ラベルは較正の材料なので、ラベルの付いた記録はいつまでも残す。ラベルのファイルには触らない。
// apply が false なら数えるだけで書き換えない。apply なら同じディレクトリに書いてから rename で置き換える(途中で落ちても元のファイルは壊れない)。
func (s Store) GC(specID string, before time.Time, apply bool) (GCResult, error) {
	var res GCResult
	labeled := map[string]bool{}
	err := scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil && l.TraceID != "" {
			labeled[l.TraceID] = true
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	var keep [][]byte
	err = scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) != nil || r.TraceID == "" {
			res.KeptUnparsed++
			keep = append(keep, append([]byte(nil), line...))
			return nil
		}
		switch {
		case labeled[r.TraceID]:
			res.KeptLabeled++
		case !r.At.Before(before):
			res.KeptRecent++
		default:
			res.Removed++
			return nil
		}
		keep = append(keep, append([]byte(nil), line...))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}
	if !apply || res.Removed == 0 {
		return res, nil
	}
	path := s.recordsPath(specID)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".gc-*")
	if err != nil {
		return res, err
	}
	discard := func(e error) (GCResult, error) {
		tmp.Close()
		os.Remove(tmp.Name())
		return res, e
	}
	w := bufio.NewWriter(tmp)
	for _, line := range keep {
		if _, err := w.Write(append(line, '\n')); err != nil {
			return discard(err)
		}
	}
	if err := w.Flush(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		return discard(err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return discard(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return discard(err)
	}
	return res, nil
}

func appendJSONL(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxLineBytes {
		return fmt.Errorf("record too large: %d bytes > %d (inputs must stay under the line limit)", len(b), MaxLineBytes)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func scanJSONL(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 2*MaxLineBytes) // 書く側の上限より大きく(古い記録や手で直した行でも読める)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := fn([]byte(sc.Text())); err != nil {
			return err
		}
	}
	return sc.Err()
}
