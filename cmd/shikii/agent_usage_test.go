package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// cliCall runs a command the way main does, for the commands an agent calls.
func cliCall(args []string) error {
	switch args[0] {
	case "work":
		return cmdWork(args[1:])
	case "reflect":
		return cmdReflect(args[1:])
	case "hook":
		return cmdHook(args[1:])
	case "wiki":
		return cmdWiki(args[1:])
	case "handoff":
		return cmdHandoff(args[1:])
	}
	return fmt.Errorf("not a command an agent calls: %v", args)
}

type usageForm struct {
	args  []string
	input string
}

var usagePlaceholder = regexp.MustCompile(`<[^<>]+>`)

// usageForms reads the runnable forms out of a usage text: a line `shikii <arguments>`, alone or ending in <<'TAG' and followed
// by the standard input up to a line TAG. A <placeholder> is one argument even when it holds spaces.
func usageForms(text string) []usageForm {
	var forms []usageForm
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "shikii ") {
			continue
		}
		form := usageForm{}
		tag := ""
		if at := strings.Index(line, " <<'"); at >= 0 {
			tag = strings.TrimSuffix(line[at+len(" <<'"):], "'")
			line = line[:at]
		}
		depth := 0
		var token strings.Builder
		for _, r := range strings.TrimPrefix(line, "shikii ") + " " {
			switch {
			case r == '<':
				depth++
			case r == '>' && depth > 0:
				depth--
			}
			if r == ' ' && depth == 0 {
				if token.Len() > 0 {
					form.args = append(form.args, token.String())
					token.Reset()
				}
				continue
			}
			token.WriteRune(r)
		}
		if tag != "" {
			var input []string
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != tag; i++ {
				input = append(input, strings.TrimSpace(lines[i]))
			}
			form.input = strings.Join(input, "\n") + "\n"
		}
		forms = append(forms, form)
	}
	return forms
}

// Counted on 2026-11-11 in this machine's Claude Code and Codex sessions: the most frequent refused call was `shikii work prepare
// --help` (and work --help, work record --help, reflect query --help, wiki --help, wiki capture --help, handoff --help), tried at
// the start of nearly every session because the hook notice names the command but not how the JSON reaches it. Each answered with
// one line naming the subcommands, so the next call was a guess: the JSON as an argument, --json, --query, project inside it.
// Help now prints the forms, and this test runs every form the help prints, as written, through the real command: a form that the
// command would refuse cannot stay in the text.
func TestHelpPrintsFormsThatRunAsWritten(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"<session_id>": "planning", "<source id>": source.ID, "<page id>": page.Page.ID, "<now, RFC 3339>": time.Now().UTC().Format(time.RFC3339)}
	fill := func(text string) string {
		for name, value := range values {
			text = strings.ReplaceAll(text, name, value)
		}
		return usagePlaceholder.ReplaceAllString(text, "csv-export")
	}
	for _, c := range []struct {
		command []string
		want    map[string]int // the forms the help must carry, by their first arguments, with the least number of each
	}{
		{[]string{"work"}, map[string]int{"work prepare": 3, "work record": 1}},
		{[]string{"hook", "plan"}, map[string]int{"hook plan query": 1}},
		{[]string{"reflect"}, map[string]int{"reflect query": 1, "reflect place": 1}},
		{[]string{"wiki"}, map[string]int{"wiki query": 3, "wiki capture": 1, "wiki ingest": 1}},
		{[]string{"handoff"}, map[string]int{"handoff list": 1, "handoff read": 1, "handoff write": 1, "handoff append": 1}},
	} {
		name := strings.Join(c.command, " ")
		help, err := captureErr(func() error { return cliCall(append(append([]string{}, c.command...), "--help")) })
		if err != nil || !strings.HasPrefix(help, "usage: shikii "+name+" ") {
			t.Errorf("shikii %s --help prints the forms on standard output and succeeds: %v %q", name, err, help)
			continue
		}
		// a new request, so each command's forms meet the session a hook notice would name
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest + " (" + name + ")"})
		values["<turn_id>"] = loadSession(root, "planning").TurnID
		got := map[string]int{}
		for _, form := range usageForms(help) {
			args := make([]string, len(form.args))
			for i, arg := range form.args {
				args[i] = fill(arg)
			}
			var out string
			var runErr error
			withStdin(t, fill(form.input), func() { out, runErr = captureErr(func() error { return cliCall(args) }) })
			if runErr != nil {
				t.Errorf("a form printed by shikii %s --help is refused as written: shikii %s <<< %s: %v", name, strings.Join(form.args, " "), form.input, runErr)
				continue
			}
			got[strings.Join(form.args[:len(c.command)+1], " ")]++
			var reply map[string]any
			if json.Unmarshal([]byte(out), &reply) == nil {
				if receipt, ok := reply["receipt"].(string); ok && receipt != "" {
					values["<receipt>"] = receipt
				}
			}
		}
		for form, least := range c.want {
			if got[form] < least {
				t.Errorf("shikii %s --help carries %d runnable forms of shikii %s, want at least %d:\n%s", name, got[form], form, least, help)
			}
		}
	}
	// the other spellings of asking for help give the same text
	want, _ := captureErr(func() error { return cliCall([]string{"work", "--help"}) })
	for _, args := range [][]string{{"work", "-h"}, {"work", "help"}, {"work", "prepare", "--help"}, {"work", "record", "-h"}, {"work", "prepare", "help"}} {
		if got, err := captureErr(func() error { return cliCall(args) }); err != nil || got != want || want == "" {
			t.Errorf("shikii %s prints the same help: %v %q", strings.Join(args, " "), err, got)
		}
	}
}

