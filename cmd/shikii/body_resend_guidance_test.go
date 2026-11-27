package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// 0.44.0 leftover U-11: an id save replaces the whole body, and a body copied out by hand can lose a part of it below the line the refusal
// draws (49% lost was accepted). The texts an agent reads about adding to an article therefore name the patch first (before a unique
// ending of the body, after that ending plus the new text) and the whole body sent again only after it, for what a patch does not fit.
//
// The texts are found, not listed: every line of the agent-facing sources (the Go files of the program that are not tests, and the skill
// files) that tells to send a body again must name the unique-ending patch earlier on the same line. A new text that tells an agent to
// send the whole body first fails here without anyone adding it to a list.

// resendsABody finds where a line tells to send a body again. It is not a list of the phrasings seen so far (the first scan knew "send
// the whole body" and a few more, and "resend the whole body", "include the full body" and "send all of the body" went through: 0.44.1
// audit W6). A mention is a verb of handing a text over (send, resend, submit, put, include, copy, paste, give, provide, supply, write,
// attach, upload, repeat; the forms of the infinitive and the participle, not "sends") followed, within the same clause (no full stop,
// semicolon, colon or bracket, and at most 40 bytes), by a whole body: whole, entire, full, complete, existing, previous or old, then
// body or text; all of the body or text; or the body or text with again or in full. Any wording that a writer puts between the verb and
// the body is allowed, so a new phrasing of the same instruction is found without anyone adding it here.
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

// agentFacingSources are the files whose text an agent reads: the program (comments are read as the text too, so a comment that tells to
// send the whole body first is held to the same rule) and the skill files.
func agentFacingSources(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, root := range []string{"../../cmd", "../../internal", "../../skills"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			name := entry.Name()
			if (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")) || strings.HasSuffix(name, ".md") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestTextsThatTellToSendABodyAgainNameTheAppendPatchFirst(t *testing.T) {
	// the scan finds the texts the audit read (reflect.go, reflect_place.go, the skill's wiki.md) and the refusal itself: if it found none of
	// them it would pass for the wrong reason
	found := map[string]bool{}
	for _, path := range agentFacingSources(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(resendMentions(string(data))) > 0 {
			found[filepath.Base(path)] = true
		}
		if bad := resendBeforePatch(string(data)); len(bad) > 0 {
			t.Errorf("%s tells to send a body again without naming the patch that adds at the end first (a unique ending of the body as before, that ending plus the new text as after): %.200q", path, bad)
		}
	}
	for _, want := range []string{"reflect.go", "reflect_place.go", "wiki.md", "review.md", "import-memory.md", "ingest_body_loss.go"} {
		if !found[want] {
			t.Errorf("%s is expected to tell to send a body again (as its text after the patch) and the scan did not see it: %v", want, slices.Sorted(maps.Keys(found)))
		}
	}
	// negative examples: the old texts of the audit, and a new one that sends the body first, are found; the texts that name the patch first are not
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
	text := reflectHowToSubmit
	patch := strings.Index(text, "shikii wiki patch")
	revision := strings.Index(text, "current revision")
	read := strings.Index(text, "wiki query lesson:<id>")
	ending := strings.Index(text, patchAppend)
	if patch < 0 || revision < patch || revision > ending || read < patch || read > ending {
		t.Fatalf("how_to_submit must say, between naming shikii wiki patch and the unique ending, that a patch needs the article's id and current revision and that the article is read first with wiki query lesson:<id> (patch %d, current revision %d, query %d, unique ending %d)", patch, revision, read, ending)
	}
	if !strings.Contains(text[patch:ending], "id and current revision") {
		t.Fatalf("how_to_submit does not name both the id and the current revision as what a patch needs: %q", text[patch:ending])
	}
}
