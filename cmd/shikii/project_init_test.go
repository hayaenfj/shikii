package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestUninitializedHooksLeaveProjectUntouched(t *testing.T) {
	var calls atomic.Int32
	model := fakeAgentModel(t, func(string) float64 { calls.Add(1); return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	common, _ := agentHome()
	beforeProjects := registeredProjects(common)
	exclude := filepath.Join(repo, ".git", "info", "exclude")
	before, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"user-prompt-submit", "stop"} {
		out := runHook(t, event, map[string]any{"session_id": "inactive", "cwd": repo, "prompt": "README を更新してください", "last_assistant_message": "更新しました"})
		if out != "" {
			t.Fatal("without initialization, neither guidance nor scoring is injected")
		}
	}
	if _, err := os.Lstat(filepath.Join(repo, ".shikii")); !os.IsNotExist(err) {
		t.Fatal("must not create a store in an uninitialized project")
	}
	after, _ := os.ReadFile(exclude)
	if string(after) != string(before) || len(registeredProjects(common)) != len(beforeProjects) || calls.Load() != 0 || len(spawned) != 0 {
		t.Fatal("without initialization, there must be no side effects on Git, the index or the model")
	}
}

func TestExplicitInitMakesProjectReadyAndPreservesData(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	sub := filepath.Join(repo, "docs")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# the user's rules\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out := capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	for _, id := range hookSpecs {
		if _, err := spec.Load(filepath.Join(root, "decisions", id+".yaml")); err != nil {
			t.Fatalf("init alone must set up the 4 built-in questions: %v", err)
		}
	}
	marker, err := os.ReadFile(filepath.Join(root, "project.json"))
	if err != nil {
		t.Fatal("must save the fact that it was initialized: ", err)
	}
	common, _ := agentHome()
	found := false
	for _, p := range registeredProjects(common) {
		found = found || p.Path == repo
	}
	if !found || !strings.Contains(out, "Ready") || !strings.Contains(out, projectPageURL(root)) || !strings.Contains(out, "as usual") {
		t.Fatalf("init alone must show the entry to the page and the next step: %s", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "ready", "cwd": sub, "prompt": "README の導入手順を日本語で整えてください"})
	records, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if len(records) != 1 {
		t.Fatal("after initialization, recording starts in a subfolder too")
	}
	path := filepath.Join(root, "decisions", "needs-plan.yaml")
	data, _ := os.ReadFile(path)
	data = append(data, []byte("\n# keep the user's setting\n")...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return cmdInit(nil) })
	after, _ := os.ReadFile(path)
	markerAfter, _ := os.ReadFile(filepath.Join(root, "project.json"))
	recordsAfter, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if string(after) != string(data) || string(markerAfter) != string(marker) || len(recordsAfter) != 1 {
		t.Fatal("re-initialization must not break settings, conversations or the activation record")
	}
	if _, err := os.Stat(filepath.Join(sub, ".shikii")); !os.IsNotExist(err) {
		t.Fatal("must not create another store in a subfolder")
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("the existing CLAUDE.md takes priority even from a subfolder")
	}
	hub := newProjectHub(common, serveOpts{})
	url := "/p/" + projectID(repo) + "/v1/status"
	beforeRemoval := httptest.NewRecorder()
	hub.ServeHTTP(beforeRemoval, httptest.NewRequest("GET", url, nil))
	if beforeRemoval.Code != 200 {
		t.Fatal("an initialized project can be opened from the page")
	}
	if err := os.Remove(filepath.Join(root, "project.json")); err != nil {
		t.Fatal(err)
	}
	afterRemoval := httptest.NewRecorder()
	hub.ServeHTTP(afterRemoval, httptest.NewRequest("GET", url, nil))
	if afterRemoval.Code != 404 {
		t.Fatal("once the activation mark is gone, it must not open even from a cached page")
	}
}

func TestFailedInitDoesNotEnableHooks(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	if err := os.WriteFile(".mcp.json", []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	oldOut := stdout
	stdout = &strings.Builder{}
	err := cmdInit(nil)
	stdout = oldOut
	if err == nil {
		t.Fatal("initialization must fail with broken settings")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "failed", "cwd": repo, "prompt": "README の日本語化を進めてください"})
	root := filepath.Join(repo, ".shikii")
	records, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if len(records) != 0 {
		t.Fatal("an initialization that failed midway must not count as active")
	}
	common, _ := agentHome()
	for _, p := range registeredProjects(common) {
		if p.Path == repo {
			t.Fatal("a failed project must not be registered with the page")
		}
	}
}

func TestProjectActivationRejectsForeignAndLinkedMarkers(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	for _, kind := range []string{"foreign", "link", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			repo := gitRepo(t)
			root := filepath.Join(repo, ".shikii")
			if err := checkProjectStore(root, true); err != nil {
				t.Fatal(err)
			}
			if _, err := seedAgentSpecs(filepath.Join(root, "decisions")); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, "project.json")
			data, _ := json.Marshal(map[string]any{"version": 1, "scope": repo})
			switch kind {
			case "foreign":
				data, _ = json.Marshal(map[string]any{"version": 1, "scope": filepath.Dir(repo)})
			case "malformed":
				data = []byte("{broken")
			}
			if kind == "link" {
				outside := filepath.Join(root, "marker-copy.json")
				if err := os.WriteFile(outside, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(outside), marker); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(marker, data, 0600); err != nil {
				t.Fatal(err)
			}
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "bad-marker", "cwd": repo, "prompt": "README の導入手順を整えてください"})
			records, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
			if len(records) != 0 {
				t.Fatal("must not activate from another location, a link, or a broken mark")
			}
			after, _ := os.ReadFile(marker)
			if string(after) != string(data) {
				t.Fatal("the hook must not repair the initialization mark")
			}
		})
	}
}

func TestInitAliasUsesTheSameSetup(t *testing.T) {
	if os.Getenv("SHIKII_TEST_INIT_ALIAS") == "1" {
		os.Args = []string{"shikii", "--init"}
		main()
		return
	}
	dir, home := t.TempDir(), t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitAliasUsesTheSameSetup$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SHIKII_TEST_INIT_ALIAS=1", "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--init must lead to the same initialization: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".shikii", "project.json")); err != nil {
		t.Fatal("--init must activate the project: ", err)
	}
}