// A call the command cannot read is answered with what was wrong and the same forms, so the next call can be right.
func TestACallThatCannotBeReadIsAnsweredWithTheForms(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	t.Chdir(repo)
	for _, c := range []struct {
		name  string
		args  []string
		input string
		want  []string
	}{
		{"the JSON as an argument", []string{"work", "prepare", `{"query":"csv","request":"check csv"}`}, "",
			[]string{"on standard input, not as an argument", "usage: shikii work prepare | record", `{"session_id":"<session_id>","turn_id":"<turn_id>","query":"<topic words>"}`}},
		{"a flag", []string{"work", "prepare", "--query", "csv"}, "",
			[]string{"work takes no flags", "usage: shikii work prepare | record", `"query":"<topic words>"`}},
		{"no operation", []string{"work"}, "", []string{"usage: shikii work prepare | record", "shikii work record <<'JSON'"}},
		{"an operation work does not have", []string{"work", "evidence"}, "", []string{`work has no operation "evidence"`, "usage: shikii work prepare | record"}},
		{"nothing on standard input", []string{"work", "prepare"}, "  \n", []string{"standard input was empty", "usage: shikii work prepare | record", "shikii work prepare <<'JSON'"}},
		{"nothing on standard input at the end", []string{"work", "record"}, "", []string{"standard input was empty", `"result":{"changed":`}},
		{"reflect query with an id", []string{"reflect", "query", "0123456789abcdef"}, "", []string{"query takes no arguments", "shikii wiki query <source id>", "usage: shikii reflect query | place | submit"}},
		{"reflect place with nothing on standard input", []string{"reflect", "place"}, "", []string{"standard input was empty", `{"wiki":[{`}},
		{"hook plan with nothing on standard input", []string{"hook", "plan", "query"}, "", []string{"standard input was empty", "usage: shikii hook plan query | submit", `"turn_id":"<turn_id>"`}},
		{"wiki alone", []string{"wiki"}, "", []string{"usage: shikii wiki ", "shikii wiki query <search words>", "shikii wiki capture <<'JSON'", "shikii wiki ingest <<'JSON'"}},
		{"a wiki operation that does not exist", []string{"wiki", "search", "csv"}, "", []string{`wiki has no operation "search"`, "shikii wiki query <search words>"}},
		{"the MCP object piped to wiki", []string{"wiki"}, `{"operation":"query","query":"lesson:0123456789abcdef"}`, []string{"usage: shikii wiki ", "shikii wiki query lesson:<page id>"}},
		{"wiki capture with arguments", []string{"wiki", "capture", "--title", "t"}, "", []string{"wiki capture reads one JSON object from standard input", `"observed_at":"<now, RFC 3339>"`}},
		{"wiki capture with nothing on standard input", []string{"wiki", "capture"}, "", []string{"standard input was empty", `"observed_at":"<now, RFC 3339>"`}},
		{"wiki ingest with nothing on standard input", []string{"wiki", "ingest"}, "", []string{"standard input was empty", `"sources":["<source id>"]`}},
		{"handoff alone", []string{"handoff"}, "", []string{"usage: shikii handoff list | read <path> | write <path> | append <path>", "shikii handoff write <work>/report.md <<'EOF'"}},
		{"handoff read without a path", []string{"handoff", "read"}, "", []string{"usage: shikii handoff list", "shikii handoff read <work>/report.md"}},
	} {
		var err error
		withStdin(t, c.input, func() { _, err = captureErr(func() error { return cliCall(c.args) }) })
		if err == nil {
			t.Errorf("%s: shikii %s must be refused", c.name, strings.Join(c.args, " "))
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal of shikii %s lacks %q:\n%s", c.name, strings.Join(c.args, " "), w, err)
			}
		}
	}
	// wiki verify keeps its own usage, and asking wiki for help does not run a search for the word
	if _, err := captureErr(func() error { return cliCall([]string{"wiki", "verify", "--help"}) }); err == nil || strings.Contains(err.Error(), "shikii wiki capture") {
		t.Errorf("wiki verify --help is the flag help of verify: %v", err)
	}
	if out, err := captureErr(func() error { return cliCall([]string{"wiki", "query", "--help"}) }); err != nil || !strings.HasPrefix(out, "usage: shikii wiki ") {
		t.Errorf("shikii wiki query --help prints the forms: %v %q", err, out)
	}
}

