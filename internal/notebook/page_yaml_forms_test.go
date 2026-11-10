package notebook

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

type yamlForm struct{ name, text string }

// yamlSpecialForms are texts that mean something to a YAML reader or writer.
// limit is the longest value the field under test accepts.
func yamlSpecialForms(limit int) []yamlForm {
	long := func(unit string) string {
		return strings.TrimRight(strings.Repeat(unit, limit/len(unit)), " \n")
	}
	return []yamlForm{
		{"plain text", "plain text"},
		{"empty", ""},
		{"only a space", " "},
		{"sequence entry", "- item"},
		{"only a dash", "-"},
		{"mapping entry", "key: value"},
		{"ends with a colon", "key:"},
		{"colon and space inside", "a: b: c"},
		{"comment", "# comment"},
		{"comment after text", "text # comment"},
		{"literal indicator", "| text"},
		{"folded indicator", "> text"},
		{"literal indicator with a keep sign", "|+"},
		{"tag", "!tag text"},
		{"binary tag", "!!binary aGVsbG8="},
		{"anchor", "&anchor text"},
		{"alias", "*alias"},
		{"directive", "%YAML 1.2"},
		{"at sign", "@text"},
		{"backtick", "`text`"},
		{"double quoted", `"quoted"`},
		{"single quoted", "'quoted'"},
		{"unclosed double quote", `"open`},
		{"unclosed single quote", "'open"},
		{"quotes and a backslash", `a "b" 'c' \n \x41 \\`},
		{"flow mapping", "{a: b}"},
		{"flow sequence", "[a, b]"},
		{"unclosed flow", "[a, {b"},
		{"complex key", "? key"},
		{"leading comma", ", text"},
		{"merge key", "<<"},
		{"equals", "="},
		{"leading space", " text"},
		{"trailing space", "text "},
		{"leading tab", "\ttext"},
		{"tab inside", "a\tb"},
		{"trailing tab", "text\t"},
		{"CRLF", "a\r\nb"},
		{"lone CR", "a\rb"},
		{"ends with CRLF", "a\r\n"},
		{"NEL", "a\u0085b"},
		{"leading NEL", "\u0085a"},
		{"line separator", "a\u2028b"},
		{"paragraph separator", "a\u2029b"},
		{"leading BOM", "\ufeffa"},
		{"BOM inside", "a\ufeffb"},
		{"no-break space at both ends", "\u00a0a\u00a0"},
		{"ideographic space at both ends", "\u3000a\u3000"},
		{"NUL", "a\x00b"},
		{"control character", "a\x01b"},
		{"escape character", "a\x1bb"},
		{"delete character", "a\x7fb"},
		{"invalid UTF-8", "a\xffb"},
		{"outside the BMP", "a\U0001F600b"},
		{"Japanese with a colon", "\u65e5\u672c\u8a9e: \u5024 # \u6ce8"},
		{"document start", "---"},
		{"document start with text", "--- text"},
		{"document start on a later line", "a\n---\nb"},
		{"document start then text", "---\nb"},
		{"document end", "..."},
		{"document end on a later line", "a\n...\nb"},
		{"integer", "123"},
		{"leading zero", "0123"},
		{"float", "1.5"},
		{"exponent", "1e10"},
		{"hexadecimal", "0x1F"},
		{"octal", "0o17"},
		{"underscored number", "1_000"},
		{"signed number", "+1"},
		{"negative number", "-1"},
		{"dot number", ".5"},
		{"infinity", ".inf"},
		{"not a number", ".nan"},
		{"sexagesimal", "1:30"},
		{"date", "2026-11-10"},
		{"timestamp", "2026-11-10T00:00:00Z"},
		{"true", "true"},
		{"True", "True"},
		{"yes", "yes"},
		{"no", "no"},
		{"on", "on"},
		{"off", "off"},
		{"y", "y"},
		{"n", "n"},
		{"null", "null"},
		{"Null", "Null"},
		{"NULL", "NULL"},
		{"tilde", "~"},
		{"two lines", "a\nb"},
		{"blank line inside", "a\n\nb"},
		{"ends with a line break", "a\n"},
		{"ends with two line breaks", "a\n\n"},
		{"two lines ending with a line break", "a\nb\n"},
		{"space before a line break", "a \nb"},
		{"space after a line break", "a\n b"},
		{"tab after a line break", "a\n\tb"},
		{"space before the last line break", "a\nb \n"},
		{"leading space on the first of two lines", " a\nb"},
		{"leading line break", "\na"},
		{"leading blank lines", "\n\na\nb"},
		{"leading tab on the first of two lines", "\ta\nb"},
		{"list on later lines", "a\n- b\n- c"},
		{"list on every line", "- a\n- b"},
		{"indented list on later lines", "a\n  - b\n  - c"},
		{"comment on a later line", "a\n# b"},
		{"mapping over two lines", "a:\n  b: c"},
		{"block scalar inside", "key: |\n  text\nnext: >\n  text"},
		{"two lines with CRLF", "a\r\nb\r\nc"},
		{"two lines with a colon at the end", "a:\nb:"},
		{"long line without spaces", long("x")},
		{"long line of words", long("word ")},
		{"long line of mapping entries", long("key: value # c ")},
		{"long line of quotes", long(`"' `)},
		{"many short lines", long("line\n")},
		{"long Japanese line", long("\u65e5\u672c\u8a9e")},
	}
}

