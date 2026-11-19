package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// event runs fn as one hook event does for git: the answers git gives are kept until it returns.
func event(fn func()) {
	end := beginEventGit()
	defer end()
	fn()
}

const askedPlace = "rev-parse --show-toplevel --git-path objects --git-path info/exclude --verify -q HEAD"

// A folder that has no `.git` in it or above it, and does not look like a bare repository, is outside git without asking git: git would
// say the same, and every event of a project that is not under git used to start 2 to 6 processes to hear it.
func TestAFolderOutsideGitIsKnownWithoutStartingGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	folder := t.TempDir()
	calls := gitCallLog(t)
	event(func() {
		if _, ok := gitPlaceAt(context.Background(), folder); ok {
			t.Fatal("premise: the folder is outside git")
		}
		if head := gitHead(folder); head != "" {
			t.Errorf("HEAD outside git is %q", head)
		}
	})
	if got := calls(); len(got) != 0 {
		t.Errorf("outside git, these git calls were made:\n%s", strings.Join(got, "\n"))
	}
	// the CLI, the MCP server and the page get the same answer without a process
	if _, ok := gitPlaceAt(context.Background(), folder); ok || len(calls()) != 0 {
		t.Error("outside an event too, a folder with no .git above it is known to be outside git")
	}
}

// Where the folders above could still be a repository for git, git is asked (the negative examples of the test above): a `.git` that is
// a file (a worktree or a submodule), a bare repository, and the environment that tells git where the repository is.
func TestAFolderThatMayBeARepositoryStillAsksGit(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{"a .git file", func(t *testing.T) string {
			d := t.TempDir()
			if err := os.WriteFile(filepath.Join(d, ".git"), []byte("gitdir: "+filepath.Join(d, "nowhere")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return d
		}},
		{"a bare repository", func(t *testing.T) string {
			d := t.TempDir()
			if out, err := exec.Command("git", "init", "--bare", "-q", d).CombinedOutput(); err != nil {
				t.Skipf("git init --bare: %v %s", err, out)
			}
			return d
		}},
		{"inside the .git folder", func(t *testing.T) string {
			d := gitRepo(t)
			return filepath.Join(d, ".git", "refs")
		}},
		{"GIT_DIR in the environment", func(t *testing.T) string {
			d := t.TempDir()
			t.Setenv("GIT_DIR", filepath.Join(d, "nowhere"))
			return d
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			folder := c.setup(t)
			calls := gitCallLog(t)
			event(func() { gitPlaceAt(context.Background(), folder) })
			if got := calls(); len(got) == 0 || got[0] != askedPlace {
				t.Errorf("git was not asked where the repository is:\n%s", strings.Join(got, "\n"))
			}
		})
	}
}

// What git said about a repository is kept for the next events (a tool call asked git every time, and each is a process). The repository
// is told by the folders above the one asked about (the nearest `.git`), so a subfolder shares it; HEAD is not kept, as every commit
// changes it, and is asked alone when an event needs it. The CLI and the page, which live longer than a request, never use it.
func TestThePlaceOfTheRepositoryIsKeptBetweenEvents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "one")
	head := strings.TrimSpace(run(t, "git", "-C", repo, "rev-parse", "HEAD"))
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := gitCallLog(t)
	var first gitPlace
	event(func() {
		var ok bool
		if first, ok = gitPlaceAt(context.Background(), repo); !ok {
			t.Fatal("premise: git answers about the repository")
		}
	})
	if got := calls(); !reflect.DeepEqual(got, []string{askedPlace}) {
		t.Fatalf("the first event asks git once: %v", got)
	}
	for _, dir := range []string{repo, sub} {
		event(func() {
			got, ok := gitPlaceAt(context.Background(), dir)
			if !ok || !sameFolder(got.top, first.top) || got.objects != first.objects || got.exclude != first.exclude {
				t.Errorf("the kept answer for %s is %+v (%v), git said %+v", dir, got, ok, first)
			}
		})
		if got := calls(); len(got) != 0 {
			t.Errorf("a later event asked git about %s again:\n%s", dir, strings.Join(got, "\n"))
		}
	}
	event(func() {
		gitPlaceAt(context.Background(), repo)
		if got := gitHead(repo); got != head {
			t.Errorf("HEAD is %q, want %q", got, head)
		}
	})
	if got := calls(); !reflect.DeepEqual(got, []string{"rev-parse HEAD"}) {
		t.Errorf("HEAD is not kept: it is asked alone: %v", got)
	}
	if _, ok := gitPlaceAt(context.Background(), repo); !ok || len(calls()) != 1 {
		t.Error("outside an event git is asked every time")
	}
}

