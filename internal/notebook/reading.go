package notebook

import (
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"os"
	"slices"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// HostStageLimit keeps host assignment and reading persistence bounds identical.
const HostStageLimit = 200

// ReadingDetails binds an agent's quality report to measured knowledge delivery.
// It is evidence for a bounded reading heuristic, never a truth or permission.
type ReadingDetails struct {
	Topic      string             `json:"topic"`
	Stage      string             `json:"stage,omitempty"`
	Case       string             `json:"case"`
	Work       string             `json:"work"`
	Check      string             `json:"check"`
	Depth      string             `json:"depth"`
	Bytes      int                `json:"bytes"`
	Reads      int                `json:"reads"`
	Recoveries int                `json:"recoveries"`
	Sufficient bool               `json:"sufficient"`
	Articles   []ArticleReference `json:"articles"`
	Evidence   []Source           `json:"evidence"`
}

type ReadingSample struct {
	ReadingDetails
	Snapshot Source `json:"snapshot"`
}

type ReadingPolicy struct {
	Status    string   `json:"status"`
	Depth     string   `json:"depth,omitempty"`
	Cases     int      `json:"cases"`
	Check     string   `json:"check,omitempty"`
	NextDepth string   `json:"next_depth"`
	NextCheck string   `json:"next_check"`
	Sources   []string `json:"sources,omitempty"`
	Notice    string   `json:"notice"`
}

func (d ReadingDetails) Validate() error {
	if !ValidAssistanceText(d.Topic, true) || len(d.Topic) > 1500 || len(d.Stage) > HostStageLimit || !idPattern.MatchString(d.Case) || !idPattern.MatchString(d.Work) || !ValidAssistanceText(d.Check, true) || len(d.Check) > 1000 || !slices.Contains([]string{"brief", "focused", "deep"}, d.Depth) || d.Bytes <= 0 || d.Bytes > 16<<20 || d.Reads < 1 || d.Reads > 128 || d.Recoveries < 0 || d.Recoveries >= d.Reads || len(d.Articles) < 1 || len(d.Articles) > 8 || len(d.Evidence) < 1 || len(d.Evidence) > 4 {
		return errors.New("reading feedback needs a bounded measured execution, fixed articles and checked evidence")
	}
	seen := map[string]bool{}
	for _, p := range d.Articles {
		if !idPattern.MatchString(p.ID) || !idPattern.MatchString(p.Revision) || seen[p.ID] {
			return errors.New("reading articles need unique pinned revisions")
		}
		seen[p.ID] = true
	}
	for _, e := range d.Evidence {
		if !idPattern.MatchString(e.ID) || !validOutcomeHash(e.Hash) {
			return errors.New("reading quality needs captured observation sources")
		}
	}
	return nil
}

func (b Book) checkedReading(s ReadingSample) ([]string, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	o, err := b.checkedOutcomeSource(s.Snapshot)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(s.ReadingDetails)
	if o.Kind != "agent" || o.Title != "Measured reading execution" || o.Content != string(data) {
		return nil, errors.New("reading execution does not match its immutable snapshot")
	}
	return b.ReadingEvidence(s.ReadingDetails)
}

// ReadingEvidence validates immutable quality sources before accepting work,
// and returns content identities for comparison, never evidence of truth.
func (b Book) ReadingEvidence(d ReadingDetails) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	fingerprints := []string{}
	for _, e := range d.Evidence {
		o, err := b.checkedOutcomeSource(e)
		if err != nil {
			return nil, err
		}
		identity, err := readingQualityIdentity(o.Content, d)
		if err != nil {
			return nil, err
		}
		fingerprints = append(fingerprints, identity)
	}
	return fingerprints, nil
}

// Report identity excludes provenance and unrelated transport fields. Typed host
// reports preserve their full envelope as evidence, but only their actual quality
// result supports an independent case. Generic JSON is canonicalized too.
func readingQualityIdentity(content string, d ReadingDetails) (string, error) {
	value, err := readingTextResult(content)
	if err != nil {
		return "", err
	}
	if object, ok := value.(map[string]any); ok {
		observed, present := object["observed"]
		if object["type"] == "reading-quality/v1" {
			check, _ := object["check"].(string)
			sufficient, valid := object["sufficient"].(bool)
			text, textual := observed.(string)
			if check != d.Check || !valid || sufficient != d.Sufficient || !textual || !ValidAssistanceText(text, true) {
				return "", errors.New("reading quality evidence must match its check and sufficient result")
			}
		}
		if !present || observed == nil {
			return "", errors.New("structured quality evidence needs an explicit observed result separate from provenance")
		}
		if text, ok := observed.(string); ok {
			if !ValidAssistanceText(text, true) {
				return "", errors.New("reading quality needs an observed result")
			}
			observed, err = readingTextResult(text)
			if err != nil {
				return "", err
			}
		}
		return readingValueIdentity(observed)
	}
	if _, ok := value.([]any); ok {
		return "", errors.New("structured quality evidence needs an explicit observed result")
	}
	return readingValueIdentity(value)
}