// The notice every request gets named the command and this turn's ids, but not how they reach it ("Pass the identifiers below to
// shikii work prepare with topic words in query"), which is why sessions began by asking the command for help. It now says that the
// JSON goes on standard input, and the JSON line it gives, with query added, is accepted by the command as it is.
func TestPlanningNoticeSaysHowItsJSONReachesTheCommand(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest}))
	for _, w := range []string{"on standard input to ", " work prepare", "with topic words in query added", "End with work record the same way"} {
		if !strings.Contains(notice, w) {
			t.Errorf("the notice lacks %q: %s", w, notice)
		}
	}
	var ids map[string]any
	for _, line := range strings.Split(notice, "\n") {
		if strings.HasPrefix(line, `{"session_id":`) {
			if err := json.Unmarshal([]byte(line), &ids); err != nil {
				t.Fatalf("the ids line is one JSON object: %v %s", err, line)
			}
		}
	}
	if ids == nil {
		t.Fatalf("the notice gives this turn's ids as a JSON line: %s", notice)
	}
	ids["query"] = "CSV export filters"
	input, _ := json.Marshal(ids)
	var out string
	var err error
	withStdin(t, string(input), func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
	var reply map[string]any
	if err != nil || json.Unmarshal([]byte(out), &reply) != nil || reply["receipt"] == nil {
		t.Fatalf("the notice's JSON with query added prepares the task: %v %s", err, out)
	}
}

// The skill and the guide say what the commands now do: where the forms are, the CLI's bound, the order of a reply, and that a
// refusal names the place of an unknown field. (The bound is the constant itself, so the text cannot keep an old number.)
func TestTheSkillAndTheGuideDescribeTheFormsTheBoundAndTheOrder(t *testing.T) {
	bound := fmt.Sprint(cliPrepareBytes)
	for path, want := range map[string][]string{
		"../../skills/shikii/references/recording.md": {"`shikii work --help` prints these forms", bound + " bytes on the CLI", "starts with `receipt`", "the object that held it and the fields that object takes"},
		"../../docs/guide.md":                         {"`shikii work --help`", "on standard input", bound + " bytes", "starts with `receipt`", "the object that held it and the fields that object takes"},
		"../../docs/guide.ja.md":                      {"`shikii work --help`", bound, "`receipt`"},
	} {
		text := string(mustRead(t, path))
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q", path, w)
			}
		}
	}
}

