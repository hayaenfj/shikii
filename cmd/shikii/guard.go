package main

// shikii guard: turn a person's correction into a "check that stops the turn" (0.7.3).
//
// Delivering lessons alone did not change how an agent that can read the repo and CLAUDE.md behaves (the 2026-10-23 experiment, 5 tasks x 2 conditions: 4 ties).
// On the other hand, a rule a person has stated once ("keep the English and Japanese changelogs in step", "do not use the words 根 or 指紋", "write the background and verification in the commit body")
// can be made a deterministic check, which works reliably regardless of how smart the model is. At the end of a turn (Stop) the enabled guards run; if one fails, Stop is blocked and
// the reason is returned (Claude Code's decision: block. On a Stop with stop_hook_active it does not block; it only records whether the problem was fixed).
//
// Guards live in each project's .shikii/guards/<id>.yaml (fixed location). There are 3 kinds and no new options:
//   paired-files    when one file changes, the other must change too (e.g. CHANGELOG.md and CHANGELOG_ja.md)
//   forbidden-words words that must not appear in added lines (e.g. 根, 指紋). Paths in except are not checked
//   commit-body     words that the body of every commit in this turn must contain (e.g. 元の問題, 変更後, 検証)
// Enabling (enable) first confirms that the kind's negative example really stops (RED) and the positive example passes (GREEN), then sets enabled: true.
// Drafts (add) are written with enabled: false; enabling is done by a person (or by an agent a person told to).

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
	guardFilesMax           = 200     // max number of files whose added lines are read (the rest are not checked)
	guardFileBytesMax       = 1 << 20 // max size of one file
	guardNegativePath       = "shikii-guard-negative.txt"
)

var guardIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type guard struct {
	ID       string     `yaml:"id" json:"id"`
	Kind     string     `yaml:"kind" json:"kind"`
	Title    string     `yaml:"title" json:"title"`
	Reason   string     `yaml:"reason,omitempty" json:"reason,omitempty"` // origin (the gist of the person's correction)
	Source   string     `yaml:"source,omitempty" json:"source,omitempty"` // source (id of the conversation or observation)
	Enabled  bool       `yaml:"enabled" json:"enabled"`
	Pairs    [][]string `yaml:"pairs,omitempty" json:"pairs,omitempty"`       // paired-files
	Words    []string   `yaml:"words,omitempty" json:"words,omitempty"`       // forbidden-words
	Except   []string   `yaml:"except,omitempty" json:"except,omitempty"`     // forbidden-words: paths not checked (glob; a trailing /** means everything below)
	Allow    []string   `yaml:"allow,omitempty" json:"allow,omitempty"`       // forbidden-words: ordinary words that contain a forbidden word (e.g. 根拠 for 根). Removed before checking
	Required []string   `yaml:"required,omitempty" json:"required,omitempty"` // commit-body
	Red      *guardRed  `yaml:"red,omitempty" json:"red,omitempty"`           // records that enable saw the negative example stop
}

type guardRed struct {
	At       time.Time `yaml:"at" json:"at"`
	Negative string    `yaml:"negative" json:"negative"`
}

// guardInput is what was observed in the turn: changed files, added lines, and this turn's commits. When something is unknown, Known/CommitsKnown is false (that check passes).
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
	// ReasonJA is the same reason in Japanese. The hook shows it next to the English one (bi) when the person's language is Japanese
	ReasonJA string `json:"-"`
}

// guardRecord is one line of records/guards.jsonl: which check stopped (or passed) in which turn. recheck is the Stop after a block (recording whether it was fixed).
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
		return errors.New("a guard id is lowercase letters, digits and hyphens, up to 40 characters")
	}
	if strings.TrimSpace(g.Title) == "" || len(g.Title) > 200 {
		return errors.New("a guard needs a title (up to 200 bytes)")
	}
	switch g.Kind {
	case guardKindPairedFiles:
		if len(g.Pairs) == 0 {
			return errors.New("paired-files needs pairs ([[a, b], ...])")
		}
		for _, p := range g.Pairs {
			if len(p) != 2 || strings.TrimSpace(p[0]) == "" || strings.TrimSpace(p[1]) == "" || p[0] == p[1] {
				return errors.New("each pair in pairs is two different paths (glob allowed)")
			}
		}
	case guardKindForbiddenWords:
		if len(g.Words) == 0 {
			return errors.New("forbidden-words needs words")
		}
		for _, w := range g.Words {
			if strings.TrimSpace(w) == "" {
				return errors.New("words cannot contain an empty word")
			}
		}
		for _, a := range g.Allow {
			if err := validateAllowedWord(a, g.Words); err != nil {
				return err
			}
		}
	case guardKindCommitBody:
		if len(g.Required) == 0 {
			return errors.New("commit-body needs required (words the body must contain)")
		}
		for _, w := range g.Required {
			if strings.TrimSpace(w) == "" {
				return errors.New("required cannot contain an empty word")
			}
		}
	default:
		return fmt.Errorf("a guard's kind is one of %s", guardKinds)
	}
	return nil
}

