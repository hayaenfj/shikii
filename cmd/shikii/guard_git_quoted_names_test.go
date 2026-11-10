package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// h1Kanji is a two-character non-ASCII file stem (built from code points so the source stays ASCII).
var h1Kanji = string([]rune{0x624b, 0x9806})

// H1: a file whose name has non-ASCII characters (git quotes it in `ls-files --stage` unless -z is used) must still be
// read by the forbidden-words guard at Stop.
func TestStopGuardReadsFilesWithNonASCIINames(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	name := "docs/" + h1Kanji + ".md"
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(name)), []byte("# steps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h1", "cwd": repo, "prompt": "add a line to the steps document"})
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(name)), []byte("# steps\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "h1", "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") {
		t.Fatalf("a forbidden word added to a file with a non-ASCII name must stop the turn: %q", out)
	}
	if !strings.Contains(out, name) {
		t.Fatalf("the reason names the file as it is spelled on disk (%s): %q", name, out)
	}
}

// H1 (paired-files): the pair is written with the real file names, so the changed-file list must carry the real names too.
func TestStopPairedFilesMatchNonASCIINames(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	en, ja := "CHANGELOG.md", h1Kanji+".md"
	for _, f := range []string{en, ja} {
		if err := os.WriteFile(filepath.Join(repo, f), []byte("# log\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "log-pair", Kind: guardKindPairedFiles, Title: "keep both changelogs in step", Pairs: [][]string{{en, ja}}})

	// both sides change: the guard must pass
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h1p", "cwd": repo, "prompt": "add the 1.0 entry to both changelogs"})
	for _, f := range []string{en, ja} {
		if err := os.WriteFile(filepath.Join(repo, f), []byte("# log\n\n- 1.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := runHook(t, "stop", map[string]any{"session_id": "h1p", "cwd": repo, "last_assistant_message": "added to both"})
	if strings.Contains(out, "log-pair") {
		t.Fatalf("both files of the pair changed, so the paired-files guard must pass: %q", out)
	}
}

// H1 (a watch saved before the fix): its keys are the names as git quoted them. A new file that is still there must not
// read as "removed again" (reverted) once the listing carries the real names.
func TestScopeWatchSavedWithQuotedNamesIsNotReverted(t *testing.T) {
	repo := gitRepo(t)
	name := h1Kanji + ".md"
	if err := os.WriteFile(filepath.Join(repo, name), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: the listing is taken")
	}
	quoted := `"\346\211\213\351\240\206.md"` // how git prints the name without -z
	if !strings.Contains(listing, quoted) {
		t.Fatalf("premise: git quotes the name in the listing: %q", listing)
	}
	w := scopeWatch{Trace: "t", Before: map[string]string{quoted: ""}} // the file did not exist before the turn
	if w.reverted(parseListing(listing)) {
		t.Fatal("the new file is still there, so the turn's change was not reverted")
	}
}

// H1 (guard try): `git status --porcelain` quotes names with spaces or non-ASCII characters too.
func TestGuardWorkingTreeInputReadsQuotedNames(t *testing.T) {
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	spaced, kanji := "release notes.md", h1Kanji+".md"
	for _, f := range []string{spaced, kanji} {
		if err := os.WriteFile(filepath.Join(repo, f), []byte("has FORBIDDENWORD\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := guardWorkingTreeInput(repo)
	for _, f := range []string{spaced, kanji} {
		if len(in.Added[f]) != 1 {
			t.Fatalf("the added line of %q is read: changed=%q added=%v", f, in.Changed, in.Added)
		}
	}
}
