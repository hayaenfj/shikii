package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// 0.44.0 leftover U-11: an id save replaces the whole body, and a body copied out by hand can lose a part of it below the line the refusal
// draws (49% lost was accepted). The texts an agent reads about adding to an article therefore name the patch first (before a unique
// ending of the body, after that ending plus the new text) and the whole body sent again only after it, for what a patch does not fit.
//
// The check is an allowlist, not a search for bad phrasings. A search for phrasings (the first version knew "send the whole body", the
// second a verb followed by a whole body) missed a new one every time the audit looked: "the whole body must be sent again", "pass the whole
// body", "return the entire body", "send the whole article body". So every line of agent-facing text that mentions a body is listed in
// testdata/body_lines_allowlist.tsv with a kind that was decided by reading the line (patch-first, not-resend or other), and a line that is
// not listed fails, however it is worded. A changed line is a new line (the key holds the hash of the line), and an entry that no line has
// any more fails too, so the list does not keep what is gone.
//
// What is read: the string literals of the Go files under cmd and internal that are not tests (their values, split at the line breaks), and
// the Markdown files under skills. Go comments are not read: an agent never receives them. A line that tells to send the text again without
// the word body ("send the whole article text again") is out of this check, and so is a sentence built from literals joined with + or
// passed to Sprintf, since each literal is read alone (0.44.2 audit U-3: left out on purpose, only a deliberate way of writing gets past it).
//
// The regular expression below is no longer what decides. It backs two checks on the kinds: a patch-first line must really name the unique
// ending before it tells to send a body again, and a line listed as not-resend must not read as telling to send one.

// resendsABody finds where a line tells to send a body again. A mention is a verb of handing a text over (send, resend, submit, put,
// include, copy, paste, give, provide, supply, write, attach, upload, repeat; the forms of the infinitive and the participle, not "sends")
// followed, within the same clause (no full stop, semicolon, colon or bracket, and at most 40 bytes), by a whole body: whole, entire, full,
// complete, existing, previous or old, then body or text; all of the body or text; or the body or text with again or in full.
//
// A body that is only said to replace the old one ("the body you send replaces the whole body") is a description, not an instruction, and is
// not a mention: the text between the verb and the body names the replacing (resendIsDescription).
var resendsABody = regexp.MustCompile(`(?i)\b(?:re-?send|sending|send|sent|re-?submit|submitting|submit|putting|put|including|include|copying|copy|copied|pasting|paste|providing|provide|supplying|supply|giving|give|writing|write|attach|upload|repeat)\b[^.;:()]{0,40}?(?:\b(?:whole|entire|full|complete|existing|previous|old|all of the|all the)\s+(?:body|text)\b|\b(?:its|the|your)\s+(?:body|text)\s+(?:again|in full)\b)`)

const patchAppend = "unique ending"

// resendIsDescription says that a match is about what a body does to the old one, not an instruction to send it.
func resendIsDescription(match string) bool {
	return strings.Contains(strings.ToLower(match), "replac")
}

// resendMentions returns the places of text (byte offsets of the match) that tell to send a body again.
func resendMentions(text string) [][]int {
	var out [][]int
	for _, at := range resendsABody.FindAllStringIndex(text, -1) {
		if !resendIsDescription(text[at[0]:at[1]]) {
			out = append(out, at)
		}
	}
	return out
}

// resendBeforePatch returns the lines of text that tell to send a body again without naming the append patch before that.
func resendBeforePatch(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for _, at := range resendMentions(line) {
			if !strings.Contains(line[:at[0]], patchAppend) {
				out = append(out, strings.TrimSpace(line))
				break
			}
		}
	}
	return out
}

// The kinds of a listed line.
const (
	bodyPatchFirst = "patch-first"
	bodyNotResend  = "not-resend"
	bodyOther      = "other"
)

const bodyAllowlistPath = "testdata/body_lines_allowlist.tsv"

// mentionsABody finds the lines the allowlist covers: the word in any case, inside a longer word too (body_included, nobody: a new
// spelling cannot slip past a word boundary), and the plural.
var mentionsABody = regexp.MustCompile(`(?i)bod(?:y|ies)`)

// bodyLine is one line of agent-facing text that mentions a body. key is its path and the hash of the line.
type bodyLine struct{ key, text string }

