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

// resendsABody matches the instruction to send a whole or an existing body again ("send the existing body", "sending its whole body",
// "put its id in submit with its whole body").
var resendsABody = regexp.MustCompile(`(?i)\b(?:send|sending)\s+(?:its|the|your)\s+(?:whole\s+existing|whole|existing|full)\s+body\b|\bput its id in submit with its whole body\b`)

const patchAppend = "unique ending"

// resendBeforePatch returns the lines of text that tell to send a body again without naming the append patch before that.
func resendBeforePatch(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if at := resendsABody.FindStringIndex(line); at != nil && !strings.Contains(line[:at[0]], patchAppend) {
			out = append(out, strings.TrimSpace(line))
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
		if resendsABody.Match(data) {
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
	} {
		if len(resendBeforePatch(text)) != 1 {
			t.Errorf("a text that sends the body first is not found: %q", text)
		}
	}
	for _, text := range []string{
		"to add, use shikii wiki patch (before is a unique ending of the body), and only then send its whole body again with your additions",
		"The body you send replaces the whole body; it does not add to it.",
	} {
		if bad := resendBeforePatch(text); len(bad) != 0 {
			t.Errorf("a text that names the patch first, or does not tell to send a body, is found: %q", bad)
		}
	}
}