// What the audit of 0.31.0 found in the entries an agent calls (B-A4, B-I2, B-U4, B-U6, A-4): a form pasted with the indentation
// help printed it with did not end its here-document; handoff answered a call it could not read with the forms alone; handoff
// write replaced a record with nothing when its standard input was empty; a wrongly typed value was named notes.0.sources while
// an unknown key was named notes[0]; and work answered malformed JSON in encoding/json's words.
func TestTheEntriesSayWhatIsWrongAndHelpPastesAsShown(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	// a form with standard input starts at the first column, with its text and its closing tag
	for name, usage := range agentUsages {
		lines := strings.Split(usage.text(), "\n")
		for i, line := range lines {
			at := strings.Index(line, " <<'")
			if at < 0 {
				continue
			}
			tag := strings.TrimSuffix(line[at+len(" <<'"):], "'")
			if !strings.HasPrefix(line, "shikii ") {
				t.Errorf("%s: a form with a here-document starts at the first column: %q", name, line)
			}
			closed := false
			for _, next := range lines[i+1:] {
				if next == tag {
					closed = true
					break
				}
				if strings.TrimSpace(next) == tag || strings.HasPrefix(next, " ") {
					t.Errorf("%s: the text and the closing tag of %q are not indented: %q", name, line, next)
					break
				}
			}
			if !closed {
				t.Errorf("%s: the here-document of %q is closed by a line that is exactly %s", name, line, tag)
			}
		}
	}
	// handoff says what is wrong, then the forms
	for call, problem := range map[string]string{
		"delete x":       `handoff has no operation "delete"`,
		"read":           "handoff read needs the path of a work record",
		"write a b":      "handoff write takes one path (the text goes on standard input, not as an argument)",
		"write --path a": "handoff write takes no flags",
		"list x":         "handoff list takes no arguments",
	} {
		err := cmdHandoff(strings.Fields(call))
		if err == nil || !strings.HasPrefix(err.Error(), problem+"\nusage: shikii handoff ") {
			t.Errorf("shikii handoff %s says what is wrong and then the forms: %v", call, err)
		}
	}
	// an empty standard input does not replace a record
	withStdin(t, "The report.\n", func() {
		if _, err := captureErr(func() error { return cmdHandoff([]string{"write", "audit/report.md"}) }); err != nil {
			t.Fatal(err)
		}
	})
	for _, operation := range []string{"write", "append"} {
		for _, input := range []string{"", " \n"} {
			withStdin(t, input, func() {
				_, err := captureErr(func() error { return cmdHandoff([]string{operation, "audit/report.md"}) })
				if err == nil || !strings.Contains(err.Error(), "standard input was empty") || !strings.Contains(err.Error(), "nothing was written") {
					t.Errorf("handoff %s with an empty standard input is refused: %v", operation, err)
				}
			})
		}
	}
	var kept string
	withStdin(t, "", func() {
		kept, _ = captureErr(func() error { return cmdHandoff([]string{"read", "audit/report.md"}) })
	})
	if kept != "The report.\n" {
		t.Errorf("the record is as it was written: %q", kept)
	}
	// the word help asks for help only where an operation is expected, never where a path or search words are
	for _, c := range []struct {
		args  []string
		words int
		help  bool
	}{{[]string{"help"}, 1, true}, {[]string{"read", "help"}, 1, false}, {[]string{"read", "--help"}, 1, true}, {[]string{"query", "help", "me"}, 1, false}, {[]string{"plan", "help"}, 2, true}} {
		if got := helpAsked(c.args, c.words); got != c.help {
			t.Errorf("helpAsked(%v, %d) = %v", c.args, c.words, got)
		}
	}
	// a refusal of one operation shows that operation's forms, not every form
	if text := usageRefusal("work", "standard input was empty", "work record").Error(); !strings.Contains(text, "shikii work record <<'JSON'") || strings.Contains(text, "shikii work prepare <<'JSON'") {
		t.Errorf("the refusal of work record shows the forms of work record alone:\n%s", text)
	}
	// the bounds the help names are the bounds the code uses
	note := fmt.Sprintf("max_bytes (%d to %d)", workMinBudget, workSourceBudget*3)
	if _, low := workOutputBudget(workMinBudget - 1); low == nil || !strings.Contains(agentUsages["work"].text(), note) || !strings.Contains(agentUsages["work"].text(), fmt.Sprintf("within %d bytes", cliPrepareBytes)) {
		t.Errorf("the help names the bounds the command uses (%s, %d): %v", note, cliPrepareBytes, low)
	}
	if _, err := workOutputBudget(workMinBudget); err != nil {
		t.Errorf("the smallest max_bytes the help names is accepted: %v", err)
	}
	if _, err := workOutputBudget(workSourceBudget*3 + 1); err == nil {
		t.Errorf("one byte over the largest max_bytes the help names is refused")
	}
	// one way to name a place, and the JSON's own words for a malformed input
	for input, want := range map[string]string{
		`{"receipt":"r","notes":[{"sources":"new1"}]}`: "pass the JSON notes[0].sources as an array",
		`{"receipt":"r","notes":[{"nope":1}]}`:         `unknown field "nope" in notes[0]`,
		`{"receipt":"r",}`:                             "malformed",
	} {
		withStdin(t, input, func() {
			_, err := captureErr(func() error { return cmdWork([]string{"record"}) })
			if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "invalid character") || strings.Contains(err.Error(), "notes.0") {
				t.Errorf("shikii work record with %s answers %q: %v", input, want, err)
			}
		})
	}
}