// bodyEntry is what the allowlist says about a key.
type bodyEntry struct{ kind, reason, excerpt string }

// bodyLineKey names a line: the path from the repository's top folder, "#", and the first 16 hex digits of the SHA-256 of the line
// without the spaces around it.
func bodyLineKey(path, line string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
	return path + "#" + hex.EncodeToString(sum[:8])
}

// bodyExcerpt is the start of a line that a reader can use to find it: ASCII only (a line with Japanese would not pass the check
// that what ships is English), at most 72 bytes.
func bodyExcerpt(line string) string {
	end := 0
	for end < len(line) && end < 72 && line[end] >= ' ' && line[end] < 0x7f {
		end++
	}
	return line[:end]
}

// bodyLinesOf returns the lines of a text that mention a body, each with its key under path.
func bodyLinesOf(path, text string) []bodyLine {
	var out []bodyLine
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); mentionsABody.MatchString(line) {
			out = append(out, bodyLine{bodyLineKey(path, line), line})
		}
	}
	return out
}

// goBodyLines returns the lines of the string literals of a Go source that mention a body. Comments are not parsed, so they are not
// looked at.
func goBodyLines(t *testing.T, path, src string) []bodyLine {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var out []bodyLine
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: %s: %v", path, lit.Value, err)
			}
			out = append(out, bodyLinesOf(path, value)...)
		}
		return true
	})
	return out
}

