package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The reply of reflect query used to carry the whole shape of submit as how_to_submit: 3,822 bytes in every reply, about 15% of one.
// It now carries an outline (the shape of each field and what a wrong save breaks without an error to say so), and the rest is in
// the section "## Submit shape (how_to_submit)" of the skill's references/review.md, which the outline names the way how_to_record
// names a section of recording.md. What the old text said (the shape of the fields, the conditions, the exceptions, what is refused,
// the order) must still be said, in the outline or in that section. The old text is kept whole in
// testdata/how_to_submit_before.tsv, cut into phrases, one row each, with the words of the phrase that must survive. The checks:
//   - every phrase has its words on one line of the section, and the words listed for the outline are in the outline;
//   - the section is where the outline says it is, once, also in the skill that is installed;
//   - the outline is as long as the test says, in the reply of the CLI and of MCP;
//   - a submit that has the fields the outline lists is saved.
const (
	reviewReference       = "../../skills/shikii/references/review.md"
	submitShapeHeading    = "## Submit shape (how_to_submit)"
	oldHowToSubmitFile    = "testdata/how_to_submit_before.tsv"
	oldHowToSubmitBytes   = 3822
	oldHowToSubmitHash    = "1e7b5c5fda45cb69" // the first 16 hex digits of its SHA-256 (the same as the hash of its line in the body-lines allowlist of 0.44.2)
	reflectHowToSubmitMax = 1511               // the outline in bytes; the test fails above it and 64 bytes below it, so the bound follows the text
)

// submitPhrase is one phrase of the old text with the words of it that the new texts must keep.
type submitPhrase struct {
	id, text         string
	section, outline []string
}

// sectionOfHeading returns what lies between the line that is the heading and the next "## " heading, and how many lines are the heading.
func sectionOfHeading(text, heading string) (string, int) {
	lines := strings.Split(text, "\n")
	start, count := -1, 0
	for i, line := range lines {
		if strings.TrimRight(line, " ") == heading {
			if count++; start < 0 {
				start = i
			}
		}
	}
	if start < 0 {
		return "", 0
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start+1:end], "\n"), count
}

// submitShapeSection is the section of review.md that the outline names.
func submitShapeSection(t *testing.T) string {
	t.Helper()
	section, n := sectionOfHeading(string(mustRead(t, reviewReference)), submitShapeHeading)
	if n != 1 {
		t.Fatalf("review.md has %d lines that are %q, want 1", n, submitShapeHeading)
	}
	return section
}

var runOfSpace = regexp.MustCompile(`\s+`)

// normalizedText is what the words are looked for in: lower case, no backticks (a name in a code span is the same name), one space.
func normalizedText(s string) string {
	return strings.TrimSpace(runOfSpace.ReplaceAllString(strings.ToLower(strings.ReplaceAll(s, "`", "")), " "))
}

func containsAll(normalized string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(normalized, normalizedText(w)) {
			return false
		}
	}
	return true
}

func splitWords(cell string) []string {
	if cell == "" {
		return nil
	}
	return strings.Split(cell, " | ")
}

// loadOldHowToSubmit reads the old text and its phrases: a "text" row, then "phrase" rows of id, phrase, section words, outline words.
func loadOldHowToSubmit(t *testing.T) (string, []submitPhrase) {
	t.Helper()
	var old string
	var phrases []submitPhrase
	for n, line := range strings.Split(string(mustRead(t, oldHowToSubmitFile)), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		switch {
		case fields[0] == "text" && len(fields) == 2:
			old = fields[1]
		case fields[0] == "phrase" && len(fields) == 5:
			phrases = append(phrases, submitPhrase{id: fields[1], text: fields[2], section: splitWords(fields[3]), outline: splitWords(fields[4])})
		default:
			t.Fatalf("%s:%d: want a text row or a phrase row of 5 tab-separated columns: %.80q", oldHowToSubmitFile, n+1, line)
		}
	}
	return old, phrases
}