type yamlFormBook struct {
	t      *testing.T
	b      Book
	source string
	n      int
	fixed  []Page
}

func newYAMLFormBook(t *testing.T) *yamlFormBook {
	t.Helper()
	e := &yamlFormBook{t: t, b: Book{Root: t.TempDir(), Scope: t.TempDir()}}
	e.source = roundTripSource(t, e.b, 1)
	return e
}

func (e *yamlFormBook) draft() Draft {
	e.n++
	return Draft{Title: fmt.Sprintf("Round trip case %d", e.n), Lesson: "Keep the saved text exactly.", AppliesTo: "saving wiki text", Kind: "rule", Scene: "when saving text", Sources: []string{e.source}}
}

// conversation records a person's reply, which a correction must quote.
func (e *yamlFormBook) conversation(reply string) (string, bool) {
	e.n++
	id := fmt.Sprintf("%016x", 0xc0000000+e.n)
	err := (store.Store{Dir: e.b.Root}).AppendFeedback(e.b.Scope, store.Feedback{ID: id, Request: "reorder the steps", Summary: "reordered", Reply: reply})
	return id, err == nil
}

// experience saves another article whose body holds quote exactly once.
func (e *yamlFormBook) experience(quote string) (Page, bool) {
	d := e.draft()
	body := "BODYSTART " + quote + " BODYEND"
	d.Body, d.Kind = &body, "fact"
	r, err := e.b.Ingest(d)
	if err != nil {
		return Page{}, false
	}
	return *r.Page, true
}

// experiences returns two fixed articles a synthesis can connect.
func (e *yamlFormBook) experiences() []Page {
	e.t.Helper()
	for len(e.fixed) < 2 {
		p, ok := e.experience(fmt.Sprintf("FIXED-EVIDENCE-%d", len(e.fixed)))
		if !ok {
			e.t.Fatal("cannot prepare a connected experience")
		}
		e.fixed = append(e.fixed, p)
	}
	return e.fixed
}

func connect(p Page, quote string) ExperienceConnection {
	return ExperienceConnection{ID: p.ID, Revision: p.Revision, Quote: quote, Conditions: "the same parser", Reason: "the same failure", Relation: "supports"}
}

type yamlFormField struct {
	name  string
	limit int
	trim  bool              // the save trims surrounding white space
	stop  func(string) bool // the texts the save must refuse; every other text must be saved
	at    func(d *Draft) *string
	get   func(p Page) string
}

// yamlFormGroup is a set of fields one draft can carry together. Saving costs
// file-system calls, so a form is first written into every field of the group
// at once; only a refused form is tried again one field at a time.
type yamlFormGroup struct {
	name   string
	base   func(e *yamlFormBook) Draft
	finish func(e *yamlFormBook, d *Draft) bool // what depends on the values; false when a prerequisite is refused
	fields []yamlFormField
}

