package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// patientGit lets the git that the hook's code starts take a minute, not the three seconds real hooks have (hookGitTimeout, the one limit
// of every question the hook asks git, where the repository is and the origin included: time_limits_test.go keeps it the only one). A
// test that compares what the hook reads from git with what git says, or that asks whether git was started, must not take a loaded
// machine (make test runs eight groups at once) for a wrong answer: past the limit the hook answers "" (HEAD, the listing, the commits)
// by design, and a question that ran out of time is asked again by the next step that needs it (one git call more than the test expects).
// hookEnv calls it; a test that does not use hookEnv calls it first. A test whose subject is the limit sets its own after this.
func patientGit(t *testing.T) {
	t.Helper()
	oldGit := hookGitTimeout
	hookGitTimeout = time.Minute
	t.Cleanup(func() { hookGitTimeout = oldGit })
}

// patientProbe is patientGit for the probe of the local model and of the project's page (hookProbeTimeout, 300 ms in a real hook): a test
// that starts a server and asserts that the probe found it up must not take a loaded machine for "not up". Unlike hookEnv it is not
// applied to every test, because a probe of a socket that never answers waits out the whole limit.
func patientProbe(t *testing.T) {
	t.Helper()
	oldProbe := hookProbeTimeout
	hookProbeTimeout = 10 * time.Second
	t.Cleanup(func() { hookProbeTimeout = oldProbe })
}

// hookEnv puts the tools in a temporary HOME, prepares .shikii for the given work, and replaces the model and the page.
//
// What hook install --yes and init leave behind is made for real once per test process and copied after that (prepareHookEnv and
// hookEnvTemplate below; TestHookEnvCopyIsTheSameAsTheRealPreparation keeps the copy the same as the real run). A test whose subject is
// install or init runs the real command itself in its body; hookEnv is only where it starts from.
func hookEnv(t *testing.T, model *httptest.Server, roots ...string) (agent string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hookEnvGlobals(t, model)
	var root string
	if len(roots) > 0 {
		root = roots[0]
	} else {
		root = t.TempDir()
	}
	agent, _ = prepareHookEnv(t, home, root, true)
	return agent
}

// hookEnvGlobals replaces what the hook reaches outside the process (the model, the page, the background starts, the browser), and
// restores it when the test ends.
func hookEnvGlobals(t *testing.T, model *httptest.Server) {
	t.Helper()
	oldLocal, oldPage, oldCal, oldTune, oldSpawn, oldOpen := hookLocalURL, hookPageURL, hookCalibrate, hookTune, hookSpawn, openBrowser
	hookCalibrate = runCalibrateInProcess // tests draw lines in-process (production starts a detached process)
	hookTune = runTuneInProcess
	spawned = nil
	hookSpawn = recordSpawn // real up / serve are not started (only recorded)
	opened = nil
	openBrowser = func(url string) { opened = append(opened, url) }
	hookLocalURL = "http://127.0.0.1:1" // a closed port (no model)
	if model != nil {
		hookLocalURL = model.URL
	}
	hookPageURL = "http://127.0.0.1:1"
	patientGit(t)
	t.Cleanup(func() {
		hookLocalURL, hookPageURL, hookCalibrate, hookTune, hookSpawn, openBrowser = oldLocal, oldPage, oldCal, oldTune, oldSpawn, oldOpen // without restoring these, tests that check TestMain's replacements depend on test order (re-audit GH)
	})
}

// prepareHookEnv does what hookEnv does inside the home: hook install --yes there, then init in root. With useCopy it copies what a
// real run leaves behind where it can (hookEnvCopyUsable, copyProjectInit), and runs the real command where it cannot. copied says
// whether both were copied.
func prepareHookEnv(t *testing.T, home, root string, useCopy bool) (store string, copied bool) {
	t.Helper()
	var tmpl *hookEnvTemplate
	if useCopy && hookEnvCopyUsable() {
		tmpl = hookEnvTemplateOf(t)
	}
	if tmpl != nil {
		writeTree(t, home, tmpl.home)
	} else {
		capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	}
	spawned = nil // what install started is not counted
	t.Chdir(root)
	if tmpl != nil {
		if store, ok := copyProjectInit(t, tmpl, root); ok {
			return store, true
		}
	}
	store, _ = initTestProject(t, root)
	return store, false
}

// treeEntry is one folder or file of a tree kept in memory: its path under the top of the tree, its permission bits and its bytes.
type treeEntry struct {
	rel  string
	perm fs.FileMode
	dir  bool
	data []byte
}

// readTree keeps the folders and files under base, parents before their children, except the paths skip names. A link or any other kind
// of file is an error: a copy of it would not be what it is.
func readTree(base string, skip func(rel string) bool) ([]treeEntry, error) {
	var out []treeEntry
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == "." {
			return err
		}
		if skip != nil && skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			out = append(out, treeEntry{rel: rel, perm: info.Mode().Perm(), dir: true})
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out = append(out, treeEntry{rel: rel, perm: info.Mode().Perm(), data: data})
		default:
			return fmt.Errorf("%s: only folders and regular files are copied, not %v", rel, info.Mode().Type())
		}
		return nil
	})
	return out, err
}