// oldHowToSubmitProblems checks the table itself: the old text is the one of 0.44.2, the phrases hold all of it in order (between two of
// them lie only separators, so a phrase that is left out leaves words behind), and the words listed for a phrase are in the phrase.
func oldHowToSubmitProblems(old string, phrases []submitPhrase) []string {
	var problems []string
	sum := sha256.Sum256([]byte(old))
	if hash := hex.EncodeToString(sum[:8]); len(old) != oldHowToSubmitBytes || hash != oldHowToSubmitHash {
		problems = append(problems, fmt.Sprintf("the old text is %d bytes with hash %s, not the %d bytes with hash %s of 0.44.2: it is a fixed value", len(old), hash, oldHowToSubmitBytes, oldHowToSubmitHash))
	}
	const separators = " ;,.:()"
	rest, seen := old, map[string]bool{}
	for _, p := range phrases {
		if seen[p.id] {
			problems = append(problems, p.id+": the id is used twice")
		}
		seen[p.id] = true
		if len(p.section) == 0 {
			problems = append(problems, p.id+": no word of the phrase is listed for the section")
		}
		for _, w := range append(slices.Clone(p.section), p.outline...) {
			if !strings.Contains(normalizedText(p.text), normalizedText(w)) {
				problems = append(problems, fmt.Sprintf("%s: the word %q is not in the phrase", p.id, w))
			}
		}
		at := strings.Index(rest, p.text)
		if at < 0 {
			problems = append(problems, p.id+": the phrase is not in the old text, after the phrase before it")
			continue
		}
		if skipped := rest[:at]; strings.Trim(skipped, separators) != "" {
			problems = append(problems, fmt.Sprintf("%s: the old text has %.80q before it that no phrase holds", p.id, skipped))
		}
		rest = rest[at+len(p.text):]
	}
	if strings.Trim(rest, separators) != "" {
		problems = append(problems, fmt.Sprintf("the old text has %.80q after the last phrase that no phrase holds", rest))
	}
	return problems
}

// submitPhraseProblems says which phrase of the old text the outline and the section no longer keep: a word listed for the outline
// that is not in it, and a phrase whose words are not all on one line of the section (a bullet is one line, so a deleted bullet is noticed
// though a word of it is said somewhere else too).
func submitPhraseProblems(phrases []submitPhrase, outline, section string) []string {
	var problems []string
	flat := normalizedText(outline)
	var lines []string
	for _, line := range strings.Split(section, "\n") {
		lines = append(lines, normalizedText(line))
	}
	for _, p := range phrases {
		for _, w := range p.outline {
			if !strings.Contains(flat, normalizedText(w)) {
				problems = append(problems, fmt.Sprintf("%s: the outline in the reply lacks %q", p.id, w))
			}
		}
		if !slices.ContainsFunc(lines, func(line string) bool { return containsAll(line, p.section) }) {
			var missing []string
			for _, w := range p.section {
				if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, normalizedText(w)) }) {
					missing = append(missing, w)
				}
			}
			problems = append(problems, fmt.Sprintf("%s: no one line of the section has all of %q (not said anywhere in it: %q)", p.id, p.section, missing))
		}
	}
	return problems
}

func TestTheOutlineOfHowToSubmitLosesNoPhraseOfTheOldText(t *testing.T) {
	old, phrases := loadOldHowToSubmit(t)
	for _, problem := range oldHowToSubmitProblems(old, phrases) {
		t.Error(problem)
	}
	if len(phrases) < 30 {
		t.Fatalf("the table holds %d phrases; the old text has about 40", len(phrases))
	}
	section := submitShapeSection(t)
	for _, problem := range submitPhraseProblems(phrases, reflectHowToSubmit, section) {
		t.Error(problem)
	}
}

// withoutWord takes a word out of every line of a text, as normalizedText sees the text.
func withoutWord(text, word string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, strings.ReplaceAll(normalizedText(line), normalizedText(word), ""))
	}
	return strings.Join(out, "\n")
}

// withoutLinesHolding takes out the lines that have all the words: the bullet of a phrase.
func withoutLinesHolding(text string, words []string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if !containsAll(normalizedText(line), words) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// scatteredLinesHolding puts the words of the lines that have all of them each on a line of its own: all said, but no longer together.
func scatteredLinesHolding(text string, words []string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if containsAll(normalizedText(line), words) {
			out = append(out, words...)
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func mentionsPhrase(problems []string, id string) bool {
	return slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, id+":") })
}

