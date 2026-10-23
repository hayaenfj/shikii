package main

// shikii guard: 人の訂正を「止める検査」にする(0.7.3)。
//
// 教訓を届けるだけでは、repo と CLAUDE.md を読める agent の行動は変わらなかった(2026-10-23 の 5 課題 × 2 条件の実験で 4 同点)。
// 一方、人が一度言った決まり(「変更履歴は英語と日本語を揃える」「根・指紋という語は使わない」「コミット本文に背景と検証を書く」)は
// 決定論の検査にでき、モデルの賢さに依らず確実に効く。ターンの終わり(Stop)に有効な guard を当て、引っかかれば Stop を止めて
// 理由を返す(Claude Code の decision: block。stop_hook_active の Stop では止めず、直ったかを記録するだけ)。
//
// 置き場は各プロジェクトの .shikii/guards/<id>.yaml(固定)。種類は 3 つで、新しいオプションは無い:
//   paired-files    片方が変わったらもう片方も変わっていること(例: CHANGELOG.md と CHANGELOG_ja.md)
//   forbidden-words 足した行に含めない語(例: 根、指紋)。except のパスは見ない
//   commit-body     このターンのコミットの本文に全部含む語(例: 元の問題、変更後、検証)
// 有効にする(enable)時は、その種類の負例で実際に止まること(RED)と正例で通ること(GREEN)を確かめてから enabled: true にする。
// 草稿(add)は enabled: false で置かれ、有効にするのは人(か、人に言われたエージェント)。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	guardKindPairedFiles    = "paired-files"
	guardKindForbiddenWords = "forbidden-words"
	guardKindCommitBody     = "commit-body"
	guardKinds              = guardKindPairedFiles + " | " + guardKindForbiddenWords + " | " + guardKindCommitBody
	guardFilesMax           = 200     // 足した行を読むファイル数の上限(超えた分は見ない)
	guardFileBytesMax       = 1 << 20 // 1 ファイルの上限
	guardNegativePath       = "shikii-guard-negative.txt"
)

var guardIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type guard struct {
	ID       string     `yaml:"id" json:"id"`
	Kind     string     `yaml:"kind" json:"kind"`
	Title    string     `yaml:"title" json:"title"`
	Reason   string     `yaml:"reason,omitempty" json:"reason,omitempty"` // 由来(人の訂正の要点)
	Source   string     `yaml:"source,omitempty" json:"source,omitempty"` // 出典(会話・観察の id)
	Enabled  bool       `yaml:"enabled" json:"enabled"`
	Pairs    [][]string `yaml:"pairs,omitempty" json:"pairs,omitempty"`       // paired-files
	Words    []string   `yaml:"words,omitempty" json:"words,omitempty"`       // forbidden-words
	Except   []string   `yaml:"except,omitempty" json:"except,omitempty"`     // forbidden-words: 見ないパス(glob。末尾 /** で配下)
	Allow    []string   `yaml:"allow,omitempty" json:"allow,omitempty"`       // forbidden-words: 禁止語を含む普通の語(例: 根 に対する 根拠)。先に取り除いてから見る
	Required []string   `yaml:"required,omitempty" json:"required,omitempty"` // commit-body
	Red      *guardRed  `yaml:"red,omitempty" json:"red,omitempty"`           // enable が負例で止まったことを記録する
}

type guardRed struct {
	At       time.Time `yaml:"at" json:"at"`
	Negative string    `yaml:"negative" json:"negative"`
}

// guardInput はターンの観測: 変わったファイル、足した行、このターンのコミット。分からない所は Known/CommitsKnown が false(その検査は通す)。
type guardInput struct {
	Changed      []string
	Known        bool
	Added        map[string][]string
	Commits      []string
	CommitsKnown bool
}

type guardResult struct {
	ID     string `json:"guard"`
	Pass   bool   `json:"pass"`
	Reason string `json:"reason,omitempty"`
}