func yamlFormGroups() []yamlFormGroup {
	passage := func(d *Draft) *KnowledgePassage { return &(*d.Passages)[0] }
	probe := func(d *Draft) *KnowledgeProbe { return d.Learning.Probe }
	step := func(d *Draft) *PracticeStep { return &d.Learning.Practice.Steps[0] }
	connection := func(d *Draft) *ExperienceConnection { return &d.Learning.Connections[0] }
	any := func(stops ...func(string) bool) func(string) bool {
		return func(s string) bool {
			return slices.ContainsFunc(stops, func(stop func(string) bool) bool { return stop(s) })
		}
	}
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	invalid := func(s string) bool { return !utf8.ValidString(s) }
	control := func(s string) bool { return strings.ContainsFunc(strings.TrimSpace(s), unicode.IsControl) }
	// The YAML encoder cannot write these multi-line texts so that they read back
	// (storablePage): one that starts with a line break or a tab, and inside a
	// list also one that starts with a space.
	leadingBreak := func(s string) bool { return strings.Contains(s, "\n") && (s[0] == '\n' || s[0] == '\t') }
	leadingSpace := func(s string) bool { return strings.Contains(s, "\n") && s[0] == ' ' }
	required, optional := any(blank, invalid), invalid
	inMapping := any(blank, invalid, leadingBreak)
	inList := any(blank, invalid, leadingBreak, leadingSpace)
	return []yamlFormGroup{
		{name: "top level", base: func(e *yamlFormBook) Draft {
			d := e.draft()
			body, rationale := "The default body.", "The default rationale."
			d.Body, d.Rationale, d.ChangeReason = &body, &rationale, "the default reason"
			return d
		}, fields: []yamlFormField{
			{"title", 200, true, required, func(d *Draft) *string { return &d.Title }, func(p Page) string { return p.Title }},
			{"lesson", 2000, true, required, func(d *Draft) *string { return &d.Lesson }, func(p Page) string { return p.Lesson }},
			{"applies_to", 1000, true, required, func(d *Draft) *string { return &d.AppliesTo }, func(p Page) string { return p.AppliesTo }},
			{"body", 16 << 10, true, optional, func(d *Draft) *string { return d.Body }, func(p Page) string { return p.Body }},
			{"rationale", 1600, true, optional, func(d *Draft) *string { return d.Rationale }, func(p Page) string { return p.Rationale }},
			{"change_reason", 1000, false, any(invalid, leadingBreak), func(d *Draft) *string { return &d.ChangeReason }, func(p Page) string { return p.ChangeReason }},
		}},
		{name: "scene", base: (*yamlFormBook).draft, fields: []yamlFormField{
			{"scene", SceneMax, true, any(blank, invalid, control), func(d *Draft) *string { return &d.Scene }, func(p Page) string { return p.Scene }},
		}},
		{name: "correction, passages, probe and practice", base: func(e *yamlFormBook) Draft {
			d := e.draft()
			d.Kind = "howto"
			d.Correction = &store.Correction{Quote: "the other way round", Action: "Run the focused test first."}
			d.Passages = &[]KnowledgePassage{{Role: "current", Quote: "FIXED-QUOTE", Conditions: "before a commit", Reason: "the order matters"}}
			d.Learning = &Learning{
				Probe:    &KnowledgeProbe{Question: "Does the order matter?", Method: "Run both orders.", ReconsiderWhen: "the suite changes", Results: []ProbeResult{{Conditions: "on the current suite", Conclusion: "the order matters", Assessment: "supports", Sources: []string{e.source}}}},
				Practice: &KnowledgePractice{Steps: []PracticeStep{{Condition: "before a commit", Action: "run the focused test", Avoid: "skipping it", Check: "it passes", Sources: []string{e.source}}}},
			}
			return d
		}, finish: func(e *yamlFormBook, d *Draft) bool {
			body := "BODYSTART " + passage(d).Quote + " BODYEND"
			d.Body = &body
			id, ok := e.conversation(d.Correction.Quote) // a correction quotes the person's recorded reply
			d.Correction.SourceID, d.Sources = id, append(d.Sources, id)
			return ok
		}, fields: []yamlFormField{
			{"correction.quote", 400, false, inMapping, func(d *Draft) *string { return &d.Correction.Quote }, func(p Page) string { return p.Correction.Quote }},
			{"correction.action", 400, false, inMapping, func(d *Draft) *string { return &d.Correction.Action }, func(p Page) string { return p.Correction.Action }},
			{"passages.quote", 600, false, inList, func(d *Draft) *string { return &passage(d).Quote }, func(p Page) string { return p.Passages[0].Quote }},
			{"passages.conditions", 400, false, inList, func(d *Draft) *string { return &passage(d).Conditions }, func(p Page) string { return p.Passages[0].Conditions }},
			{"passages.reason", 400, false, inList, func(d *Draft) *string { return &passage(d).Reason }, func(p Page) string { return p.Passages[0].Reason }},
			{"learning.probe.question", 600, false, inMapping, func(d *Draft) *string { return &probe(d).Question }, func(p Page) string { return p.Learning.Probe.Question }},
			{"learning.probe.method", 1000, false, inMapping, func(d *Draft) *string { return &probe(d).Method }, func(p Page) string { return p.Learning.Probe.Method }},
			{"learning.probe.reconsider_when", 600, false, inMapping, func(d *Draft) *string { return &probe(d).ReconsiderWhen }, func(p Page) string { return p.Learning.Probe.ReconsiderWhen }},
			{"learning.probe.results.conditions", 600, false, inList, func(d *Draft) *string { return &probe(d).Results[0].Conditions }, func(p Page) string { return p.Learning.Probe.Results[0].Conditions }},
			{"learning.probe.results.conclusion", 1000, false, inList, func(d *Draft) *string { return &probe(d).Results[0].Conclusion }, func(p Page) string { return p.Learning.Probe.Results[0].Conclusion }},
			{"learning.practice.steps.condition", 400, false, inList, func(d *Draft) *string { return &step(d).Condition }, func(p Page) string { return p.Learning.Practice.Steps[0].Condition }},
			{"learning.practice.steps.action", 600, false, inList, func(d *Draft) *string { return &step(d).Action }, func(p Page) string { return p.Learning.Practice.Steps[0].Action }},
			{"learning.practice.steps.avoid", 400, false, inList, func(d *Draft) *string { return &step(d).Avoid }, func(p Page) string { return p.Learning.Practice.Steps[0].Avoid }},
			{"learning.practice.steps.check", 600, false, inList, func(d *Draft) *string { return &step(d).Check }, func(p Page) string { return p.Learning.Practice.Steps[0].Check }},
		}},
		{name: "connections", base: func(e *yamlFormBook) Draft {
			d := e.draft()
			fixed := e.experiences()
			d.Learning = &Learning{Connections: []ExperienceConnection{connect(fixed[0], "FIXED-EVIDENCE-0"), connect(fixed[1], "FIXED-EVIDENCE-1")}}
			return d
		}, finish: func(e *yamlFormBook, d *Draft) bool {
			c := connection(d)
			if c.Quote == "FIXED-EVIDENCE-0" {
				return true
			}
			cited, ok := e.experience(c.Quote) // the quote must come from the body of the connected article
			c.ID, c.Revision = cited.ID, cited.Revision
			return ok
		}, fields: []yamlFormField{
			{"learning.connections.quote", 600, false, inList, func(d *Draft) *string { return &connection(d).Quote }, func(p Page) string { return p.Learning.Connections[0].Quote }},
			{"learning.connections.conditions", 400, false, inList, func(d *Draft) *string { return &connection(d).Conditions }, func(p Page) string { return p.Learning.Connections[0].Conditions }},
			{"learning.connections.reason", 400, false, inList, func(d *Draft) *string { return &connection(d).Reason }, func(p Page) string { return p.Learning.Connections[0].Reason }},
		}},
	}
}

