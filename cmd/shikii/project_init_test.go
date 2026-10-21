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
			t.Fatal("未初期化では案内も採点も差し込まない")
		}
	}
	if _, err := os.Lstat(filepath.Join(repo, ".shikii")); !os.IsNotExist(err) {
		t.Fatal("未初期化のプロジェクトに保存先を作らない")
	}
	after, _ := os.ReadFile(exclude)
	if string(after) != string(before) || len(registeredProjects(common)) != len(beforeProjects) || calls.Load() != 0 || len(spawned) != 0 {
		t.Fatal("未初期化ではGit・索引・モデルに副作用を起こさない")
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
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# 利用者の作法\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out := capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	for _, id := range hookSpecs {
		if _, err := spec.Load(filepath.Join(root, "decisions", id+".yaml")); err != nil {
			t.Fatalf("initだけで既製の4問が揃う: %v", err)
		}
	}
	marker, err := os.ReadFile(filepath.Join(root, "project.json"))
	if err != nil {
		t.Fatal("初期化した事実を保存する: ", err)
	}
	common, _ := agentHome()
	found := false
	for _, p := range registeredProjects(common) {
		found = found || p.Path == repo
	}
	if !found || !strings.Contains(out, "利用可能") || !strings.Contains(out, projectPageURL(root)) || !strings.Contains(out, "普段どおり") {
		t.Fatalf("initだけで画面の入口と次の行動まで分かる: %s", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "ready", "cwd": sub, "prompt": "README の導入手順を日本語で整えてください"})
	records, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if len(records) != 1 {
		t.Fatal("初期化後はサブフォルダでも記録が始まる")
	}
	path := filepath.Join(root, "decisions", "needs-plan.yaml")
	data, _ := os.ReadFile(path)
	data = append(data, []byte("\n# 利用者の設定を保持する\n")...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return cmdInit(nil) })
	after, _ := os.ReadFile(path)
	markerAfter, _ := os.ReadFile(filepath.Join(root, "project.json"))
	recordsAfter, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if string(after) != string(data) || string(markerAfter) != string(marker) || len(recordsAfter) != 1 {
		t.Fatal("再初期化で設定・会話・有効化の記録を壊さない")
	}
	if _, err := os.Stat(filepath.Join(sub, ".shikii")); !os.IsNotExist(err) {
		t.Fatal("サブフォルダへ別の置き場を作らない")
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("サブフォルダからでも既存のCLAUDE.mdを優先する")
	}
	hub := newProjectHub(common, serveOpts{})
	url := "/p/" + projectID(repo) + "/v1/status"
	beforeRemoval := httptest.NewRecorder()
	hub.ServeHTTP(beforeRemoval, httptest.NewRequest("GET", url, nil))
	if beforeRemoval.Code != 200 {
		t.Fatal("初期化済みのプロジェクトは画面から開ける")
	}
	if err := os.Remove(filepath.Join(root, "project.json")); err != nil {
		t.Fatal(err)
	}
	afterRemoval := httptest.NewRecorder()
	hub.ServeHTTP(afterRemoval, httptest.NewRequest("GET", url, nil))
	if afterRemoval.Code != 404 {
		t.Fatal("有効化の印が無くなったらキャッシュ済みの画面からも開かない")
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
		t.Fatal("壊れた設定では初期化を失敗にする")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "failed", "cwd": repo, "prompt": "README の日本語化を進めてください"})
	root := filepath.Join(repo, ".shikii")
	records, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if len(records) != 0 {
		t.Fatal("途中で失敗した初期化を有効扱いしない")
	}
	common, _ := agentHome()
	for _, p := range registeredProjects(common) {
		if p.Path == repo {
			t.Fatal("失敗したプロジェクトを画面へ登録しない")
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
				t.Fatal("別の場所・リンク・壊れた印では有効にしない")
			}
			after, _ := os.ReadFile(marker)
			if string(after) != string(data) {
				t.Fatal("hookは初期化の印を修復しない")
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
		t.Fatalf("--initも同じ初期化へ進む: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".shikii", "project.json")); err != nil {
		t.Fatal("--initでプロジェクトが有効になる: ", err)
	}
}
