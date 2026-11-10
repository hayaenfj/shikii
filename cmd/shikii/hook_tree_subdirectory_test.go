package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// H5: the working tree snapshot writes new blobs into a temporary object store and reads the repo's own objects as alternates.
// From a subdirectory of the repo (the agent ran cd, or the session started there) the alternates path must still be the
// repo's objects folder; a path that does not exist makes git write every file of the repo into the temporary store on
// every hook, which is what pushes large repos past the 3 second limit (then nothing is compared and no guard runs).
func TestTreeSnapshotFromASubdirectoryUsesTheReposObjects(t *testing.T) {
	repo := gitRepo(t)
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"README.md", "pkg/inner/a.go"} {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(f)), []byte("content of "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCommitAll(t, repo, "init")

	// a git in front of the real one that writes down the alternates path each call was given, then runs the real git
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	bin, seen := t.TempDir(), filepath.Join(t.TempDir(), "alternates.log")
	script := "#!/bin/sh\nif [ -n \"$GIT_ALTERNATE_OBJECT_DIRECTORIES\" ]; then printf '%s\\n' \"$GIT_ALTERNATE_OBJECT_DIRECTORIES\" >> '" + seen + "'; fi\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, cwd := range []string{repo, sub} {
		if err := os.WriteFile(seen, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, listed, _, _ := treeStateWithScope(cwd); !listed {
			t.Fatalf("premise: the snapshot from %s is taken", cwd)
		}
		lines := strings.Fields(string(mustRead(t, seen)))
		if len(lines) == 0 {
			t.Fatalf("premise: the snapshot from %s passes an alternates path to git", cwd)
		}
		for _, dir := range lines {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Fatalf("from %s the alternates path must be the repo's objects folder, but %s does not exist", cwd, dir)
			}
		}
	}
}
