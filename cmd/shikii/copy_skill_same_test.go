package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	shikii "github.com/hayaenfj/shikii"
)

// Tests for copySkill leaving an identical skill alone. Deleting it first leaves a window with no SKILL.md for an agent that starts
// in the middle of `shikii init`, so an identical skill is not rewritten; anything else (extra, missing, changed, link, wrong
// permissions) is rewritten as before.

// skillTree lists what is at dir without following links: relative path -> kind, permissions and (for a file) its bytes.
func skillTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		desc := info.Mode().String()
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			desc += " " + string(b)
		}
		out[rel] = desc
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// freshSkill is what copySkill writes into a folder that has no skill yet (with this process's umask).
func freshSkill(t *testing.T) map[string]string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "fresh", "shikii")
	if err := copySkill(d); err != nil {
		t.Fatal(err)
	}
	return skillTree(t, d)
}

// needFreshModes sets the umask to 022 for the test (and puts the old one back), so copySkill makes the modes it compares with.
// Under another umask an identical skill is always rewritten (what copySkill always did); TestCopySkillUnderAnotherUmaskRewrites
// checks that side. cmd/shikii's tests do not run in parallel, so the process-wide umask is the test's own while it runs.
func needFreshModes(t *testing.T) {
	t.Helper()
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
}

// Under a umask that takes away more than 022, the modes copySkill makes are not the ones it compares with, so an identical skill
// is rewritten every time (never left alone in doubt) and still ends up the same as a fresh copy.
func TestCopySkillUnderAnotherUmaskRewrites(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("under umask 077 the skill was left alone, but its modes are not the ones copySkill compares with")
	}
}

func requireSameTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	for rel, w := range want {
		g, ok := got[rel]
		switch {
		case !ok:
			t.Errorf("%s is missing after copySkill", rel)
		case g != w:
			t.Errorf("%s differs from a fresh copy after copySkill", rel)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Errorf("%s is left over after copySkill (not in the embedded skill)", rel)
		}
	}
}

func sameFileAt(t *testing.T, a, b os.FileInfo, what string) {
	t.Helper()
	if !os.SameFile(a, b) {
		t.Errorf("%s was deleted and written again although the skill was identical", what)
	}
}

// The same content is not deleted: the folders and files keep their inodes, and the lock is still held while comparing.
func TestCopySkillLeavesIdenticalSkillInPlace(t *testing.T) {
	needFreshModes(t)
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	paths := []string{".", "SKILL.md", "references", filepath.Join("references", "wiki.md")}
	before := map[string]os.FileInfo{}
	for _, p := range paths {
		fi, err := os.Lstat(filepath.Join(dst, p))
		if err != nil {
			t.Fatal(err)
		}
		before[p] = fi
	}
	ran := false
	old := whileCopyingSkill
	t.Cleanup(func() { whileCopyingSkill = old })
	whileCopyingSkill = func() { ran = true }
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("whileCopyingSkill did not run: the comparison must happen under the lock")
	}
	for _, p := range paths {
		fi, err := os.Lstat(filepath.Join(dst, p))
		if err != nil {
			t.Fatal(err)
		}
		sameFileAt(t, before[p], fi, p)
	}
	requireSameTree(t, skillTree(t, dst), freshSkill(t))
}