func mustReadTree(t *testing.T, base string, skip func(rel string) bool) []treeEntry {
	t.Helper()
	entries, err := readTree(base, skip)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// writeTree writes the entries under base (which exists), with the permission bits they were read with. The file permissions it knows
// are the three a real install, init and git init leave (a permission is written as a number, which TestAnExecutableWrittenOutside-
// TheStubHelpersIsRefused needs, and a file with an execute bit goes through writeUnrunExecutable: the sample hooks of git are such
// files, and nothing runs them); any other permission stops the test.
func writeTree(t *testing.T, base string, entries []treeEntry) {
	t.Helper()
	for _, e := range entries {
		path := filepath.Join(base, e.rel)
		if e.dir {
			if err := os.Mkdir(path, e.perm); err != nil && !errors.Is(err, fs.ErrExist) {
				t.Fatal(err)
			}
			continue
		}
		var err error
		switch e.perm {
		case 0o600:
			err = os.WriteFile(path, e.data, 0o600)
		case 0o644:
			err = os.WriteFile(path, e.data, 0o644)
		case 0o755:
			writeUnrunExecutable(t, path, string(e.data))
		default:
			t.Fatalf("%s has the permission %v, and the copy knows only 0600, 0644 and 0755", e.rel, e.perm)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// hookEnvTemplate is what a real hook install --yes and a real init leave behind, kept in memory. It holds no path of the folders it was
// made in: everything that names a project (its registration, the kept answer of git, its mark and its id) is left out and made by the
// code that makes it (copyProjectInit), so the copy has the paths and ids of the folders it is written to.
type hookEnvTemplate struct {
	home    []treeEntry // the home after install and init, without projects/ and places/ under ~/.shikii/agent
	project []treeEntry // the project folder after init, without .shikii/project.json and .shikii/records/store-id (and no .git)
}

var hookEnvTemplates struct {
	sync.Mutex
	made *hookEnvTemplate
}

// hookEnvTemplateOf makes the template at the first call of the test process, by running install and init for real in a folder of its
// own (a plain folder, so no git; copyProjectInit and the equivalence test do the git part), and returns it at every call.
func hookEnvTemplateOf(t *testing.T) *hookEnvTemplate {
	t.Helper()
	hookEnvTemplates.Lock()
	defer hookEnvTemplates.Unlock()
	if hookEnvTemplates.made == nil {
		hookEnvTemplates.made = buildHookEnvTemplate(t)
	}
	return hookEnvTemplates.made
}

func buildHookEnvTemplate(t *testing.T) *hookEnvTemplate {
	t.Helper()
	base, err := os.MkdirTemp("", "shikii-hookenv-template-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	home, root := filepath.Join(base, "home"), filepath.Join(base, "root")
	for _, dir := range []string{home, root} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// the real run changes the process's home, folder, and the records of what was started or opened: put them back after
	oldHome, oldSpawned, oldOpened := os.Getenv("HOME"), spawned, opened
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("HOME", home)
	defer func() {
		os.Setenv("HOME", oldHome)
		spawned, opened = oldSpawned, oldOpened
		_ = os.Chdir(oldDir)
	}()
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	initTestProject(t, root)
	agent := filepath.Join(".shikii", "agent")
	return &hookEnvTemplate{
		home: mustReadTree(t, home, func(rel string) bool {
			return rel == filepath.Join(agent, "projects") || rel == filepath.Join(agent, "places")
		}),
		project: mustReadTree(t, root, func(rel string) bool {
			return rel == filepath.Join(".shikii", "project.json") || rel == filepath.Join(".shikii", "records", "store-id")
		}),
	}
}

// hookEnvCopyUsable says whether a real install and init would leave what the template holds. They would not where the plugin of
// OpenCode goes outside the home (an absolute XDG_CONFIG_HOME), and where install starts the model (llama-server is found on PATH and
// no model answers) and writes its guard file.
func hookEnvCopyUsable() bool {
	if filepath.IsAbs(os.Getenv("XDG_CONFIG_HOME")) {
		return false
	}
	_, err := lookPath("llama-server")
	return err != nil
}

// copyProjectInit does what init does in root, which has nothing of init's in it yet, by writing the folders and files a real init
// writes (the template) and calling the code that names the project and asks git: where git says the repository is (and keeps that
// answer), the exclude file, the registration with the store's id and the mark. ok is false, and nothing is written, where init would
// do something else (a folder that is not the top of its working tree, one that holds init's files already).
func copyProjectInit(t *testing.T, tmpl *hookEnvTemplate, root string) (store string, ok bool) {
	t.Helper()
	for _, name := range []string{".shikii", ".mcp.json", "opencode.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, fs.ErrNotExist) {
			return "", false
		}
	}
	scope := projectTopAt(root)
	if scope == "" || !sameFolder(scope, root) || isSharedPlaceScope(scope) {
		return "", false
	}
	writeTree(t, scope, tmpl.project)
	if err := excludeProjectStore(scope); err != nil {
		t.Fatal(err)
	}
	if err := finishProjectInitModel(filepath.Join(scope, ".shikii"), scope, projectInitVersionNow, ""); err != nil {
		t.Fatal(err)
	}
	store, _, err := projectStoreAt(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, true
}

func initTestProject(t *testing.T, root string) (string, string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	capture(t, func() error { return cmdInit(nil) })
	t.Chdir(previous)
	store, scope, err := projectStoreAt(root)
	if err != nil {
		t.Fatal(err)
	}
	// existing projects keep the old format even after an update. Prepare the state for that regression check.
	// the new format's inputs and paths are checked in hook_planning_test.go.
	path := filepath.Join(store, "decisions", "needs-plan.yaml")
	sp := mustLoad(t, path)
	sp.Version = 1
	sp.Question = "A user sent `request` to a coding agent. Before changing anything, should the agent first agree on an approach with the user (because the change is large or ambiguous, several designs are reasonable, it changes existing behaviour, or the user is still thinking aloud), rather than just doing it?\n"
	sp.State = spec.State{Template: "request: {{ request }}", Required: []string{"request"}, Context: "previous assistant turn: {{ previous }}\nrequest: {{ request }}"}
	data, err := yaml.Marshal(sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return store, scope
}

// describeTree is the entries of a tree as text to compare: each path with its kind and permission bits, and the bytes of each file.
// Three things differ between two preparations that are the same, and are replaced by a fixed word: the store's random id (also in the
// name of its registration), the device and inode numbers shikii notes of a folder or file, and the times a kept answer of git was
// written.
func describeTree(side string, entries []treeEntry, storeID string, out map[string]string) {
	storeDirRe := regexp.MustCompile(`"store_dir":"\d+:\d+"`)
	placeTimeRe := regexp.MustCompile(`"at":\d+`)
	placeFileRe := regexp.MustCompile(`"(i|m|c)":\d+`)
	for _, e := range entries {
		key := side + "/" + filepath.ToSlash(e.rel)
		if storeID != "" {
			key = strings.ReplaceAll(key, storeID, "<store-id>")
		}
		if e.dir {
			out[key] = fmt.Sprintf("folder %v", e.perm)
			continue
		}
		text := string(e.data)
		if storeID != "" {
			text = strings.ReplaceAll(text, storeID, "<store-id>")
		}
		switch {
		case strings.HasPrefix(key, "home/.shikii/agent/projects/"):
			text = storeDirRe.ReplaceAllString(text, `"store_dir":"<device:inode>"`)
		case strings.HasPrefix(key, "home/.shikii/agent/places/"):
			text = placeTimeRe.ReplaceAllString(text, `"at":<time>`)
			text = placeFileRe.ReplaceAllString(text, `"$1":<number>`)
		}
		out[key] = fmt.Sprintf("file %v\n%s", e.perm, text)
	}
}

// diffDescribed lists where two described trees differ.
func diffDescribed(want, got map[string]string, wantName, gotName string) []string {
	var out []string
	for key, w := range want {
		g, ok := got[key]
		switch {
		case !ok:
			out = append(out, "only in the "+wantName+": "+key)
		case g != w:
			wl, gl := strings.Split(w, "\n"), strings.Split(g, "\n")
			at := 0
			for at < len(wl) && at < len(gl) && wl[at] == gl[at] {
				at++
			}
			line := func(lines []string) string {
				if at >= len(lines) {
					return "(end)"
				}
				if len(lines[at]) > 100 {
					return lines[at][:100] + "..."
				}
				return lines[at]
			}
			out = append(out, fmt.Sprintf("differs: %s (line %d: %s %q, %s %q)", key, at, wantName, line(wl), gotName, line(gl)))
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			out = append(out, "only in the "+gotName+": "+key)
		}
	}
	sort.Strings(out)
	return out
}

// staleTemplate is a copy of the template for a test to make stale: its bytes are its own.
func staleTemplate(whole *hookEnvTemplate) *hookEnvTemplate {
	stale := &hookEnvTemplate{}
	for _, side := range []struct {
		from []treeEntry
		to   *[]treeEntry
	}{{whole.home, &stale.home}, {whole.project, &stale.project}} {
		for _, e := range side.from {
			e.data = append([]byte(nil), e.data...)
			*side.to = append(*side.to, e)
		}
	}
	return stale
}

// preparedTrees runs the preparation hookEnv does (install in a home, init in a folder) twice, in the same two paths, once for real and
// once as hookEnv does it, and returns both results as described trees. The same paths, so a path that a copy keeps from the folders the
// template was made in, or a name made from a path, shows as a difference. A test that wants a stale template passes makeStale.
func preparedTrees(t *testing.T, inGit bool, makeStale func(*hookEnvTemplate)) (real, copied map[string]string) {
	t.Helper()
	base := t.TempDir()
	home, root := filepath.Join(base, "home"), filepath.Join(base, "root")
	t.Setenv("HOME", home)
	hookEnvGlobals(t, nil)
	if makeStale != nil {
		whole := hookEnvTemplateOf(t)
		stale := staleTemplate(whole)
		makeStale(stale)
		hookEnvTemplates.Lock()
		hookEnvTemplates.made = stale
		hookEnvTemplates.Unlock()
		t.Cleanup(func() {
			hookEnvTemplates.Lock()
			hookEnvTemplates.made = whole
			hookEnvTemplates.Unlock()
		})
	}
	prepare := func(useCopy bool) map[string]string {
		t.Chdir(base)
		for _, dir := range []string{home, root} {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if inGit {
			if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
				t.Skipf("git init: %v %s", err, out)
			}
		}
		if _, did := prepareHookEnv(t, home, root, useCopy); did != useCopy {
			t.Fatalf("prepared by copying is %v, want %v: the copy was not used where it must be", did, useCopy)
		}
		homeTree, rootTree := mustReadTree(t, home, nil), mustReadTree(t, root, nil)
		storeID := ""
		for _, e := range rootTree {
			if e.rel == filepath.Join(".shikii", "records", "store-id") {
				storeID = strings.TrimSpace(string(e.data))
			}
		}
		out := map[string]string{}
		describeTree("home", homeTree, storeID, out)
		describeTree("root", rootTree, storeID, out)
		return out
	}
	real = prepare(false)
	copied = prepare(true)
	return real, copied
}

// The preparation hookEnv copies is, in every path, permission and byte, the one a real hook install --yes and init leave behind, in
// a git repository and in a plain folder. The copy keeps no path of the folders the template was made in: the registration of the
// project, the kept answer of git and the id of the store are the ones the code makes for the folders it is written to.
func TestHookEnvCopyIsTheSameAsTheRealPreparation(t *testing.T) {
	for _, inGit := range []bool{true, false} {
		name := map[bool]string{true: "git repository", false: "plain folder"}[inGit]
		t.Run(name, func(t *testing.T) {
			real, copied := preparedTrees(t, inGit, nil)
			// two trees that are both empty are the same: what only a real run makes must be in the real one
			want := []string{"home/.claude/settings.json", "home/.codex/config.toml", "home/.claude/skills/shikii/SKILL.md", "home/.shikii/agent/projects/<store-id>.json", "root/.shikii/project.json", "root/.shikii/decisions/needs-plan.yaml", "root/.mcp.json"}
			places := 0
			for key := range real {
				if strings.HasPrefix(key, "home/.shikii/agent/places/") {
					places++
				}
			}
			if inGit {
				if places != 1 {
					t.Errorf("the real preparation in a git repository holds %d kept answers of git, want 1", places)
				}
			}
			for _, key := range want {
				if _, ok := real[key]; !ok {
					t.Errorf("the real preparation holds no %s; the comparison would prove nothing", key)
				}
			}
			if diffs := diffDescribed(real, copied, "real run", "copy"); len(diffs) > 0 {
				t.Fatalf("the copy differs from the real preparation in %d places:\n%s", len(diffs), strings.Join(diffs, "\n"))
			}
		})
	}
}

// The comparison above finds a copy that is stale: a file that changed, one that is gone, permission bits that changed, and a path of
// another folder kept in a file.
func TestHookEnvCopyComparisonCatchesAStaleCopy(t *testing.T) {
	edit := func(entries []treeEntry, rel string, change func(e *treeEntry)) {
		for i := range entries {
			if entries[i].rel == filepath.FromSlash(rel) {
				change(&entries[i])
				return
			}
		}
		t.Fatalf("the template has no %s", rel)
	}
	drop := func(entries []treeEntry, rel string) []treeEntry {
		for i := range entries {
			if entries[i].rel == filepath.FromSlash(rel) {
				return append(entries[:i:i], entries[i+1:]...)
			}
		}
		t.Fatalf("the template has no %s", rel)
		return nil
	}
	for _, tc := range []struct {
		name, want string
		stale      func(m *hookEnvTemplate)
	}{
		{"a file of the home has other bytes", "home/.claude/settings.json", func(m *hookEnvTemplate) {
			edit(m.home, ".claude/settings.json", func(e *treeEntry) { e.data = append(e.data, ' ') })
		}},
		{"a file of the home is gone", "home/.codex/hooks.json", func(m *hookEnvTemplate) { m.home = drop(m.home, ".codex/hooks.json") }},
		{"a file of the project has other permission bits", "root/.mcp.json", func(m *hookEnvTemplate) {
			edit(m.project, ".mcp.json", func(e *treeEntry) { e.perm = 0o600 })
		}},
		{"a file of the project is gone", "root/.shikii/wiki/index.md", func(m *hookEnvTemplate) { m.project = drop(m.project, ".shikii/wiki/index.md") }},
		{"a file of the project keeps a path of another folder", "root/opencode.json", func(m *hookEnvTemplate) {
			edit(m.project, "opencode.json", func(e *treeEntry) { e.data = append(e.data, []byte("/tmp/another-folder/.shikii")...) })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			real, copied := preparedTrees(t, false, tc.stale)
			diffs := diffDescribed(real, copied, "real run", "copy")
			if !strings.Contains(strings.Join(diffs, "\n"), tc.want) {
				t.Fatalf("the stale copy was not found at %s; differences: %v", tc.want, diffs)
			}
		})
	}
}

// describedRepo is a repository folder as the text describeTree makes of it.
func describedRepo(t *testing.T, repo string) map[string]string {
	t.Helper()
	out := map[string]string{}
	describeTree(".", mustReadTree(t, repo, nil), "", out)
	return out
}

// The .git folder gitRepo copies is the one git init makes, and git works in it.
func TestGitRepoCopyIsTheSameAsGitInit(t *testing.T) {
	made, did := gitRepoWith(t, false)
	if did {
		t.Fatal("a repository made without copying was copied")
	}
	copied, did := gitRepoWith(t, true)
	if !did {
		t.Fatal("the repository was not copied where it must be")
	}
	real := describedRepo(t, made)
	if len(real) < 20 {
		t.Fatalf("git init made %d paths: the comparison would prove nothing", len(real))
	}
	if diffs := diffDescribed(real, describedRepo(t, copied), "git init", "copy"); len(diffs) > 0 {
		t.Fatalf("the copy differs from git init in %d places:\n%s", len(diffs), strings.Join(diffs, "\n"))
	}
	out, err := exec.Command("git", "-C", copied, "rev-parse", "--git-dir", "--is-inside-work-tree").CombinedOutput()
	if err != nil || string(out) != ".git\ntrue\n" {
		t.Fatalf("git does not work in the copy: %v %q", err, out)
	}
}

// The comparison finds a copy that is stale: other bytes in a file, a file that is gone and other permission bits.
func TestGitRepoCopyComparisonCatchesAStaleCopy(t *testing.T) {
	_, did := gitRepoWith(t, true) // makes the template (or finds git missing)
	key, _ := gitInitEnvKey()
	whole := gitInitTemplateFor(key)
	if !did || whole == nil {
		t.Skip("git is missing")
	}
	for _, tc := range []struct {
		name, want string
		stale      func(e *treeEntry)
		path       string
	}{
		{"a file has other bytes", "./.git/config", func(e *treeEntry) { e.data = append(e.data, []byte("\tbare = true\n")...) }, "config"},
		{"a file has other permission bits", "./.git/description", func(e *treeEntry) { e.perm = 0o600 }, "description"},
		{"a file is gone", "./.git/HEAD", nil, "HEAD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stale []treeEntry
			for _, e := range whole {
				e.data = append([]byte(nil), e.data...)
				switch {
				case e.rel != filepath.Join(".git", tc.path):
					stale = append(stale, e)
				case tc.stale != nil:
					tc.stale(&e)
					stale = append(stale, e)
				}
			}
			gitInitTemplates.Lock()
			gitInitTemplates.made = stale
			gitInitTemplates.Unlock()
			t.Cleanup(func() {
				gitInitTemplates.Lock()
				gitInitTemplates.made = whole
				gitInitTemplates.Unlock()
			})
			made, _ := gitRepoWith(t, false)
			copied, did := gitRepoWith(t, true)
			if !did {
				t.Fatal("the repository was not copied")
			}
			diffs := diffDescribed(describedRepo(t, made), describedRepo(t, copied), "git init", "copy")
			if !strings.Contains(strings.Join(diffs, "\n"), tc.want) {
				t.Fatalf("the stale copy was not found at %s; differences: %v", tc.want, diffs)
			}
		})
	}
}

// spawned is the record of hookSpawn (tests). opened is the record of URLs opened in the browser.
var spawned, opened []string

// spawnedUps is only the local model starts (up) among those started (excluding the page's serve).
func spawnedUps() []string {
	var out []string
	for _, s := range spawned {
		if s == "up" || strings.HasPrefix(s, "up ") {
			out = append(out, s)
		}
	}
	return out
}

func recordSpawn(agent, logName string, args ...string) int {
	spawned = append(spawned, strings.Join(args, " "))
	return 0
}

// Existing hook tests isolate their original guard/decision by supplying a
// completed reasoning prerequisite. Reasoning tests use runUnreviewedHook and
// exercise the real prepare/record flow, including missing evidence and recovery.
func runHook(t *testing.T, event string, in map[string]any) string {
	if event == "stop" {
		cwd, _ := in["cwd"].(string)
		session, _ := in["session_id"].(string)
		if root, _, err := projectStoreAt(cwd); err == nil {
			supplyReasoningFixture(root, session)
		}
	}
	return runManualHook(t, event, in)
}

// Legacy guard fixtures supply their reasoning prerequisite and exercise the
// manual preparation path. New flow tests keep the real hook preparation.
func runManualHook(t *testing.T, event string, in map[string]any) string {
	previous := hookWorkPreparation
	hookWorkPreparation = func(string, *hookInput, *hookOut) {}
	defer func() { hookWorkPreparation = previous }()
	return runUnreviewedHook(t, event, in)
}

func supplyReasoningFixture(root, session string) {
	if s, err := readSession(root, session); err == nil && s.Reasoning != nil {
		r := s.Reasoning
		entry := &reasoningEntry{workReasoning: workReasoning{Phase: "finish", Focus: "Synthetic prerequisite for another hook contract", Assessment: "retain", Reason: "This fixture isolates the original hook behavior.", Evidence: []string{"current"}, NextAction: "Check the hook under test.", Result: "The fixture supplies only the reasoning prerequisite."}, Generation: r.Generation}
		r.Start, r.Latest, r.Finish = entry, entry, entry
		// Storage-failure tests intentionally make this unavailable; the real
		// hook must observe that failure rather than the fixture ending the test.
		_ = saveSession(root, session, s)
	}
}

// runUnreviewedHook preserves every production prerequisite.
func runUnreviewedHook(t *testing.T, event string, in map[string]any) string {
	b, _ := json.Marshal(in)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	return strings.TrimSpace(capture(t, func() error { return cmdHook([]string{event}) }))
}

// fakeAgentModel is a fake that answers the hook's 4 specs: choice (prompt-kind) is edit 0.9, noul is decided by the function p(state) of the state text.
func fakeAgentModel(t *testing.T, p func(state string) float64) *httptest.Server {
	return fakeAgentModelEdit(t, p, func(string) float64 { return 0.9 })
}

// heldDecisionModel is a model that holds its answer to the needs-plan decision until release is called, and answers every other question
// at once. entered is closed when the decision itself has reached it. The first question a request raises is not the decision: the
// hook asks whether the lesson it is about to deliver applies, and a model that took that call for the decision held it (until the hook gave up after
// hookDecideTimeout), closed entered, and let the real decision through unheld: the edit a test makes "during the decision" then raced with
// the submit, and under load ended after it ("detects a change ..." with no error).
func heldDecisionModel(t *testing.T) (model *httptest.Server, entered <-chan struct{}, release func()) {
	t.Helper()
	in, out := make(chan struct{}), make(chan struct{})
	var enter, leave sync.Once
	model = fakeAgentModel(t, func(state string) float64 {
		if strings.HasPrefix(state, "Current request:") {
			enter.Do(func() { close(in) })
			<-out
		}
		return .9
	})
	return model, in, func() { leave.Do(func() { close(out) }) }
}

// fakeAgentModelEdit is fakeAgentModel whose choice answer gives edit the probability edit(state) (the rest share what is left).
func fakeAgentModelEdit(t *testing.T, p func(state string) float64, edit func(state string) float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State     string `json:"state"`
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		body, _ := readAll(r)
		_ = json.Unmarshal(body, &req)
		ans := map[string]any{}
		for k, q := range req.Questions {
			if q.Type == "noul" {
				ans[k] = map[string]any{"type": "noul", "noul": p(req.State)}
			} else if _, three := q.Criteria["not_applicable"]; three {
				// needs-plan's three answers: yes gets p and the other two split what is left evenly, so the top (the band) is p when p is the largest, as noul's p(yes) is
				y := p(req.State)
				probs := map[string]float64{"yes": y, "no": (1 - y) / 2, "not_applicable": (1 - y) / 2}
				top := "yes"
				// a fixed order: no and not_applicable tie below p, and the top must not depend on map order
				for _, id := range []string{"yes", "no", "not_applicable"} {
					if probs[id] > probs[top] {
						top = id
					}
				}
				ans[k] = map[string]any{"type": "choice", "choice": top, "probabilities": probs, "confidence": y}
			} else {
				e := edit(req.State)
				rest := (1 - e) / 10
				ans[k] = map[string]any{"type": "choice", "choice": "edit", "probabilities": map[string]float64{"edit": e, "explain": 4 * rest, "investigate": 3 * rest, "run": 2 * rest, "decide": rest}, "confidence": e}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "kev-4b", "answers": ans})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var b []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil {
			return b, nil
		}
	}
}

// gitRepo is a temporary git repository (skip if git is missing). Its .git is the one a real `git init` made once in this test process,
// copied, where git init would make the same (gitInitEnvKey, gitInitTemplateFor); TestGitRepoCopyIsTheSameAsGitInit keeps the copy the same.
func gitRepo(t *testing.T) string {
	repo, _ := gitRepoWith(t, true)
	return repo
}

// gitRepoWith is gitRepo that says whether the .git was copied, and, with useCopy false, always runs git init.
func gitRepoWith(t *testing.T, useCopy bool) (repo string, copied bool) {
	t.Helper()
	repo = t.TempDir()
	if useCopy {
		if key, ok := gitInitEnvKey(); ok {
			if tmpl := gitInitTemplateFor(key); tmpl != nil {
				writeTree(t, repo, tmpl)
				return repo, true
			}
		}
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	return repo, false
}

var gitInitTemplates struct {
	sync.Mutex
	tried bool
	key   string // gitInitEnvKey where the template was made
	made  []treeEntry
}

// gitInitTemplateFor is the folder a real git init makes (the .git folder and what is in it), made at the first call of the test
// process, under the environment key says, and returned for that key; nil for another key, where git is missing, and where it leaves
// something that is not a folder or a regular file.
func gitInitTemplateFor(key string) []treeEntry {
	gitInitTemplates.Lock()
	defer gitInitTemplates.Unlock()
	if !gitInitTemplates.tried {
		gitInitTemplates.tried = true
		if dir, err := os.MkdirTemp("", "shikii-gitrepo-template-*"); err == nil {
			defer os.RemoveAll(dir)
			if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err == nil {
				gitInitTemplates.key = key
				gitInitTemplates.made, _ = readTree(dir, nil)
			}
		}
	}
	if gitInitTemplates.key != key {
		return nil
	}
	return gitInitTemplates.made
}

// gitInitEnvKey is what git init reads that a test can change: the GIT_ variables and XDG_CONFIG_HOME of the process. The template
// is used only under the key it was made under. ok is false where the home holds a git configuration of its own, which git init reads
// too: the real command runs there.
func gitInitEnvKey() (key string, ok bool) {
	var vars []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			vars = append(vars, kv)
		}
	}
	sort.Strings(vars)
	home := os.Getenv("HOME")
	for _, rel := range []string{".gitconfig", filepath.Join(".config", "git")} {
		if _, err := os.Lstat(filepath.Join(home, rel)); !errors.Is(err, fs.ErrNotExist) {
			return "", false
		}
	}
	return strings.Join(vars, "\n"), true
}

// lowerMinN lowers min_n of a spec copied by install (as if the person edited the spec).
func lowerMinN(t *testing.T, agent, specID string, n int) string {
	p := filepath.Join(agent, "decisions", specID+".yaml")
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "min_n: 50") {
		t.Fatalf("%s: min_n: 50 not found", p)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "min_n: 50", fmt.Sprintf("min_n: %d", n), 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runTuneInProcess is the in-process version of hookTune (for tests).
func runTuneInProcess(agent, path, storeDir string) {
	var buf bytes.Buffer
	_, err := func() (res *tuneResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("tune panicked: %v", r)
			}
		}()
		return runTune(tuneOpts{output: &buf, specPath: path, ef: endpointFlags{local: hookLocalURL}, storeDir: storeDir, labeled: "none"})
	}()
	line := lastLine(buf.String())
	if err != nil {
		line += " ERROR: " + err.Error()
	}
	hookLog(fmt.Sprintf("auto-tune %s: %s", strings.TrimSuffix(filepath.Base(path), ".yaml"), line))
}

// runCalibrateInProcess runs calibration in-process in tests.
func runCalibrateInProcess(agent, path, storeDir, model string) {
	var buf bytes.Buffer
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("calibrate panicked: %v", r)
			}
		}()
		return runCalibrate(path, calibrateOpts{output: &buf, ef: endpointFlags{local: hookLocalURL, model: model}, storeDir: storeDir, labeled: "none", bins: 10, write: true, rung: "minimal"})
	}()
	line := lastLine(buf.String())
	if err != nil {
		line += " ERROR: " + err.Error()
	}
	hookLog(fmt.Sprintf("auto-calibrate %s (%s): %s", strings.TrimSuffix(filepath.Base(path), ".yaml"), model, line))
}

