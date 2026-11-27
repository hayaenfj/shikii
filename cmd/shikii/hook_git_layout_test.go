package main

import (
	"context"
	"encoding/json"
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

// gitAt runs git in dir with an identity and with the file transport allowed (a submodule of a folder next to it). Automatic maintenance
// is off: after a commit git starts `git maintenance run --auto`, a process more (about 7 ms of a commit that takes 18) that a repository
// of a few files never needs, and the layouts of this file are built by dozens of commits.
func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "protocol.file.allow=always", "-c", "maintenance.auto=false"}, args...)...)
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
	if len(args) == 0 { // a plain repository: the .git that a real git init made once in this process (gitRepoWith), copied
		if key, ok := gitInitEnvKey(); ok {
			if tmpl := gitInitTemplateFor(key); tmpl != nil {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				writeTree(t, dir, tmpl)
				return
			}
		}
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

// where git keeps the files of the working tree at top (both answers come from one run of git: a run is a process, and a layout is
// built for most of the cases of this file)
func gitDirsOf(t *testing.T, top string) (string, string) {
	t.Helper()
	lines := strings.Split(gitAt(t, top, "rev-parse", "--absolute-git-dir", "--path-format=absolute", "--git-common-dir"), "\n")
	if len(lines) != 2 {
		t.Fatalf("git rev-parse gave %d lines for the git folder and the shared one, want 2: %q", len(lines), lines)
	}
	return lines[0], lines[1]
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
	// the stub that writes the calls down is written once for the layouts (a new file is slow to run the first time on macOS)
	calls := gitCallLog(t)
	for _, b := range layoutBuilders {
		t.Run(b.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			c := b.build(t)
			sub := filepath.Join(c.top, "pkg", "inner")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			calls() // what the build ran is not counted
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
	patientGit(t)
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

// layoutFolder is the folder that holds all of a layout: the working tree, its git folder, the shared one and the repository a worktree
// was made from (the nearest folder above all of them), found by the real paths. It must be inside the folders the test owns (inside the
// parent of own, a folder made by t.TempDir), because the caller removes it.
func layoutFolder(t *testing.T, l layoutCase, own string) string {
	t.Helper()
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	dir := filepath.Dir(real(l.top))
	for _, p := range []string{l.gitDir, l.commonDir, l.main} {
		if p == "" {
			continue
		}
		p = real(p)
		for p != dir && !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			dir = filepath.Dir(dir)
		}
	}
	if above := real(filepath.Dir(own)); !strings.HasPrefix(dir, above+string(filepath.Separator)) {
		t.Fatalf("the folder of the layout, %s, is not inside the folders of the test, %s", dir, above)
	}
	return dir
}

// copyFolder copies the folder src to dst, which is not there yet. cp keeps the modes and the links, and the files are new ones, written
// now, as the files of a build are (a case about what is written in the same second as the answer depends on that).
func copyFolder(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("cp", "-R", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
}

// A kept answer is trusted while the folders are as they were: each of these is asked about again, and nothing that did not change is.
// A layout is built once for its cases (a build is about eight runs of git) and put back as it was before each case, as each changes it:
// the copy is of the folder that was built, so a case starts from the same files as it would from a build of its own.
func TestAKeptPlaceOfAWorktreeOrSubmoduleIsLeftWhenTheFoldersChange(t *testing.T) {
	// the stub that writes the calls down is written once (a new file is slow to run the first time on macOS); the calls a case runs
	// before it reads them are not counted
	calls := gitCallLog(t)
	for _, b := range []layoutBuilder{layoutBuilders[0], layoutBuilders[4], layoutBuilders[5]} { // a worktree, a submodule, a separate git folder
		own := t.TempDir()
		l := b.build(t)
		folder := layoutFolder(t, l, own)
		saved := filepath.Join(own, "saved")
		copyFolder(t, folder, saved)
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
				mustRemove(t, folder)
				copyFolder(t, saved, folder)
				event(func() { gitPlaceAt(context.Background(), l.top) })
				c.change(t, l)
				calls() // what the first question and the change ran is not counted
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
	script := "if [ \"$1\" = rev-parse ] && [ \"$2\" = --show-toplevel ]; then\n  out=$('" + real + "' \"$@\"); rc=$?\n  printf '%s\\n' \"$out\" | sed '" + sedProgram + "'\n  exit $rc\nfi\nexec '" + real + "' \"$@\"\n"
	writeStub(t, bin, "git", script)
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
	builders := append([]layoutBuilder{plain}, layoutBuilders[0], layoutBuilders[4])
	seds := []struct{ name, sed string }{
		{"another object folder", `s#objects$#lying-objects#`},
		{"another exclude file", `s#info/exclude$#info/lying-exclude#`},
		{"another HEAD", `4s#.*#0000000000000000000000000000000000000000#`},
	}
	// A layout is built once for its cases and a git that lies is written once for each way of lying (a new file is slow to run the first
	// time on macOS): a case only asks about the place, twice, and writes nothing in the layout (what it keeps goes to its own HOME). A
	// case puts the stub on PATH itself, after the layout is built by the real git. The calls are written down by git itself behind the
	// stub (traceCallLog: a git that lies still runs the real one for every call), so no stub is written for the log.
	layouts := make([]layoutCase, len(builders))
	for i, b := range builders {
		layouts[i] = b.build(t)
	}
	cleanPath := os.Getenv("PATH")
	paths := make([]string, len(seds))
	for i, c := range seds {
		lyingGit(t, c.sed)
		paths[i] = os.Getenv("PATH")
		t.Setenv("PATH", cleanPath)
	}
	for i, b := range builders {
		for j, c := range seds {
			t.Run(b.name+": "+c.name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("PATH", paths[j])
				l, calls := layouts[i], traceCallLog(t)
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
	// Each layout is built once, the first time a case needs it, and put back as it was before each case (the cases change it), as in
	// TestAKeptPlaceOfAWorktreeOrSubmoduleIsLeftWhenTheFoldersChange; the stub that writes the calls down is written once.
	type builtLayout struct {
		l             layoutCase
		folder, saved string
	}
	built := map[string]builtLayout{}
	calls := gitCallLog(t)
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
		b, ok := built[c.build.name]
		if !ok {
			own := t.TempDir()
			l := c.build.build(t)
			folder := layoutFolder(t, l, own)
			saved := filepath.Join(own, "saved")
			copyFolder(t, folder, saved)
			b = builtLayout{l, folder, saved}
			built[c.build.name] = b
		}
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			mustRemove(t, b.folder)
			copyFolder(t, b.saved, b.folder)
			event(func() { gitPlaceAt(context.Background(), b.l.top) })
			c.do(t, b.l)
			calls() // what the first question and the change ran is not counted
			event(func() { gitPlaceAt(context.Background(), b.l.top) })
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

// keptThenChanged keeps the place of the working tree at top (one event asks git, the next uses the answer), makes `change`, and asks
// again: what the last event answered and how many git calls it made.
func keptThenChanged(t *testing.T, top string, change func()) (gitPlace, bool, int) {
	t.Helper()
	calls := gitCallLog(t)
	event(func() {
		if _, ok := gitPlaceAt(context.Background(), top); !ok {
			t.Fatal("premise: git answers about the working tree")
		}
	})
	calls()
	event(func() { gitPlaceAt(context.Background(), top) })
	if n := len(calls()); n != 0 {
		t.Fatalf("premise: the answer is kept (the second event asked git %d times)", n)
	}
	change()
	calls() // what the change itself ran is not counted
	var got gitPlace
	var ok bool
	event(func() { got, ok = gitPlaceAt(context.Background(), top) })
	return got, ok, len(calls())
}

func gitTopOf(t *testing.T, dir string) string {
	t.Helper()
	return gitAt(t, dir, "rev-parse", "--show-toplevel")
}

// The top folder of a working tree can be named by the configuration (core.worktree), through a link; when the link is pointed
// elsewhere, git's answer is another folder while the `.git` file, the git folder and the configuration are all as they were. The kept
// answer is not used then.
func TestAKeptTopFollowsALinkThatTheConfigurationNamesItThrough(t *testing.T) {
	for _, kind := range []string{"a working tree whose git folder is kept elsewhere", "a plain repository", "a submodule"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a, b, link := filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "link")
			top := filepath.Join(a, "w")
			switch kind {
			case "a working tree whose git folder is kept elsewhere":
				newRepoAt(t, top, "--separate-git-dir="+filepath.Join(base, "sep.git"))
			case "a plain repository":
				newRepoAt(t, top)
			case "a submodule":
				src := filepath.Join(base, "subsrc")
				newRepoAt(t, src)
				commitIn(t, src, "s")
				newRepoAt(t, a)
				gitAt(t, a, "submodule", "add", "-q", "../subsrc", "w")
			}
			if err := os.MkdirAll(filepath.Join(b, "w"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(a, link); err != nil {
				t.Fatal(err)
			}
			gitAt(t, top, "config", "core.worktree", filepath.Join(link, "w"))
			if got := gitTopOf(t, top); got != top {
				t.Fatalf("premise: git says the top folder is %q", got)
			}
			got, ok, asked := keptThenChanged(t, top, func() {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(b, link); err != nil {
					t.Fatal(err)
				}
			})
			if want := gitTopOf(t, top); asked == 0 && (!ok || got.top != want) {
				t.Errorf("the kept top folder %q (%v) was used with no git call, git says %q", got.top, ok, want)
			}
		})
	}
	t.Run("a submodule whose own folder is a link", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		base, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(base, "subsrc")
		newRepoAt(t, src)
		commitIn(t, src, "s")
		super := filepath.Join(base, "super")
		newRepoAt(t, super)
		gitAt(t, super, "submodule", "add", "-q", "../subsrc", "w")
		x, y := filepath.Join(base, "X"), filepath.Join(base, "Y")
		if err := os.Rename(filepath.Join(super, "w"), x); err != nil { // the checkout moves out, a link stays in its place
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(x, ".git"), "gitdir: "+filepath.Join(super, ".git", "modules", "w")+"\n")
		if err := os.Symlink(x, filepath.Join(super, "w")); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(y, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := gitTopOf(t, x); got != x {
			t.Fatalf("premise: git says the top folder is %q, want %q", got, x)
		}
		got, ok, asked := keptThenChanged(t, x, func() {
			if err := os.Remove(filepath.Join(super, "w")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(y, filepath.Join(super, "w")); err != nil {
				t.Fatal(err)
			}
		})
		if want := gitTopOf(t, x); asked == 0 && (!ok || got.top != want) {
			t.Errorf("the kept top folder %q (%v) was used with no git call, git says %q", got.top, ok, want)
		}
	})
}

// A repository git reads in a form it accepts is kept as before: a refs folder that is a link (git follows it), and a HEAD written
// `ref:refs/...` (no space; git skips the spaces after the colon).
func TestAPlainRepositoryInAFormGitAcceptsIsStillKept(t *testing.T) {
	patientGit(t)
	for _, c := range []struct {
		name string
		do   func(t *testing.T, repo string)
	}{
		{"the refs folder is a link", func(t *testing.T, repo string) {
			refs := filepath.Join(repo, ".git", "refs")
			if err := os.Rename(refs, refs+"-real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(refs+"-real", refs); err != nil {
				t.Fatal(err)
			}
		}},
		{"HEAD has no space after the colon", func(t *testing.T, repo string) {
			branch := gitAt(t, repo, "symbolic-ref", "HEAD")
			writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref:"+branch+"\n")
		}},
		{"HEAD has a tab after the colon", func(t *testing.T, repo string) {
			branch := gitAt(t, repo, "symbolic-ref", "HEAD")
			writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref:\t"+branch+"\n")
		}},
		{"HEAD has two spaces after the colon", func(t *testing.T, repo string) {
			branch := gitAt(t, repo, "symbolic-ref", "HEAD")
			writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref:  "+branch+"\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo := gitRepo(t)
			commitIn(t, repo, "a")
			c.do(t, repo)
			want := realHead(t, repo)
			calls := gitCallLog(t)
			event(func() { gitPlaceAt(context.Background(), repo) })
			calls()
			event(func() { gitPlaceAt(context.Background(), repo) })
			if n := len(calls()); n != 0 {
				t.Errorf("the second event asked git %d times for a repository git reads without trouble", n)
			}
			var got string
			event(func() { got = gitHead(repo) })
			if got != want || want == "" {
				t.Errorf("HEAD is %q, git says %q", got, want)
			}
		})
	}
}

// An answer dated in the future is not used (a clock that was set back, or a file put in place from elsewhere), and a `.git` that is a
// link to a folder is left to git (it is not the file or the folder the layouts are derived from).
func TestAKeptAnswerFromTheFutureAndADotGitLinkAreLeftToGit(t *testing.T) {
	t.Run("an answer dated ten minutes ahead", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo := gitRepo(t)
		commitIn(t, repo, "a")
		event(func() { gitPlaceAt(context.Background(), repo) })
		file := mustPlaceFile(t, repo)
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var entry placeEntry // (not a map: the times of the files are larger than a float holds, and the answer would differ for that)
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatal(err)
		}
		entry.At = time.Now().Add(10 * time.Minute).Unix()
		raw, _ = json.Marshal(entry)
		writeFile(t, file, string(raw))
		calls := gitCallLog(t)
		event(func() { gitPlaceAt(context.Background(), repo) })
		if len(calls()) == 0 {
			t.Error("an answer dated in the future was used")
		}
		// control: the same entry dated now is used, so the answer above was left for its date alone
		entry.At = time.Now().Unix()
		raw, _ = json.Marshal(entry)
		writeFile(t, file, string(raw))
		event(func() { gitPlaceAt(context.Background(), repo) })
		if n := len(calls()); n != 0 {
			t.Errorf("control: the entry dated now was not used (%d git calls)", n)
		}
	})
	t.Run(".git is a link to a folder", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo := gitRepo(t)
		commitIn(t, repo, "a")
		real := filepath.Join(repo, ".git")
		moved := filepath.Join(filepath.Dir(repo), "moved.git")
		if err := os.Rename(real, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, real); err != nil {
			t.Fatal(err)
		}
		if l, ok := layoutAt(repo); ok {
			t.Errorf("derived %+v from a .git that is a link to a folder", l)
		}
	})
}

// core.worktree is read from the configuration only in the forms git writes it; any other form is not read here, and the answer is not
// kept (git is asked).
func TestTheWorkTreeTheConfigurationNamesIsReadOnlyInTheFormsGitWritesIt(t *testing.T) {
	submodule := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n\tworktree = ../../../w\n\tignorecase = true\n" +
		"[remote \"origin\"]\n\turl = ../subsrc\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n[submodule \"w\"]\n\tactive = true\n\turl = ../a#b;c\n"
	for _, c := range []struct {
		name, text string
		value      string
		present    bool
		ok         bool
	}{
		{"the configuration of a submodule", submodule, "../../../w", true, true},
		{"no such key and no include", "[core]\n\tbare = false\n", "", false, true},
		{"a key that only holds the word", "[core]\n\tbare = false\n[extensions]\n\tworktreeConfig = true\n", "", false, true},
		{"capital letters", "[Core]\n\tWorkTree = ../x\n", "../x", true, true},
		{"no line end at the end", "[core]\n\tworktree = ../x", "../x", true, true},
		{"spaces around the value", "[core]\n  worktree   =   ../x  \n", "../x", true, true},
		{"the key of a section with a name", "[core \"x\"]\n\tworktree = y\n", "", false, true},
		{"the key of another section", "[other]\n\tworktree = y\n", "", false, true},
		{"a comment line", "# worktree = y\n[core]\n; worktree = z\n\tworktree = ../x\n", "../x", true, true},
		{"a quoted value", "[core]\n\tworktree = \"../x\"\n", "", false, false},
		{"a comment after the value", "[core]\n\tworktree = ../x # here\n", "", false, false},
		{"a backslash in the value", "[core]\n\tworktree = ..\\x\n", "", false, false},
		{"a line that goes on", "[core]\n\tbare = fal\\\nse\n\tworktree = ../x\n", "", false, false},
		{"an include and the key", "[include]\n\tpath = other\n[core]\n\tworktree = ../x\n", "../x", true, true},
		{"a conditional include (the key of its section is not core's)", "[includeIf \"gitdir:/a/\"]\n\tworktree = y\n", "", false, true},
		{"an include with capital letters and no key", "[INCLUDE]\n\tpath = other\n", "", false, true},
		{"a carriage return", "[core]\r\n\tworktree = ../x\r\n", "", false, false},
		{"a carriage return at the end of the value only", "[core]\n\tworktree = ../x\r\n", "", false, false},
		{"two values", "[core]\n\tworktree = ../x\n[core]\n\tworktree = ../y\n", "", false, false},
		{"a key on the line of the section", "[core] worktree = ../x\n", "", false, false},
		{"a section written the old way", "[core.x]\n\tworktree = ../x\n", "", false, false},
		{"the key with no value", "[core]\n\tworktree\n", "", false, false},
		{"an empty value", "[core]\n\tworktree =\n", "", false, false},
		{"a no-break space before the key (git cannot read the file)", "[core]\n\t\u00a0worktree = ../x\n", "", false, false},
		{"a key git does not write", "[core]\n\t1x = y\n\tworktree = ../x\n", "", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			value, present, ok := coreWorktreeOf(c.text)
			if value != c.value || present != c.present || ok != c.ok {
				t.Errorf("read (%q, present %v, ok %v), want (%q, %v, %v)", value, present, ok, c.value, c.present, c.ok)
			}
		})
	}
}

// A repository whose configuration includes another file is kept like any other: git does not read core.worktree in an included file
// when it looks for the working tree (checked here against the git that runs), and one whose configuration names the folder it is in
// (core.worktree = .., as `git init` does for some set-ups) is kept too; one that names another folder is not.
func TestTheWorkTreeTheConfigurationNamesDecidesWhetherTheAnswerIsKept(t *testing.T) {
	asked := func(t *testing.T, setup func(t *testing.T, repo string)) int {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		repo := gitRepo(t)
		commitIn(t, repo, "a")
		setup(t, repo)
		calls := gitCallLog(t)
		event(func() { gitPlaceAt(context.Background(), repo) })
		calls()
		event(func() { gitPlaceAt(context.Background(), repo) })
		return len(calls())
	}
	t.Run("an include that names another folder", func(t *testing.T) {
		var repo string
		n := asked(t, func(t *testing.T, r string) {
			repo = r
			other := filepath.Join(filepath.Dir(r), "other")
			if err := os.MkdirAll(other, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(filepath.Dir(r), "shared.gitconfig"), "[core]\n\tworktree = "+other+"\n")
			gitAt(t, r, "config", "include.path", filepath.Join(filepath.Dir(r), "shared.gitconfig"))
		})
		if got := gitTopOf(t, repo); !sameFolder(got, repo) {
			t.Skipf("this git reads core.worktree in an included file (top %q): the include is left to it", got)
		}
		if n != 0 {
			t.Errorf("the second event asked git %d times: git does not read the included file for the working tree", n)
		}
	})
	t.Run("the working tree is named, and it is here", func(t *testing.T) {
		if n := asked(t, func(t *testing.T, repo string) { gitAt(t, repo, "config", "core.worktree", "..") }); n != 0 {
			t.Errorf("the second event asked git %d times for a configuration that names the folder the repository is in", n)
		}
	})
	t.Run("the working tree is named, and it is elsewhere", func(t *testing.T) {
		if n := asked(t, func(t *testing.T, repo string) {
			other := filepath.Join(filepath.Dir(repo), "other")
			if err := os.MkdirAll(other, 0o755); err != nil {
				t.Fatal(err)
			}
			gitAt(t, repo, "config", "core.worktree", other)
		}); n == 0 {
			t.Error("the answer was kept for a repository whose configuration names another folder as the working tree")
		}
	})
}

// A configuration larger than any a repository has is not read for the folder it names: git is asked.
func TestAConfigurationTooLargeToReadIsNotKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	commitIn(t, repo, "a")
	config := filepath.Join(repo, ".git", "config")
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, config, string(raw)+strings.Repeat("# a comment line that only makes the file large\n", 30000))
	calls := gitCallLog(t)
	event(func() { gitPlaceAt(context.Background(), repo) })
	calls()
	event(func() { gitPlaceAt(context.Background(), repo) })
	if len(calls()) == 0 {
		t.Error("the answer was kept for a configuration that was not read")
	}
}

// git trims a value of the spaces and tabs around it and nothing else: another character that Unicode calls a space belongs to the
// folder name (a link of that name may be pointed elsewhere). The value read here is the one git reads, for each of them.
func TestTheValueOfCoreWorktreeIsTrimmedOfSpacesAndTabsOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, c := range []struct{ name, around string }{
		{"a space", " "}, {"a tab", "\t"}, {"a no-break space", "\u00a0"}, {"a vertical tab", "\v"}, {"a form feed", "\f"},
		{"a next-line character", "\u0085"}, {"a line separator", "\u2028"}, {"an ideographic space", "\u3000"},
	} {
		for _, side := range []string{"before", "after"} {
			t.Run(c.name+" "+side, func(t *testing.T) {
				value := "/a/b"
				text := "[core]\n\tworktree = " + c.around + value + "\n"
				if side == "after" {
					text = "[core]\n\tworktree = " + value + c.around + "\n"
				}
				file := filepath.Join(dir, "config")
				writeFile(t, file, text)
				out, err := exec.Command("git", "config", "--file", file, "--get-all", "core.worktree").Output()
				if err != nil {
					t.Skipf("git does not read this form: %v", err)
				}
				want := strings.TrimSuffix(string(out), "\n")
				got, present, ok := coreWorktreeOf(text)
				if !ok || !present || got != want {
					t.Errorf("read (%q, present %v, ok %v), git reads %q", got, present, ok, want)
				}
			})
		}
	}
}

// The same, end to end: a value that ends in such a character names a folder (here a link) of that name; once the link is pointed
// elsewhere, the kept answer is not used.
func TestAValueEndingInASpaceLikeCharacterNamesThatFolder(t *testing.T) {
	for _, around := range []string{"\u00a0", "\v", "\u0085"} {
		t.Run(fmt.Sprintf("%U", []rune(around)[0]), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			top, other := filepath.Join(base, "w"), filepath.Join(base, "other")
			newRepoAt(t, top)
			if err := os.MkdirAll(other, 0o755); err != nil {
				t.Fatal(err)
			}
			link := top + around
			if err := os.Symlink(top, link); err != nil {
				t.Skipf("no such name here: %v", err)
			}
			config := filepath.Join(top, ".git", "config")
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, config, string(raw)+"[core]\n\tworktree = "+link+"\n")
			if got := gitTopOf(t, top); !sameFolder(got, top) {
				t.Skipf("premise: git says the top folder is %q", got)
			}
			got, ok, asked := keptThenChanged(t, top, func() {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, link); err != nil {
					t.Fatal(err)
				}
			})
			if want := gitTopOf(t, top); asked == 0 && (!ok || !sameFolder(got.top, want)) {
				t.Errorf("the kept top folder %q (%v) was used with no git call, git says %q", got.top, ok, want)
			}
		})
	}
}

