package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The edges of the answer about the repository that the first audit of 0.37.3 found: where a relative path git gives is joined, what the
// environment does to the listing, when a kept answer is no longer trusted, and the shapes of HEAD only git can read.

// Git answers `--git-path objects` relative to the folder it runs in, and a folder reached through a link is that folder's real path
// to git: joined to the link's own path it pointed above the link, to a `.git` of another folder (0.37.3 audit, finding 1).
func TestAPlaceAskedFromALinkToASubfolderIsTheRepositorys(t *testing.T) {
	for _, committed := range []bool{false, true} { // the answer has three lines before the first commit and four after it
		name := "before the first commit"
		if committed {
			name = "with a commit"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo := gitRepo(t)
			proj := filepath.Join(repo, "services", "proj")
			if err := os.MkdirAll(proj, 0o755); err != nil {
				t.Fatal(err)
			}
			if committed {
				if err := os.WriteFile(filepath.Join(proj, "a.txt"), []byte("a\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				gitCommitAll(t, repo, "one")
			}
			link := filepath.Join(t.TempDir(), "proj")
			if err := os.Symlink(proj, link); err != nil {
				t.Fatal(err)
			}
			real, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			var got gitPlace
			var ok bool
			event(func() { got, ok = gitPlaceAt(context.Background(), link) })
			if !ok || got.objects != filepath.Join(real, ".git", "objects") || got.exclude != filepath.Join(real, ".git", "info", "exclude") {
				t.Errorf("asked from a link to %s: objects %q, exclude %q (ok %v), want the ones under %s/.git", proj, got.objects, got.exclude, ok, real)
			}
		})
	}
}

// A relative folder is the folder it names from here: it was said to be outside git, because the walk stopped at its first step.
func TestARelativeFolderIsFoundLikeTheAbsoluteOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	sub := filepath.Join(repo, "x", "y")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if outsideGit(".") {
		t.Error("outsideGit(.) inside a repository")
	}
	var got gitPlace
	var ok bool
	event(func() { got, ok = gitPlaceAt(context.Background(), ".") })
	real, _ := filepath.EvalSymlinks(repo)
	if !ok || got.objects != filepath.Join(real, ".git", "objects") {
		t.Errorf("gitPlaceAt(.): objects %q (ok %v), want %s", got.objects, ok, filepath.Join(real, ".git", "objects"))
	}
}

// A folder whose name has a line end in it: git's four lines are no longer four, and the folder was said to be outside git. The
// three questions asked one by one, as before the answers were put together, still tell.
func TestAFolderNamedWithALineEndIsStillInItsRepository(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := filepath.Join(t.TempDir(), "a\nb")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	real, _ := filepath.EvalSymlinks(repo)
	var got gitPlace
	var ok bool
	event(func() { got, ok = gitPlaceAt(context.Background(), repo) })
	if !ok || got.top != real || got.objects != filepath.Join(real, ".git", "objects") {
		t.Errorf("top %q, objects %q (ok %v), want %q", got.top, got.objects, ok, real)
	}
}

// `GIT_LITERAL_PATHSPECS=1` (some tools export it) reads `:(exclude).shikii` as a file name: the records came into the listing and the
// hash, and the working tree seemed to change at every record the hook itself wrote.
func TestTheRecordsStayOutOfTheListingWhenGitReadsPathspecsLiterally(t *testing.T) {
	t.Setenv("GIT_LITERAL_PATHSPECS", "1")
	repo := gitRepo(t)
	for name, body := range map[string]string{"a.txt": "a\n", ".shikii/wiki/page.md": "one\n"} {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed || !strings.Contains(listing, "a.txt") || strings.Contains(listing, ".shikii") {
		t.Errorf("listed %v: %q", listed, listing)
	}
}

// keepAndChange keeps the place of a new repository (one event asks git), then makes `change` and asks again: how many git calls the
// second ask made.
func keepAndChange(t *testing.T, change func(repo string)) int {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "one")
	event(func() { gitPlaceAt(context.Background(), repo) })
	calls := gitCallLog(t)
	change(repo)
	calls() // what the change itself ran is not counted
	event(func() { gitPlaceAt(context.Background(), repo) })
	return len(calls())
}

