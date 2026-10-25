package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	shikii "github.com/hayaenfj/shikii"
)

type updateFixture struct {
	opts                                  updateOptions
	tag                                   string
	files                                 map[string][]byte
	badHash, unsafeURL, draft, prerelease bool
	badDigest                             bool // the GitHub asset's digest disagrees with its content (audit R2 mutation M7: the digest check had never run)
	entryType                             byte
	extraPath                             string
	requests                              atomic.Int32
	downloads                             atomic.Int32
	finished                              bool
}

func fixtureUpdate(t *testing.T) *updateFixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "my bin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shikii", "shikii-readout"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := &updateFixture{tag: "v0.4.2", entryType: tar.TypeReg, files: map[string][]byte{
		"shikii":         []byte("#!/bin/sh\nprintf 'shikii 0.4.2\\n'\n"),
		"shikii-readout": []byte("new-readout"), "LICENSE": []byte("license"), "LICENSE-APACHE": []byte("apache"), "LICENSE-MIT": []byte("mit"), "THIRD-PARTY-NOTICES": []byte("notices"),
	}}
	f.opts = updateOptions{executable: filepath.Join(dir, "shikii"), version: "0.4.1", goos: "darwin", goarch: "arm64"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		version := strings.TrimPrefix(f.tag, "v")
		name := "shikii_" + version + "_darwin_arm64.tar.gz"
		archive := fixtureUpdateArchive(t, f)
		digest := sha256.Sum256(archive)
		sum := hex.EncodeToString(digest[:])
		if f.badHash {
			sum = strings.Repeat("0", 64)
		}
		prefix := f.opts.downloadBase + "/hayaenfj/shikii/releases/download/" + f.tag + "/"
		if f.unsafeURL {
			prefix = "https://example.invalid/"
		}
		switch {
		case r.URL.Path == "/latest":
			asset := map[string]any{"name": name, "browser_download_url": prefix + name}
			if f.badDigest {
				asset["digest"] = "sha256:" + strings.Repeat("0", 64)
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "draft": f.draft, "prerelease": f.prerelease, "assets": []any{
				asset,
				map[string]any{"name": "SHA256SUMS", "browser_download_url": prefix + "SHA256SUMS"},
			}})
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			f.downloads.Add(1)
			fmt.Fprintf(w, "%s  %s\n", sum, name)
		case strings.HasSuffix(r.URL.Path, "/"+name):
			f.downloads.Add(1)
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	f.opts.latestURL = server.URL + "/latest"
	f.opts.downloadBase = server.URL
	f.opts.client = server.Client()
	f.opts.finish = func(_ context.Context, exe string) error {
		f.finished = true
		if body, _ := os.ReadFile(exe); !bytes.Equal(body, f.files["shikii"]) {
			return errors.New("shikii not updated")
		}
		if body, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "shikii-readout")); !bytes.Equal(body, f.files["shikii-readout"]) {
			return errors.New("readout not updated")
		}
		return nil
	}
	return f
}

func fixtureUpdateArchive(t *testing.T, f *updateFixture) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"shikii", "shikii-readout", "LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES"} {
		body, ok := f.files[name]
		if !ok {
			continue
		}
		h := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if name == "shikii-readout" && f.entryType != tar.TypeReg {
			h.Typeflag = f.entryType
			h.Linkname = "outside"
			h.Size = 0
			body = nil
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if f.extraPath != "" {
		if err := tw.WriteHeader(&tar.Header{Name: f.extraPath, Mode: 0o644, Size: 1}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte("x"))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertUpdateUnchanged(t *testing.T, f *updateFixture) {
	t.Helper()
	for _, name := range []string{"shikii", "shikii-readout"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(f.opts.executable), name))
		if err != nil || string(b) != "old-"+name {
			t.Fatalf("must keep the existing %s on failure: %q %v", name, b, err)
		}
	}
	if f.finished {
		t.Fatal("ran the post-update step on failure")
	}
}

func TestUpdateInstallsVerifiedPairAtExecutable(t *testing.T) {
	f := fixtureUpdate(t)
	sentinel := filepath.Join(t.TempDir(), "records.jsonl")
	os.WriteFile(sentinel, []byte("keep-records"), 0o600)
	if err := runUpdate(context.Background(), f.opts); err != nil {
		t.Fatal(err)
	}
	if !f.finished {
		t.Fatal("did not run the post-update apply step")
	}
	for _, name := range []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(f.opts.executable), "shikii-licenses", name))
		if err != nil || !bytes.Equal(b, f.files[name]) {
			t.Fatalf("notices must be bundled: %s %v", name, err)
		}
	}
	if b, _ := os.ReadFile(sentinel); string(b) != "keep-records" {
		t.Fatal("changed the conversation records")
	}
	info, err := os.Stat(f.opts.executable)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("no execute permission")
	}
}

