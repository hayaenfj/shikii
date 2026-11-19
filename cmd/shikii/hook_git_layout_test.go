package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Where git keeps the files of a working tree is not always `<top>/.git`: a linked worktree and a submodule have a `.git` that is a file
// naming the folder, and a worktree shares its objects, refs and info/exclude with the repository it was made from. These tests build each
// of those, ask for the place of the working tree the way a hook does, and compare it with what git says.

type layoutCase struct {
	top    string // the working tree
	main   string // the repository a linked worktree was made from ("" when the layout is not a linked worktree)
	commit func(t *testing.T, name string)
	// where git keeps this working tree's files (git's own answer)
	gitDir, commonDir string
}

type layoutBuilder struct {
	name  string
	build func(t *testing.T) layoutCase
}

// gitAt runs git in dir with an identity and with the file transport allowed (a submodule of a folder next to it).
func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "protocol.file.allow=always"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("git %v: %v %s", args, err, out) // an older git without the option, or no git
	}
	return strings.TrimSpace(string(out))
}

func newRepoAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", append([]string{"init", "-q"}, append(args, dir)...)...).CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
}

func commitIn(t *testing.T, top, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(top, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, top, "add", "-A")
	gitAt(t, top, "commit", "-q", "-m", name)
}

// where git keeps the files of the working tree at top
func gitDirsOf(t *testing.T, top string) (string, string) {
	t.Helper()
	return gitAt(t, top, "rev-parse", "--absolute-git-dir"), gitAt(t, top, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

func finishLayout(t *testing.T, c layoutCase) layoutCase {
	t.Helper()
	c.gitDir, c.commonDir = gitDirsOf(t, c.top)
	c.commit = func(t *testing.T, name string) { commitIn(t, c.top, name) }
	return c
}

var layoutBuilders = []layoutBuilder{
	{"a linked worktree on a branch", func(t *testing.T) layoutCase {
		main := filepath.Join(t.TempDir(), "main")
		newRepoAt(t, main)
		commitIn(t, main, "a")
		top := filepath.Join(filepath.Dir(main), "wt")
		gitAt(t, main, "worktree", "add", "-q", top, "-b", "feat")
		return finishLayout(t, layoutCase{top: top, main: main})
	}},
	{"a linked worktree with a detached HEAD", func(t *testing.T) layoutCase {
		main := filepath.Join(t.TempDir(), "main")
		newRepoAt(t, main)
		commitIn(t, main, "a")
		top := filepath.Join(filepath.Dir(main), "wtd")
		gitAt(t, main, "worktree", "add", "-q", "--detach", top)
		return finishLayout(t, layoutCase{top: top, main: main})
	}},
	{"a linked worktree that names its place with a relative path", func(t *testing.T) layoutCase {
		main := filepath.Join(t.TempDir(), "main")
		newRepoAt(t, main)
		commitIn(t, main, "a")
		top := filepath.Join(filepath.Dir(main), "wtr")
		gitAt(t, main, "worktree", "add", "-q", "--relative-paths", top, "-b", "rel")
		return finishLayout(t, layoutCase{top: top, main: main})
	}},
	{"a linked worktree of a bare repository", func(t *testing.T) layoutCase {
		base := t.TempDir()
		src := filepath.Join(base, "src")
		newRepoAt(t, src)
		commitIn(t, src, "a")
		bare := filepath.Join(base, "bare.git")
		gitAt(t, base, "clone", "-q", "--bare", src, bare)
		top := filepath.Join(base, "wbare")
		gitAt(t, bare, "worktree", "add", "-q", top, "-b", "bw")
		return finishLayout(t, layoutCase{top: top, main: bare})
	}},
	{"a submodule", func(t *testing.T) layoutCase {
		base := t.TempDir()
		src := filepath.Join(base, "subsrc")
		newRepoAt(t, src)
		commitIn(t, src, "s")
		super := filepath.Join(base, "super")
		newRepoAt(t, super)
		gitAt(t, super, "submodule", "add", "-q", "../subsrc", "sub")
		gitAt(t, super, "commit", "-q", "-m", "add the submodule")
		return finishLayout(t, layoutCase{top: filepath.Join(super, "sub")})
	}},
	{"a working tree whose git folder is kept elsewhere", func(t *testing.T) layoutCase {
		base := t.TempDir()
		top := filepath.Join(base, "sepwt")
		newRepoAt(t, top, "--separate-git-dir="+filepath.Join(base, "sep.gitdir"))
		commitIn(t, top, "a")
		return finishLayout(t, layoutCase{top: top})
	}},
}

// A repository whose `.git` is a file is asked about once, like a plain one: the next events use the answer, read HEAD from the files
// (the worktree's own HEAD, never the one of the repository it was made from), and start no git.
func TestAPlaceWhereGitKeepsItsFilesElsewhereIsKeptLikeAPlainOne(t *testing.T) {
	for _, b := range layoutBuilders {
		t.Run(b.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			c := b.build(t)
			sub := filepath.Join(c.top, "pkg", "inner")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			calls := gitCallLog(t)
			var first gitPlace
			event(func() {
				var ok bool
				if first, ok = gitPlaceAt(context.Background(), c.top); !ok {
					t.Fatal("premise: git answers about the working tree")
				}
			})
			if got := calls(); !reflect.DeepEqual(got, []string{askedPlace}) {
				t.Fatalf("the first event asks git once: %v", got)
			}
			for _, dir := range []string{c.top, sub} {
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
			// a commit in this working tree moves its HEAD; a commit in the repository it was made from does not
			c.commit(t, "x")
			if c.main != "" { // a commit on the branch of the repository it was made from (the repository may be a bare one: no working tree)
				tree := gitAt(t, c.main, "rev-parse", "HEAD^{tree}")
				gitAt(t, c.main, "update-ref", "HEAD", gitAt(t, c.main, "commit-tree", tree, "-p", "HEAD", "-m", "elsewhere"))
			}
			want := realHead(t, c.top)
			mainHead := ""
			if c.main != "" {
				mainHead = realHead(t, c.main)
			}
			calls() // what the test itself ran is not counted
			var got string
			event(func() { got = gitHead(c.top) })
			if got != want || want == "" {
				t.Errorf("HEAD is %q, git says %q", got, want)
			}
			if c.main != "" && got == mainHead {
				t.Errorf("HEAD %q is the one of %s, not of the worktree", got, c.main)
			}
			if got := calls(); len(got) != 0 {
				t.Errorf("HEAD took %d git calls, want none:\n%s", len(got), strings.Join(got, "\n"))
			}
		})
	}
}

// HEAD of a linked worktree is read from its files in every state they can be in, compared with git's own answer.
func TestTheHeadOfALinkedWorktreeIsReadFromItsFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	main := filepath.Join(t.TempDir(), "main")
	newRepoAt(t, main)
	commitIn(t, main, "a")
	top := filepath.Join(filepath.Dir(main), "wt")
	gitAt(t, main, "worktree", "add", "-q", top, "-b", "feat")
	sub := filepath.Join(top, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	event(func() { gitPlaceAt(context.Background(), top) }) // git answers once and the answer is kept
	calls := gitCallLog(t)
	for _, c := range []struct {
		name  string
		state func()
		from  string
	}{
		{"a commit (a loose ref in the shared refs)", func() { commitIn(t, top, "b") }, top},
		{"from a subfolder", func() {}, sub},
		{"refs packed into one file", func() { gitAt(t, main, "pack-refs", "--all", "--prune") }, top},
		{"a commit on top of a packed ref", func() { commitIn(t, top, "c") }, top},
		{"a branch with a slash in its name", func() { gitAt(t, top, "checkout", "-q", "-b", "feature/x"); commitIn(t, top, "d") }, top},
		{"a detached HEAD", func() { gitAt(t, top, "checkout", "-q", "--detach") }, top},
		{"back on a branch, packed", func() { gitAt(t, top, "checkout", "-q", "feature/x"); gitAt(t, top, "pack-refs", "--all", "--prune") }, top},
		{"another worktree moved the shared branch", func() { gitAt(t, main, "update-ref", "refs/heads/feature/x", gitAt(t, main, "rev-parse", "HEAD")) }, top},
	} {
		c.state()
		want := realHead(t, top)
		calls()
		var got string
		event(func() { got = gitHead(c.from) })
		if got != want {
			t.Errorf("%s: HEAD is %q, git says %q", c.name, got, want)
		}
		if got := calls(); len(got) != 0 {
			t.Errorf("%s: %d git calls for HEAD, want none:\n%s", c.name, len(got), strings.Join(got, "\n"))
		}
	}
}

// What the files do not settle is left to git: a ref that belongs to the worktree alone (the files hold it in the worktree's own folder,
// not in the shared one), a reftable, and a symbolic ref.
func TestWhatTheFilesOfALinkedWorktreeDoNotSettleIsLeftToGit(t *testing.T) {
	for _, c := range []struct {
		name  string
		state func(t *testing.T, main, top, commonDir string)
	}{
		{"a ref of the worktree alone", func(t *testing.T, main, top, commonDir string) {
			gitAt(t, top, "symbolic-ref", "HEAD", "refs/bisect/x")
		}},
		{"a reftable folder", func(t *testing.T, main, top, commonDir string) {
			if err := os.MkdirAll(filepath.Join(commonDir, "reftable"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"a symbolic ref", func(t *testing.T, main, top, commonDir string) {
			gitAt(t, top, "symbolic-ref", "refs/heads/alias", "refs/heads/feat")
			gitAt(t, top, "symbolic-ref", "HEAD", "refs/heads/alias")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			main := filepath.Join(t.TempDir(), "main")
			newRepoAt(t, main)
			commitIn(t, main, "a")
			top := filepath.Join(filepath.Dir(main), "wt")
			gitAt(t, main, "worktree", "add", "-q", top, "-b", "feat")
			event(func() { gitPlaceAt(context.Background(), top) })
			_, commonDir := gitDirsOf(t, top)
			c.state(t, main, top, commonDir)
			want := realHead(t, top)
			calls := gitCallLog(t)
			var got string
			event(func() { got = gitHead(top) })
			if got != want {
				t.Errorf("HEAD is %q, git says %q", got, want)
			}
			if len(calls()) == 0 {
				t.Error("git was not asked, though the files do not settle HEAD")
			}
		})
	}
}

// A kept answer is trusted while the folders are as they were: each of these is asked about again, and nothing that did not change is.
func TestAKeptPlaceOfAWorktreeOrSubmoduleIsLeftWhenTheFoldersChange(t *testing.T) {
	for _, b := range []layoutBuilder{layoutBuilders[0], layoutBuilders[4], layoutBuilders[5]} { // a worktree, a submodule, a separate git folder
		for _, c := range []struct {
			name   string
			change func(t *testing.T, l layoutCase)
			asked  bool
		}{
			{"nothing changed", func(*testing.T, layoutCase) {}, false},
			{"the .git file names another place", func(t *testing.T, l layoutCase) {
				writeFile(t, filepath.Join(l.top, ".git"), "gitdir: "+filepath.Join(l.top, "nowhere")+"\n")
			}, true},
			{"the .git file has a carriage return", func(t *testing.T, l layoutCase) {
				writeFile(t, filepath.Join(l.top, ".git"), "gitdir: "+l.gitDir+"\r\n")
			}, true},
			{"the .git file has two lines", func(t *testing.T, l layoutCase) {
				writeFile(t, filepath.Join(l.top, ".git"), "gitdir: "+l.gitDir+"\nmore\n")
			}, true},
			{"the .git file is far longer than a path", func(t *testing.T, l layoutCase) {
				writeFile(t, filepath.Join(l.top, ".git"), "gitdir: "+l.gitDir+strings.Repeat("/", 5000)+"\n")
			}, true},
			{"HEAD of the git folder is gone", func(t *testing.T, l layoutCase) { mustRemove(t, filepath.Join(l.gitDir, "HEAD")) }, true},
			{"the object folder is gone", func(t *testing.T, l layoutCase) { mustRemove(t, filepath.Join(l.commonDir, "objects")) }, true},
			{"the refs folder is gone", func(t *testing.T, l layoutCase) { mustRemove(t, filepath.Join(l.commonDir, "refs")) }, true},
			{"the shared configuration was written later", func(t *testing.T, l layoutCase) {
				later := time.Now().Add(10 * time.Second) // after the answer the test is about to keep, however long the cases before took
				if err := os.Chtimes(filepath.Join(l.commonDir, "config"), later, later); err != nil {
					t.Fatal(err)
				}
			}, true},
			{"the configuration of the worktree alone was written later", func(t *testing.T, l layoutCase) {
				file := filepath.Join(l.gitDir, "config.worktree")
				writeFile(t, file, "")
				later := time.Now().Add(10 * time.Second)
				if err := os.Chtimes(file, later, later); err != nil {
					t.Fatal(err)
				}
			}, true},
		} {
			t.Run(b.name+": "+c.name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				l := b.build(t)
				event(func() { gitPlaceAt(context.Background(), l.top) })
				calls := gitCallLog(t)
				c.change(t, l)
				calls() // what the change itself ran is not counted
				event(func() { gitPlaceAt(context.Background(), l.top) })
				if n := len(calls()); c.asked && n == 0 {
					t.Error("the kept answer was used, git was not asked")
				} else if !c.asked && n != 0 {
					t.Errorf("nothing changed, yet git was asked %d times", n)
				}
			})
		}
	}
}

// Once the worktree is made again, with the `.git` file naming another git folder, the places are those of the new one, not what was kept.
func TestAWorktreeMadeAgainIsNotTakenForTheOldOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	main := filepath.Join(t.TempDir(), "main")
	newRepoAt(t, main)
	commitIn(t, main, "a")
	other := filepath.Join(filepath.Dir(main), "other")
	newRepoAt(t, other)
	commitIn(t, other, "o")
	top := filepath.Join(filepath.Dir(main), "wt")
	gitAt(t, main, "worktree", "add", "-q", top, "-b", "feat")
	event(func() { gitPlaceAt(context.Background(), top) })
	// the folder becomes a worktree of another repository (the .git file names a git folder there)
	gitAt(t, other, "worktree", "add", "-q", filepath.Join(filepath.Dir(main), "tmpwt"), "-b", "feat2")
	newGitFile, err := os.ReadFile(filepath.Join(filepath.Dir(main), "tmpwt", ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, ".git"), newGitFile, 0o644); err != nil {
		t.Fatal(err)
	}
	calls := gitCallLog(t)
	var got gitPlace
	event(func() {
		var ok bool
		if got, ok = gitPlaceAt(context.Background(), top); !ok {
			t.Fatal("premise: the working tree is still a repository")
		}
	})
	if len(calls()) == 0 {
		t.Error("the answer kept for the old git folder was used for the one the .git file names now")
	}
	otherObjects := filepath.Join(other, ".git", "objects")
	if realObjects, _ := filepath.EvalSymlinks(otherObjects); got.objects != otherObjects && got.objects != realObjects {
		t.Errorf("objects are %q, want those of the repository the .git file names now (%s)", got.objects, otherObjects)
	}
}

// In a hook event the git calls of a request, a tool call and a Stop are the same in a linked worktree or a submodule as in a plain
// repository (TestAHookEventAsksGitWhereTheRepositoryIsOnce): one question for the place at the very first, and none after.
func TestAHookEventInAWorktreeOrSubmoduleAsksGitWhereTheRepositoryIsOnce(t *testing.T) {
	const place = "rev-parse --show-toplevel --git-path objects --git-path info/exclude --verify -q HEAD"
	snapshot := []string{"add -A -- .", "ls-files -z --cached --ignored --exclude-standard", "ls-files --stage --full-name -- . :(exclude).shikii"}
	for n, b := range []layoutBuilder{layoutBuilders[0], layoutBuilders[4]} {
		t.Run(b.name, func(t *testing.T) {
			l := b.build(t)
			model := fakeAgentModel(t, func(string) float64 { return .9 })
			root := hookEnv(t, model, l.top)
			usePlanningSpec(t, root)
			t.Chdir(l.top)
			forgetKeptPlaces(t)
			calls := gitCallLog(t)
			for i := 0; i < 2; i++ {
				session := "layout-" + string(rune('a'+n)) + string(rune('a'+i)) // a session of its own: a request that follows another of its session does more
				runManualHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": l.top, "prompt": planningTestRequest})
				want := snapshot
				if i == 0 {
					want = append([]string{place}, snapshot...)
				}
				if got := calls(); !reflect.DeepEqual(got, want) {
					t.Errorf("request %d made these git calls:\n%s\nwant:\n%s", i+1, strings.Join(got, "\n"), strings.Join(want, "\n"))
				}
				runManualHook(t, "tool-lesson", map[string]any{"session_id": session, "cwd": l.top, "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}})
				if got := calls(); len(got) != 0 {
					t.Errorf("a tool call made git calls:\n%s", strings.Join(got, "\n"))
				}
				writeFile(t, filepath.Join(l.top, "README.md"), "# changed "+string(rune('a'+i))+"\n")
				runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": l.top, "last_assistant_message": "Done."})
				if got := calls(); !reflect.DeepEqual(got, snapshot) {
					t.Errorf("a Stop made these git calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(snapshot, "\n"))
				}
			}
		})
	}
}

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

// lyingGit puts a git in front of the real one that changes what the question about the place answers (sed on its lines), to be
// asked about a repository whose files say something else than git does.
func lyingGit(t *testing.T, sedProgram string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = rev-parse ] && [ \"$2\" = --show-toplevel ]; then\n  out=$('" + real + "' \"$@\"); rc=$?\n  printf '%s\\n' \"$out\" | sed '" + sedProgram + "'\n  exit $rc\nfi\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A place is kept, and HEAD read from the files, only where the files say what git said about the same working tree: the object folder,
// the exclude file and HEAD. Where git says something else (here, a git that answers another object folder, another exclude file or
// another HEAD), nothing is kept and git is asked every time.
func TestWhatTheFilesSayIsNotTrustedWhereGitSaidSomethingElse(t *testing.T) {
	plain := layoutBuilder{"a plain repository", func(t *testing.T) layoutCase {
		top := gitRepo(t)
		commitIn(t, top, "a")
		return finishLayout(t, layoutCase{top: top})
	}}
	for _, b := range append([]layoutBuilder{plain}, layoutBuilders[0], layoutBuilders[4]) {
		for _, c := range []struct{ name, sed string }{
			{"another object folder", `s#objects$#lying-objects#`},
			{"another exclude file", `s#info/exclude$#info/lying-exclude#`},
			{"another HEAD", `4s#.*#0000000000000000000000000000000000000000#`},
		} {
			t.Run(b.name+": "+c.name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				l := b.build(t)
				lyingGit(t, c.sed)
				calls := gitCallLog(t)
				var first gitPlace
				event(func() { first, _ = gitPlaceAt(context.Background(), l.top) })
				if len(calls()) == 0 {
					t.Fatal("premise: the first event asks git")
				}
				event(func() { gitPlaceAt(context.Background(), l.top) })
				if len(calls()) == 0 {
					t.Error("a place the files do not agree with git about was kept: the next event did not ask git")
				}
				if first.gitDir != "" {
					t.Errorf("HEAD would be read from files that do not agree with git: %+v", first)
				}
			})
		}
	}
}

// A `.git` file is read the way git reads it, and the forms git reads another way are left to it: nothing is derived from them.
func TestAGitFileGitMayReadAnotherWayIsLeftToGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	top := filepath.Join(base, "sepwt")
	newRepoAt(t, top, "--separate-git-dir="+filepath.Join(base, "sep.gitdir"))
	commitIn(t, top, "a")
	gitDir, _ := gitDirsOf(t, top)
	elsewhere := filepath.Join(base, "other", "deep")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(base, "lnk")); err != nil {
		t.Fatal(err)
	}
	dotGit := filepath.Join(top, ".git")
	for _, c := range []struct {
		name, content string
		derived       bool
	}{
		{"an absolute path", "gitdir: " + gitDir + "\n", true},
		{"no line end", "gitdir: " + gitDir, true},
		{"a path from the folder of the file", "gitdir: ../sep.gitdir\n", true},
		{"a link followed by a step up (the folder above what the link names)", "gitdir: ../lnk/../sep.gitdir\n", false},
		{"a carriage return", "gitdir: " + gitDir + "\r\n", false},
		{"a second line", "gitdir: " + gitDir + "\nmore\n", false},
		{"a space before the path", "gitdir:  " + gitDir + "\n", false},
		{"a space after the path", "gitdir: " + gitDir + " \n", false},
		{"another word", "GITDIR: " + gitDir + "\n", false},
		{"no path", "gitdir: \n", false},
		{"a path that is not there", "gitdir: ../nowhere\n", false},
		{"nothing in it", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := os.RemoveAll(dotGit); err != nil {
				t.Fatal(err)
			}
			writeFile(t, dotGit, c.content)
			l, ok := layoutAt(top)
			if ok != c.derived {
				t.Fatalf("derived %v (%+v), want %v", ok, l, c.derived)
			}
			if ok && l.gitDir != gitDir {
				t.Errorf("the git folder is %q, git says %q", l.gitDir, gitDir)
			}
		})
	}
	t.Run("a link in place of the file", func(t *testing.T) {
		if err := os.Remove(dotGit); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(base, "gitfile")
		writeFile(t, target, "gitdir: "+gitDir+"\n")
		if err := os.Symlink(target, dotGit); err != nil {
			t.Fatal(err)
		}
		if l, ok := layoutAt(top); ok {
			t.Errorf("derived %+v from a link", l)
		}
	})
}

// What git asks of a git folder is asked here: its HEAD, an object folder and a refs folder that are folders (not links), and a
// commondir file that is a plain one-line file naming a folder.
func TestAGitFolderThatIsNotOneIsLeftToGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	main := filepath.Join(t.TempDir(), "main")
	newRepoAt(t, main)
	commitIn(t, main, "a")
	top := filepath.Join(filepath.Dir(main), "wt")
	gitAt(t, main, "worktree", "add", "-q", top, "-b", "feat")
	gitDir, commonDir := gitDirsOf(t, top)
	if _, ok := layoutAt(top); !ok {
		t.Fatal("premise: the worktree is derived")
	}
	move := func(t *testing.T, path string) {
		t.Helper()
		if err := os.Rename(path, path+".away"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Rename(path+".away", path) })
	}
	linkInstead := func(t *testing.T, path string) {
		t.Helper()
		move(t, path)
		if err := os.Symlink(path+".away", path); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(path) })
	}
	for _, c := range []struct {
		name   string
		break_ func(t *testing.T)
	}{
		{"no HEAD", func(t *testing.T) { move(t, filepath.Join(gitDir, "HEAD")) }},
		{"HEAD is a link", func(t *testing.T) { linkInstead(t, filepath.Join(gitDir, "HEAD")) }},
		{"no object folder", func(t *testing.T) { move(t, filepath.Join(commonDir, "objects")) }},
		{"the object folder is a link", func(t *testing.T) { linkInstead(t, filepath.Join(commonDir, "objects")) }},
		{"no refs folder", func(t *testing.T) { move(t, filepath.Join(commonDir, "refs")) }},
		{"the refs folder is a link", func(t *testing.T) { linkInstead(t, filepath.Join(commonDir, "refs")) }},
		{"no commondir file", func(t *testing.T) { move(t, filepath.Join(gitDir, "commondir")) }},
		{"the commondir file is a link", func(t *testing.T) { linkInstead(t, filepath.Join(gitDir, "commondir")) }},
		{"the commondir file has a carriage return", func(t *testing.T) {
			move(t, filepath.Join(gitDir, "commondir"))
			writeFile(t, filepath.Join(gitDir, "commondir"), "../..\r\n")
			t.Cleanup(func() { os.Remove(filepath.Join(gitDir, "commondir")) })
		}},
		{"the commondir file names nothing", func(t *testing.T) {
			move(t, filepath.Join(gitDir, "commondir"))
			writeFile(t, filepath.Join(gitDir, "commondir"), "../nowhere\n")
			t.Cleanup(func() { os.Remove(filepath.Join(gitDir, "commondir")) })
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.break_(t)
			// (without a commondir file the git folder of a worktree is its own common one, and has no object folder: it is no git folder)
			if l, ok := layoutAt(top); ok {
				t.Errorf("derived %+v", l)
			}
		})
	}
	// a `.git` folder that says it is shared with another is left to git (a folder is derived only when it stands alone)
	plain := gitRepo(t)
	writeFile(t, filepath.Join(plain, ".git", "commondir"), "../elsewhere\n")
	if l, ok := layoutAt(plain); ok {
		t.Errorf("derived %+v for a .git folder with a commondir", l)
	}
}