// What moves the repository in the environment is left to git, also when it is set to nothing (git reads an empty GIT_DIR as an error),
// and so is a `.git` folder that says it is shared with another (commondir): the kept answer is for the plain case only.
func TestAKeptPlaceIsLeftToGitWhileTheEnvironmentOrACommondirMovesTheRepository(t *testing.T) {
	for _, c := range []struct {
		name  string
		value func(repo string) string
	}{
		{"GIT_WORK_TREE", func(repo string) string { return repo }},
		{"GIT_COMMON_DIR", func(repo string) string { return filepath.Join(repo, ".git") }},
		{"GIT_OBJECT_DIRECTORY", func(repo string) string { return filepath.Join(repo, ".git", "objects") }},
		{"GIT_DISCOVERY_ACROSS_FILESYSTEM", func(string) string { return "1" }},
		{"GIT_DIR", func(string) string { return "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			if n := keepAndChange(t, func(repo string) { t.Setenv(c.name, c.value(repo)) }); n == 0 {
				t.Errorf("%s set: the kept answer was used, git was not asked", c.name)
			}
		})
	}
	t.Run("commondir", func(t *testing.T) {
		n := keepAndChange(t, func(repo string) {
			if err := os.WriteFile(filepath.Join(repo, ".git", "commondir"), []byte("../elsewhere\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		})
		if n == 0 {
			t.Error("a .git folder with a commondir: the kept answer was used, git was not asked")
		}
	})
}

// A kept answer is trusted while the `.git` folder is as it was: a repository whose HEAD is gone, or whose configuration was written
// after the answer (core.bare, core.worktree), is asked about again.
func TestAKeptPlaceIsLeftOnceHeadIsGoneOrTheConfigurationChanged(t *testing.T) {
	t.Run("HEAD removed", func(t *testing.T) {
		if n := keepAndChange(t, func(repo string) {
			if err := os.Remove(filepath.Join(repo, ".git", "HEAD")); err != nil {
				t.Fatal(err)
			}
		}); n == 0 {
			t.Error("the kept answer was used with no HEAD in the repository")
		}
	})
	t.Run("configuration written later", func(t *testing.T) {
		if n := keepAndChange(t, func(repo string) {
			config := filepath.Join(repo, ".git", "config")
			later := time.Now().Add(10 * time.Second)
			if err := os.Chtimes(config, later, later); err != nil {
				t.Fatal(err)
			}
		}); n == 0 {
			t.Error("the kept answer was used after the configuration of the repository was written")
		}
	})
	t.Run("configuration as it was", func(t *testing.T) {
		if n := keepAndChange(t, func(string) {}); n != 0 {
			t.Errorf("nothing changed, yet git was asked %d times", n)
		}
	})
}

// The files of a bare repository count when HEAD or objects is a link (git follows them): the folder was said to be outside git.
func TestABareRepositoryWithLinksIsStillARepository(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bare := filepath.Join(t.TempDir(), "b.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	moved := filepath.Join(t.TempDir(), "objs")
	if err := os.Rename(filepath.Join(bare, "objects"), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, filepath.Join(bare, "objects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bare, "HEAD")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("refs/heads/master", filepath.Join(bare, "HEAD")); err != nil {
		t.Fatal(err)
	}
	if outsideGit(bare) {
		t.Error("a bare repository whose HEAD and objects are links was said to be outside git")
	}
}

// HEAD files that git itself never writes (a hand-made name, a packed-refs from another system) are left to git: it may read them
// another way, so the answer from the files is only given for what git writes.
func TestHeadFilesGitNeverWritesAreLeftToGit(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		name         string
		head, packed string
	}{
		{"a doubled slash", "ref: refs/heads//x\n", sha + " refs/heads/x\n"},
		{"a trailing slash", "ref: refs/heads/x/\n", sha + " refs/heads/x\n"},
		{"a dot step", "ref: refs/./heads/x\n", sha + " refs/heads/x\n"},
		{"line ends of another system", "ref: refs/heads/x\n", sha + " refs/heads/x\r\n"},
		{"capital letters in the id", "ref: refs/heads/x\n", strings.ToUpper(sha) + " refs/heads/x\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			gitDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(c.head), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(c.packed), 0o644); err != nil {
				t.Fatal(err)
			}
			if head, known := headFromFiles(gitDir); known {
				t.Errorf("answered %q from files git may read differently", head)
			}
		})
	}
}

// `..` after a link names the folder above what the link points to, not the folder above the link: a path made absolute by folding the
// `..` away (filepath.Abs) looked in the wrong place and said a real repository was outside git (found by the confirmation audit).
func TestDotDotAfterALinkNamesTheFolderAboveWhatTheLinkPointsTo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	proj := filepath.Join(repo, "services", "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "proj") // the folder above the link is outside the repository; the one above proj is inside it
	if err := os.Symlink(proj, link); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(repo)
	check := func(label, dir string) {
		t.Helper()
		var got gitPlace
		var ok bool
		event(func() { got, ok = gitPlaceAt(context.Background(), dir) })
		if !ok || got.top != real || outsideGit(dir) {
			t.Errorf("%s: top %q (ok %v), outsideGit %v, want %s", label, got.top, ok, outsideGit(dir), real)
		}
	}
	check("an absolute path ending in the link and ..", link+string(filepath.Separator)+"..")
	t.Chdir(link)
	check("a relative ..", "..")
}

// An empty GIT_NAMESPACE or GIT_DISCOVERY_ACROSS_FILESYSTEM is no setting to git (an empty GIT_DIR is an error: the test above), and some
// tools export them empty: the kept answer stays in use then.
func TestAnEmptyNamespaceOrDiscoveryVariableDoesNotTurnTheKeptPlaceOff(t *testing.T) {
	for _, name := range []string{"GIT_NAMESPACE", "GIT_DISCOVERY_ACROSS_FILESYSTEM"} {
		t.Run(name, func(t *testing.T) {
			if n := keepAndChange(t, func(string) { t.Setenv(name, "") }); n != 0 {
				t.Errorf("%s set to nothing: git was asked %d times", name, n)
			}
		})
	}
}

// A `.git/config` that is a link: it is the file it points to that is written (git writes through it), so its time is what counts.
func TestAKeptPlaceIsLeftWhenTheFileAConfigLinkPointsToIsWritten(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "one")
	config := filepath.Join(repo, ".git", "config")
	moved := filepath.Join(t.TempDir(), "config")
	if err := os.Rename(config, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, config); err != nil {
		t.Fatal(err)
	}
	event(func() { gitPlaceAt(context.Background(), repo) })
	calls := gitCallLog(t)
	later := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(moved, later, later); err != nil {
		t.Fatal(err)
	}
	event(func() { gitPlaceAt(context.Background(), repo) })
	if n := len(calls()); n == 0 {
		t.Error("the file the configuration link points to was written, yet the kept answer was used")
	}
}

// With GIT_ICASE_PATHSPECS=1 the exclude pathspec for the records would also leave out a folder that differs only in case. A file
// system that does not tell the two names apart (the default on macOS) cannot hold both: the test is for the ones that can.
func TestTheListingKeepsAFolderThatDiffersOnlyInCaseWhenGitMatchesPathspecsWithoutCase(t *testing.T) {
	t.Setenv("GIT_ICASE_PATHSPECS", "1")
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".shikii"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".SHIKII"), 0o755); err != nil {
		t.Skip("this file system does not tell .shikii and .SHIKII apart")
	}
	for _, p := range []string{".shikii/record.md", ".SHIKII/notes.md"} {
		if err := os.WriteFile(filepath.Join(repo, p), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed || strings.Contains(listing, ".shikii/record.md") || !strings.Contains(listing, ".SHIKII/notes.md") {
		t.Errorf("listed %v: %q", listed, listing)
	}
}