// The check above must fail where a phrase is lost: for each phrase, the bullet of the section deleted, a word that the outline must keep
// deleted from the outline, a word deleted from the section and the outline both, and, for the table, a phrase left out of it.
func TestTheCheckOfTheOldPhrasesFailsWhereItMust(t *testing.T) {
	old, phrases := loadOldHowToSubmit(t)
	outline, section := reflectHowToSubmit, submitShapeSection(t)
	if problems := append(oldHowToSubmitProblems(old, phrases), submitPhraseProblems(phrases, outline, section)...); len(problems) > 0 {
		t.Fatalf("the check must pass on the real texts before it is tried on others: %q", problems)
	}
	for i, p := range phrases {
		t.Run(p.id, func(t *testing.T) {
			if cut := withoutLinesHolding(section, p.section); !mentionsPhrase(submitPhraseProblems(phrases, outline, cut), p.id) {
				t.Errorf("the bullet of the phrase is deleted from the section and nothing is reported for it")
			}
			if len(p.section) > 1 {
				if scattered := scatteredLinesHolding(section, p.section); !mentionsPhrase(submitPhraseProblems(phrases, outline, scattered), p.id) {
					t.Errorf("the words of the phrase are said, but each on a line of its own, and nothing is reported for it")
				}
			}
			for _, w := range p.outline {
				if !mentionsPhrase(submitPhraseProblems(phrases, withoutWord(outline, w), section), p.id) {
					t.Errorf("%q is deleted from the outline and nothing is reported for the phrase", w)
				}
			}
			for _, w := range p.section {
				if !mentionsPhrase(submitPhraseProblems(phrases, withoutWord(outline, w), withoutWord(section, w)), p.id) {
					t.Errorf("%q is deleted from the outline and the section and nothing is reported for the phrase", w)
				}
			}
			left := slices.Concat(phrases[:i:i], phrases[i+1:])
			if problems := oldHowToSubmitProblems(old, left); len(problems) == 0 {
				t.Errorf("the phrase is left out of the table and the table is still taken for the whole old text")
			}
		})
	}
	t.Run("the old text is a fixed value", func(t *testing.T) {
		if len(oldHowToSubmitProblems(strings.Replace(old, "your own truth", "the agent's truth", 1), phrases)) == 0 {
			t.Error("an edited old text is taken for the old text")
		}
	})
	t.Run("a phrase that is not in the old text", func(t *testing.T) {
		changed := slices.Clone(phrases)
		changed[3].text += " and more"
		if len(oldHowToSubmitProblems(old, changed)) == 0 {
			t.Error("a phrase that the old text does not have is accepted")
		}
	})
	t.Run("a phrase with no word to keep", func(t *testing.T) {
		changed := slices.Clone(phrases)
		changed[3].section = nil
		if len(oldHowToSubmitProblems(old, changed)) == 0 {
			t.Error("a phrase with no word listed for the section is accepted")
		}
	})
	t.Run("a word that the phrase does not have", func(t *testing.T) {
		changed := slices.Clone(phrases)
		changed[3].section = []string{"a word that is nowhere in the phrase"}
		if len(oldHowToSubmitProblems(old, changed)) == 0 {
			t.Error("a word that is not in its phrase is accepted")
		}
	})
}

var outlinePointer = regexp.MustCompile("read `(## [^`]+)` in the skill's references/([a-z-]+\\.md), not the whole file")

// howToSubmitPointerProblem says what is wrong when the outline names a section that review.md does not have exactly once.
func howToSubmitPointerProblem(outline, review string) string {
	found := outlinePointer.FindAllStringSubmatch(outline, -1)
	if len(found) != 1 {
		return fmt.Sprintf("the outline names %d sections to read, want 1 (read `## <heading>` in the skill's references/<file>, not the whole file)", len(found))
	}
	heading, file := found[0][1], found[0][2]
	if file != "review.md" {
		return "the outline sends to " + file + ", where its details are in review.md"
	}
	if _, n := sectionOfHeading(review, heading); n != 1 {
		return fmt.Sprintf("review.md has %d lines that are %q, the section the outline names; want 1", n, heading)
	}
	if heading != submitShapeHeading {
		return fmt.Sprintf("the outline names %q, not %q, whose phrases the table holds", heading, submitShapeHeading)
	}
	return ""
}