// requireReadable fails when the wiki holds a page that no longer reads back.
func requireReadable(t *testing.T, b Book, what string, saveErr error) bool {
	t.Helper()
	issues, err := b.PageIssues()
	if err != nil || len(issues) > 0 {
		t.Errorf("%s: save error=%v, but the wiki now has unreadable pages: %+v %v", what, saveErr, issues, err)
		return false
	}
	return true
}

// requireReadsBack fails unless the saved page is found again at its revision.
func requireReadsBack(t *testing.T, b Book, what string, saved Page) (Page, bool) {
	t.Helper()
	v, err := b.Query("lesson:" + saved.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].Revision != saved.Revision {
		t.Errorf("%s: saved as %s@%s but it does not read back: %v %+v", what, saved.ID, saved.Revision, err, v.Issues)
		return Page{}, false
	}
	return v.Lessons[0], true
}

// A save either is refused and leaves the wiki as it was, or succeeds and the
// page reads back as the same revision with the text that was sent. A text that
// looks like YAML must never be saved as a page that cannot be read again, and
// only the texts each field names (stop) may be refused.
func TestSavedPageTextSurvivesYAMLSpecialForms(t *testing.T) {
	short := func(s string) string {
		if len(s) > 40 {
			return fmt.Sprintf("%q... (%d bytes)", s[:40], len(s))
		}
		return fmt.Sprintf("%q", s)
	}
	for _, g := range yamlFormGroups() {
		t.Run(g.name, func(t *testing.T) {
			e := newYAMLFormBook(t)
			forms := make([][]yamlForm, len(g.fields)) // the long forms depend on each field's limit
			all := make([]int, len(g.fields))
			for i, f := range g.fields {
				forms[i], all[i] = yamlSpecialForms(f.limit), i
			}
			// save writes form n into the given fields and reports whether the page was saved.
			save := func(fields []int, n int) bool {
				d := g.base(e)
				names := []string{}
				for _, i := range fields {
					*g.fields[i].at(&d) = forms[i][n].text
					names = append(names, g.fields[i].name)
				}
				what := strings.Join(names, ", ") + " = " + forms[fields[0]][n].name + " " + short(forms[fields[0]][n].text)
				var result Result
				var err error
				ready := g.finish == nil || g.finish(e, &d)
				if ready {
					result, err = e.b.Ingest(d)
				}
				if !requireReadable(t, e.b, what, err) {
					e = newYAMLFormBook(t) // keep checking the other forms on a clean wiki
					return false
				}
				if !ready || err != nil {
					return false
				}
				back, ok := requireReadsBack(t, e.b, what, *result.Page)
				if !ok {
					return false
				}
				for _, i := range fields {
					want := forms[i][n].text
					if g.fields[i].trim {
						want = strings.TrimSpace(want)
					}
					if got := g.fields[i].get(back); got != want {
						t.Errorf("%s: %s read back as %s", what, g.fields[i].name, short(got))
					}
				}
				return true
			}
			for n := range forms[0] {
				if n%25 == 24 { // every save reads all pages: keep each wiki small
					if _, err := e.b.Ingest(e.draft()); err != nil {
						t.Errorf("%s: a later unrelated save is refused: %v", g.name, err)
					}
					e = newYAMLFormBook(t)
				}
				together := save(all, n)
				for _, i := range all {
					f, form := g.fields[i], forms[i][n]
					saved := together || len(all) > 1 && save([]int{i}, n)
					if saved == f.stop(form.text) {
						t.Errorf("%s = %s %s: saved=%v, want saved=%v", f.name, form.name, short(form.text), saved, !saved)
					}
				}
			}
			if _, err := e.b.Ingest(e.draft()); err != nil {
				t.Errorf("%s: a later unrelated save is refused: %v", g.name, err)
			}
			for i, f := range g.fields {
				refused := []string{}
				for _, form := range forms[i] {
					if f.stop(form.text) {
						refused = append(refused, form.name)
					}
				}
				t.Logf("%s: %d forms are saved and read back, %d are refused before saving (%s)", f.name, len(forms[i])-len(refused), len(refused), strings.Join(refused, "; "))
			}
		})
	}
}