// matchPath is a glob or exact match, against the whole path or just the name. A trailing /** matches everything below.
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

// validateAllowedWord checks one allow word: only ordinary words that contain a forbidden word (根拠 for 根). The forbidden word itself, or the forbidden word
// with only particles, spaces or punctuation added (根を, 「根」), would be a loophole, so they are refused (audit R3). What remains after removing the forbidden word must contain a non-kana letter (kanji, katakana, Latin).
func validateAllowedWord(a string, words []string) error {
	if strings.TrimSpace(a) != a || a == "" {
		return errors.New("an allow word cannot contain spaces: " + a)
	}
	for _, r := range a {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return errors.New("an allow word cannot contain symbols or punctuation: " + a)
		}
	}
	for _, w := range words {
		if a == w {
			return errors.New("allow cannot contain the forbidden word itself: " + a)
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
		return errors.New("allow takes only ordinary words that contain a forbidden word (e.g. 根拠 for 根; a word with only a particle added is not allowed): " + a)
	}
	return nil
}

// evalGuard runs one check against what was observed in the turn. Unknown observations (the file list or commits could not be read) pass: it stops only when certain.
func evalGuard(g guard, in guardInput) guardResult {
	r := guardResult{ID: g.ID, Pass: true}
	switch g.Kind {
	case guardKindPairedFiles:
		if !in.Known {
			return r
		}
		var missing, missingJA []string
		for _, p := range g.Pairs {
			a, b := anyMatch(p[0], in.Changed), anyMatch(p[1], in.Changed)
			switch {
			case a && !b:
				missing = append(missing, p[0]+" changed but "+p[1]+" did not")
				missingJA = append(missingJA, p[0]+" が変わったのに "+p[1]+" が変わっていない")
			case b && !a:
				missing = append(missing, p[1]+" changed but "+p[0]+" did not")
				missingJA = append(missingJA, p[1]+" が変わったのに "+p[0]+" が変わっていない")
			}
		}
		if len(missing) > 0 {
			r.Pass, r.Reason, r.ReasonJA = false, strings.Join(missing, "; "), strings.Join(missingJA, "; ")
		}
	case guardKindForbiddenWords:
		var hits, hitsJA []string
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
				for _, a := range g.Allow { // remove the allowed words (such as 根拠) first
					line = strings.ReplaceAll(line, a, "")
				}
				for _, w := range g.Words {
					if strings.Contains(line, w) {
						hits = append(hits, p+": \""+w+"\"")
						hitsJA = append(hitsJA, p+": 「"+w+"」")
					}
				}
			}
		}
		if len(hits) > 0 {
			if len(hits) > 5 {
				hits = append(hits[:5], fmt.Sprintf("... and %d more", len(hits)-5))
				hitsJA = append(hitsJA[:5], fmt.Sprintf("… ほか %d 件", len(hitsJA)-5))
			}
			r.Pass, r.Reason, r.ReasonJA = false, "added lines contain a word not to use: "+strings.Join(hits, ", "), "足した行に使わない語がある: "+strings.Join(hitsJA, ", ")
		}
	case guardKindCommitBody:
		if !in.CommitsKnown {
			return r
		}
		var bad, badJA []string
		for _, m := range in.Commits {
			subject, body, _ := strings.Cut(strings.TrimSpace(m), "\n")
			subjectJA := subject
			if strings.TrimSpace(m) == "" {
				subject, subjectJA = "(no subject and no body)", "(件名も本文も無い)"
			}
			var lack, lackJA []string
			for _, w := range g.Required {
				if !strings.Contains(body, w) {
					lack = append(lack, "\""+w+"\"")
					lackJA = append(lackJA, "「"+w+"」")
				}
			}
			if len(lack) > 0 {
				bad = append(bad, "the body of commit \""+cut(subject, 60)+"\" lacks "+strings.Join(lack, ", "))
				badJA = append(badJA, "コミット「"+cut(subjectJA, 60)+"」の本文に "+strings.Join(lackJA, "・")+" が無い")
			}
		}
		if len(bad) > 0 {
			r.Pass, r.Reason, r.ReasonJA = false, strings.Join(bad, "; "), strings.Join(badJA, "; ")
		}
	}
	return r
}

