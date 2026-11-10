package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A line the person wrote before the request and had not committed (a draft in a tracked file, a line of a file Git does not
// track yet) is not a line the turn added. The listing keeps no content, so the request keeps, for those files only, a table
// of keyed hashes of their lines next to the listing (sessions/<id>.lines, sessions/<id>.guard.lines for a slash command's
// stretch), and the added lines are the lines of the current file that are not in that table.

const draftLine = "the person's draft with FORBIDDENWORD"

func uncommittedLinesRepo(t *testing.T, words ...string) (repo, root, readme string) {
	t.Helper()
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo = gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root = filepath.Join(repo, ".shikii")
	readme = filepath.Join(repo, "README.md")
	writeFile(t, readme, "# project\n")
	gitCommitAll(t, repo, "init")
	if len(words) == 0 {
		words = []string{"FORBIDDENWORD"}
	}
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: words})
	return repo, root, readme
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func organizedRequest(t *testing.T, repo, root, session string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "add a paragraph about the storage layout to the README"})
	organizeTurn(t, session, loadSession(root, session).TurnID)
}

func keptLinesPath(root, session, suffix string) string {
	return strings.TrimSuffix(sessionPath(root, session), ".json") + suffix
}

// (a) and (b): the person's uncommitted line, in a tracked file or in a file Git does not track yet, stays out of the added
// lines when the turn changes that file; only the turn's own line is added.
func TestUncommittedLinesFromBeforeTheRequestAreNotAddedLines(t *testing.T) {
	for name, file := range map[string]string{"a tracked file with an uncommitted line": "README.md", "a file not tracked yet": "draft.md"} {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := uncommittedLinesRepo(t)
			p := filepath.Join(repo, file)
			start := ""
			if file == "README.md" {
				start = "# project\n"
			}
			writeFile(t, p, start+draftLine+"\n")
			organizedRequest(t, repo, root, "a")
			writeFile(t, p, start+draftLine+"\na clean line\n")
			out := runHook(t, "stop", map[string]any{"session_id": "a", "cwd": repo, "last_assistant_message": "added"})
			if stoppedOrEnded(out) || strings.Contains(out, "no-word") {
				t.Fatalf("a line the person wrote before the request stopped the turn: %q; records %+v", out, guardRecordsFor(root, "no-word"))
			}
		})
	}
}

// Only the line the turn adds counts: with a second forbidden word in the turn's line, the reason names that word and not the
// word of the person's earlier draft.
func TestTurnLineInAnUncommittedFileStillStops(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t, "FORBIDDENWORD", "NEWWORD")
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	organizedRequest(t, repo, root, "n")
	writeFile(t, readme, "# project\n"+draftLine+"\nthe turn wrote NEWWORD\n")
	out := runHook(t, "stop", map[string]any{"session_id": "n", "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `README.md: \"NEWWORD\"`) {
		t.Fatalf("a forbidden word the turn added to an uncommitted file must stop the turn: %q", out)
	}
	if strings.Contains(out, `README.md: \"FORBIDDENWORD\"`) {
		t.Fatalf("the person's draft line was counted as added: %q", out)
	}
}

// The same line written once more than at the request is an added line.
func TestRepeatedUncommittedLineIsAnAddedLine(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	organizedRequest(t, repo, root, "r")
	writeFile(t, readme, "# project\n"+draftLine+"\n"+draftLine+"\n")
	out := runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `README.md: \"FORBIDDENWORD\"`) {
		t.Fatalf("a line written more times than at the request must stop the turn: %q", out)
	}
}

// (c) The check before a publication compares with the same table: the turn adds a clean line and commits it with the person's
// draft, and the push is not refused; a forbidden line the turn adds is still refused.
func TestPublicationIgnoresUncommittedLinesFromBeforeTheRequest(t *testing.T) {
	for name, turnLine := range map[string]string{"a clean line": "a clean line", "control: a forbidden line": "the turn wrote FORBIDDENWORD"} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := uncommittedLinesRepo(t)
			writeFile(t, readme, "# project\n"+draftLine+"\n")
			writeFile(t, filepath.Join(repo, "draft.md"), draftLine+"\n")
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "p", "cwd": repo, "prompt": "add a paragraph about the storage layout and publish it"})
			writeFile(t, readme, "# project\n"+draftLine+"\n"+turnLine+"\n")
			writeFile(t, filepath.Join(repo, "draft.md"), draftLine+"\nmore notes\n")
			gitCommitAll(t, repo, "docs: the storage layout")
			out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "p", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "git push origin HEAD"}})
			denied := strings.Contains(out, `"permissionDecision":"deny"`) && strings.Contains(out, "no-word")
			if want := strings.Contains(turnLine, "FORBIDDENWORD"); denied != want {
				t.Fatalf("push refused = %v, want %v: %q; records %+v", denied, want, out, guardRecordsFor(root, "no-word"))
			}
		})
	}
}