// Anything that is not exactly the embedded skill is put back as before: nothing extra stays, nothing is missing, bytes are the embedded ones.
func TestCopySkillRewritesASkillThatDiffers(t *testing.T) {
	flipLastByte := func(t *testing.T, p string) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)-1] ^= 1 // same length, one byte different
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name   string
		damage func(t *testing.T, dst string)
	}{
		{"an extra file next to SKILL.md", func(t *testing.T, dst string) {
			os.WriteFile(filepath.Join(dst, "old-reference.md"), []byte("stale"), 0o644)
		}},
		{"an extra file in references", func(t *testing.T, dst string) {
			os.WriteFile(filepath.Join(dst, "references", "old.md"), []byte("stale"), 0o644)
		}},
		{"an extra empty folder", func(t *testing.T, dst string) {
			os.Mkdir(filepath.Join(dst, "old-folder"), 0o755)
		}},
		{"an extra empty folder in references", func(t *testing.T, dst string) {
			os.Mkdir(filepath.Join(dst, "references", "old-folder"), 0o755)
		}},
		{"one byte different in SKILL.md", func(t *testing.T, dst string) {
			flipLastByte(t, filepath.Join(dst, "SKILL.md"))
		}},
		{"one byte different in a reference", func(t *testing.T, dst string) {
			flipLastByte(t, filepath.Join(dst, "references", "wiki.md"))
		}},
		{"one byte added to a reference", func(t *testing.T, dst string) {
			f, err := os.OpenFile(filepath.Join(dst, "references", "wiki.md"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.WriteString("x")
			f.Close()
		}},
		{"a reference is missing", func(t *testing.T, dst string) {
			if err := os.Remove(filepath.Join(dst, "references", "tidy.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"SKILL.md is missing", func(t *testing.T, dst string) {
			if err := os.Remove(filepath.Join(dst, "SKILL.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"the references folder is missing", func(t *testing.T, dst string) {
			if err := os.RemoveAll(filepath.Join(dst, "references")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a file has other permissions", func(t *testing.T, dst string) {
			if err := os.Chmod(filepath.Join(dst, "SKILL.md"), 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{"a folder has other permissions", func(t *testing.T, dst string) {
			if err := os.Chmod(filepath.Join(dst, "references"), 0o777); err != nil {
				t.Fatal(err)
			}
		}},
		{"the top folder has other permissions", func(t *testing.T, dst string) {
			if err := os.Chmod(dst, 0o777); err != nil {
				t.Fatal(err)
			}
		}},
	}
	want := freshSkill(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "skills", "shikii")
			if err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			c.damage(t, dst)
			if err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			requireSameTree(t, skillTree(t, dst), want)
		})
	}
}

// A link inside the skill (a file or a folder) is not the real thing even when its target has the right content: it is replaced by a
// real file or folder, and what it pointed at is not touched.
func TestCopySkillReplacesLinksInsideTheSkill(t *testing.T) {
	want := freshSkill(t)
	t.Run("a file is a link to a file with the same content", func(t *testing.T) {
		base := t.TempDir()
		dst := filepath.Join(base, "skills", "shikii")
		if err := copySkill(dst); err != nil {
			t.Fatal(err)
		}
		real := filepath.Join(base, "outside.md")
		b, _ := os.ReadFile(filepath.Join(dst, "SKILL.md"))
		os.WriteFile(real, b, 0o644)
		os.Remove(filepath.Join(dst, "SKILL.md"))
		if err := os.Symlink(real, filepath.Join(dst, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		if err := copySkill(dst); err != nil {
			t.Fatal(err)
		}
		requireSameTree(t, skillTree(t, dst), want)
		if got, _ := os.ReadFile(real); !bytes.Equal(got, b) {
			t.Error("the file the link pointed at was changed")
		}
	})
	t.Run("the references folder is a link to a folder with the same content", func(t *testing.T) {
		base := t.TempDir()
		dst := filepath.Join(base, "skills", "shikii")
		if err := copySkill(dst); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(base, "outside-references")
		if err := os.Rename(filepath.Join(dst, "references"), outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dst, "references")); err != nil {
			t.Fatal(err)
		}
		beforeOutside := skillTree(t, outside)
		if err := copySkill(dst); err != nil {
			t.Fatal(err)
		}
		requireSameTree(t, skillTree(t, dst), want)
		requireSameTree(t, skillTree(t, outside), beforeOutside)
	})
}

// dst itself may be a link (to a folder holding the same skill, or another one): the link is replaced by a real folder and the folder it
// pointed at is not changed, as before.
func TestCopySkillReplacesALinkAtDst(t *testing.T) {
	want := freshSkill(t)
	for _, name := range []string{"same content", "other content"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			target := filepath.Join(base, "elsewhere")
			if err := copySkill(target); err != nil {
				t.Fatal(err)
			}
			if name == "other content" {
				os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("not shikii's"), 0o644)
			}
			beforeTarget := skillTree(t, target)
			dst := filepath.Join(base, "skills", "shikii")
			os.MkdirAll(filepath.Dir(dst), 0o755)
			if err := os.Symlink(target, dst); err != nil {
				t.Fatal(err)
			}
			if err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			if fi, err := os.Lstat(dst); err != nil || !fi.IsDir() {
				t.Fatalf("dst must be a real folder after copySkill: %v %v", fi, err)
			}
			requireSameTree(t, skillTree(t, dst), want)
			requireSameTree(t, skillTree(t, target), beforeTarget)
		})
	}
}

// dst may be a plain file: it is replaced by the skill folder.
func TestCopySkillReplacesAFileAtDst(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst, []byte("a file where the folder goes"), 0o644)
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	requireSameTree(t, skillTree(t, dst), freshSkill(t))
}

// The embedded skill really has a folder inside (the tests above rely on references/ being there).
func TestCopySkillTestsRelyOnTheEmbeddedLayout(t *testing.T) {
	for _, p := range []string{"skills/shikii/SKILL.md", "skills/shikii/references/wiki.md", "skills/shikii/references/tidy.md"} {
		if _, err := fs.Stat(shikii.SkillFS, p); err != nil {
			t.Fatalf("%s is not embedded: the copy_skill tests need updating: %v", p, err)
		}
	}
}