// guardNegative returns the negative example the check must stop (used for enable's RED) and its description. guardPositive is the positive example that must pass.
func guardNegative(g guard) (guardInput, string) {
	switch g.Kind {
	case guardKindPairedFiles:
		return guardInput{Changed: []string{g.Pairs[0][0]}, Known: true}, "a turn where only " + g.Pairs[0][0] + " changed"
	case guardKindForbiddenWords:
		path := negativePathFor(g)
		return guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {"this line contains " + g.Words[0] + " here"}}}, "a turn that added a line containing \"" + g.Words[0] + "\""
	case guardKindCommitBody:
		return guardInput{Commits: []string{"a commit with only a subject"}, CommitsKnown: true}, "a turn that made a commit with no body"
	}
	return guardInput{}, ""
}

// negativePathFor is a made-up path used for the negative and positive examples. It picks a name that matches none of except (so enable works even when except is *.txt or **).
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
		return guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {withoutWords("this line is clean", g.Words)}}}
	case guardKindCommitBody:
		return guardInput{Commits: []string{"subject\n\n" + strings.Join(g.Required, "\n") + "\n"}, CommitsKnown: true}
	}
	return guardInput{}
}

// withoutWords removes every forbidden word from the positive example (a word such as "his" or "line" would otherwise stop it and enable would refuse).
// It repeats until nothing is left (each pass makes the line shorter), because removing one word can join the text around it into another.
func withoutWords(line string, words []string) string {
	for changed := true; changed; {
		changed = false
		for _, w := range words {
			if w != "" && strings.Contains(line, w) {
				line, changed = strings.ReplaceAll(line, w, ""), true
			}
		}
	}
	return line
}

// ---------------------------------------------------------------- storage

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
		return g, errors.New("the file name and the id differ")
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
	buf.WriteString("# shikii check that stops the turn (" + guardKinds + "). Change enabled with shikii guard enable/disable (enable confirms that the negative example stops)\n")
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

// guardStats is for status: the number enabled, the number of turns stopped, and the number of turns fixed by the Stop after the block.
type guardStats struct{ Enabled, Stopped, Fixed int }

func countGuardStats(guards []guard, recs []guardRecord) guardStats {
	var s guardStats
	for _, g := range guards {
		if g.Enabled && !isBuiltinGuard(g.ID) {
			s.Enabled++
		}
	}
	stopped := map[string]bool{}           // session/turn
	failed := map[string]map[string]bool{} // session/turn → guard → not fixed yet
	for _, r := range recs {
		if isBuiltinGuard(r.Guard) {
			continue // ritual checks are counted separately in countRitualStats (0.8.0)
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
	// Ritual checks (0.8.0): of the times asked, how many were already done and how many were done after a block. This shows the person "whether the AI follows the rituals"
	r := countRitualStats(recs)
	kinds := ""
	if r.Kinds.Asked > 0 { // only while pages from before the version bump remain (0.9.0)
		kinds = fmt.Sprintf("; wiki-kinds done %d of %d asked (after a stop %d)", r.Kinds.Done, r.Kinds.Asked, r.Kinds.Fixed)
	}
	fmt.Fprintf(out, "rituals: plan-before-work done %d of %d asked (after a stop %d); reflect-before-end done %d of %d asked (after a stop %d)%s\n", r.Plan.Done, r.Plan.Asked, r.Plan.Fixed, r.Reflect.Done, r.Reflect.Asked, r.Reflect.Fixed, kinds)
}

// ritualStat counts how one ritual was carried out: turns asked (checked), turns done from the start, and turns done after a block.
type ritualStat struct{ Asked, Done, Fixed int }

type ritualStats struct{ Plan, Reflect, Kinds ritualStat }

func countRitualStats(recs []guardRecord) ritualStats {
	var out ritualStats
	for _, id := range []string{ritualPlan, ritualReflect, ritualKinds} {
		var st ritualStat
		failed := map[string]bool{} // session/turn → blocked and not done yet
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
				// a second failure in the same turn (the Stop after the notice) is not counted
			case !r.Recheck && !r.Pass:
				st.Asked++
				failed[key] = true
			case r.Recheck && r.Pass && failed[key]:
				delete(failed, key)
				st.Fixed++
			}
		}
		switch id {
		case ritualPlan:
			out.Plan = st
		case ritualKinds:
			out.Kinds = st
		default:
			out.Reflect = st
		}
	}
	return out
}