// treeSnapshot is only the hash of the working tree content (the first half of treeState).
func treeSnapshot(cwd string) string {
	h, _, _, _ := treeState(cwd)
	return h
}

// hookCommands checks what is registered in settings.json in tests.
func hookCommands(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	var out []string
	for _, groups := range cfg.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if isShikiiHook(h.Command) {
					out = append(out, h.Command)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// treeState reads the working tree state other than scope in tests.
func treeState(cwd string) (hash, listing string, listed bool, project string) {
	hash, listing, listed, project, _ = treeStateWithScope(cwd)
	return
}

// 0.9.2: shikii no longer writes into a person's AGENTS.md; 0.9.7: it no longer edits it either. A section an older version added is only
// told (compat_cleanup_test.go).

func agentsOld(heading string) string {
	return agentsMarker + "\n" + heading + "\n\n- old rules\n" + agentsMarker + "\n"
}

// Reproductions from the independent audit of 0.9.0 (added by the auditor; confirmed to fail before the fix).

// R1 (re-launching within 30 seconds of a refusal shifts by one) is not fixed, because SubagentStart has no call id and can only be matched by time and order;
// it is written as a limitation in integrations/claude-code.md

// doneTraceAfter runs one request and its Stop in a session and returns that request's done-as-asked trace.
func doneTraceAfter(t *testing.T, repo, root, session, prompt string) string {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
	for _, r := range loadSession(root, session).Reviews {
		if r.Spec == "done-as-asked" {
			return r.Trace
		}
	}
	t.Fatalf("the request must have a done-as-asked decision: %+v", loadSession(root, session))
	return ""
}

// 0.9.3 audit 3. Truths about done-as-asked come only from fixed rules on the person's message, never from the agent's
// continues_previous (the agent being judged must not be able to erase a restatement or manufacture a "done" truth).

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func correctionSeed(t *testing.T) (store.Store, string, http.Handler, string) {
	t.Helper()
	st, scope, h, draft, _ := memoryEffectFixture(t)
	body := map[string]any{"operation": "submit", "wiki": []map[string]any{{
		"id": draft.ID, "sources": draft.Sources, "title": draft.Title,
		"lesson": draft.Lesson, "applies_to": draft.AppliesTo,
		"correction": map[string]string{"source_id": draft.Sources[0], "quote": "まだ英語が残っています", "action": "変更した案内の全文を日本語で確認する"},
	}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("save the quote of the person's correction and the action to change next into the lesson")
	}
	ctx := memoryEffectPrompt(t, scope, "correction")
	if !strings.Contains(ctx, "まだ英語が残っています") || !strings.Contains(ctx, "変更した案内の全文を日本語で確認する") || len(ctx) > 1800 {
		t.Fatal("deliver the correction and the action, briefly, to the next similar request")
	}
	id := memoryEffectComplete(t, st, scope, "correction")
	item := memoryEffectItem(t, h, id)
	return st, scope, h, item["quote"].(map[string]any)["key"].(string)
}

// 0.9.3: a deferred outcome whose session was closed or killed mid-turn (no organizing, no Stop, no next request) is settled
// by any later hook of the project or a reflect query once it is older than deferralStale, with the provisional grouping, once.

// a long message on other work: settles the previous outcome as "yes" when it stands alone
const sweepReply = "Draft a migration guide for v0.8 users explaining how backups work now."

// doneLabelCount is how many label lines the trace has (LabeledTraces keeps only the newest).
func doneLabelCount(t *testing.T, root, trace string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "labels", "done-as-asked.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), `"`+trace+`"`)
}

// ageDeferral moves the waiting turn's start back past the limit (as if the session was left alone that long).
func ageDeferral(t *testing.T, root string) {
	t.Helper()
	if err := updateSession(root, "planning", func(s *hookSession) { s.Planning.At = time.Now().Add(-deferralStale - time.Minute) }); err != nil {
		t.Fatal(err)
	}
}

func deferredTurn(t *testing.T, reply string) (repo, root, done string) {
	t.Helper()
	repo, root, _ = planningFixture(t)
	done = continuesTurn(t, repo, root, reply)
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatalf("the outcome must wait for the organizing or the Stop: %+v", loadSession(root, "planning").Planning)
	}
	if _, err := os.Stat(deferralMarkerPath(root, "planning")); err != nil {
		t.Fatalf("an open deferral must have its marker: %v", err)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("not settled yet: %+v", l)
	}
	return repo, root, done
}

func otherPrompt(t *testing.T, repo string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "Explain how the page lists waiting answers."})
}