func TestTheSectionTheOutlineNamesIsInTheSkillOnceAndInTheInstalledSkill(t *testing.T) {
	review := string(mustRead(t, reviewReference))
	if problem := howToSubmitPointerProblem(reflectHowToSubmit, review); problem != "" {
		t.Fatal(problem)
	}
	for _, c := range []struct{ name, outline, review string }{
		{"a heading that was renamed", reflectHowToSubmit, strings.Replace(review, submitShapeHeading, "## Submit shape (how to submit)", 1)},
		{"a heading that is there twice", reflectHowToSubmit, review + "\n" + submitShapeHeading + "\n\nanother section of the same name\n"},
		{"an outline that names another heading", strings.Replace(reflectHowToSubmit, submitShapeHeading, "## Combined reflection (0.4)", 1), review},
		{"an outline that names wiki.md", strings.Replace(reflectHowToSubmit, "references/review.md", "references/wiki.md", 1), review},
		{"an outline that sends to the whole file", strings.Replace(reflectHowToSubmit, "Details and exceptions: read `"+submitShapeHeading+"` in", "Details are in", 1), review},
	} {
		if howToSubmitPointerProblem(c.outline, c.review) == "" {
			t.Errorf("%s is not noticed", c.name)
		}
	}
	// the skill that init copies out of the binary has the section too, with every phrase of the old text
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if _, err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	installed := string(mustRead(t, filepath.Join(dst, "references", "review.md")))
	if problem := howToSubmitPointerProblem(reflectHowToSubmit, installed); problem != "" {
		t.Fatalf("the installed skill: %s", problem)
	}
	section, _ := sectionOfHeading(installed, submitShapeHeading)
	_, phrases := loadOldHowToSubmit(t)
	for _, problem := range submitPhraseProblems(phrases, reflectHowToSubmit, section) {
		t.Errorf("the installed skill: %s", problem)
	}
}

// The reply of reflect query carries the outline, through the command line and through MCP, and the outline is as long as the test says.
func TestReflectQueryRepliesCarryTheOutlineOfHowToSubmit(t *testing.T) {
	review := string(mustRead(t, reviewReference)) // read before the fixture moves the working directory
	if n := len(reflectHowToSubmit); n > reflectHowToSubmitMax || n+64 < reflectHowToSubmitMax {
		t.Fatalf("the outline is %d bytes; the test says reflectHowToSubmitMax = %d (above it, or more than 64 below it): read what the new text must keep in testdata/how_to_submit_before.tsv, then set the number to the length", n, reflectHowToSubmitMax)
	}
	st, scope, h := reviewFixture(t)
	reviewSeed(t, st, scope, 1)
	cli := map[string]any{}
	if err := json.Unmarshal([]byte(capture(t, func() error { return cmdReflect([]string{"query"}) })), &cli); err != nil {
		t.Fatal(err)
	}
	result, _ := mcpToolCall(t, h, "shikii_reflect", `{"operation":"query"}`)
	structured, _ := result["structuredContent"].(map[string]any)
	for name, reply := range map[string]map[string]any{"the command line": cli, "MCP": structured} {
		how, _ := reply["how_to_submit"].(string)
		if how != reflectHowToSubmit {
			t.Errorf("%s: how_to_submit is not the outline: %.200q", name, how)
		}
		if len(how) > reflectHowToSubmitMax || len(how) >= oldHowToSubmitBytes/2 {
			t.Errorf("%s: how_to_submit is %d bytes, over %d", name, len(how), reflectHowToSubmitMax)
		}
		if problem := howToSubmitPointerProblem(how, review); problem != "" {
			t.Errorf("%s: %s", name, problem)
		}
	}
}

var outlineShape = regexp.MustCompile(`\b(reviews|wiki|memory):\[\{([^}]*)\}\]`)