// A watched file path is written into the page as it is named on disk. Only a
// hidden path segment and a control character are refused.
func TestSavedPageWithYAMLLikeWatchedPathsReadsBack(t *testing.T) {
	stop := []string{"...", "a\u0085b"}
	names := []string{"- item.md", "key: value.md", "# comment.md", "| text", "> text", "!tag", "&anchor", "*alias", "%YAML", "@at", "`tick`", `"quoted"`, "'quoted'", "{a: b}", "[a, b]", "? key", ", text", "---", "...", "123", "1e10", "0x1F", "true", "yes", "no", "null", "~", "<<", "text ", " text", "a\u2028b", "a\u0085b", "\ufeffa", "\u65e5\u672c\u8a9e: \u5024.md", "dir/- item.md", "on/null", strings.Repeat("x", 250)}
	b := Book{Root: t.TempDir(), Scope: t.TempDir()}
	source := roundTripSource(t, b, 1)
	if err := os.WriteFile(filepath.Join(b.Scope, "plain.md"), []byte("plain\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		what := fmt.Sprintf("watch = %q", name)
		path := filepath.Join(b.Scope, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("watched\n"), 0o600); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		d := Draft{Title: fmt.Sprintf("Watched path %d", i), Lesson: "Recheck when the file changes.", AppliesTo: "editing the watched file", Kind: "fact", Scene: "when editing the file", Sources: []string{source}, Watch: &[]string{name, "plain.md"}}
		r, err := b.Ingest(d)
		if !requireReadable(t, b, what, err) {
			b = Book{Root: t.TempDir(), Scope: b.Scope}
			source = roundTripSource(t, b, 1)
			continue
		}
		if saved := err == nil; saved == slices.Contains(stop, name) {
			t.Errorf("%s: saved=%v (%v), want saved=%v", what, saved, err, !saved)
		}
		if err != nil {
			continue
		}
		back, ok := requireReadsBack(t, b, what, *r.Page)
		if !ok {
			continue
		}
		if len(back.Premises) != 2 || back.Premises[0].Path != name || len(back.Recheck) != 0 {
			t.Errorf("%s: premises read back as %+v, recheck %+v", what, back.Premises, back.Recheck)
		}
	}
	t.Logf("watch: %d paths are saved and read back, %d are refused before saving (%q)", len(names)-len(stop), len(stop), stop)
}