// the JSON the block asks for, as written in its reason
func askedJSON(t *testing.T, reason string) map[string]any {
	t.Helper()
	start := strings.Index(reason, `{"agreed"`)
	end := strings.Index(reason[max(start, 0):], `"}`)
	if start < 0 || end < 0 {
		t.Fatalf("the block must carry the JSON to pass: %s", reason)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(reason[start:start+end+2]), &payload); err != nil {
		t.Fatalf("the JSON in the block must be valid: %v (%s)", err, reason)
	}
	return payload
}

func guardAndGroupingOnce(t *testing.T, reply string) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	continuesTurn(t, repo, root, reply)
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("the first Stop blocks (guard and grouping ask): %s", out)
	}
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]bool{"unknown": true}, "open": map[string]bool{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
	if _, block := stopReason(t, out); block {
		t.Fatalf("the Stop after the block is not blocked, even with the guard still failing: %s", out)
	}
	if s := loadSession(root, "planning"); !s.Judged {
		t.Fatalf("the Stop after the block judges the turn: %+v", s)
	}
}

// Audit 0.9.2 (translation): the guard texts after the move to English.

// setHookLanguage writes language into the test HOME's ~/.claude/settings.json (after hookEnv installed the hooks there).
func setHookLanguage(t *testing.T, language string) {
	t.Helper()
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(settings); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg["language"] = language
	b, _ := json.Marshal(cfg)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// newest is the first of the order Records returns (newest first).
func newest(recs []store.Record) *store.Record {
	if len(recs) == 0 {
		return nil
	}
	return &recs[0]
}

// pageGlossary reads the old words of the page's glossary (web/scripts/test-glossary.mjs, the first item of each row) for one language,
// so the notices the person reads in the terminal are held to the same words as the page.
func pageGlossary(t *testing.T, lang string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "scripts", "test-glossary.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "\n  "+lang+": [\n")
	if start < 0 {
		t.Fatalf("the glossary has no %s rows", lang)
	}
	end := strings.Index(src[start:], "\n  ],")
	if end < 0 {
		t.Fatalf("the %s rows of the glossary do not end", lang)
	}
	var words []string
	for _, m := range regexp.MustCompile(`(?m)^\s*\['([^']+)',`).FindAllStringSubmatch(src[start:start+end], -1) {
		words = append(words, m[1])
	}
	if len(words) < 10 {
		t.Fatalf("the %s glossary was not read: %q", lang, words)
	}
	return words
}

// noticeProblems lists what a notice to the person must not hold: a spec id, a raw probability (p=, p(yes)=), and the glossary's old words
// (Japanese also: act). The page link is removed first (its query names the spec, which the person does not read).
func noticeProblems(t *testing.T, text string, glossary map[string][]string) []string {
	t.Helper()
	en, ja, _ := strings.Cut(text, "\n")
	var found []string
	for lang, line := range map[string]string{"en": en, "ja": ja} {
		line = noticeURL.ReplaceAllString(line, "")
		if line == "" {
			continue
		}
		bad := append(append([]string{"p=", "p(", "p(はい)"}, hookSpecs...), wikiSpecs...)
		bad = append(bad, glossary[lang]...)
		if lang == "ja" {
			bad = append(bad, "act")
		}
		for _, w := range bad {
			if strings.Contains(strings.ToLower(line), strings.ToLower(w)) {
				found = append(found, lang+": "+w+" in "+line)
			}
		}
	}
	return found
}

// startSubagent runs from the parent passing a task to a child with the Agent tool (PreToolUse agent-task) to that child starting (SubagentStart) (0.9.1)
func startSubagent(t *testing.T, session, scope, agentID, agentType, prompt string) string {
	t.Helper()
	if out := runHook(t, "agent-task", map[string]any{"session_id": session, "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"description": "d", "prompt": prompt, "subagent_type": agentType}}); out != "" {
		t.Fatalf("the hook that keeps the child's task outputs nothing (does not stop the tool): %q", out)
	}
	return runHook(t, "subagent-start", map[string]any{"session_id": session, "cwd": scope, "agent_id": agentID, "agent_type": agentType})
}

func subagentContext(t *testing.T, out string) string {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("SubagentStart returns JSON: %q", out)
	}
	hs, _ := parsed["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "SubagentStart" {
		t.Fatalf("returns it in the SubagentStart context: %s", out)
	}
	ctx, _ := hs["additionalContext"].(string)
	return ctx
}

func memoryEffectItem(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	for _, raw := range q["sources"].([]any) {
		s := raw.(map[string]any)
		if s["source_material"].(map[string]any)["id"] == id {
			items, ok := s["memories"].([]any)
			if !ok || len(items) != 1 {
				t.Fatal("returns the quote actually delivered to that request together with the unassessed memory")
			}
			return items[0].(map[string]any)
		}
	}
	t.Fatal("even after scoring and the wiki are done, the unassessed memory is not forgotten")
	return nil
}

// Audit 0.9.2 (behavior), test holes: checks that survived when their line was broken on purpose.

// hookSettingsWith writes a settings file with one handler per command, each under its event, and returns its path.
func hookSettingsWith(t *testing.T, commands map[string]string) string {
	t.Helper()
	settings := filepath.Join(t.TempDir(), "settings.json")
	hooks := map[string]any{}
	for command, event := range commands {
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
		list, _ := hooks[event].([]any)
		hooks[event] = append(list, group)
	}
	b, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return settings
}

// readHookCommands is every handler's command in a settings file.
func readHookCommands(t *testing.T, settings string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, groups := range cfg.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				got[h.Command] = true
			}
		}
	}
	return got
}

// Found on 2026-11-15: under make test (eight groups of cmd/shikii tests at the same time) TestTheNoticeAndTheStopBlockAskForOneFlow
// once took 22 seconds, and its Stop block had no line for the plan: reading the working tree took longer than hookGitTimeout, so
// the hook counted the changed files as unknown. Setting that timeout to a nanosecond gave the same block. hookEnv gives hook tests
// a minute, so a loaded machine does not change what they see; real hooks keep three seconds.
func TestHookTestsDoNotDependOnGitFinishingInTime(t *testing.T) {
	old := hookGitTimeout
	if old != 3*time.Second {
		t.Fatalf("real hooks wait %v for git; this test expects the three seconds the hooks have", old)
	}
	hookGitTimeout = time.Nanosecond
	t.Cleanup(func() { hookGitTimeout = old })
	TestTheNoticeAndTheStopBlockAskForOneFlow(t)
}