// agentBodyLines reads every source an agent's text comes from and returns the lines that mention a body: the string literals of the Go
// files of the program that are not tests, and the skill files.
func agentBodyLines(t *testing.T) []bodyLine {
	t.Helper()
	const top = "../.."
	var out []bodyLine
	for _, dir := range []string{"cmd", "internal", "skills"} {
		err := filepath.WalkDir(filepath.Join(top, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			name := entry.Name()
			isGo := dir != "skills" && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
			if !isGo && !(dir == "skills" && strings.HasSuffix(name, ".md")) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(top, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if isGo {
				out = append(out, goBodyLines(t, rel, string(data))...)
			} else {
				out = append(out, bodyLinesOf(rel, string(data))...)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// loadBodyAllowlist reads the allowlist: one entry per line, "key<TAB>kind<TAB>reason<TAB>excerpt"; empty lines and lines that start with #
// are skipped. A malformed file fails the test.
func loadBodyAllowlist(t *testing.T, path string) map[string]bodyEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	allow := map[string]bodyEntry{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || len(fields) > 4 {
			t.Fatalf("%s:%d: want key, kind, reason and excerpt separated by tabs: %.120q", path, n+1, line)
		}
		if !slices.Contains([]string{bodyPatchFirst, bodyNotResend, bodyOther}, fields[1]) {
			t.Fatalf("%s:%d: the kind %q is not patch-first, not-resend or other", path, n+1, fields[1])
		}
		if strings.TrimSpace(fields[2]) == "" {
			t.Fatalf("%s:%d: an entry needs the reason for its kind", path, n+1)
		}
		if _, dup := allow[fields[0]]; dup {
			t.Fatalf("%s:%d: %s is listed twice", path, n+1, fields[0])
		}
		entry := bodyEntry{kind: fields[1], reason: fields[2]}
		if len(fields) == 4 {
			entry.excerpt = fields[3]
		}
		allow[fields[0]] = entry
	}
	return allow
}

// bodyLineProblems compares the lines found with the allowlist and returns what is wrong, in the order of the lines and then of the keys:
// a line that is not listed (with the entry to add), a listed line that does not fit its kind, and an entry that no line has.
func bodyLineProblems(lines []bodyLine, allow map[string]bodyEntry) []string {
	var problems []string
	seen := map[string]bool{}
	for _, l := range lines {
		if seen[l.key] {
			continue
		}
		seen[l.key] = true
		path, _, _ := strings.Cut(l.key, "#")
		entry, listed := allow[l.key]
		switch {
		case !listed:
			problems = append(problems, fmt.Sprintf("%s\n\tline: %.300q\n\tthis line mentions a body and is not on the allowlist. Read it: a line that tells to send a body again must name the patch with a unique ending earlier on the same line (patch-first), one that does not is not-resend, anything else needs a reason (other). To list it, add to %s:\n\t%s\t<kind>\t<reason>\t%s", l.key, l.text, bodyAllowlistPath, l.key, bodyExcerpt(l.text)))
		case entry.kind == bodyPatchFirst && !strings.Contains(l.text, patchAppend):
			problems = append(problems, fmt.Sprintf("%s (%s) is listed as patch-first but does not name %q: %.200q", l.key, path, patchAppend, l.text))
		case entry.kind == bodyPatchFirst && len(resendBeforePatch(l.text)) > 0:
			problems = append(problems, fmt.Sprintf("%s (%s) is listed as patch-first but tells to send a body again before it names the patch with a unique ending: %.200q", l.key, path, l.text))
		case entry.kind == bodyNotResend && len(resendMentions(l.text)) > 0:
			problems = append(problems, fmt.Sprintf("%s (%s) is listed as not-resend but reads as telling to send a body again (list it as patch-first if it names the patch first): %.200q", l.key, path, l.text))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(allow)) {
		if !seen[key] {
			problems = append(problems, fmt.Sprintf("%s is on the allowlist but no line has it any more (the line was changed or removed): remove the entry (%s): %.80q", key, allow[key].kind, allow[key].excerpt))
		}
	}
	return problems
}

func TestEveryLineThatMentionsABodyIsOnTheAllowlist(t *testing.T) {
	lines := agentBodyLines(t)
	allow := loadBodyAllowlist(t, bodyAllowlistPath)
	goLines, mdLines := 0, 0
	for _, l := range lines {
		if strings.HasPrefix(l.key, "skills/") {
			mdLines++
		} else {
			goLines++
		}
	}
	if goLines == 0 || mdLines == 0 {
		t.Fatalf("the scan must see the string literals of the program and the skill files (go %d, markdown %d lines)", goLines, mdLines)
	}
	for _, problem := range bodyLineProblems(lines, allow) {
		t.Error(problem)
	}
}

// The allowlist itself: what the 0.44.1 audit found missing, a line changed by one word and an entry taken out must each fail, and a
// listed line whose kind does not fit it must fail too, so a check that lists everything and decides nothing cannot pass.
func TestTheBodyAllowlistFailsWhereItMust(t *testing.T) {
	lines := agentBodyLines(t)
	allow := loadBodyAllowlist(t, bodyAllowlistPath)
	if problems := bodyLineProblems(lines, allow); len(problems) > 0 {
		t.Fatalf("the allowlist must pass on the real lines before it is tried on others: %v", problems)
	}
	only := func(t *testing.T, problems []string, what string) {
		t.Helper()
		if len(problems) != 1 || !strings.Contains(problems[0], what) {
			t.Fatalf("want exactly one problem containing %q, got %q", what, problems)
		}
	}
	// the phrasings that a search for bad wording missed, as a Go string and as a line of a skill
	for _, phrase := range []string{
		"the whole body must be sent again",
		"pass the whole body",
		"return the entire body",
		"send the body with your sentences",
		"send the whole article body",
		"send body = old body + your addition",
	} {
		t.Run(phrase, func(t *testing.T) {
			goLine := goBodyLines(t, "cmd/shikii/zz_new.go", "package main\n\nvar zzNew = "+strconv.Quote(phrase)+"\n")
			only(t, bodyLineProblems(append(slices.Clone(lines), goLine...), allow), "cmd/shikii/zz_new.go#")
			mdLine := bodyLinesOf("skills/shikii/references/zz_new.md", "- "+phrase+"\n")
			only(t, bodyLineProblems(append(slices.Clone(lines), mdLine...), allow), "skills/shikii/references/zz_new.md#")
		})
	}
	// a line that is changed by one word is a new line, and the listed line is gone
	var changed []bodyLine
	for i, l := range lines {
		if allow[l.key].kind != bodyPatchFirst {
			continue
		}
		words := strings.Fields(l.text)
		for w := range words {
			if !mentionsABody.MatchString(words[w]) && len(words[w]) > 3 {
				words[w] = "changed"
				break
			}
		}
		text := strings.Join(words, " ")
		path, _, _ := strings.Cut(l.key, "#")
		changed = slices.Clone(lines)
		changed[i] = bodyLine{bodyLineKey(path, text), text}
		break
	}
	problems := bodyLineProblems(changed, allow)
	if len(problems) != 2 || !strings.Contains(problems[0], "is not on the allowlist") || !strings.Contains(problems[1], "no line has it any more") {
		t.Fatalf("a patch-first line changed by one word must be a new line and leave its entry without a line, got %q", problems)
	}
	// an entry taken out leaves its line unlisted (a patch-first one and a not-resend one)
	for _, kind := range []string{bodyPatchFirst, bodyNotResend} {
		without := maps.Clone(allow)
		var removed string
		for _, key := range slices.Sorted(maps.Keys(allow)) {
			if allow[key].kind == kind {
				removed = key
				break
			}
		}
		delete(without, removed)
		problems := bodyLineProblems(lines, without)
		only(t, problems, "this line mentions a body and is not on the allowlist")
		only(t, problems, removed)
	}
	// a kind that does not fit its line: a body sent first, listed as patch-first or as not-resend, and one sent after the patch
	const first = "To add, send the whole existing body with your addition."
	const late = "To add, send the whole existing body with your addition; a patch with a unique ending of the body is for small edits."
	const after = "To add, use a patch whose before is a unique ending of the body, then send the whole existing body again."
	for _, c := range []struct{ text, kind, want string }{
		{first, bodyPatchFirst, "does not name"},
		{first, bodyNotResend, "reads as telling to send a body again"},
		{late, bodyPatchFirst, "before it names the patch"},
		{first, bodyOther, ""},
		{after, bodyPatchFirst, ""},
	} {
		key := bodyLineKey("skills/shikii/references/zz_kind.md", c.text)
		got := bodyLineProblems([]bodyLine{{key, c.text}}, map[string]bodyEntry{key: {kind: c.kind, reason: "tried"}})
		if c.want == "" && len(got) != 0 || c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)) {
			t.Errorf("%q listed as %s: want %q, got %q", c.text, c.kind, c.want, got)
		}
	}
}

// The helpers of the kinds: the old texts of the audit, and a new one that sends the body first, are found; the texts that name the patch
// first are not.
func TestResendBeforePatchFindsABodySentBeforeThePatch(t *testing.T) {
	for _, text := range []string{
		"to add to one, put its id in submit with its whole body and your additions (a body that drops half or more of the old one is refused)",
		"an id replaces the whole body, so send the existing body again with your additions",
		"To add, send the whole existing body with your addition (read it with `wiki query lesson:<id>` first).",
		"When a page already covers the topic, update it with `id` (sending its whole body with your addition)",
		"To change it, send shikii wiki patch. To add to it, send its whole body again with your additions",
		// 0.44.1 audit W6: the phrases the first scan did not know
		"To add to an article, resend the whole body with your addition first; patch is for small changes.",
		"To add, include the full body of the article with your addition; use a patch only for a small change.",
		"An id save adds when you send all of the body again with your sentences.",
		"When an article needs a new section, put the entire text of the article in body with the section at the end.",
		"Copy the existing body, add your sentences and submit it; patch is for a small change.",
		"To add, use a patch whose before is the old ending. Also resend the whole body.",                                        // names a patch, not the append
		"The body you send replaces the whole body; resend the whole body with your addition first, a patch is for small edits.", // the description is not what is found: the instruction after it is
	} {
		if len(resendBeforePatch(text)) != 1 {
			t.Errorf("a text that sends the body first is not found: %q", text)
		}
	}
	for _, text := range []string{
		"to add, use shikii wiki patch (before is a unique ending of the body), and only then send its whole body again with your additions",
		"The body you send replaces the whole body; it does not add to it.",
		"To add, use a patch whose before is a unique ending of the body, and only when it does not fit resend the whole body with your addition.",
		// texts that talk about a whole body without telling to send one: reading it, the notice that it was supplied, what a result holds
		"A long body comes in parts: read the whole body with shikii wiki query <id>.",
		"Reuse the full body supplied earlier under this receipt.",
		"It returns the selected full text as article:<id>:<revision> in source_material.",
		"Exact passages of this revision, not a complete body.",
	} {
		if bad := resendBeforePatch(text); len(bad) != 0 {
			t.Errorf("a text that names the patch first, or does not tell to send a body, is found: %q", bad)
		}
	}
}

// 0.44.1 audit D-3: a patch needs the id of the article and its current revision, and the article is read first (the unique ending has to
// be one that the body really has). how_to_submit says so in the place where it names the patch, before the unique ending it asks for, and
// not only in the branch for a patch that does not fit.
func TestHowToSubmitSaysWhatAPatchNeedsBeforeTheUniqueEnding(t *testing.T) {
	// the outline in the reply, and the line of the section of review.md that it names (the outline's details are there)
	for name, text := range map[string]string{"how_to_submit": reflectHowToSubmit, "the section of review.md that how_to_submit names": lineWith(t, "../../skills/shikii/references/review.md", "An `id` replaces the whole body")} {
		patch := strings.Index(text, "shikii wiki patch")
		revision := strings.Index(text, "current revision")
		read := strings.Index(text, "wiki query lesson:<id>")
		ending := strings.Index(text, patchAppend)
		if patch < 0 || revision < patch || revision > ending || read < patch || read > ending {
			t.Fatalf("%s must say, between naming shikii wiki patch and the unique ending, that a patch needs the article's id and current revision and that the article is read first with wiki query lesson:<id> (patch %d, current revision %d, query %d, unique ending %d)", name, patch, revision, read, ending)
		}
		if !strings.Contains(text[patch:ending], "id and current revision") {
			t.Fatalf("%s does not name both the id and the current revision as what a patch needs: %q", name, text[patch:ending])
		}
	}
}

// A patch edit without a field is refused, and the texts that send an agent to a patch said only "before/after edits" (found when a patch
// was built from the refusal of a body save). how_to_submit, and the note that place gives with its candidates (0.44.2 audit U-6: only the
// hash of its line in the allowlist held its shape, and a person can list a changed line again), show the shape of the edits where they name
// the patch, before the unique ending they ask for.
func TestHowToSubmitShowsTheShapeOfAPatchEdit(t *testing.T) {
	const shape = `edits:[{field:"body",before,after}]`
	for name, text := range map[string]string{"how_to_submit": reflectHowToSubmit, "the section of review.md that how_to_submit names": lineWith(t, "../../skills/shikii/references/review.md", "An `id` replaces the whole body"), "the note of place": placeNote} {
		patch, at, ending := strings.Index(text, "shikii wiki patch"), strings.Index(text, shape), strings.Index(text, patchAppend)
		if patch < 0 || at < patch || at > ending {
			t.Errorf("%s must show %s between naming shikii wiki patch and the unique ending (patch %d, shape %d, unique ending %d)", name, shape, patch, at, ending)
		}
	}
}

// shikii wiki --help carries a patch in the shape of the other forms: its JSON names the article, its revision, the sources, the reason and
// the edits, and an edit names its field. The form is run as written by TestHelpPrintsFormsThatRunAsWritten.
func TestTheWikiHelpShowsAPatchWithItsEdits(t *testing.T) {
	help, err := captureErr(func() error { return cliCall([]string{"wiki", "--help"}) })
	if err != nil {
		t.Fatal(err)
	}
	var patch *usageForm
	for _, form := range usageForms(help) {
		if len(form.args) >= 2 && form.args[0] == "wiki" && form.args[1] == "patch" {
			form := form
			patch = &form
		}
	}
	if patch == nil {
		t.Fatalf("shikii wiki --help has no form of wiki patch:\n%s", help)
	}
	var shape struct {
		ID           string   `json:"id"`
		Revision     string   `json:"revision"`
		ChangeReason string   `json:"change_reason"`
		Sources      []string `json:"sources"`
		Edits        []struct {
			Field  string `json:"field"`
			Before string `json:"before"`
			After  string `json:"after"`
		} `json:"edits"`
	}
	input := usagePlaceholder.ReplaceAllString(patch.input, "x")
	if err := json.Unmarshal([]byte(input), &shape); err != nil {
		t.Fatalf("the JSON of the wiki patch form is not JSON once its placeholders are filled: %v\n%s", err, patch.input)
	}
	if shape.ID == "" || shape.Revision == "" || shape.ChangeReason == "" || len(shape.Sources) == 0 || len(shape.Edits) != 1 || shape.Edits[0].Field != "body" || shape.Edits[0].Before == "" || shape.Edits[0].After == "" {
		t.Fatalf("the wiki patch form shows id, revision, sources, change_reason and edits [{field body, before, after}]: %+v\n%s", shape, patch.input)
	}
}