// guardRecord は records/guards.jsonl の 1 行: どのターンでどの検査が止めた(か通った)か。recheck は止めた後の Stop(直ったかの記録)。
type guardRecord struct {
	At      time.Time `json:"at"`
	Session string    `json:"session_id"`
	Turn    string    `json:"turn_id"`
	Guard   string    `json:"guard"`
	Pass    bool      `json:"pass"`
	Reason  string    `json:"reason,omitempty"`
	Recheck bool      `json:"recheck,omitempty"`
}

func (g guard) validate() error {
	if !guardIDRe.MatchString(g.ID) {
		return errors.New("guard の id は英小文字・数字・ハイフンで 40 文字まで")
	}
	if strings.TrimSpace(g.Title) == "" || len(g.Title) > 200 {
		return errors.New("guard には title(200 バイトまで)が要る")
	}
	switch g.Kind {
	case guardKindPairedFiles:
		if len(g.Pairs) == 0 {
			return errors.New("paired-files には pairs([[a, b], …])が要る")
		}
		for _, p := range g.Pairs {
			if len(p) != 2 || strings.TrimSpace(p[0]) == "" || strings.TrimSpace(p[1]) == "" || p[0] == p[1] {
				return errors.New("pairs の各組は違う 2 つのパス(glob 可)")
			}
		}
	case guardKindForbiddenWords:
		if len(g.Words) == 0 {
			return errors.New("forbidden-words には words が要る")
		}
		for _, w := range g.Words {
			if strings.TrimSpace(w) == "" {
				return errors.New("words に空の語は置けない")
			}
		}
		for _, a := range g.Allow {
			if err := validateAllowedWord(a, g.Words); err != nil {
				return err
			}
		}
	case guardKindCommitBody:
		if len(g.Required) == 0 {
			return errors.New("commit-body には required(本文に含む語)が要る")
		}
		for _, w := range g.Required {
			if strings.TrimSpace(w) == "" {
				return errors.New("required に空の語は置けない")
			}
		}
	default:
		return fmt.Errorf("guard の kind は %s のどれか", guardKinds)
	}
	return nil
}

// matchPath は glob か完全一致。パス全体か名前だけに合えばよい。末尾が /** なら配下全部。
func matchPath(pattern, p string) bool {
	if pattern == p {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		return strings.HasPrefix(p, strings.TrimSuffix(pattern, "**"))
	}
	if ok, _ := filepath.Match(pattern, p); ok {
		return true
	}
	ok, _ := filepath.Match(pattern, filepath.Base(p))
	return ok
}

func anyMatch(pattern string, paths []string) bool {
	for _, p := range paths {
		if matchPath(pattern, p) {
			return true
		}
	}
	return false
}

// validateAllowedWord は allow の 1 語を確かめる: 禁止語を含む普通の語(根 に対する 根拠)だけ。禁止語そのもの、禁止語に助詞・空白・
// 句読点を足しただけの語(根を、「根」)は抜け道になるので拒む(監査 R3)。禁止語を除いた残りに、仮名でない文字(漢字・カタカナ・英字)が要る。
func validateAllowedWord(a string, words []string) error {
	if strings.TrimSpace(a) != a || a == "" {
		return errors.New("allow の語に空白は置けない: " + a)
	}
	for _, r := range a {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return errors.New("allow の語に記号や句読点は置けない: " + a)
		}
	}
	for _, w := range words {
		if a == w {
			return errors.New("allow に禁止語そのものは置けない: " + a)
		}
	}
	ok := false
	for _, w := range words {
		if !strings.Contains(a, w) {
			continue
		}
		rest := strings.ReplaceAll(a, w, "")
		for _, r := range rest {
			if unicode.IsLetter(r) && !unicode.In(r, unicode.Hiragana) {
				ok = true
			}
		}
	}
	if !ok {
		return errors.New("allow には禁止語を含む普通の語だけを置く(例: 根 に対する 根拠。助詞を足しただけの語は置けない): " + a)
	}
	return nil
}

