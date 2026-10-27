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

	"github.com/hayaenfj/shikii"
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

// Tests that pin down the findings of the audit (2026-10-23, 0.7.3).

// 1. A .shikii/ made by 0.6.0 to 0.7.2 has no guards/. Treat it as registered anyway, and wiping across 0.7.0 still works (missing places are created later).
func TestStoresWithoutGuardsDirStayRegisteredAndAreWiped(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	root := filepath.Join(repo, ".shikii")
	if err := os.RemoveAll(filepath.Join(root, "guards")); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range registeredProjects(common) {
		found = found || p.Path == repo
	}
	if !found {
		t.Fatal("a store without guards/ (from an old version) is also treated as registered")
	}
	leftover := filepath.Join(root, "records", "old.jsonl")
	if err := os.WriteFile(leftover, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600) // the --init mark from 0.7.3 and earlier
	os.Remove(installedVersionPath(common))
	v, _ := updateVersion("0.7.3")
	wiped, err := resetStoresOnFirstRun(common, v, func(path string) error {
		prev, _ := os.Getwd()
		if err := os.Chdir(path); err != nil {
			return err
		}
		defer os.Chdir(prev)
		return cmdInit(nil)
	})
	if err != nil || !wiped {
		t.Fatalf("an old store is also wiped and initialized again: wiped=%v err=%v", wiped, err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("old records are gone")
	}
	if _, err := os.Stat(filepath.Join(root, "guards")); err != nil {
		t.Fatal("initializing again creates guards/")
	}
}

// 2. Do not initialize HOME. Wiping never touches the shared store (the parent of ~/.shikii/agent), does not wipe when there are unknown entries, keeps project.json, and does not stop at one failure.
func TestWipeNeverTouchesTheSharedPlaceAndKeepsUnknownEntries(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	t.Chdir(home)
	if err := cmdInit(nil); err == nil {
		t.Fatal("do not initialize HOME (wiping would take ~/.shikii/agent with it)")
	}
	common, _ := agentHome()
	if err := wipeProjectStores([]projectEntry{{Path: home}}, func(string) error { return nil }); err == nil {
		t.Fatal("do not wipe the parent of the shared store")
	}
	if _, err := os.Stat(filepath.Join(common, "decisions")); err != nil {
		t.Fatal("the shared store is intact: ", err)
	}
	// Do not wipe a project with unknown entries. Do not delete project.json. Move on to the next even after a failure
	known := t.TempDir()
	for _, d := range []string{"decisions", "records", "wiki"} {
		os.MkdirAll(filepath.Join(known, ".shikii", d), 0o700)
	}
	os.WriteFile(filepath.Join(known, ".shikii", "records", "x.jsonl"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(known, ".shikii", "project.json"), []byte(`{"version":1}`), 0o600)
	odd := t.TempDir()
	os.MkdirAll(filepath.Join(odd, ".shikii", "records"), 0o700)
	os.WriteFile(filepath.Join(odd, ".shikii", "shikii_aside"), []byte("?"), 0o600)
	failing := t.TempDir()
	os.MkdirAll(filepath.Join(failing, ".shikii", "records"), 0o700)
	var reinit []string
	err := wipeProjectStores([]projectEntry{{Path: failing}, {Path: odd}, {Path: known}}, func(path string) error {
		reinit = append(reinit, path)
		if path == failing {
			return os.ErrPermission
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), failing) || !strings.Contains(err.Error(), odd) {
		t.Fatalf("report every failed and refused project: %v", err)
	}
	if _, e := os.Stat(filepath.Join(odd, ".shikii", "shikii_aside")); e != nil {
		t.Fatal("a project with unknown entries has nothing wiped")
	}
	if _, e := os.Stat(filepath.Join(known, ".shikii", "records", "x.jsonl")); !os.IsNotExist(e) {
		t.Fatal("known entries are wiped")
	}
	if _, e := os.Stat(filepath.Join(known, ".shikii", "project.json")); e != nil {
		t.Fatal("keep project.json (so a failed re-initialization does not drop it from the registry)")
	}
	if len(reinit) != 2 || reinit[1] != known {
		t.Fatalf("do not stop at one failure; move on to the next: %v", reinit)
	}
	// Do not wipe .shikii if it is a link (audit mutation A)
	linked := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linked, ".shikii")); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(target, "keep"), []byte("k"), 0o600)
	if err := wipeProjectStores([]projectEntry{{Path: linked}}, func(string) error { return nil }); err == nil {
		t.Fatal("a .shikii that is a link is not wiped")
	}
	if _, e := os.Stat(filepath.Join(target, "keep")); e != nil {
		t.Fatal("the link target is intact")
	}
}