// The configuration of one worktree (config.worktree, read by git where extensions.worktreeConfig is on) can name the working tree
// too, and its relative value is read from the git folder of that worktree.
func TestTheConfigurationOfOneWorktreeIsReadForTheFolderItNames(t *testing.T) {
	setup := func(t *testing.T, linked bool) (top, link, a, b, gitDir string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		base, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		a, b, link = filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "link")
		top = filepath.Join(a, "w")
		if linked {
			main := filepath.Join(base, "main")
			newRepoAt(t, main)
			commitIn(t, main, "m")
			if err := os.MkdirAll(a, 0o755); err != nil {
				t.Fatal(err)
			}
			gitAt(t, main, "worktree", "add", "-q", top, "-b", "lw")
			gitAt(t, main, "config", "extensions.worktreeConfig", "true")
		} else {
			newRepoAt(t, top)
			commitIn(t, top, "p")
			gitAt(t, top, "config", "extensions.worktreeConfig", "true")
		}
		if err := os.MkdirAll(filepath.Join(b, "w"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(a, link); err != nil {
			t.Fatal(err)
		}
		gitDir, _ = gitDirsOf(t, top)
		return top, link, a, b, gitDir
	}
	for _, linked := range []bool{false, true} {
		name := map[bool]string{false: "a plain repository", true: "a linked worktree"}[linked]
		t.Run(name+" naming itself through a link", func(t *testing.T) {
			top, link, _, b, _ := setup(t, linked)
			gitAt(t, top, "config", "--worktree", "core.worktree", filepath.Join(link, "w"))
			if got := gitTopOf(t, top); !sameFolder(got, top) {
				t.Fatalf("premise: git says the top folder is %q", got)
			}
			got, ok, asked := keptThenChanged(t, top, func() {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(b, link); err != nil {
					t.Fatal(err)
				}
			})
			if want := gitTopOf(t, top); asked == 0 && (!ok || !sameFolder(got.top, want)) {
				t.Errorf("the kept top folder %q (%v) was used with no git call, git says %q", got.top, ok, want)
			}
		})
	}
	t.Run("a linked worktree naming itself with a path from its git folder", func(t *testing.T) {
		top, _, _, _, gitDir := setup(t, true)
		rel, err := filepath.Rel(gitDir, top)
		if err != nil {
			t.Fatal(err)
		}
		gitAt(t, top, "config", "--worktree", "core.worktree", rel)
		if got := gitTopOf(t, top); !sameFolder(got, top) {
			t.Fatalf("premise: git says the top folder is %q", got)
		}
		calls := gitCallLog(t)
		event(func() { gitPlaceAt(context.Background(), top) })
		calls()
		event(func() { gitPlaceAt(context.Background(), top) })
		if n := len(calls()); n != 0 {
			t.Errorf("the second event asked git %d times for a value that names the worktree from its git folder", n)
		}
	})
}