// Every textual result uses the same parser, whether direct or in observed.
func readingTextResult(content string) (any, error) {
	if value, ok := readingJSON(content); ok {
		return value, nil
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return nil, errors.New("malformed structured quality evidence cannot teach a preference")
	}
	return strings.Join(strings.Fields(content), " "), nil
}

func readingJSON(content string) (any, bool) {
	var v any
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if decoder.Decode(&v) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return v, true
}

// Canonicalize values without converting exact JSON numbers to floating point.
// Numbers retain a coefficient and exponent; even huge powers are never expanded.
func readingValueIdentity(value any) (string, error) {
	var present func(any) bool
	present = func(v any) bool {
		switch x := v.(type) {
		case nil:
			return false
		case string:
			return ValidAssistanceText(x, true)
		case []any:
			for _, item := range x {
				if present(item) {
					return true
				}
			}
			return false
		case map[string]any:
			for _, item := range x {
				if present(item) {
					return true
				}
			}
			return false
		default:
			return true
		}
	}
	if !present(value) {
		return "", errors.New("quality evidence needs an actual observed result")
	}

	var normalize func(any) (any, error)
	normalize = func(v any) (any, error) {
		switch x := v.(type) {
		case json.Number:
			n, err := readingNumber(string(x))
			if err != nil {
				return nil, err
			}
			return []any{"number", n}, nil
		case string:
			return []any{"text", strings.Join(strings.Fields(norm.NFC.String(x)), " ")}, nil
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				v, err := normalize(item)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return []any{"array", out}, nil
		case map[string]any:
			out := map[string]any{}
			for key, item := range x {
				v, err := normalize(item)
				if err != nil {
					return nil, err
				}
				key = norm.NFC.String(key)
				if _, exists := out[key]; exists {
					return nil, errors.New("quality evidence has ambiguous Unicode-equivalent keys")
				}
				out[key] = v
			}
			return []any{"object", out}, nil
		default:
			return []any{"scalar", v}, nil
		}
	}
	v, err := normalize(value)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func readingNumber(raw string) (string, error) {
	sign := ""
	if strings.HasPrefix(raw, "-") {
		sign = "-"
		raw = raw[1:]
	}
	mantissa, power, hasPower := strings.Cut(strings.ToLower(raw), "e")
	whole, fraction, _ := strings.Cut(mantissa, ".")
	coefficient := strings.TrimLeft(whole+fraction, "0")
	if coefficient == "" {
		return "0", nil
	}
	trimmed := strings.TrimRight(coefficient, "0")
	exponent := new(big.Int)
	if hasPower {
		if len(power) > 128 {
			return "", errors.New("quality numeric exponent exceeds its fixed comparison budget")
		}
		if _, ok := exponent.SetString(power, 10); !ok {
			return "", errors.New("invalid quality numeric exponent")
		}
	}
	exponent.Add(exponent, big.NewInt(int64(len(coefficient)-len(trimmed)-len(fraction))))
	return sign + trimmed + "e" + exponent.String(), nil
}

func sameReadingArticles(a, b []ArticleReference) bool {
	key := func(ps []ArticleReference) string {
		keys := []string{}
		for _, p := range ps {
			keys = append(keys, p.ID+"@"+p.Revision)
		}
		sort.Strings(keys)
		return strings.Join(keys, ",")
	}
	return len(a) == len(b) && key(a) == key(b)
}

func (b Book) RecordReading(s ReadingSample) error {
	if _, err := b.checkedReading(s); err != nil {
		return err
	}
	pages, err := b.directory(false, "pages")
	if err != nil {
		return err
	}
	unlock, err := lock(pages)
	if err != nil {
		return err
	}
	defer unlock()
	for _, p := range s.Articles {
		v, err := b.Query("lesson:" + p.ID + "@" + p.Revision)
		if err != nil || len(v.Lessons) != 1 || v.Lessons[0].Revision != p.Revision {
			return errors.New("the measured reading revision is unavailable; recover its original source")
		}
	}
	dir, err := b.directory(true, "searches")
	if err != nil {
		return err
	}
	unsearch, err := lock(dir)
	if err != nil {
		return err
	}
	defer unsearch()
	j, err := readSearchJournal(dir)
	if err != nil {
		return err
	}
	for _, old := range j.Readings {
		if old.Snapshot.ID == s.Snapshot.ID {
			if string(mustOutcomeJSON(old)) != string(mustOutcomeJSON(s)) {
				return errors.New("a reading execution cannot be rewritten")
			}
			return nil
		}
	}
	j.Readings = append(j.Readings, s)
	for len(j.Readings) > 128 {
		trimReadingHistory(&j)
	}
	return saveSearchJournal(dir, j)
}

// Prefer losing old support to erasing a counterexample. If the bounded journal
// cannot retain distinct failures, disable automatic preferences conservatively.
func trimReadingHistory(j *searchJournal) {
	for i, s := range j.Readings {
		if s.Sufficient {
			j.Readings = append(j.Readings[:i], j.Readings[i+1:]...)
			return
		}
	}
	seen := map[string]bool{}
	for i := len(j.Readings) - 1; i >= 0; i-- {
		s := j.Readings[i]
		key := digest(mustOutcomeJSON([]any{s.Topic, s.Stage, s.Check, s.Depth, s.Articles}))
		if seen[key] {
			j.Readings = append(j.Readings[:i], j.Readings[i+1:]...)
			return
		}
		seen[key] = true
	}
	j.ReadingSaturated = true
	j.Readings = j.Readings[1:]
}

// ReadingPreference compares matched cases. Two distinct cases with disjoint
// work/evidence are required to change a default. Agent quality remains a report.
func (b Book) ReadingPreference(topic, stage string, articles []ArticleReference) (*ReadingPolicy, error) {
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	if j.ReadingSaturated {
		return &ReadingPolicy{Status: "unavailable", NextDepth: "focused", NextCheck: "The counterexample retention budget was reached; use explicit reading choices and original evidence.", Notice: "Automatic reading preferences are disabled because the bounded history cannot retain all distinct counterexamples."}, nil
	}
	type checked struct {
		ReadingSample
		prints []string
	}
	latest := map[string]checked{}
	failures := map[string]bool{}
	invalid := false
	for _, s := range j.Readings {
		if s.Topic != topic || s.Stage != stage || !sameReadingArticles(s.Articles, articles) {
			continue
		}
		prints, err := b.checkedReading(s)
		if err != nil {
			invalid = true
			continue
		}
		latest[s.Check+"\x00"+s.Case+"\x00"+s.Depth] = checked{s, prints}
		if !s.Sufficient {
			failures[s.Check+"\x00"+s.Depth] = true
		}
	}
	if len(latest) == 0 && !invalid {
		return nil, nil
	}
	p := &ReadingPolicy{Status: "collecting", NextDepth: "brief", NextCheck: "Compare a smaller read with focused under the same task and quality check.", Notice: "A bounded heuristic from measured delivery and agent-reported quality; inspect its check and conditions. It establishes no truth, independence, permission or general improvement."}
	if invalid {
		p.Status, p.NextDepth, p.NextCheck = "unavailable", "focused", "Recover changed or unavailable reading evidence before reusing its preference."
		return p, nil
	}
	keys := make([]string, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type score struct {
		depth, check string
		cases        int
		saving       int
		failed       bool
		used         map[string]bool
		sources      []string
	}
	scores := map[string]*score{}
	for _, k := range keys {
		x := latest[k]
		if !x.Sufficient {
			p.NextDepth, p.NextCheck = "deep", "A reported omission requires a deeper check; do not repeat the failed depth unchanged."
		}
		if x.Depth == "focused" {
			continue
		}
		key := x.Check + "\x00" + x.Depth
		s := scores[key]
		if s == nil {
			s = &score{depth: x.Depth, check: x.Check, used: map[string]bool{}, failed: failures[key]}
			scores[key] = s
		}
		if !x.Sufficient {
			s.failed = true
			continue
		}
		base, ok := latest[x.Check+"\x00"+x.Case+"\x00focused"]
		if !ok || x.Work == base.Work || base.Sufficient && x.Bytes >= base.Bytes {
			continue
		}
		evidence := append([]string{"work:" + x.Work, "work:" + base.Work}, x.prints...)
		evidence = append(evidence, base.prints...)
		if slices.ContainsFunc(evidence, func(id string) bool { return s.used[id] }) {
			continue
		}
		for _, id := range evidence {
			s.used[id] = true
		}
		s.cases++
		s.saving += base.Bytes - x.Bytes
		s.sources = append(s.sources, base.Snapshot.ID, x.Snapshot.ID)
	}
	var best *score
	scoreKeys := make([]string, 0, len(scores))
	for k := range scores {
		scoreKeys = append(scoreKeys, k)
	}
	sort.Strings(scoreKeys)
	for _, k := range scoreKeys {
		s := scores[k]
		if s.cases > 0 && (best == nil || best.failed && !s.failed || s.failed == best.failed && (s.cases > best.cases || s.cases == best.cases && s.saving > best.saving)) {
			best = s
		}
	}
	if best == nil {
		return p, nil
	}
	p.Depth, p.Cases, p.Check = best.depth, best.cases, best.check
	p.Sources = best.sources[:min(8, len(best.sources))]
	p.Status, p.NextDepth, p.NextCheck = "candidate", best.depth, "Verify this candidate on another task with the same check and a focused baseline."
	if best.failed {
		p.Status, p.NextDepth, p.NextCheck = "suspended", "deep", "A counterexample suspended this depth; recover the missing condition and narrow or revise the procedure."
	} else if best.cases >= 2 {
		p.Status, p.NextCheck = "adopted", "Check the next actual task; a reported omission retracts this default."
	}
	return p, nil
}