// guardSummaryJSON is for the page (summary.guards in /v1/status): counts, and each check's state and last result.
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
	guards = append(builtinGuards(), guards...) // list the ritual checks (built in) too (0.8.0)
	for _, g := range guards {
		item := map[string]any{"id": g.ID, "kind": g.Kind, "title": g.Title, "enabled": g.Enabled, "reason": g.Reason, "builtin": isBuiltinGuard(g.ID)}
		if r, ok := last[g.ID]; ok {
			item["last"] = map[string]any{"pass": r.Pass, "at": r.At, "reason": r.Reason, "recheck": r.Recheck}
		}
		items = append(items, item)
	}
	s := countGuardStats(guards, recs)
	r := countRitualStats(recs)
	own := 0 // number of checks made by people (built-ins excluded)
	for _, g := range guards {
		if !isBuiltinGuard(g.ID) {
			own++
		}
	}
	return map[string]any{"total": own, "enabled": s.Enabled, "stopped": s.Stopped, "fixed": s.Fixed, "items": items,
		"rituals": map[string]any{"plan": map[string]int{"asked": r.Plan.Asked, "done": r.Plan.Done, "fixed": r.Plan.Fixed}, "reflect": map[string]int{"asked": r.Reflect.Asked, "done": r.Reflect.Done, "fixed": r.Reflect.Fixed}, "kinds": map[string]int{"asked": r.Kinds.Asked, "done": r.Kinds.Done, "fixed": r.Kinds.Fixed}}}
}

// ---------------------------------------------------------------- CLI

func cmdGuard(args []string) error {
	usage := errors.New("usage: shikii guard list | add (JSON on standard input) | enable <id> | disable <id> | try")
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
		fmt.Fprintln(stdout, "No checks made from a person's correction yet. To make one: shikii guard add < draft.json (id, kind, title, pairs/words/except/allow/required)")
	}
	guards = append(builtinGuards(), guards...) // ritual checks (built in; cannot be removed)
	last := map[string]guardRecord{}
	for _, r := range readGuardRecords(root) {
		last[r.Guard] = r
	}
	for _, g := range guards {
		state := "disabled (draft)"
		if g.Enabled {
			state = "enabled"
		}
		if isBuiltinGuard(g.ID) {
			state = "built in (always enabled)"
		}
		line := fmt.Sprintf("%-24s %-16s %s  %s", g.ID, g.Kind, state, g.Title)
		if r, ok := last[g.ID]; ok {
			if r.Pass {
				line += fmt.Sprintf("  (last: passed %s)", r.At.Format("01-02 15:04"))
			} else {
				line += fmt.Sprintf("  (last: stopped %s: %s)", r.At.Format("01-02 15:04"), cut(strings.ReplaceAll(r.Reason, "\n", " / "), 80)) // a Japanese reason is on its own line (bi): keep the list one line per check
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
		return errors.New("plan-before-work / reflect-before-end are ids of built-in ritual checks and cannot be used in a draft")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var g guard
	if err := dec.Decode(&g); err != nil {
		return errors.New("guard add reads one JSON object (id, kind, title, reason, source, pairs/words/except/allow/required)")
	}
	g.Enabled, g.Red = false, nil // drafts are disabled. enable turns them on (after seeing the negative example stop)
	if err := g.validate(); err != nil {
		return err
	}
	if fileExists(guardPath(root, g.ID)) {
		return fmt.Errorf("%s already exists (to change it, edit the file; if it is not needed, delete it)", guardPath(root, g.ID))
	}
	if err := saveGuard(root, g); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: wrote the draft (disabled). A person checks it, then enables it: shikii guard enable %s\n", guardPath(root, g.ID), g.ID)
	return nil
}

// guardSetEnabled, for enable, confirms that the negative example stops (RED) and the positive example passes (GREEN) before enabling. If either is wrong, it does not enable.
func guardSetEnabled(root, id string, enable bool) error {
	if !guardIDRe.MatchString(id) {
		return errors.New("a guard id is lowercase letters, digits and hyphens")
	}
	if isBuiltinGuard(id) {
		return errors.New(id + " is a built-in ritual check and is always enabled (there is no setting to turn it off)")
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
		fmt.Fprintf(stdout, "%s: disabled (the file stays)\n", id)
		return nil
	}
	neg, what := guardNegative(g)
	if r := evalGuard(g, neg); r.Pass {
		return fmt.Errorf("%s: not enabled because the negative example (%s) does not stop", id, what)
	} else {
		fmt.Fprintf(stdout, "RED:   the negative example \"%s\" stopped: %s\n", what, r.Reason)
	}
	if r := evalGuard(g, guardPositive(g)); !r.Pass {
		return fmt.Errorf("%s: not enabled because the positive example stops: %s", id, r.Reason)
	}
	fmt.Fprintln(stdout, "GREEN: the positive example passed")
	g.Enabled = true
	g.Red = &guardRed{At: time.Now().UTC(), Negative: what}
	if err := saveGuard(root, g); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: enabled. If it fails at the end of a turn, it blocks Stop and returns the reason\n", id)
	return nil
}

// guardTry runs the enabled checks against the current working tree (its diff from HEAD) and shows the result (it does not stop anything). It does not look at commits.
func guardTry(root string) error {
	guards, err := enabledGuards(root)
	if err != nil {
		return err
	}
	if len(guards) == 0 {
		fmt.Fprintln(stdout, "No enabled checks that stop the turn")
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