// evalGuard は 1 つの検査をターンの観測に当てる。分からない観測(一覧やコミットが取れない)は通す: 止めるのは確かな時だけ。
func evalGuard(g guard, in guardInput) guardResult {
	r := guardResult{ID: g.ID, Pass: true}
	switch g.Kind {
	case guardKindPairedFiles:
		if !in.Known {
			return r
		}
		var missing []string
		for _, p := range g.Pairs {
			a, b := anyMatch(p[0], in.Changed), anyMatch(p[1], in.Changed)
			switch {
			case a && !b:
				missing = append(missing, p[0]+" が変わったのに "+p[1]+" が変わっていない")
			case b && !a:
				missing = append(missing, p[1]+" が変わったのに "+p[0]+" が変わっていない")
			}
		}
		if len(missing) > 0 {
			r.Pass, r.Reason = false, strings.Join(missing, "; ")
		}
	case guardKindForbiddenWords:
		var hits []string
		paths := make([]string, 0, len(in.Added))
		for p := range in.Added {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			skip := false
			for _, e := range g.Except {
				if matchPath(e, p) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			for _, line := range in.Added[p] {
				for _, a := range g.Allow { // 許す語(根拠など)を先に取り除く
					line = strings.ReplaceAll(line, a, "")
				}
				for _, w := range g.Words {
					if strings.Contains(line, w) {
						hits = append(hits, p+": 「"+w+"」")
					}
				}
			}
		}
		if len(hits) > 0 {
			if len(hits) > 5 {
				hits = append(hits[:5], fmt.Sprintf("… ほか %d 件", len(hits)-5))
			}
			r.Pass, r.Reason = false, "足した行に使わない語がある: "+strings.Join(hits, ", ")
		}
	case guardKindCommitBody:
		if !in.CommitsKnown {
			return r
		}
		var bad []string
		for _, m := range in.Commits {
			subject, body, _ := strings.Cut(strings.TrimSpace(m), "\n")
			if strings.TrimSpace(m) == "" {
				subject = "(件名も本文も無い)"
			}
			var lack []string
			for _, w := range g.Required {
				if !strings.Contains(body, w) {
					lack = append(lack, "「"+w+"」")
				}
			}
			if len(lack) > 0 {
				bad = append(bad, "コミット「"+cut(subject, 60)+"」の本文に "+strings.Join(lack, "・")+" が無い")
			}
		}
		if len(bad) > 0 {
			r.Pass, r.Reason = false, strings.Join(bad, "; ")
		}
	}
	return r
}

// guardNegative はその検査が止めるべき負例(enable の RED に使う)と、その説明。guardPositive は通るべき正例。
func guardNegative(g guard) (guardInput, string) {
	switch g.Kind {
	case guardKindPairedFiles:
		return guardInput{Changed: []string{g.Pairs[0][0]}, Known: true}, g.Pairs[0][0] + " だけが変わったターン"
	case guardKindForbiddenWords:
		path := negativePathFor(g)
		return guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {"この行には " + g.Words[0] + " が入っている"}}}, "「" + g.Words[0] + "」を含む行を足したターン"
	case guardKindCommitBody:
		return guardInput{Commits: []string{"件名だけのコミット"}, CommitsKnown: true}, "本文の無いコミットをしたターン"
	}
	return guardInput{}, ""
}

// negativePathFor は負例・正例に使う架空のパス。except のどれにも合わない名前にする(except が *.txt や ** でも enable できる)。
func negativePathFor(g guard) string {
	for _, p := range []string{guardNegativePath, "shikii-guard-negative/example.go", "shikii-guard-negative/a/b/c.md", "shikii-guard-negative"} {
		skip := false
		for _, e := range g.Except {
			if matchPath(e, p) {
				skip = true
			}
		}
		if !skip {
			return p
		}
	}
	return guardNegativePath
}

func guardPositive(g guard) guardInput {
	switch g.Kind {
	case guardKindPairedFiles:
		return guardInput{Changed: []string{g.Pairs[0][0], g.Pairs[0][1]}, Known: true}
	case guardKindForbiddenWords:
		path := negativePathFor(g)
		return guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {"この行には使わない語が無い"}}}
	case guardKindCommitBody:
		return guardInput{Commits: []string{"件名\n\n" + strings.Join(g.Required, "\n") + "\n"}, CommitsKnown: true}
	}
	return guardInput{}
}

// ---------------------------------------------------------------- 置き場