// Run a real new version through as a release, all the way to hook install after the replacement.
// HOME and the project are isolated; no published Release or settings in use are touched.
func TestUpdateRefreshesSkillAndPreservesExistingPrompts(t *testing.T) {
	compiled := filepath.Join(t.TempDir(), "new-shikii")
	if out, err := exec.Command("go", "build", "-o", compiled, ".").CombinedOutput(); err != nil {
		t.Fatalf("build the binary to update to: %v %s", err, out)
	}
	body, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	f := fixtureUpdate(t)
	f.tag, f.opts.version, f.files["shikii"] = "v"+shikii.Version, "0.4.3", body
	// Use an absolute path without spaces, the same as a real hook's command.
	f.opts.executable = filepath.Join(t.TempDir(), "shikii")
	for _, name := range []string{"shikii", "shikii-readout"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(f.opts.executable), name), []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // keep hook install from starting a model.
	skill := filepath.Join(home, ".claude", "skills", "shikii")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("old skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(map[string]any{"fixture": "keep", "hooks": map[string]any{
		"UserPromptSubmit": []any{map[string]any{"hooks": []any{
			map[string]any{"type": "command", "command": f.opts.executable + " hook user-prompt-submit"},
			map[string]any{"type": "command", "command": "keep-other-hook"},
		}}},
	}})
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
		t.Fatal(err)
	}
	// The update must not rewrite existing questions, calibrations or human answers.
	preserved := map[string][]byte{}
	for _, rel := range []string{
		".shikii/agent/decisions/needs-plan.yaml",
		"project/.shikii/decisions/needs-plan.yaml",
		"project/.shikii/labels/needs-plan.jsonl",
	} {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		preserved[path] = []byte("fixture: existing question and observations\n")
		if err := os.WriteFile(path, preserved[path], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.opts.finish = finishUpdate
	if err := runUpdate(context.Background(), f.opts); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"SKILL.md", "references/wiki.md", "references/planning.md", "references/review.md"} {
		want, err := shikii.SkillFS.ReadFile("skills/shikii/" + rel)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(skill, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("update must put the new version's skill and prompts in place: %s %v", rel, err)
		}
	}
	for path, want := range preserved {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("must keep the existing decision texts and answers: %s %v", path, err)
		}
	}
	got, _ := os.ReadFile(settingsPath)
	if !bytes.Contains(got, []byte("keep-other-hook")) || !bytes.Contains(got, []byte(`"fixture": "keep"`)) {
		t.Fatal("must keep other hooks and settings")
	}
	// A newly initialized project gets the new version's decision prompts.
	fresh := t.TempDir()
	cmd := exec.Command(f.opts.executable, "--init")
	cmd.Dir = fresh
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize a new project with the new version: %v %s", err, out)
	}
	want, _ := shikii.AgentSpecsFS.ReadFile("integrations/claude-code/specs/needs-plan.yaml")
	if got, err := os.ReadFile(filepath.Join(fresh, ".shikii", "decisions", "needs-plan.yaml")); err != nil || !bytes.Equal(got, want) {
		t.Fatal("a new project's decision prompts must be the new version's", err)
	}
}

func TestUpdateDoesNotDowngradeOrRedownload(t *testing.T) {
	for _, version := range []string{"0.4.2", "0.5.0"} {
		t.Run(version, func(t *testing.T) {
			f := fixtureUpdate(t)
			f.opts.version = version
			if err := runUpdate(context.Background(), f.opts); err != nil {
				t.Fatal(err)
			}
			assertUpdateUnchanged(t, f)
			if f.downloads.Load() != 0 {
				t.Fatal("downloaded a release even though already latest or a newer development version")
			}
		})
	}
}

func TestUpdateRejectsUnverifiedReleases(t *testing.T) {
	cases := map[string]func(*updateFixture){
		"hash mismatch":    func(f *updateFixture) { f.badHash = true },
		"digest mismatch":  func(f *updateFixture) { f.badDigest = true },
		"missing readout":  func(f *updateFixture) { delete(f.files, "shikii-readout") },
		"symlink":          func(f *updateFixture) { f.entryType = tar.TypeSymlink },
		"hard link":        func(f *updateFixture) { f.entryType = tar.TypeLink },
		"path escape":      func(f *updateFixture) { f.extraPath = "../outside" },
		"version mismatch": func(f *updateFixture) { f.files["shikii"] = []byte("#!/bin/sh\nprintf 'shikii 0.0.0\\n'\n") },
		"external URL":     func(f *updateFixture) { f.unsafeURL = true },
		"draft":            func(f *updateFixture) { f.draft = true },
		"prerelease":       func(f *updateFixture) { f.prerelease = true },
		"invalid tag":      func(f *updateFixture) { f.tag = "v0.4.2/other" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixtureUpdate(t)
			change(f)
			if err := runUpdate(context.Background(), f.opts); err == nil {
				t.Fatal("accepted an invalid update")
			}
			assertUpdateUnchanged(t, f)
		})
	}
}