// HEAD of a worktree is read where git keeps it: the ref in the shared folder, except for the refs that belong to one worktree.
func TestTheRefsOfAWorktreeAloneAreNotReadFromTheSharedFolder(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, ref := range []string{"refs/bisect/x", "refs/worktree/x", "refs/rewritten/x"} {
		t.Run(ref, func(t *testing.T) {
			gitDir, commonDir := t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: "+ref+"\n")
			if err := os.MkdirAll(filepath.Dir(filepath.Join(commonDir, ref)), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(commonDir, ref), sha+"\n")
			if head, known := headFromFiles(gitDir, commonDir); known {
				t.Errorf("answered %q from the shared folder for a ref of one worktree", head)
			}
			// in one folder (a plain repository) the ref is where it is
			if err := os.MkdirAll(filepath.Dir(filepath.Join(gitDir, ref)), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(gitDir, ref), sha+"\n")
			if head, known := headFromFiles(gitDir, gitDir); !known || head != sha {
				t.Errorf("a plain repository reads %q (%v), want %s", head, known, sha)
			}
		})
	}
}

// A kept answer of 0.37.3 and before holds no state of the configurations: it is not used (git is asked once, and keeps a new one).
func TestAKeptAnswerOfAnEarlierVersionIsNotUsed(t *testing.T) {
	for _, b := range []layoutBuilder{
		{"a plain repository", func(t *testing.T) layoutCase {
			top := gitRepo(t)
			commitIn(t, top, "a")
			return finishLayout(t, layoutCase{top: top})
		}},
		layoutBuilders[0],
	} {
		t.Run(b.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			l := b.build(t)
			file := mustPlaceFile(t, l.top)
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			at, err := filepath.EvalSymlinks(l.top)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, file, fmt.Sprintf(`{"top":%q,"at":%d}`, at, time.Now().Unix()))
			calls := gitCallLog(t)
			event(func() { gitPlaceAt(context.Background(), l.top) })
			if len(calls()) == 0 {
				t.Error("an answer that holds no state of the configurations was used")
			}
			event(func() { gitPlaceAt(context.Background(), l.top) })
			if n := len(calls()); n != 0 {
				t.Errorf("the answer git gave was not kept: %d git calls", n)
			}
		})
	}
}