// A slash command's stretch keeps its own table: the person's draft written after the request and before /commit is not the
// stretch's added line.
func TestSlashCommandStretchIgnoresUncommittedLinesFromBeforeIt(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	cleanRequestTurn(t, repo, root, readme, "d", "")
	writeFile(t, readme, "# project\n\na clean line\n"+draftLine+"\n")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "d", "cwd": repo, "prompt": "/commit"})
	_, kept := os.Stat(keptLinesPath(root, "d", ".guard.lines"))
	writeFile(t, readme, "# project\n\na clean line\n"+draftLine+"\nanother clean line\n")
	gitCommitAll(t, repo, "docs: the storage layout")
	out := runHook(t, "stop", map[string]any{"session_id": "d", "cwd": repo, "last_assistant_message": "committed"})
	if stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("a line the person wrote before the slash command stopped its stretch: %q; records %+v", out, guardRecordsFor(root, "no-word"))
	}
	if kept != nil {
		t.Fatalf("the slash command's stretch keeps the table of its uncommitted lines: %v", kept)
	}
}

// The table keeps keyed hashes only: neither the person's words nor the content of a file Git does not track (.env) is in it.
func TestKeptLinesHoldNoContent(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	writeFile(t, filepath.Join(repo, ".env"), "API_TOKEN=supersecretvalue\n")
	organizedRequest(t, repo, root, "k")
	p := keptLinesPath(root, "k", ".lines")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("the request keeps the table of its uncommitted lines: %v", err)
	}
	for _, word := range []string{"FORBIDDENWORD", "draft", "API_TOKEN", "supersecretvalue", "# project"} {
		if strings.Contains(string(b), word) {
			t.Fatalf("the kept table holds the content %q: %q", word, b)
		}
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the kept table is readable by the person only: %v %v", info.Mode(), err)
	}
}

// A project without uncommitted changes at the request keeps no table (as before), and a table an earlier request kept goes
// when the next request finds nothing uncommitted.
func TestNoKeptLinesWithoutUncommittedChanges(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "c")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "/commit"})
	for _, suffix := range []string{".lines", ".guard.lines"} {
		if _, err := os.Stat(keptLinesPath(root, "c", suffix)); !os.IsNotExist(err) {
			t.Fatalf("a clean project keeps a table %s: %v", suffix, err)
		}
	}
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	organizedRequest(t, repo, root, "c")
	if _, err := os.Stat(keptLinesPath(root, "c", ".lines")); err != nil {
		t.Fatalf("premise: a request with an uncommitted line keeps a table: %v", err)
	}
	gitCommitAll(t, repo, "docs: the person's draft")
	organizedRequest(t, repo, root, "c")
	if _, err := os.Stat(keptLinesPath(root, "c", ".lines")); !os.IsNotExist(err) {
		t.Fatalf("the earlier request's table is left after a request without uncommitted changes: %v", err)
	}
}

// Past the number of files a guard reads, a file is not kept: it is compared with the request's HEAD as before, so the
// person's draft in it counts as added once the turn changes it.
func TestFilesPastTheLimitAreComparedWithHead(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	for i := 0; i < guardFilesMax; i++ {
		writeFile(t, filepath.Join(repo, fmt.Sprintf("a%03d.txt", i)), "a note\n")
	}
	last := filepath.Join(repo, "z.md")
	writeFile(t, last, draftLine+"\n")
	organizedRequest(t, repo, root, "l")
	writeFile(t, last, draftLine+"\na clean line\n")
	out := runHook(t, "stop", map[string]any{"session_id": "l", "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `z.md: \"FORBIDDENWORD\"`) {
		t.Fatalf("a file past the limit is compared with HEAD, as before: %q", out)
	}
}

// When the request is regrouped (the organizing or the Stop attaches it to the previous one), the table follows the listing
// to the regrouped request's header, so the Stop still reads it.
func TestKeptLinesFollowARegroupedRequest(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{sessionFilesPath(root, "s"), keptLinesPath(root, "s", ".lines")} {
		if err := writeAtomic(p, []byte(listingHeader("alone")+"#body\n")); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.OpenRoot(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := moveListingHeader(dir, "s.json", "alone", "together"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{sessionFilesPath(root, "s"), keptLinesPath(root, "s", ".lines")} {
		if b, _ := os.ReadFile(p); string(b) != listingHeader("together")+"#body\n" {
			t.Fatalf("%s does not follow the regrouped request: %q", filepath.Base(p), b)
		}
	}
	p := keptLinesPath(root, "s", ".lines")
	if err := moveKeptHeader(p, "together", "again"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != listingHeader("again")+"#body\n" {
		t.Fatalf("the Stop's regrouping does not move the table: %q", b)
	}
}

// The tables are cleaned up like the listings: with their old session record, or alone when they are old orphans.
func TestKeptLinesArePrunedWithTheListings(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if err := saveSession(root, "live", &hookSession{}); err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, id := range []string{"live", "orphan"} {
		for _, suffix := range []string{".lines", ".guard.lines"} {
			p := keptLinesPath(root, id, suffix)
			if err := writeAtomic(p, []byte(listingHeader(id))); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
			paths[p] = id == "orphan"
		}
	}
	pruneSessions(root)
	for p, gone := range paths {
		_, err := os.Stat(p)
		if gone && !os.IsNotExist(err) {
			t.Errorf("an old orphan table is left behind: %s", filepath.Base(p))
		}
		if !gone && err != nil {
			t.Errorf("the table of a session still in use was removed: %s %v", filepath.Base(p), err)
		}
	}
}
