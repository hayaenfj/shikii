package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	if _, err := copySkill(d); err != nil {
		t.Fatal(err)
	}
	return skillTree(t, d)
}

// underUmask sets the process's umask for the test and puts the old one back. cmd/shikii's tests do not run in parallel, so the
// process-wide umask is the test's own while it runs. (The product never calls syscall.Umask: it reads the umask a skill was written under
// off the skill itself.)
func underUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

// The skill that is already exactly the embedded one is left alone, whatever umask wrote it and whatever umask is set now.
// skillMatchesEmbedded makes nothing to learn the umask: it reads the umask off the skill's own top folder (what 0755 lost) and checks
// that every file and folder in it fits that one umask (files 0644 less it, folders 0755 less it). So a skill written under umask 077
// is kept by a later init under umask 022, and the reverse. 044 is in the list because it takes the read bit but not the execute bit
// from a folder (0711): the execute bits cannot be guessed from the files. Each call after the first says it left the skill (rewrote is
// false), and every folder and file keeps its inode.
func TestCopySkillLeavesTheSameSkillUnderAnyUmask(t *testing.T) {
	masks := []int{0o000, 0o002, 0o022, 0o027, 0o044, 0o077}
	for _, wrote := range masks {
		for _, now := range masks {
			t.Run(fmt.Sprintf("written under %03o, init under %03o", wrote, now), func(t *testing.T) {
				dst := filepath.Join(t.TempDir(), "skills", "shikii")
				underUmask(t, wrote)
				if rewrote, err := copySkill(dst); err != nil || !rewrote {
					t.Fatalf("writing the skill where there is none: rewrote %v, err %v, want true and nil", rewrote, err)
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
				written := skillTree(t, dst)
				underUmask(t, now)
				rewrote, err := copySkill(dst)
				if err != nil {
					t.Fatal(err)
				}
				if rewrote {
					t.Errorf("the skill written under umask %03o was written again under umask %03o although nothing in it changed", wrote, now)
				}
				for _, p := range paths {
					fi, err := os.Lstat(filepath.Join(dst, p))
					if err != nil {
						t.Fatal(err)
					}
					sameFileAt(t, before[p], fi, p)
				}
				requireSameTree(t, skillTree(t, dst), written)
			})
		}
	}
}

// Under umask 077 a new write gives 0600 and 0700, so a file or folder with the modes 0644 and 0755 is not what copySkill would write:
// it is put back as a new write gets it (the files and folders no longer fit the one umask the top folder shows).
func TestCopySkillRewritesWiderPermissionsUnderUmask077(t *testing.T) {
	underUmask(t, 0o077)
	for _, tc := range []struct {
		name   string
		widen  func(t *testing.T, dst string)
		wantAt string
	}{
		{"a file of 0644", func(t *testing.T, dst string) {
			if err := os.Chmod(filepath.Join(dst, "SKILL.md"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "SKILL.md"},
		{"a folder of 0755", func(t *testing.T, dst string) {
			if err := os.Chmod(filepath.Join(dst, "references"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "references"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "skills", "shikii")
			if _, err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			tc.widen(t, dst)
			rewrote, err := copySkill(dst)
			if err != nil {
				t.Fatal(err)
			}
			if !rewrote {
				t.Fatalf("%s was left alone, but under umask 077 a new write does not give it these modes", tc.name)
			}
			requireSameTree(t, skillTree(t, dst), freshSkill(t))
			if fi, err := os.Lstat(filepath.Join(dst, tc.wantAt)); err != nil || fi.Mode().Perm()&0o077 != 0 {
				t.Fatalf("%s was written again with %v, want nothing for group and others", tc.wantAt, fi.Mode())
			}
		})
	}
}

// Modes that no one umask gives to the whole skill are written again: one file of 0600 among files of 0644 (the two cannot come from
// one umask), a file of 0664 (a umask only takes bits away from 0644, so no umask leaves the group write bit), a folder of 0775 (the
// same for 0755), and one folder or the top folder that does not fit the umask the rest fit. Each is tried on SKILL.md (the file
// listed first), on a file deeper in, and on folders, because a check that looks at one entry only passes some of them.
func TestCopySkillRewritesModesThatNoSingleUmaskGives(t *testing.T) {
	wiki := filepath.Join("references", "wiki.md")
	for _, tc := range []struct {
		name  string
		chmod func(dst string) error
	}{
		{"SKILL.md of 0600 among files of 0644", func(dst string) error { return os.Chmod(filepath.Join(dst, "SKILL.md"), 0o600) }},
		{"a reference of 0600 among files of 0644", func(dst string) error { return os.Chmod(filepath.Join(dst, wiki), 0o600) }},
		{"SKILL.md of 0664", func(dst string) error { return os.Chmod(filepath.Join(dst, "SKILL.md"), 0o664) }},
		{"a reference of 0664", func(dst string) error { return os.Chmod(filepath.Join(dst, wiki), 0o664) }},
		{"the references folder of 0775", func(dst string) error { return os.Chmod(filepath.Join(dst, "references"), 0o775) }},
		{"the references folder of 0700 among folders of 0755", func(dst string) error { return os.Chmod(filepath.Join(dst, "references"), 0o700) }},
		{"the top folder of 0700 among folders of 0755", func(dst string) error { return os.Chmod(dst, 0o700) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underUmask(t, 0o022)
			dst := filepath.Join(t.TempDir(), "skills", "shikii")
			if _, err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			if err := tc.chmod(dst); err != nil {
				t.Fatal(err)
			}
			rewrote, err := copySkill(dst)
			if err != nil {
				t.Fatal(err)
			}
			if !rewrote {
				t.Fatalf("a skill with %s was left alone", tc.name)
			}
			requireSameTree(t, skillTree(t, dst), freshSkill(t))
		})
	}
}

// A skill under umask 077 with one file of 0644 (a file widened by hand) is not the one umask either: the top folder says 077, and that
// file does not fit it.
func TestCopySkillRewritesOneWideFileInAnOtherwiseNarrowSkill(t *testing.T) {
	underUmask(t, 0o077)
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if _, err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dst, "references", "wiki.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rewrote, err := copySkill(dst); err != nil || !rewrote {
		t.Fatalf("one file of 0644 in a skill of 0600 and 0700: rewrote %v, err %v, want true and nil", rewrote, err)
	}
	requireSameTree(t, skillTree(t, dst), freshSkill(t))
}

// copySkill makes nothing next to the skill to find out how it should look: before and after, the folder the skill is in holds the
// same names, apart from the lock (which is made the first time and stays). A name the person has there, a folder or a hidden file,
// is not touched.
func TestCopySkillMakesNothingNextToTheSkill(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "skills")
	dst := filepath.Join(parent, "shikii")
	names := func() string {
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			if e.Name() != ".shikii-skill.lock" {
				out = append(out, e.Name())
			}
		}
		return strings.Join(out, " ")
	}
	if err := os.MkdirAll(filepath.Join(parent, "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := names()
	for i, step := range []string{"writes the skill where there is none", "leaves the same skill", "writes a skill that differs"} {
		if i == 2 {
			if err := os.WriteFile(filepath.Join(dst, "old.md"), []byte("stale"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := copySkill(dst); err != nil {
			t.Fatalf("copySkill %s: %v", step, err)
		}
		want := before + " shikii"
		if got := names(); got != want {
			t.Errorf("after copySkill %s the folder holds %q, want %q (nothing made next to the skill)", step, got, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(parent, ".shikii-skill.lock")); err != nil {
		t.Errorf("the lock is not there: %v", err)
	}
}

// copySkill says which it did: true when it wrote the skill (none there, or a different one), false when it left it. The folder the
// skill is in holds nothing but the skill and the lock afterwards.
func TestCopySkillSaysWhetherItWroteTheSkill(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "skills")
	dst := filepath.Join(parent, "shikii")
	for i, step := range []struct {
		name   string
		damage func()
		want   bool
	}{
		{"no skill there", func() {}, true},
		{"the same skill", func() {}, false},
		{"an extra file", func() {
			if err := os.WriteFile(filepath.Join(dst, "old.md"), []byte("stale"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"the same again", func() {}, false},
	} {
		step.damage()
		rewrote, err := copySkill(dst)
		if err != nil {
			t.Fatalf("step %d (%s): %v", i, step.name, err)
		}
		if rewrote != step.want {
			t.Errorf("step %d (%s): rewrote %v, want %v", i, step.name, rewrote, step.want)
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if strings.Join(names, " ") != ".shikii-skill.lock shikii" {
			t.Errorf("step %d (%s): the folder holds %q, want only the lock and the skill", i, step.name, names)
		}
	}
}

// A skills folder that takes no new file (read-only, as a person who keeps their home folder in a read-only store has it) does not stop
// init when the skill in it is already the embedded one: nothing is made to compare, so the skill is left and the answer is "no
// error, not written". A skill that differs there still cannot be put back, and the error of the write is said (as copySkill always
// did for such a folder). The lock is made once while the folder is writable; it is opened again, not created, afterwards.
func TestCopySkillInAReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a folder that takes no file")
	}
	for _, tc := range []struct {
		name    string
		damage  func(t *testing.T, dst string) // while the folder can still be written
		wantErr bool
	}{
		{"the same skill", func(t *testing.T, dst string) {}, false},
		{"the same skill with no lock file yet", func(t *testing.T, dst string) {
			if err := os.Remove(filepath.Join(filepath.Dir(dst), ".shikii-skill.lock")); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a skill with an extra file and no lock file yet", func(t *testing.T, dst string) {
			if err := os.Remove(filepath.Join(filepath.Dir(dst), ".shikii-skill.lock")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, "old.md"), []byte("stale"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"a skill with an extra file", func(t *testing.T, dst string) {
			if err := os.WriteFile(filepath.Join(dst, "old.md"), []byte("stale"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "skills")
			dst := filepath.Join(parent, "shikii")
			if _, err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			tc.damage(t, dst)
			before := skillTree(t, dst)
			if err := os.Chmod(parent, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(parent, 0o755) })
			rewrote, err := copySkill(dst)
			if tc.wantErr {
				if err == nil {
					t.Fatal("a skill that differs, in a folder that takes no new file, was said to be put back")
				}
				return
			}
			if err != nil || rewrote {
				t.Fatalf("the same skill in a read-only folder: rewrote %v, err %v, want false and nil", rewrote, err)
			}
			requireSameTree(t, skillTree(t, dst), before)
		})
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
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if _, err := copySkill(dst); err != nil {
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
	if _, err := copySkill(dst); err != nil {
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
			if _, err := copySkill(dst); err != nil {
				t.Fatal(err)
			}
			c.damage(t, dst)
			if _, err := copySkill(dst); err != nil {
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
		if _, err := copySkill(dst); err != nil {
			t.Fatal(err)
		}
		real := filepath.Join(base, "outside.md")
		b, _ := os.ReadFile(filepath.Join(dst, "SKILL.md"))
		os.WriteFile(real, b, 0o644)
		os.Remove(filepath.Join(dst, "SKILL.md"))
		if err := os.Symlink(real, filepath.Join(dst, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := copySkill(dst); err != nil {
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
		if _, err := copySkill(dst); err != nil {
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
		if _, err := copySkill(dst); err != nil {
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
			if _, err := copySkill(target); err != nil {
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
			if _, err := copySkill(dst); err != nil {
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
	if _, err := copySkill(dst); err != nil {
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

// A skill under a setgid folder (on Linux its folders are setgid too when copySkill makes them) is the same skill: the bit says which
// group new files get. macOS does not let a test set the bit on a folder, so the comparison is checked on modes.
func TestSkillModeIgnoresTheSetgidBitOfAFolderOnly(t *testing.T) {
	dir, file := fs.ModeDir|0o755, fs.FileMode(0o644)
	for _, c := range []struct {
		got, want fs.FileMode
		same      bool
	}{
		{dir, dir, true},
		{dir | fs.ModeSetgid, dir, true},
		{dir | fs.ModeSetuid, dir, false},
		{dir | fs.ModeSticky, dir, false},
		{fs.ModeDir | 0o775 | fs.ModeSetgid, dir, false},
		{file, file, true},
		{file | fs.ModeSetgid, file, false},
		{file, dir, false},
	} {
		if got := sameSkillMode(c.got, c.want); got != c.same {
			t.Errorf("sameSkillMode(%v, %v) = %v, want %v", c.got, c.want, got, c.same)
		}
	}
}