// What was written to the configuration after git answered is found whenever it was written: in the same second as the answer, and with
// a modification time set back (a file moved or copied into place, a restore), which a comparison of times missed.
func TestAConfigurationWrittenAfterTheAnswerIsFoundHoweverItsTimeIsSet(t *testing.T) {
	sep := layoutBuilders[5]
	for _, c := range []struct {
		name  string
		build layoutBuilder
		do    func(t *testing.T, l layoutCase)
	}{
		{"core.worktree written at once, in the same second", sep, func(t *testing.T, l layoutCase) {
			gitAt(t, l.top, "config", "core.worktree", filepath.Join(filepath.Dir(l.top), "other"))
		}},
		{"a configuration moved into place with an old time", sep, func(t *testing.T, l layoutCase) {
			config := filepath.Join(l.commonDir, "config")
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			moved := filepath.Join(filepath.Dir(l.commonDir), "config.moved")
			writeFile(t, moved, string(raw)+"[core]\n\tworktree = "+filepath.Join(filepath.Dir(l.top), "other")+"\n")
			old := time.Now().Add(-48 * time.Hour)
			if err := os.Chtimes(moved, old, old); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(moved, config); err != nil {
				t.Fatal(err)
			}
		}},
		{"a configuration overwritten in place with the same size and its time set back", sep, func(t *testing.T, l layoutCase) {
			config := filepath.Join(l.commonDir, "config")
			before, err := os.Stat(config)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "bare = false") {
				t.Fatalf("premise: the configuration says bare = false:\n%s", raw)
			}
			// the same length (git trims the space after true), the same inode, the same modification time: only the change time moves
			if err := os.WriteFile(config, []byte(strings.Replace(string(raw), "bare = false", "bare = true ", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(config, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			if after, _ := os.Stat(config); after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
				t.Fatalf("premise: the file looks as it did (%d %v, was %d %v)", after.Size(), after.ModTime(), before.Size(), before.ModTime())
			}
		}},
		{"a configuration of the worktree alone moved into place with an old time", layoutBuilders[0], func(t *testing.T, l layoutCase) {
			moved := filepath.Join(filepath.Dir(l.top), "config.worktree.moved")
			writeFile(t, moved, "[core]\n\tworktree = "+filepath.Join(filepath.Dir(l.top), "other")+"\n")
			old := time.Now().Add(-48 * time.Hour)
			if err := os.Chtimes(moved, old, old); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(moved, filepath.Join(l.gitDir, "config.worktree")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a configuration of the worktree alone made", layoutBuilders[0], func(t *testing.T, l layoutCase) {
			writeFile(t, filepath.Join(l.gitDir, "config.worktree"), "")
			old := time.Now().Add(-48 * time.Hour)
			if err := os.Chtimes(filepath.Join(l.gitDir, "config.worktree"), old, old); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			l := c.build.build(t)
			event(func() { gitPlaceAt(context.Background(), l.top) })
			calls := gitCallLog(t)
			c.do(t, l)
			calls()
			event(func() { gitPlaceAt(context.Background(), l.top) })
			if len(calls()) == 0 {
				t.Error("the kept answer was used after the configuration was written")
			}
		})
	}
}

// A submodule folder that is replaced by another one (a copy of it) is the other one: the `.git` file now names another git folder,
// which was there before the answer was kept and says another top folder in its configuration (core.worktree). The answer kept for the
// first is not used for it.
func TestASubmoduleReplacedByAnotherIsNotTakenForTheOldOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	for _, name := range []string{"s1", "s2"} {
		src := filepath.Join(base, name)
		newRepoAt(t, src)
		commitIn(t, src, name)
	}
	super := filepath.Join(base, "super")
	newRepoAt(t, super)
	gitAt(t, super, "submodule", "add", "-q", "../s1", "a")
	gitAt(t, super, "submodule", "add", "-q", "../s2", "b")
	gitAt(t, super, "commit", "-q", "-m", "submodules")
	a, b := filepath.Join(super, "a"), filepath.Join(super, "b")
	event(func() { gitPlaceAt(context.Background(), a) })
	calls := gitCallLog(t)
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-R", b, a).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	calls()
	var got gitPlace
	var ok bool
	event(func() { got, ok = gitPlaceAt(context.Background(), a) })
	if len(calls()) == 0 {
		t.Error("the answer kept for the first submodule was used for the one that replaced it")
	}
	// what git says is the truth, whichever way it is told
	if want := gitAt(t, a, "rev-parse", "--show-toplevel"); !ok || !sameFolder(got.top, want) {
		t.Errorf("the top folder is %q (%v), git says %q", got.top, ok, want)
	}
}

// A git folder whose HEAD holds neither a ref nor a commit, or whose object folder cannot be entered, is no repository to git: nothing is
// derived from it, so a kept answer is not used for it.
func TestAGitFolderGitWouldRefuseIsNotDerived(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	main := filepath.Join(t.TempDir(), "main")
	newRepoAt(t, main)
	commitIn(t, main, "a")
	top := filepath.Join(filepath.Dir(main), "wt")
	gitAt(t, main, "worktree", "add", "-q", top, "-b", "feat")
	gitDir, commonDir := gitDirsOf(t, top)
	for _, c := range []struct {
		name string
		do   func(t *testing.T) func()
	}{
		{"HEAD holds nothing", func(t *testing.T) func() { writeFile(t, filepath.Join(gitDir, "HEAD"), ""); return func() {} }},
		{"HEAD holds something else", func(t *testing.T) func() { writeFile(t, filepath.Join(gitDir, "HEAD"), "garbage\n"); return func() {} }},
		{"the object folder cannot be entered", func(t *testing.T) func() {
			if err := os.Chmod(filepath.Join(commonDir, "objects"), 0o600); err != nil {
				t.Fatal(err)
			}
			return func() { os.Chmod(filepath.Join(commonDir, "objects"), 0o755) }
		}},
		{"the refs folder cannot be entered", func(t *testing.T) func() {
			if err := os.Chmod(filepath.Join(commonDir, "refs"), 0o600); err != nil {
				t.Fatal(err)
			}
			return func() { os.Chmod(filepath.Join(commonDir, "refs"), 0o755) }
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			head, _ := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			defer os.WriteFile(filepath.Join(gitDir, "HEAD"), head, 0o644)
			defer c.do(t)()
			if os.Getuid() == 0 && strings.Contains(c.name, "entered") {
				t.Skip("root enters every folder")
			}
			if l, ok := layoutAt(top); ok {
				t.Errorf("derived %+v from a git folder git refuses", l)
			}
		})
	}
}

// A loose ref that is a link is a symbolic ref written the old way, which git follows from the folder the link is in: the files do not
// say what HEAD is, git does.
func TestAHeadThatNamesALinkAsItsRefIsLeftToGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	commitIn(t, repo, "a")
	branch := strings.TrimSpace(gitAt(t, repo, "symbolic-ref", "--short", "HEAD"))
	gitAt(t, repo, "branch", "f")
	loose := filepath.Join(repo, ".git", "refs", "heads", "f")
	if err := os.Remove(loose); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("refs/heads/"+branch, loose); err != nil {
		t.Fatal(err)
	}
	gitAt(t, repo, "symbolic-ref", "HEAD", "refs/heads/f")
	if head, known := headFromFiles(filepath.Join(repo, ".git"), filepath.Join(repo, ".git")); known {
		t.Errorf("answered %q from a loose ref that is a link; git says %q", head, realHead(t, repo))
	}
}

// A `.git` file turned to another git folder is the other one even where the two folders hold the same configuration file (a hard link:
// the same inode, size and times): the kept answer names the git folder it was given for, and the files are not asked to tell them apart.
func TestAKeptAnswerNamesTheGitFolderItWasGivenFor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	var tops, gitDirs [2]string
	for i, name := range []string{"a", "b"} {
		tops[i] = filepath.Join(base, name)
		newRepoAt(t, tops[i], "--separate-git-dir="+filepath.Join(base, name+".gitdir"))
		commitIn(t, tops[i], name)
		gitDirs[i], _ = gitDirsOf(t, tops[i])
	}
	if err := os.Remove(filepath.Join(gitDirs[1], "config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(gitDirs[0], "config"), filepath.Join(gitDirs[1], "config")); err != nil {
		t.Fatal(err)
	}
	event(func() { gitPlaceAt(context.Background(), tops[0]) })
	writeFile(t, filepath.Join(tops[0], ".git"), "gitdir: "+gitDirs[1]+"\n")
	calls := gitCallLog(t)
	event(func() { gitPlaceAt(context.Background(), tops[0]) })
	if len(calls()) == 0 {
		t.Error("the answer kept for one git folder was used for another that holds the same configuration file")
	}
}

// The change time is read on this system, and it moves when the modification time is set back (which is why it is part of a file's id).
func TestTheChangeTimeMovesWhenTheModificationTimeIsSetBack(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	writeFile(t, file, "[core]\n")
	first, ok := statID(file)
	if !ok || !first.Exists || first.Ctime <= 0 {
		t.Fatalf("the id of a file is %+v (%v)", first, ok)
	}
	time.Sleep(20 * time.Millisecond)
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	second, _ := statID(file)
	if second.Mtime != old.UnixNano() {
		t.Fatalf("premise: the modification time was set back (%d)", second.Mtime)
	}
	if second == first || second.Ctime <= first.Ctime {
		t.Errorf("the id did not change when the modification time was set back: %+v then %+v", first, second)
	}
	if gone, ok := statID(file + ".none"); !ok || gone.Exists {
		t.Errorf("a file that is not there is a zero id: %+v (%v)", gone, ok)
	}
}