// requestJSONFields is the names of the JSON fields a request type reads.
func requestJSONFields(v any) []string {
	var names []string
	typ := reflect.TypeOf(v)
	for i := 0; i < typ.NumField(); i++ {
		if name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

// The fields the outline lists are the fields the requests read: reviews, memory, maintenance and correction as the whole of them, wiki
// as some of the many a draft has.
func TestTheFieldsTheOutlineListsAreTheFieldsOfTheRequests(t *testing.T) {
	shapes := map[string][]string{}
	for _, m := range outlineShape.FindAllStringSubmatch(reflectHowToSubmit, -1) {
		shapes[m[1]] = strings.Split(m[2], ",")
	}
	if m := regexp.MustCompile(`maintenance: send the latest ([a-z_, ]+?) and ([a-z_]+)[.;]`).FindStringSubmatch(reflectHowToSubmit); m != nil {
		shapes["maintenance"] = append(strings.Split(m[1], ", "), m[2])
	}
	if m := regexp.MustCompile(`correction:\{([^}]*)\}`).FindStringSubmatch(reflectHowToSubmit); m != nil {
		shapes["correction"] = strings.Split(m[1], ",")
	}
	for _, c := range []struct {
		shape string
		typ   any
		whole bool
	}{
		{"reviews", reviewRequest{}, true},
		{"memory", memoryAssessmentRequest{}, true},
		{"wiki", notebook.Draft{}, false},
		{"maintenance", notebook.MaintenanceReview{}, true},
		{"correction", store.Correction{}, true},
	} {
		// operation is what a reviews item can say of itself for the older entries; a submit does not need it
		listed, fields := shapes[c.shape], slices.DeleteFunc(requestJSONFields(c.typ), func(name string) bool { return name == "operation" })
		if len(listed) == 0 {
			t.Errorf("the outline lists no fields for %s", c.shape)
		}
		for _, name := range listed {
			if !slices.Contains(fields, name) {
				t.Errorf("the outline lists %s for %s, which its request does not read (it reads %v)", name, c.shape, fields)
			}
		}
		if c.whole {
			for _, name := range fields {
				if !slices.Contains(listed, name) {
					t.Errorf("the request of %s reads %s, which the outline does not list (it lists %v)", c.shape, name, listed)
				}
			}
		}
	}
}

// A submit that has the fields the outline lists is saved, through MCP and through the command line: the outline is the shape an
// agent sends from, so a field that is wrong in it (a name that is not read, one that is missing) is refused or left out here.
func TestASubmitBuiltFromTheOutlineIsSaved(t *testing.T) {
	shapes := map[string][]string{}
	for _, m := range outlineShape.FindAllStringSubmatch(reflectHowToSubmit, -1) {
		shapes[m[1]] = strings.Split(m[2], ",")
	}
	for name, send := range map[string]func(t *testing.T, h http.Handler, payload map[string]any) map[string]any{
		"over MCP": func(t *testing.T, h http.Handler, payload map[string]any) map[string]any {
			payload["operation"] = "submit"
			return reflectCall(t, h, payload)
		},
		"on the command line": func(t *testing.T, h http.Handler, payload map[string]any) map[string]any {
			out, err := commandStdin(t, payload, func() error { return cmdReflect([]string{"submit"}) })
			if err != nil {
				t.Fatal(err)
			}
			reply := map[string]any{}
			if err := json.Unmarshal([]byte(out), &reply); err != nil {
				t.Fatalf("the reply of the submit is JSON: %v %s", err, out)
			}
			return reply
		},
	} {
		t.Run(name, func(t *testing.T) {
			st, scope, h, draft, revision := memoryEffectFixture(t)
			memoryEffectPrompt(t, scope, "outline")
			turn := memoryEffectComplete(t, st, scope, "outline")
			conversation, trace := fmt.Sprintf("%016x", 1), fmt.Sprintf("%016x", 1001) // the conversation reviewSeed made, and its trace
			values := map[string]map[string]any{
				"reviews": {"source_id": conversation, "trace_id": trace, "truth": "unknown", "reason": "the outcome could not be checked"},
				"wiki": {"sources": []string{conversation}, "title": "A lesson from the outline", "lesson": "Check the outline of a submit against what is read.",
					"body": "The detail of the lesson.", "applies_to": "when the outline of a submit is changed", "related": []string{}, "kind": "rule", "scene": "when a text is changed"},
				"memory": {"source_id": turn, "key": "lesson:" + draft.ID + ":" + revision, "result": "unknown", "decision": "the work went the way the lesson says",
					"evidence": "no record shows what the lesson changed", "correction_result": "followed"},
			}
			payload := map[string]any{}
			for _, shape := range []string{"reviews", "wiki", "memory"} {
				item := map[string]any{}
				for _, field := range shapes[shape] {
					value, ok := values[shape][field]
					if !ok {
						t.Fatalf("the outline lists %s in %s, which this test cannot fill: add a value for it, or take it out of the outline", field, shape)
					}
					item[field] = value
				}
				for field := range values[shape] {
					if !slices.Contains(shapes[shape], field) {
						t.Fatalf("the outline does not list %s in %s (it lists %v), and a submit needs it", field, shape, shapes[shape])
					}
				}
				payload[shape] = []any{item}
			}
			reply := send(t, h, payload)
			if reply["complete"] != true {
				t.Fatalf("a submit with the fields the outline lists is not saved: %v", reply)
			}
			for _, shape := range []string{"reviews", "wiki", "memory"} {
				items, _ := reply[shape].([]any)
				if len(items) != 1 {
					t.Fatalf("%s: one item was sent and the reply has %d receipts: %v", shape, len(items), reply)
				}
				if text, _ := items[0].(map[string]any)["error"].(string); text != "" {
					t.Errorf("%s: the item is refused: %s", shape, text)
				}
			}
		})
	}
}