// 1. Even if one wipe is refused and the mark cannot be written, a project that was wiped and initialized again is not wiped twice on the next run (judged by each project's mark).
// 2. --init writes the mark from 0.7.4 on (version 2), and writes the current version if there is no shared mark, so the order go install → --init → hook install --yes does not wipe either.
func TestWipeMarksEachProjectDoneAndInitWritesTheMarkers(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	os.Remove(installedVersionPath(common))
	os.RemoveAll(filepath.Join(common, "projects")) // remove what hookEnv registered, making this the first --init on this machine
	a := gitRepo(t)
	initAt(t, a)
	if _, err := os.Stat(installedVersionPath(common)); err != nil {
		t.Fatal("--init writes the current version if there is no shared mark (someone who installed with go install is not wiped on the first hook install): ", err)
	}
	if v := projectInitVersion(filepath.Join(a, ".shikii")); v < 2 {
		t.Fatalf("--init writes each project's mark as version 2: %d", v)
	}
	// 2 projects like 0.6.0 (version 1, no shared mark). One has an unknown entry
	os.Remove(installedVersionPath(common))
	b, c := gitRepo(t), gitRepo(t)
	initAt(t, b)
	initAt(t, c)
	for _, d := range []string{b, c} {
		os.WriteFile(filepath.Join(d, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+d+`"}`), 0o600)
		os.WriteFile(filepath.Join(d, ".shikii", "records", "old.jsonl"), []byte("old"), 0o600)
	}
	os.WriteFile(filepath.Join(c, ".shikii", "mystery"), []byte("?"), 0o600)
	os.Remove(installedVersionPath(common))
	v, _ := updateVersion("0.7.4")
	reinit := func(path string) error { initAt(t, path); return nil }
	if _, err := resetStoresOnFirstRun(common, v, reinit); err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("refuse the project with an unknown entry and return the reason: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b, ".shikii", "records", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatal("the one not refused is wiped and initialized again")
	}
	if _, err := os.Stat(installedVersionPath(common)); err == nil {
		t.Fatal("if a failure remains, do not write the shared mark (so the refused one is tried on the next run)")
	}
	os.WriteFile(filepath.Join(b, ".shikii", "records", "after.jsonl"), []byte("new"), 0o600)
	if _, err := resetStoresOnFirstRun(common, v, reinit); err == nil {
		t.Fatal("still a failure, since one is still refused")
	}
	if _, err := os.Stat(filepath.Join(b, ".shikii", "records", "after.jsonl")); err != nil {
		t.Fatal("a project that was initialized again is not wiped twice (it has a version 2 mark): ", err)
	}
	os.Remove(filepath.Join(c, ".shikii", "mystery"))
	if _, err := resetStoresOnFirstRun(common, v, reinit); err != nil {
		t.Fatalf("once tidied, the rest is wiped and it succeeds: %v", err)
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); strings.TrimSpace(string(b)) != "0.7.4" {
		t.Fatalf("when everything is done, write the shared mark: %q", b)
	}
}

// 3. A broken or empty mark is not taken as 0.0.0; stop without wiping.
func TestBrokenInstalledVersionMarkerRefusesToWipe(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	keep := filepath.Join(repo, ".shikii", "records", "keep.jsonl")
	os.WriteFile(keep, []byte("k"), 0o600)
	v, _ := updateVersion("0.7.4")
	for _, broken := range []string{"", "garbage\n", "0.7\n"} {
		os.WriteFile(installedVersionPath(common), []byte(broken), 0o644)
		wiped, err := resetStoresOnFirstRun(common, v, func(string) error { return nil })
		if err == nil || wiped {
			t.Fatalf("mark %q cannot be read, so stop without wiping: wiped=%v err=%v", broken, wiped, err)
		}
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("mark %q: records are intact", broken)
		}
	}
}