func TestUpdateRollsBackThePairOnReplacementFailure(t *testing.T) {
	f := fixtureUpdate(t)
	f.opts.replace = func(from, to string) error {
		if to == "shikii" {
			return errors.New("synthetic write failure")
		}
		return os.Rename(filepath.Join(filepath.Dir(f.opts.executable), from), filepath.Join(filepath.Dir(f.opts.executable), to))
	}
	if err := runUpdate(context.Background(), f.opts); err == nil {
		t.Fatal("a write failure turned into success")
	}
	assertUpdateUnchanged(t, f)
}

func TestUpdateRefusesCompanionLinksAndSupportsExecutableAlias(t *testing.T) {
	t.Run("does not write through a link", func(t *testing.T) {
		f := fixtureUpdate(t)
		path := filepath.Join(filepath.Dir(f.opts.executable), "shikii-readout")
		outside := filepath.Join(t.TempDir(), "outside")
		os.WriteFile(outside, []byte("keep"), 0o644)
		os.Remove(path)
		os.Symlink(outside, path)
		if err := runUpdate(context.Background(), f.opts); err == nil {
			t.Fatal("accepted a readout link")
		}
		if b, _ := os.ReadFile(outside); string(b) != "keep" {
			t.Fatal("changed the link target")
		}
		if b, _ := os.ReadFile(f.opts.executable); string(b) != "old-shikii" {
			t.Fatal("changed the binary")
		}
	})
	t.Run("updates the real executable", func(t *testing.T) {
		f := fixtureUpdate(t)
		alias := filepath.Join(t.TempDir(), "shikii")
		os.Symlink(f.opts.executable, alias)
		f.opts.executable = alias
		if err := runUpdate(context.Background(), f.opts); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Lstat(alias); info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("replaced the caller's link")
		}
	})
}

func TestUpdateRejectsArgumentsAndUnsupportedPlatforms(t *testing.T) {
	if err := cmdUpdate([]string{"--yes"}); err == nil {
		t.Fatal("accepted an unneeded option")
	}
	f := fixtureUpdate(t)
	f.opts.goos = "windows"
	if err := runUpdate(context.Background(), f.opts); err == nil {
		t.Fatal("looked for an unsupported release")
	}
	if f.requests.Load() != 0 {
		t.Fatal("made a network request on an unsupported platform")
	}
}

func TestUpdateSerializesConcurrentInstallations(t *testing.T) {
	f := fixtureUpdate(t)
	root, err := os.OpenRoot(filepath.Dir(f.opts.executable))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := updateLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := runUpdate(context.Background(), f.opts); err == nil || !strings.Contains(err.Error(), "is running") {
		t.Fatalf("must refuse concurrent updates: %v", err)
	}
	assertUpdateUnchanged(t, f)
	if f.downloads.Load() != 0 {
		t.Fatal("downloaded a release during a concurrent update")
	}
}

func TestUpdateReportsPostInstallFailureWithoutLosingBinaries(t *testing.T) {
	f := fixtureUpdate(t)
	f.opts.finish = func(context.Context, string) error { return errors.New("synthetic hook settings problem") }
	err := runUpdate(context.Background(), f.opts)
	if err == nil || !strings.Contains(err.Error(), "were updated") {
		t.Fatalf("must tell a binary update apart from a failed apply: %v", err)
	}
	for _, name := range []string{"shikii", "shikii-readout"} {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(f.opts.executable), name))
		if !bytes.Equal(b, f.files[name]) {
			t.Fatalf("lost the updated %s", name)
		}
	}
}

func TestUpdateBoundsResponsesAndHandlesOffline(t *testing.T) {
	f := fixtureUpdate(t)
	if _, err := updateGet(context.Background(), f.opts, f.opts.latestURL, 8); err == nil {
		t.Fatal("ignored the response size limit")
	}
	f.opts.latestURL = "http://127.0.0.1:1/latest"
	if err := runUpdate(context.Background(), f.opts); err == nil {
		t.Fatal("a network failure turned into success")
	}
	assertUpdateUnchanged(t, f)
}

func TestUpdateCLIEntry(t *testing.T) {
	if os.Getenv("SHIKII_TEST_UPDATE_ENTRY") == "1" {
		os.Args = []string{"shikii", "update", "--yes"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateCLIEntry$")
	cmd.Env = append(os.Environ(), "SHIKII_TEST_UPDATE_ENTRY=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "shikii update takes no arguments or options") {
		t.Fatalf("must register the CLI entry: %s %v", out, err)
	}
}
