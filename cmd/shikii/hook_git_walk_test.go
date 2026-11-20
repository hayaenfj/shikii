package main

import (
	"context"
	"encoding/json"
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
		{"GIT_NAMESPACE in the environment", func(t *testing.T) string {
			t.Setenv("GIT_NAMESPACE", "other")
			return gitRepo(t)
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
// changes it, and is read from the files of the repository (TestHeadIsReadFromTheFiles...).
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
	if got := calls(); len(got) != 0 {
		t.Errorf("HEAD, which is not kept, is read from the files of the repository: %v", got)
	}
	// the CLI and the MCP server use the kept answer too: it is checked against the folders at every use, so a process that lives long
	// does not hold a stale one (what is not kept is the in-process memo of one event)
	if got, ok := gitPlaceAt(context.Background(), repo); !ok || !sameFolder(got.top, repo) || len(calls()) != 0 {
		t.Errorf("outside an event the kept answer is used too: %+v (%v), %d calls", got, ok, len(calls()))
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
		{"the environment names a namespace", func(t *testing.T, repo, sub string) {
			t.Setenv("GIT_NAMESPACE", "other") // the refs git reads are another set: left to git
		}, 1, func(repo, sub string) string { return repo }},
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

// Git stops at a mount point unless told otherwise: from a folder below one, it does not see the repository above. A kept answer for the
// repository above must not be used from there, so the walk leaves such a folder to git. A test cannot mount, so the folders from one
// down are said to be on another device; the control is the same folders without the mount point, where the kept answer is used.
func TestAKeptPlaceIsNotUsedFromBelowAMountPoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	mounted := filepath.Join(repo, "mounted")
	below := filepath.Join(mounted, "below")
	if err := os.MkdirAll(below, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := gitCallLog(t)
	event(func() {
		if _, ok := gitPlaceAt(context.Background(), repo); !ok {
			t.Fatal("premise: git answers about the repository")
		}
	})
	if got := calls(); len(got) != 1 {
		t.Fatalf("premise: the first event asks git once: %v", got)
	}
	event(func() { gitPlaceAt(context.Background(), below) })
	if got := calls(); len(got) != 0 {
		t.Fatalf("control: with no mount point the kept answer is used from below, but git was asked:\n%s", strings.Join(got, "\n"))
	}
	resolved, err := filepath.EvalSymlinks(mounted)
	if err != nil {
		t.Fatal(err)
	}
	real := deviceOfFolder
	deviceOfFolder = func(path string, info os.FileInfo) (uint64, bool) {
		dev, ok := real(path, info)
		if path == resolved || strings.HasPrefix(path, resolved+string(filepath.Separator)) {
			dev++ // another device than the repository's
		}
		return dev, ok
	}
	t.Cleanup(func() { deviceOfFolder = real })
	event(func() { gitPlaceAt(context.Background(), below) })
	if got := calls(); len(got) != 1 {
		t.Errorf("from below a mount point git is asked, not the kept answer: %d calls\n%s", len(got), strings.Join(got, "\n"))
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

// The test above writes the kept answer in the form 0.37.3 wrote it, which is refused for its missing fields even without the check of its top.
// A kept answer in the current form, with the git folder and the ids of the configuration files all in order but a top that names another
// folder, must be refused for its top: it is what a copied or edited file looks like.
func TestAKeptPlaceOfAnotherTopIsNotUsedWhenTheRestOfItAgrees(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, other := gitRepo(t), gitRepo(t)
	calls := gitCallLog(t)
	event(func() { gitPlaceAt(context.Background(), repo) })
	calls()
	event(func() { gitPlaceAt(context.Background(), repo) })
	if got := calls(); len(got) != 0 {
		t.Fatalf("control: the kept answer is used while nothing differs, but git was asked: %v", got)
	}
	at, _ := filepath.EvalSymlinks(repo)
	file, ok := placeEntryFile(at)
	if !ok {
		t.Fatal("premise: the kept answer has a file")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("premise: the kept answer is on disk: %v", err)
	}
	var e placeEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if e.Git == "" || e.Common == "" {
		t.Fatalf("premise: the kept answer has the fields of the current form: %s", raw)
	}
	e.Top = other // typed, so the 64-bit times of the configuration ids are written back exactly
	raw, _ = json.Marshal(e)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var got gitPlace
	event(func() { got, _ = gitPlaceAt(context.Background(), repo) })
	if n := len(calls()); n != 1 || !sameFolder(got.top, repo) {
		t.Errorf("top %q, git calls %d: the kept answer that names %s was used for %s", got.top, n, other, repo)
	}
}

// An answer is kept only where git says the same as the files do. A git that names another top than the folder with the `.git` (stood in
// for here: the first line of what it says is replaced while SHIKII_TEST_FAKE_TOP is set) is believed for that event, and nothing is kept.
// Two checks guard this, one where the answer is trusted and one where it is written, so the test is for both: it fails only without both.
func TestAnAnswerWhoseTopIsNotTheFolderWithTheGitFolderIsNotKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nout=$('" + real + "' \"$@\"); rc=$?\ncase \"$*\" in\n*--show-toplevel*) if [ -n \"$SHIKII_TEST_FAKE_TOP\" ]; then printf '%s\\n' \"$SHIKII_TEST_FAKE_TOP\"; printf '%s\\n' \"$out\" | tail -n +2; else printf '%s\\n' \"$out\"; fi;;\n*) printf '%s\\n' \"$out\";;\nesac\nexit $rc\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	faked, honest, other := gitRepo(t), gitRepo(t), t.TempDir()
	entryOf := func(repo string) bool {
		at, _ := filepath.EvalSymlinks(repo)
		file, ok := placeEntryFile(at)
		if !ok {
			t.Fatal("premise: the kept answer has a file")
		}
		_, err := os.Stat(file)
		return err == nil
	}
	t.Setenv("SHIKII_TEST_FAKE_TOP", "")
	var got gitPlace
	event(func() { got, _ = gitPlaceAt(context.Background(), honest) })
	if !sameFolder(got.top, honest) || !entryOf(honest) {
		t.Fatalf("control: an answer that agrees with the files is kept (top %q, kept %v)", got.top, entryOf(honest))
	}
	t.Setenv("SHIKII_TEST_FAKE_TOP", other)
	event(func() { got, _ = gitPlaceAt(context.Background(), faked) })
	if !sameFolder(got.top, other) {
		t.Fatalf("premise: git's answer is the one given for the event (top %q)", got.top)
	}
	if entryOf(faked) {
		t.Errorf("an answer whose top is %s was kept for the repository at %s", other, faked)
	}
}

// What git answered is kept in small files, one for each repository; a file nobody asked for in a week goes when the next one is
// written, so the folder does not grow with every repository this machine has seen.
func TestOldKeptPlacesAreRemovedWhenANewOneIsKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	dir := filepath.Dir(mustPlaceFile(t, repo))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old, recent := filepath.Join(dir, "0000000000000000.json"), filepath.Join(dir, "1111111111111111.json")
	for _, f := range []string{old, recent} {
		if err := os.WriteFile(f, []byte(`{"top":"/nowhere","at":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(old, time.Now().Add(-8*24*time.Hour), time.Now().Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	event(func() { gitPlaceAt(context.Background(), repo) })
	if _, err := os.Stat(old); err == nil {
		t.Error("a kept answer a week old was left")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("a recent kept answer was removed: %v", err)
	}
	if _, err := os.Stat(mustPlaceFile(t, repo)); err != nil {
		t.Errorf("the new answer was not kept: %v", err)
	}
}

func mustPlaceFile(t *testing.T, repo string) string {
	t.Helper()
	at, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := placeEntryFile(at)
	if !ok {
		t.Fatal("no place for the kept answers")
	}
	return file
}

// realHead is what git says HEAD is ("" before the first commit).
func realHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "-q", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// In a repository with a `.git` folder of its own, HEAD is read from the files git keeps (HEAD, the ref it names, packed-refs) instead
// of a process of its own. The answers are compared with git's own in every state the files can be in; where the files are not
// the whole story (a linked worktree, a reftable) git is asked.
func TestHeadIsReadFromTheFilesOfARepositoryWithAGitFolder(t *testing.T) {
	patientGit(t)
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	commit := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", name)
	}
	event(func() { gitPlaceAt(context.Background(), repo) }) // git answers once and the answer is kept
	calls := gitCallLog(t)
	for _, c := range []struct {
		name  string
		state func()
		from  string
	}{
		{"before the first commit", func() {}, repo},
		{"a commit (a loose ref)", func() { commit("a") }, repo},
		{"from a subfolder", func() {}, sub},
		{"refs packed into one file", func() { git("pack-refs", "--all", "--prune") }, repo},
		{"a commit on top of a packed ref", func() { commit("b") }, repo},
		{"a branch with a slash in its name", func() { git("checkout", "-q", "-b", "feature/x"); commit("c") }, repo},
		{"a detached HEAD", func() { git("checkout", "-q", "--detach") }, repo},
		{"back on a branch, packed", func() { git("checkout", "-q", "feature/x"); git("pack-refs", "--all", "--prune") }, repo},
	} {
		c.state()
		want := realHead(t, repo)
		calls() // what the test itself ran is not counted
		var got string
		event(func() { got = gitHead(c.from) })
		if got != want {
			t.Errorf("%s: HEAD is %q, git says %q", c.name, got, want)
		}
		if got := calls(); len(got) != 0 {
			t.Errorf("%s: %d git calls for HEAD, want none:\n%s", c.name, len(got), strings.Join(got, "\n"))
		}
	}
	// the files are not the whole story: git says. A branch that is a symbolic ref names another branch in its file.
	git("symbolic-ref", "refs/heads/alias", "refs/heads/feature/x")
	git("symbolic-ref", "HEAD", "refs/heads/alias")
	var viaAlias string
	event(func() { viaAlias = gitHead(repo) })
	if want := realHead(t, repo); viaAlias != want || want == "" {
		t.Errorf("HEAD through a symbolic ref is %q, git says %q", viaAlias, want)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git", "reftable"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := realHead(t, repo)
	calls()
	var got string
	event(func() { got = gitHead(repo) })
	if n := len(calls()); got != want || n == 0 {
		t.Errorf("with a reftable folder git is asked: HEAD %q (git says %q), %d calls", got, want, n)
	}
}