// A kept answer is used only while the folders say the same as when git answered: a repository made below, a repository gone, the `.git`
// turned into a file, the environment naming the repository, or the answer being too old each make the next event ask git again.
func TestAKeptPlaceIsLeftOnceTheRepositoryHasChanged(t *testing.T) {
	for _, c := range []struct {
		name      string
		change    func(t *testing.T, repo, sub string)
		wantCalls int
		wantTop   func(repo, sub string) string // "" when git says no
	}{
		{"a repository made below it", func(t *testing.T, repo, sub string) {
			if out, err := exec.Command("git", "-C", sub, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
		}, 1, func(repo, sub string) string { return sub }},
		{"the repository gone", func(t *testing.T, repo, sub string) {
			if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
				t.Fatal(err)
			}
		}, 0, func(repo, sub string) string { return "" }},
		{"the .git turned into a file", func(t *testing.T, repo, sub string) {
			if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: "+filepath.Join(repo, "nowhere")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, 1, func(repo, sub string) string { return "" }},
		{"the environment names a repository", func(t *testing.T, repo, sub string) {
			t.Setenv("GIT_DIR", filepath.Join(repo, ".git")) // git then takes the folder it is asked from as the top of the work tree
		}, 1, func(repo, sub string) string { return sub }},
		{"the answer too old", func(t *testing.T, repo, sub string) {
			old := placeCacheTTL
			placeCacheTTL = -1
			t.Cleanup(func() { placeCacheTTL = old })
		}, 1, func(repo, sub string) string { return repo }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo := gitRepo(t)
			sub := filepath.Join(repo, "pkg", "inner")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			event(func() { gitPlaceAt(context.Background(), sub) }) // git answers and the answer is kept
			calls := gitCallLog(t)
			c.change(t, repo, sub)
			calls() // what the change itself ran is not counted
			event(func() {
				got, ok := gitPlaceAt(context.Background(), sub)
				want := c.wantTop(repo, sub)
				if ok != (want != "") || ok && !sameFolder(got.top, want) {
					t.Errorf("the place is %q (%v), want %q", got.top, ok, want)
				}
			})
			if got := calls(); len(got) != c.wantCalls {
				t.Errorf("%d git calls, want %d:\n%s", len(got), c.wantCalls, strings.Join(got, "\n"))
			}
		})
	}
}

// GIT_CEILING_DIRECTORIES: git does not search a ceiling folder nor any above it, but always searches the folder it is asked from. The
// walk answers as git does (the answers are compared with git's own), and where nothing is found no process is started.
func TestTheWalkStopsWhereGitDoesNotSearch(t *testing.T) {
	for _, c := range []struct {
		name      string
		ceiling   func(outer, repo, deep string) string
		wantFound bool
		wantCalls int
	}{
		{"no ceiling", func(outer, repo, deep string) string { return "" }, true, 1},
		{"the nearest repository is the ceiling", func(outer, repo, deep string) string { return repo }, false, 0},
		{"the ceiling is above the nearest repository", func(outer, repo, deep string) string { return outer }, true, 1},
		{"the ceiling is the folder asked from", func(outer, repo, deep string) string { return deep }, true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			outer := t.TempDir()
			repo := filepath.Join(outer, "repo")
			deep := filepath.Join(repo, "a", "b")
			for _, d := range []string{repo, deep} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
				t.Skipf("git init: %v %s", err, out)
			}
			t.Setenv("GIT_CEILING_DIRECTORIES", c.ceiling(outer, repo, deep))
			want := exec.Command("git", "rev-parse", "--show-toplevel")
			want.Dir = deep
			raw, err := want.Output()
			if (err == nil) != c.wantFound {
				t.Fatalf("premise: git says found=%v, the case expects %v", err == nil, c.wantFound)
			}
			calls := gitCallLog(t)
			event(func() {
				got, ok := gitPlaceAt(context.Background(), deep)
				if ok != c.wantFound || ok && !sameFolder(got.top, strings.TrimSpace(string(raw))) {
					t.Errorf("the place is %q (%v); git says %q (%v)", got.top, ok, strings.TrimSpace(string(raw)), err == nil)
				}
			})
			if got := calls(); len(got) != c.wantCalls {
				t.Errorf("%d git calls, want %d:\n%s", len(got), c.wantCalls, strings.Join(got, "\n"))
			}
		})
	}
}

// A kept answer that names another folder than the one whose `.git` the walk found (a file that was copied or changed by hand) is not
// used: git is asked.
func TestAKeptPlaceThatNamesAnotherFolderIsNotUsed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, other := gitRepo(t), gitRepo(t)
	event(func() { gitPlaceAt(context.Background(), repo) })
	at, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := placeEntryFile(at)
	if !ok {
		t.Fatal("premise: a place for the kept answers")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("premise: git's answer was kept: %v", err)
	}
	if err := os.WriteFile(file, []byte(`{"top":"`+other+`","at":`+strconv.FormatInt(time.Now().Unix(), 10)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := gitCallLog(t)
	event(func() {
		if got, ok := gitPlaceAt(context.Background(), repo); !ok || !sameFolder(got.top, repo) {
			t.Errorf("the place is %q (%v), want %s", got.top, ok, repo)
		}
	})
	if got := calls(); len(got) != 1 {
		t.Errorf("git was asked %d times, want once:\n%s", len(got), strings.Join(got, "\n"))
	}
}