func guardsDir(root string) string        { return filepath.Join(root, "guards") }
func guardPath(root, id string) string    { return filepath.Join(guardsDir(root), id+".yaml") }
func guardRecordsPath(root string) string { return filepath.Join(root, "records", "guards.jsonl") }

func loadGuards(root string) ([]guard, error) {
	files, _ := filepath.Glob(filepath.Join(guardsDir(root), "*.yaml"))
	sort.Strings(files)
	var out []guard
	for _, f := range files {
		g, err := loadGuard(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out = append(out, g)
	}
	return out, nil
}

func loadGuard(path string) (guard, error) {
	var g guard
	b, err := os.ReadFile(path)
	if err != nil {
		return g, err
	}
	if err := yaml.Unmarshal(b, &g); err != nil {
		return g, err
	}
	if err := g.validate(); err != nil {
		return g, err
	}
	if filepath.Base(path) != g.ID+".yaml" {
		return g, errors.New("ファイル名と id が違う")
	}
	return g, nil
}

func saveGuard(root string, g guard) error {
	if err := g.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(guardsDir(root), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# shikii の止める検査(" + guardKinds + ")。enabled は shikii guard enable/disable で変える(enable は負例で止まることを確かめる)\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(g); err != nil {
		return err
	}
	return writeAtomic(guardPath(root, g.ID), buf.Bytes())
}

func enabledGuards(root string) ([]guard, error) {
	all, err := loadGuards(root)
	if err != nil {
		return nil, err
	}
	var out []guard
	for _, g := range all {
		if g.Enabled {
			out = append(out, g)
		}
	}
	return out, nil
}

func appendGuardRecords(root string, recs []guardRecord) error {
	if len(recs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(guardRecordsPath(root)), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(guardRecordsPath(root), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func readGuardRecords(root string) []guardRecord {
	b, err := os.ReadFile(guardRecordsPath(root))
	if err != nil {
		return nil
	}
	var out []guardRecord
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r guardRecord
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// guardStats は status 用: 有効な数、止めたターン数、止めた後の Stop で直っていたターン数。
type guardStats struct{ Enabled, Stopped, Fixed int }

func countGuardStats(guards []guard, recs []guardRecord) guardStats {
	var s guardStats
	for _, g := range guards {
		if g.Enabled && !isBuiltinGuard(g.ID) {
			s.Enabled++
		}
	}
	stopped := map[string]bool{}           // session/turn
	failed := map[string]map[string]bool{} // session/turn → guard → まだ直っていない
	for _, r := range recs {
		if isBuiltinGuard(r.Guard) {
			continue // 作法の検査は countRitualStats で別に数える(0.8.0)
		}
		key := r.Session + "/" + r.Turn
		if !r.Recheck && !r.Pass {
			stopped[key] = true
			if failed[key] == nil {
				failed[key] = map[string]bool{}
			}
			failed[key][r.Guard] = true
		}
		if r.Recheck && r.Pass && failed[key] != nil {
			delete(failed[key], r.Guard)
		}
	}
	for key := range stopped {
		s.Stopped++
		if len(failed[key]) == 0 {
			s.Fixed++
		}
	}
	return s
}

func printGuardStats(out io.Writer, root string) {
	guards, err := loadGuards(root)
	if err != nil {
		fmt.Fprintf(out, "guards: %v\n", err)
		return
	}
	recs := readGuardRecords(root)
	if len(guards) == 0 {
		fmt.Fprintln(out, "guards: none (shikii guard add turns a correction into a check that stops the turn)")
	} else {
		s := countGuardStats(guards, recs)
		fmt.Fprintf(out, "guards: %d of %d enabled; stopped %d turns, fixed before the turn ended %d\n", s.Enabled, len(guards), s.Stopped, s.Fixed)
	}
	// 作法の検査(0.8.0): 頼んだ回数のうち済んでいた回数と、止めてから済んだ回数。人には「AI が作法をやっているか」が見える
	r := countRitualStats(recs)
	fmt.Fprintf(out, "rituals: plan-before-work done %d of %d asked (after a stop %d); reflect-before-end done %d of %d asked (after a stop %d)\n", r.Plan.Done, r.Plan.Asked, r.Plan.Fixed, r.Reflect.Done, r.Reflect.Asked, r.Reflect.Fixed)
}

// ritualStat は 1 つの作法の実施の数: 頼んだ(検査した)ターン、最初から済んでいたターン、止めてから済んだターン。
type ritualStat struct{ Asked, Done, Fixed int }

type ritualStats struct{ Plan, Reflect ritualStat }

func countRitualStats(recs []guardRecord) ritualStats {
	var out ritualStats
	for _, id := range []string{ritualPlan, ritualReflect} {
		var st ritualStat
		failed := map[string]bool{} // session/turn → 止めてまだ済んでいない
		for _, r := range recs {
			if r.Guard != id {
				continue
			}
			key := r.Session + "/" + r.Turn
			switch {
			case !r.Recheck && r.Pass:
				st.Asked++
				st.Done++
			case !r.Recheck && !r.Pass && failed[key]:
				// 同じターンの二度目の失敗(通知の後の Stop)は数えない
			case !r.Recheck && !r.Pass:
				st.Asked++
				failed[key] = true
			case r.Recheck && r.Pass && failed[key]:
				delete(failed, key)
				st.Fixed++
			}
		}
		if id == ritualPlan {
			out.Plan = st
		} else {
			out.Reflect = st
		}
	}
	return out
}

// guardSummaryJSON は画面用(/v1/status の summary.guards): 数と、各検査の状態・最後の結果。
func guardSummaryJSON(root string) map[string]any {
	guards, err := loadGuards(root)
	items := []map[string]any{}
	if err != nil {
		return map[string]any{"total": 0, "enabled": 0, "stopped": 0, "fixed": 0, "items": items, "error": err.Error()}
	}
	recs := readGuardRecords(root)
	last := map[string]guardRecord{}
	for _, r := range recs {
		last[r.Guard] = r
	}
	guards = append(builtinGuards(), guards...) // 作法の検査(組み込み)も並べる(0.8.0)
	for _, g := range guards {
		item := map[string]any{"id": g.ID, "kind": g.Kind, "title": g.Title, "enabled": g.Enabled, "reason": g.Reason, "builtin": isBuiltinGuard(g.ID)}
		if r, ok := last[g.ID]; ok {
			item["last"] = map[string]any{"pass": r.Pass, "at": r.At, "reason": r.Reason, "recheck": r.Recheck}
		}
		items = append(items, item)
	}
	s := countGuardStats(guards, recs)
	r := countRitualStats(recs)
	own := 0 // 人が作った検査の数(組み込みを除く)
	for _, g := range guards {
		if !isBuiltinGuard(g.ID) {
			own++
		}
	}
	return map[string]any{"total": own, "enabled": s.Enabled, "stopped": s.Stopped, "fixed": s.Fixed, "items": items,
		"rituals": map[string]any{"plan": map[string]int{"asked": r.Plan.Asked, "done": r.Plan.Done, "fixed": r.Plan.Fixed}, "reflect": map[string]int{"asked": r.Reflect.Asked, "done": r.Reflect.Done, "fixed": r.Reflect.Fixed}}}
}

// ---------------------------------------------------------------- CLI

func cmdGuard(args []string) error {
	usage := errors.New("使い方: shikii guard list | add (JSON を標準入力) | enable <id> | disable <id> | try")
	if len(args) == 0 {
		return usage
	}
	root, _, err := currentProjectStore()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return guardList(root)
	case "add":
		if len(args) != 1 {
			return usage
		}
		return guardAdd(root, stdin)
	case "enable", "disable":
		if len(args) != 2 {
			return usage
		}
		return guardSetEnabled(root, args[1], args[0] == "enable")
	case "try":
		return guardTry(root)
	}
	return usage
}

func guardList(root string) error {
	guards, err := loadGuards(root)
	if err != nil {
		return err
	}
	if len(guards) == 0 {
		fmt.Fprintln(stdout, "人の訂正から作った止める検査はまだ無い。作るなら: shikii guard add < 草稿.json(id, kind, title, pairs/words/except/allow/required)")
	}
	guards = append(builtinGuards(), guards...) // 作法の検査(組み込み。外せない)
	last := map[string]guardRecord{}
	for _, r := range readGuardRecords(root) {
		last[r.Guard] = r
	}
	for _, g := range guards {
		state := "無効(草稿)"
		if g.Enabled {
			state = "有効"
		}
		if isBuiltinGuard(g.ID) {
			state = "組み込み(常に有効)"
		}
		line := fmt.Sprintf("%-24s %-16s %s  %s", g.ID, g.Kind, state, g.Title)
		if r, ok := last[g.ID]; ok {
			if r.Pass {
				line += fmt.Sprintf("  (最後: 通過 %s)", r.At.Format("01-02 15:04"))
			} else {
				line += fmt.Sprintf("  (最後: 止めた %s: %s)", r.At.Format("01-02 15:04"), cut(r.Reason, 80))
			}
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}

func guardAdd(root string, in io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(in, 64<<10))
	if err != nil {
		return err
	}
	if bytes.Contains(data, []byte(ritualPlan)) || bytes.Contains(data, []byte(ritualReflect)) {
		return errors.New("plan-before-work / reflect-before-end は組み込みの作法の検査の id で、草稿には使えません")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var g guard
	if err := dec.Decode(&g); err != nil {
		return errors.New("guard add は JSON(id, kind, title, reason, source, pairs/words/except/allow/required)を 1 つ読む")
	}
	g.Enabled, g.Red = false, nil // 草稿は無効。有効にするのは enable(負例で止まるのを見てから)
	if err := g.validate(); err != nil {
		return err
	}
	if fileExists(guardPath(root, g.ID)) {
		return fmt.Errorf("%s はもうある(直すならファイルを編集し、要らなければ消す)", guardPath(root, g.ID))
	}
	if err := saveGuard(root, g); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: 草稿を置いた(無効)。人が確かめてから有効にする: shikii guard enable %s\n", guardPath(root, g.ID), g.ID)
	return nil
}

// guardSetEnabled は enable なら負例で止まる(RED)・正例で通る(GREEN)のを確かめてから有効にする。どちらかが違えば有効にしない。
func guardSetEnabled(root, id string, enable bool) error {
	if !guardIDRe.MatchString(id) {
		return errors.New("guard の id は英小文字・数字・ハイフン")
	}
	if isBuiltinGuard(id) {
		return errors.New(id + " は組み込みの作法の検査で、常に有効です(外す設定はありません)")
	}
	g, err := loadGuard(guardPath(root, id))
	if err != nil {
		return err
	}
	if !enable {
		g.Enabled = false
		if err := saveGuard(root, g); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: 無効にした(ファイルは残る)\n", id)
		return nil
	}
	neg, what := guardNegative(g)
	if r := evalGuard(g, neg); r.Pass {
		return fmt.Errorf("%s: 負例(%s)で止まらないので有効にしない", id, what)
	} else {
		fmt.Fprintf(stdout, "RED:   負例「%s」で止まった: %s\n", what, r.Reason)
	}
	if r := evalGuard(g, guardPositive(g)); !r.Pass {
		return fmt.Errorf("%s: 正例で止まってしまうので有効にしない: %s", id, r.Reason)
	}
	fmt.Fprintln(stdout, "GREEN: 正例は通った")
	g.Enabled = true
	g.Red = &guardRed{At: time.Now().UTC(), Negative: what}
	if err := saveGuard(root, g); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: 有効にした。ターンの終わりに引っかかると Stop を止めて理由を返す\n", id)
	return nil
}

// guardTry は今の作業ツリー(HEAD との差)に有効な検査を当てて見せる(止めない)。コミットは見ない。
func guardTry(root string) error {
	guards, err := enabledGuards(root)
	if err != nil {
		return err
	}
	if len(guards) == 0 {
		fmt.Fprintln(stdout, "有効な止める検査は無い")
		return nil
	}
	in := guardWorkingTreeInput(filepath.Dir(root))
	for _, g := range guards {
		r := evalGuard(g, in)
		if r.Pass {
			fmt.Fprintf(stdout, "ok   %s\n", g.ID)
		} else {
			fmt.Fprintf(stdout, "STOP %s: %s\n", g.ID, r.Reason)
		}
	}
	return nil
}