// 6. The shared-store check works on its own too (audit R2 mutation M6: it does not rely on the allowlist). planWipe makes HOME "not wiped".
func TestSharedPlaceIsRefusedByItself(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	if !isSharedPlaceScope(home) || !isSharedPlaceScope(home+string(filepath.Separator)) {
		t.Fatal("HOME is a shared store")
	}
	if isSharedPlaceScope(t.TempDir()) {
		t.Fatal("other folders are not a shared store")
	}
	plan := planWipe([]projectEntry{{Path: home, InitVersion: 1}})
	if len(plan) != 1 || plan[0].Action != wipeRefuse || !strings.Contains(plan[0].Reason, "shared store") {
		t.Fatalf("HOME is refused before looking at the allowlist: %+v", plan)
	}
}

// Pins down the findings of audit round 3 (2026-10-23).

// 1. Even if --init is redone with a new binary on a store with a mark from 0.7.3 or earlier (version 1), do not write version 2 while the wipe across 0.7.0 is still pending (so the wipe is never skipped forever).
func TestReinitDoesNotMarkAnOldStoreDoneWhileTheWipeIsPending(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	os.WriteFile(filepath.Join(root, "records", "old.jsonl"), []byte("old"), 0o600)
	os.Remove(installedVersionPath(common))
	initAt(t, repo) // redo --init with the new binary (the README steps)
	if v := projectInitVersion(root); v != 1 {
		t.Fatalf("a store whose wipe is not done stays at version 1: %d", v)
	}
	v, _ := updateVersion("0.7.5")
	wiped, err := resetStoresOnFirstRun(common, v, func(path string) error { initAt(t, path); return nil })
	if err != nil || !wiped {
		t.Fatalf("the following hook install wipes it: wiped=%v err=%v", wiped, err)
	}
	if _, err := os.Stat(filepath.Join(root, "records", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatal("old records are gone")
	}
	if v := projectInitVersion(root); v != 2 {
		t.Fatalf("after wiping, version 2: %d", v)
	}
}

// 3. Stray files such as .DS_Store do not make the wipe refuse (treated as strays that may be wiped).
func TestWipeTreatsFinderJunkAsDisposable(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, ".project-init-123"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	plan := planWipe([]projectEntry{{Path: repo, InitVersion: 1}})
	if len(plan) != 1 || plan[0].Action != wipeDo {
		t.Fatalf("not refused for .DS_Store and init leftovers: %+v", plan)
	}
	if err := wipeProjectStores([]projectEntry{{Path: repo}}, func(string) error { return nil }); err != nil {
		t.Fatalf("can be wiped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatal("the strays are gone too")
	}
}

// An update from 0.6.0 is run by the old binary, which finally calls the new binary's `hook install --yes`. The old binary has no reset inside update, so
// the wipe across 0.7.0 is decided by the new binary's first hook install using the shared mark (~/.shikii/agent/version):
// if there is no mark (0.6.0 or earlier) or it is before 0.7.0, wipe the inside of each project's .shikii/, initialize it again, and set the mark to the current version. The second time does not wipe.
func TestFirstHookInstallAfter070WipesRegisteredProjectsOnce(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	if len(registeredProjects(common)) != 2 { // 1 initialized by hookEnv + this repo
		t.Fatal("init registers the project")
	}
	// hookEnv's hook install wrote the mark, so go back to the state of 0.6.0 or earlier (no mark, project.json at version 1)
	if err := os.Remove(installedVersionPath(common)); err != nil {
		t.Fatal("the new binary's hook install writes the mark: ", err)
	}
	for _, p := range registeredProjects(common) {
		os.WriteFile(filepath.Join(p.Path, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+p.Path+`"}`), 0o600)
	}
	store := filepath.Join(repo, ".shikii")
	leftover := filepath.Join(store, "records", "prompt-kind.jsonl")
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(leftover, "old")
	var reinit []string
	run := func(to string) bool {
		v, _ := updateVersion(to)
		var wiped bool
		capture(t, func() error {
			var err error
			wiped, err = resetStoresOnFirstRun(common, v, func(path string) error {
				reinit = append(reinit, path)
				prev, _ := os.Getwd()
				if err := os.Chdir(path); err != nil {
					return err
				}
				defer os.Chdir(prev)
				return cmdInit(nil)
			})
			return err
		})
		return wiped
	}

	// No mark (from 0.6.0 or earlier) → wipe and initialize again; the mark becomes 0.7.0
	if !run("0.7.0") {
		t.Fatal("with no mark, wipe as an update from 0.6.0 or earlier")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("old records are gone")
	}
	if _, err := os.Stat(filepath.Join(store, "project.json")); err != nil {
		t.Fatal("initialize again after wiping: ", err)
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); string(b) != "0.7.0\n" {
		t.Fatalf("set the mark to the current version: %q", b)
	}
	if len(reinit) != 2 || (reinit[0] != repo && reinit[1] != repo) {
		t.Fatalf("initialize each registered project again once: %v", reinit)
	}

	// Again with the same version (hook install run by hand) → do not wipe
	write(leftover, "new")
	if run("0.7.0") {
		t.Fatal("do not wipe if the mark is the current version")
	}
	if b, _ := os.ReadFile(leftover); string(b) != "new" {
		t.Fatal("the second time keeps the records")
	}

	// 0.7.0 → 0.7.1 does not cross → do not wipe; only the mark moves forward
	if run("0.7.1") {
		t.Fatal("an update that does not cross 0.7.0 does not wipe")
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); string(b) != "0.7.1\n" {
		t.Fatalf("the mark moves to the new version: %q", b)
	}
	if b, _ := os.ReadFile(leftover); string(b) != "new" {
		t.Fatal("an update that does not cross keeps the records")
	}

	// Mark is 0.6.0 (in case an old binary someday wrote a mark) → projects with a mark from 0.7.3 or earlier (version 1) are wiped
	for _, p := range registeredProjects(common) {
		write(filepath.Join(p.Path, ".shikii", "project.json"), `{"version":1,"scope":"`+p.Path+`"}`)
	}
	write(installedVersionPath(common), "0.6.0\n")
	if !run("0.7.0") {
		t.Fatal("wipe if the mark is before 0.7.0")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("if the mark is old, the records are gone")
	}
}

// An update to 0.7.0 deletes everything inside each initialized project's .shikii/ and re-initializes it (the owner's decision, 2026-10-23).
// An update that does not cross 0.7.0 deletes nothing.
func TestUpdateResetsProjectStoresWhenCrossing070(t *testing.T) {
	for _, c := range []struct {
		from, to string
		want     bool
	}{
		{"0.6.0", "0.7.0", true}, {"0.5.0", "0.7.2", true}, {"0.6.9", "0.8.0", true},
		{"0.7.0", "0.7.1", false}, {"0.6.0", "0.6.1", false}, {"0.7.1", "0.8.0", false},
	} {
		from, _ := updateVersion(c.from)
		to, _ := updateVersion(c.to)
		if got := crossesStoreReset(from, to); got != c.want {
			t.Errorf("%s → %s: reset=%v, want %v", c.from, c.to, got, c.want)
		}
	}

	project := t.TempDir()
	store := filepath.Join(project, ".shikii")
	for _, p := range []string{"decisions/prompt-kind.yaml", "records/prompt-kind.jsonl", "labels/prompt-kind.jsonl", "wiki/lessons/x/y.md", "project.json", "sessions/a.json"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(store, p)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(store, p), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(project, "README.md")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var reinit []string
	err := wipeProjectStores([]projectEntry{{Path: project}}, func(path string) error {
		reinit = append(reinit, path)
		return os.WriteFile(filepath.Join(path, ".shikii", "project.json"), []byte("new"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(store)
	if len(entries) != 1 || entries[0].Name() != "project.json" {
		t.Fatalf("everything inside .shikii/ must be deleted before re-initializing: %v", entries)
	}
	if b, _ := os.ReadFile(filepath.Join(store, "project.json")); string(b) != "new" {
		t.Fatal("the re-initialized mark must be new")
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Fatal("nothing outside .shikii/ may be touched")
	}
	if len(reinit) != 1 || reinit[0] != project {
		t.Fatalf("each project must be re-initialized exactly once: %v", reinit)
	}

	// In the update flow, reset is called only when crossing the version
	f := fixtureUpdate(t)
	var seen [][2]string
	f.opts.version, f.tag = "0.6.0", "v0.7.0"
	f.files["shikii"] = []byte("#!/bin/sh\nprintf 'shikii 0.7.0\\n'\n") // the downloaded binary reports its own version
	f.opts.reset = func(_ context.Context, _ string, from, to [3]uint64) error {
		seen = append(seen, [2]string{versionString(from), versionString(to)})
		return nil
	}
	if err := runUpdate(context.Background(), f.opts); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != [2]string{"0.6.0", "0.7.0"} {
		t.Fatalf("the update flow must pass the old and new versions to reset: %v", seen)
	}
}
