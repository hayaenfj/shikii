package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestHookSeparatesProjectStores(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	hookEnv(t, model)
	a, b := gitRepo(t), gitRepo(t)
	initTestProject(t, a)
	initTestProject(t, b)
	const session = "same-session"
	const request = "README のインストール手順を詳しく書き直して"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": a, "prompt": request})
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": a, "last_assistant_message": "手順を直しました"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": a, "prompt": "モデルの起動手順が足りないので追記して"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": b, "prompt": request})
	for _, root := range []string{a, b} {
		recs, err := (store.Store{Dir: filepath.Join(root, ".shikii")}).Records("prompt-kind", 0)
		want := 1
		if root == a {
			want = 2
		}
		if err != nil || len(recs) != want {
			t.Fatalf("decisions must be saved in each project's .shikii: got=%d want=%d err=%v", len(recs), want, err)
		}
	}
	aStore, bStore := filepath.Join(a, ".shikii"), filepath.Join(b, ".shikii")
	if loadSession(bStore, session).Previous != "" {
		t.Fatal("must not hand over another work's replies even with the same session id")
	}
	if ctx, err := memoryContext(bStore, feedbackScope(b), request, &hookSession{}); err != nil || ctx != "" {
		t.Fatalf("must not return another work's conversation: %v", err)
	}
	if ctx, err := memoryContext(aStore, feedbackScope(a), request, &hookSession{}); err != nil || !strings.Contains(ctx, "起動手順") {
		t.Fatalf("the original work can refer to its conversation: %v", err)
	}
	status, err := exec.Command("git", "-C", a, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil || strings.Contains(string(status), ".shikii/") {
		t.Fatal("conversations must not be picked up by git add -A")
	}
	before := treeSnapshot(a)
	// Internal files a person deliberately tracked are not counted in the outcome decisions either.
	if out, err := exec.Command("git", "-C", a, "add", "-f", ".shikii/decisions/needs-plan.yaml").CombinedOutput(); err != nil {
		t.Fatalf("synthetic tracking setup: %v %s", err, out)
	}
	path := filepath.Join(aStore, "decisions", "needs-plan.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\n# 内部の控え\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if after := treeSnapshot(a); before == "" || after != before {
		t.Fatal("a tracked .shikii must not count as a change of the work either")
	}
	sub := filepath.Join(a, "docs")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	root, scope, err := currentProjectStore()
	if err != nil || root != aStore || scope != feedbackScope(a) {
		t.Fatal("the CLI in a subfolder must also use the project root's store")
	}
	if q := notebookCommand(t, "query", ""); len(q["pending_sources"].([]any)) != 1 {
		t.Fatal("the CLI's search must also reach the same work's conversations")
	}
	global, _ := agentHome()
	if recs, _ := (store.Store{Dir: global}).Records("prompt-kind", 0); len(recs) != 0 {
		t.Fatal("must not save decisions on the shared side")
	}
	// Initialization alone must not add outcome labels for file changes.
	if labels, err := (store.Store{Dir: aStore}).LabeledTraces("prompt-kind"); err != nil {
		t.Fatal(err)
	} else {
		for _, label := range labels {
			if label.Truth == "edit" {
				t.Fatal("shikii's own saves must not count as changes of the work")
			}
		}
	}

	if _, err := os.Stat(filepath.Join(aStore, "decisions", "needs-plan.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestProjectHubKeepsReadsAndWritesWithinTheSelectedProject(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	a := gitRepo(t)
	aStore := hookEnv(t, model, a)
	b := filepath.Join(t.TempDir(), filepath.Base(a)) // another project with the same name
	if err := os.Mkdir(b, 0700); err != nil {
		t.Fatal(err)
	}
	bStore, bScope := initTestProject(t, b)
	aScope := feedbackScope(a)
	id, trace := reviewSeed(t, store.Store{Dir: aStore}, aScope, 1)
	if _, err := runNotebook(aStore, aScope, notebookRequest{Operation: "ingest", Draft: notebook.Draft{Sources: []string{id}, Title: "起動手順", Lesson: "モデルを起動する手順を書く", Kind: "rule", Scene: "テストの時", AppliesTo: "README の導入手順"}}); err != nil {
		t.Fatal(err)
	}
	common, _ := agentHome()
	h := newProjectHub(common, serveOpts{})
	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code, rec.Body.String()
	}
	aURL, bURL := "/p/"+projectID(aScope)+"/", "/p/"+projectID(bScope)+"/"
	if code, body := get("/v1/status"); code != 200 || !strings.Contains(body, aURL) || !strings.Contains(body, bURL) || strings.Contains(body, "起動手順") {
		t.Fatal("the entry page must show only each project's name and link")
	}
	for _, path := range []string{"/v1/queue?spec=done-as-asked", "/p/unknown/v1/status"} {
		if code, _ := get(path); code != 404 {
			t.Fatalf("records must not open from the shared side or an unregistered work: %s code=%d", path, code)
		}
	}
	for _, prefix := range []string{aURL, bURL} {
		req := httptest.NewRequest("POST", prefix+"v1/review", strings.NewReader(`{"operation":"query"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), trace) != (prefix == aURL) {
			t.Fatal("scoring sources must be read only from the chosen work")
		}
		req = httptest.NewRequest("POST", prefix+"v1/wiki/notebook", strings.NewReader(`{"operation":"query","sources":["`+id+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if (rec.Code == 200) != (prefix == aURL) {
			t.Fatal("a lesson's original text must be read only from the chosen work")
		}

		code, body := get(prefix + "v1/wiki")
		if code != 200 || strings.Contains(body, "モデルを起動する手順を書く") != (prefix == aURL) {
			t.Fatal("must read only the chosen work's lessons")
		}
		code, body = get(prefix + "v1/status")
		if code != 200 || strings.Contains(body, `"records": 1`) != (prefix == aURL) {
			t.Fatal("scoring and line results must be read per project")
		}
	}
	// Even with the same trace, scores and truths cannot be written from another work.
	for _, endpoint := range []string{"review", "label"} {
		body := `{"operation":"submit","source_id":"` + id + `","trace_id":"` + trace + `","truth":"no","reason":"起動手順が不足"}`
		if endpoint == "label" {
			body = `{"trace_id":"` + trace + `","truth":"no","source":"ui"}`
		}
		req := httptest.NewRequest("POST", bURL+"v1/"+endpoint, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code < 400 {
			t.Fatalf("must refuse writes into another work: %s", endpoint)
		}
	}
	if _, err := runReview(aStore, aScope, reviewRequest{Operation: "submit", SourceID: id, TraceID: trace, Truth: "no", Reason: "起動手順が不足"}); err != nil {
		t.Fatal(err)
	}
	q, err := reviewQuery(store.Store{Dir: bStore}, bScope)
	if err != nil || q["quality"].(map[string]store.ReviewStats)["done-as-asked"].Scored != 0 {
		t.Fatal("another work's scores must not count toward the trust record")
	}
	// After the store is swapped for a shared location, even an existing handler does not open it.
	if err := os.RemoveAll(filepath.Join(bStore, "records")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(aStore, "records"), filepath.Join(bStore, "records")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := projectStoreAt(b); err == nil {
		t.Fatal("the CLI must not use a link to a shared location either")
	}
	if code, _ := get(bURL + "v1/status"); code != 404 {
		t.Fatal("swapping the store for a link must not allow reading another work's records")
	}
}

// Without the AGENTS.md section, Codex without hooks found shikii's habits only when the skill was in ~/.agents/skills (agents-md-check, 0.9.2),
// and one OpenCode run recorded its own judgments as truths because the MCP instructions lacked the safeguard the section had.
func TestRulesReachAgentsWithoutTheAgentsSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	capture(t, func() error { return cmdInit(nil) })
	for _, d := range []string{filepath.Join(home, ".claude", "skills", "shikii"), filepath.Join(home, ".agents", "skills", "shikii")} {
		if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err != nil {
			t.Errorf("init puts the skill at %s: %v", d, err)
		}
	}
	for _, want := range []string{"never call shikii_label with your own judgment", "rule, test or procedure"} {
		if !strings.Contains(mcpInstructions, want) {
			t.Errorf("the MCP instructions carry %q: %s", want, mcpInstructions)
		}
	}
	if i, j := strings.Index(mcpInstructions, "never call shikii_label"), strings.Index(mcpInstructions, "shikii_decide"); i < 0 || i > j {
		t.Errorf("the safeguard comes first, before the tools: %s", mcpInstructions)
	}
}

// 0.9.7 re-audit 1: after a reconnection failed inside finishProjectInit (the entry written, project.json not), the next request reconnects (C8).
// The failure is made by making .shikii unwritable (registerProject writes under ~/.shikii and passes; only project.json's temp file fails).
func TestRe1RelinkFailingAtProjectJSONRecoversNextTime(t *testing.T) {
	for _, mode := range []string{"fail-then-retry", "crash-after-register"} {
		t.Run(mode, func(t *testing.T) {
			p := t.TempDir()
			hookEnv(t, nil, p)
			p = feedbackScope(p)
			r := filepath.Join(t.TempDir(), "renamed")
			if err := os.Rename(p, r); err != nil {
				t.Fatal(err)
			}
			r = feedbackScope(r)
			root := filepath.Join(r, ".shikii")
			if mode == "fail-then-retry" {
				os.Chmod(root, 0o500)
				first := relinkMovedProject(r, false)
				os.Chmod(root, 0o700)
				if first != "" {
					t.Fatalf("\u524d\u63d0: 1 \u56de\u76ee\u306f\u5931\u6557\u3059\u308b\u306f\u305a: %q", first)
				}
			} else {
				// stopped after the first step of finishProjectInit (registerProject)
				if err := registerProject(r); err != nil {
					t.Fatal(err)
				}
			}
			second := relinkMovedProject(r, false)
			if act, _ := projectActive(root, r); !act {
				t.Fatalf("C8: \u9014\u4e2d\u3067\u5931\u6557\u3057\u305f\u3064\u306a\u304e\u76f4\u3057\u304c\u3001\u6b21\u306e\u4f9d\u983c\u3067\u63c3\u308f\u306a\u3044: %q; \u77e5\u3089\u305b: %q", second, copiedNotice(r))
			}
		})
	}
}

// 0.9.7 re-audit 1: on a case-insensitive disk (the macOS default), two sessions open the same folder by paths that differ only in case.
// Nothing was moved, yet every request reconnected it as moved and stopped the other path's page (C1, once).
func TestRe1CaseOnlyDifferentCwdFlipFlops(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "Proj")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	lower := filepath.Join(base, "proj")
	if _, err := os.Stat(lower); err != nil {
		t.Skip("\u5927\u6587\u5b57\u5c0f\u6587\u5b57\u3092\u533a\u5225\u3059\u308b\u30c7\u30a3\u30b9\u30af")
	}
	hookEnv(t, nil, p)
	told := 0
	for i := 0; i < 4; i++ {
		cwd := p
		if i%2 == 1 {
			cwd = lower
		}
		if n := relinkMovedProject(cwd, false); n != "" {
			told++
			t.Logf("%d \u56de\u76ee(%s): %.80s", i, filepath.Base(cwd), n)
		}
	}
	if told > 0 {
		t.Fatalf("C1: \u79fb\u3057\u3066\u3044\u306a\u3044\u306e\u306b\u3001\u5927\u6587\u5b57\u5c0f\u6587\u5b57\u3060\u3051\u9055\u3046\u30d1\u30b9\u306e\u4f9d\u983c\u3067 %d \u56de\u300c\u79fb\u3057\u305f\u300d\u3068\u3064\u306a\u304e\u76f4\u3057\u305f", told)
	}
}

// The compatibility cleanup of 0.9.7 (2026-10-27): what released versions wrote as data is converted once, when shikii is updated; an
// older shikii running next to this one, or a setting in an older version's form, is only told to the person with the update to make.

// project096 is a project at its first path as 0.9.6 left it (no store id or name, a path-named index entry). Nothing is moved yet.
func project096(t *testing.T) (common, old string) {
	t.Helper()
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	old = feedbackScope(repo)
	root := filepath.Join(old, ".shikii")
	o, err := (notebook.Book{Root: root, Scope: old}).Capture(notebook.Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (notebook.Book{Root: root, Scope: old}).Ingest(notebook.Draft{Title: "a page", Lesson: "Check.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
	common, _ = agentHome()
	asIf096(t, common, old, old)
	return common, old
}

// G1: the update writes the store id, the store folder and the store name for every project 0.9.6 registered, so a move after the update
// is reconnected at the next request (final audit BC-2 is gone). The path-named entry is replaced by the store id's.
func TestUpdateConvertsEveryProjectThe096IndexHas(t *testing.T) {
	common, old := project096(t)
	t.Chdir(t.TempDir())
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	root := filepath.Join(old, ".shikii")
	id := storeID(root, false)
	if id == "" {
		t.Fatalf("G1: the update writes the store id of a project 0.9.6 registered\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(common, "projects", id+".json")); err != nil {
		t.Fatalf("G1: and its entry under the store id: %v", err)
	}
	if _, err := os.Stat(filepath.Join(common, "projects", projectID(old)+".json")); !os.IsNotExist(err) {
		t.Fatalf("G1: the path-named entry 0.9.6 wrote is replaced: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "records", "store-name")); err != nil || strings.TrimSpace(string(b)) != projectID(old) {
		t.Fatalf("G1: the store name is the one 0.9.6 used: %q %v", b, err)
	}
	moved := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	t.Chdir(moved)
	if note := relinkMovedProject(moved, false); !strings.Contains(note, "moved or renamed") {
		t.Fatalf("G1: a move after the update is reconnected by itself: %q", note)
	}
	if live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live(); err != nil || len(live) != 1 {
		t.Fatalf("G1: with the wiki 0.9.6 wrote: %d pages %v", len(live), err)
	}
}

// A .shikii this machine did not initialize at the old path (brought in from an archive or another machine) is never switched on by itself:
// its marker names a path this machine's index does not have, or the index has that path for another store. It stays inactive and its
// marker is not repaired; only the person's shikii --init activates it.
func TestStoreFromElsewhereIsNotReconnectedByItself(t *testing.T) {
	for _, kind := range []string{"path not in the index", "another store at that path"} {
		t.Run(kind, func(t *testing.T) {
			_, old, moved := movedProject(t)
			root := filepath.Join(moved, ".shikii")
			marker := filepath.Join(root, "project.json")
			switch kind {
			case "path not in the index": // a store from another machine: its own id, naming a path this machine never had
				data, _ := os.ReadFile(marker)
				data = []byte(strings.Replace(string(data), old, filepath.Join(t.TempDir(), "from-another-machine"), 1))
				if err := os.WriteFile(marker, data, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "records", "store-id"), []byte("fedcba9876543210fedcba9876543210\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "another store at that path":
				if err := os.WriteFile(filepath.Join(root, "records", "store-id"), []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(marker)
			if notice := relinkMovedProject(moved, false); notice != "" {
				t.Fatalf("not reconnected: %q", notice)
			}
			if after, _ := os.ReadFile(marker); string(after) != string(before) {
				t.Fatal("the marker is not repaired")
			}
			if active, _ := projectActive(root, moved); active {
				t.Fatal("it stays inactive")
			}
		})
	}
}

// finding 4: on a disk without hard links PlaceOnce makes the file with O_EXCL and then writes it. Another process that read the empty file in
// between took it for an id an earlier version left empty, removed it and put its own: two processes held two different ids. A file is
// made again only when it is old enough to be left over; a file being written is not replaced.
func TestPatch099AuditStoreIDBeingWrittenIsNotReplaced(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { store.Link = os.Link })
	root := filepath.Join(t.TempDir(), ".shikii")
	os.MkdirAll(filepath.Join(root, "records"), 0o700)
	path := filepath.Join(root, "records", "store-id")
	a, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // A: made, not yet written
	if err != nil {
		t.Fatal(err)
	}
	aID := strings.Repeat("a", 32)
	bID := storeID(root, true) // B, in between
	a.WriteString(aID + "\n")
	a.Close()
	if got := storeID(root, false); got != aID || (bID != "" && bID != aID) {
		t.Fatalf("the first writer wins: A=%s B=%s file=%s", aID, bID, got)
	}
}

// confirmation u-2: a valid store-id that could not be opened for a moment (mode 000) was taken for an invalid one and replaced.
func TestPatch099ConfirmUnreadableValidStoreIDIsKept(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	os.MkdirAll(filepath.Join(root, "records"), 0o700)
	path := filepath.Join(root, "records", "store-id")
	id := strings.Repeat("b", 32)
	os.WriteFile(path, []byte(id+"\n"), 0o600)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(path, old, old)
	os.Chmod(path, 0)
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	storeID(root, true)
	os.Chmod(path, 0o600)
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != id {
		t.Fatalf("a valid id that could not be read for a moment is kept: %q", b)
	}
}

// confirmation u-3: an empty id whose time is in the future (copied from another machine, a clock set back) was never made again.
func TestPatch099ConfirmFutureEmptyStoreIDIsMadeAgain(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	os.MkdirAll(filepath.Join(root, "records"), 0o700)
	path := filepath.Join(root, "records", "store-id")
	os.WriteFile(path, nil, 0o600)
	future := time.Now().Add(24 * time.Hour)
	os.Chtimes(path, future, future)
	if id := storeID(root, true); id == "" {
		t.Fatal("an empty id with a future time is a leftover: it is made again")
	}
}

// C confirm1-2: records/store-id was made with O_EXCL and then written; a stop in between left an empty file that was never made again
// (read gave "" and O_EXCL failed), so the store kept its path-named entry and a later move was not reconnected.
func TestPatch099EmptyStoreIDIsMadeAgain(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	if err := os.MkdirAll(filepath.Join(root, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "records", "store-id"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if storeID(root, true) != "" {
		t.Fatal("an empty id just made is another process's, being written: it is not replaced")
	}
	old := time.Now().Add(-time.Minute) // left by a stop in an earlier version
	if err := os.Chtimes(filepath.Join(root, "records", "store-id"), old, old); err != nil {
		t.Fatal(err)
	}
	id := storeID(root, true)
	if b, _ := os.ReadFile(filepath.Join(root, "records", "store-id")); id == "" || strings.TrimSpace(string(b)) != id {
		t.Fatalf("an empty store-id is made again: id=%q, file=%q", id, b)
	}
	if again := storeID(root, true); again != id {
		t.Fatalf("and then kept: %q, then %q", id, again)
	}
}

// B DF-1, DF-2 (expected fixed by 37424df, checked here): the hook and serve spelled the project path differently when it was reached
// through a link, so the hook did not find the page (and restarted serve every 30 seconds) and uninstall did not stop it. Both now key by the
// same spelling: the hook's scope, the scope serve keys its page by, and the store serve's command line names are one string.
func TestPatch099HookAndServeSpellThePageTheSameWay(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	real := gitRepo(t)
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	if err := cmdInit(nil); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{link, real, filepath.Join(link, ".")} {
		hook := projectScopeAt(cwd)
		root := filepath.Join(hook, ".shikii") // what ensureProjectPage passes as serve --store
		stores := []string{root, filepath.Join(link, ".shikii"), filepath.Join(real, ".shikii")}
		if upper := strings.ToUpper(real); upper != real {
			if a, err := os.Stat(real); err == nil {
				if b, err := os.Stat(upper); err == nil && os.SameFile(a, b) { // a case-insensitive disk
					stores = append(stores, filepath.Join(upper, ".shikii"))
				}
			}
		}
		for _, store := range stores {
			if got := pageScope(store); got != hook {
				t.Fatalf("DF-1: serve --store %s keys its page by %q, the hook (cwd %s) by %q", store, got, cwd, hook)
			}
		}
		if !isShikiiServe("/bin/shikii serve --store "+root+" --port 0 --local x", filepath.Join(pageScope(root), ".shikii")) {
			t.Fatalf("DF-2: uninstall finds the page the hook started from %s", cwd)
		}
	}
}

func TestProjectHubRedirectKeepsQuery(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	common, _ := agentHome()
	hub := newProjectHub(common, serveOpts{})
	path := "/p/" + projectID(feedbackScope(repo))
	query := "spec=done-as-asked&trace=0000000000000001"
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, httptest.NewRequest("GET", path+"?"+query, nil))
	if rec.Code != 307 || rec.Header().Get("Location") != path+"/?"+query {
		t.Fatalf("the trailing / redirect must keep the record being opened: status=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}
}

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

// The reflection and the wiki listing show a pending source by its head only (title, reference, version, length and the first
// notebook.SourceHeadBytes of the body); the whole body is read with wiki query <source id>. A short source is shown whole.
func TestPendingSourcesAreListedByTheirHead(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	root, scope, err := currentProjectStore()
	if err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	long := "x" + strings.Repeat("\u3042\u3044\u3046\u3048\u304a", 2000) + "END-OF-LONG" // 30 KB, multi-byte and off the 3-byte grid
	big, err := book.Capture(notebook.Observation{Kind: "agent", Title: "long", Content: long, Reference: "memory/long.md", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	small, err := book.Capture(notebook.Observation{Kind: "test", Title: "short", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	check := func(where string, list []notebook.ObservationHead) {
		t.Helper()
		heads := map[string]notebook.ObservationHead{}
		for _, h := range list {
			heads[h.ID] = h
		}
		h := heads[big.ID]
		if !h.ContentCut || h.ContentBytes != len(long) || len(h.Content) > notebook.SourceHeadBytes || len(h.Content) < notebook.SourceHeadBytes-3 || !strings.HasPrefix(long, h.Content) || !utf8.ValidString(h.Content) || h.Content == "" || h.Title != "long" || h.Reference != "memory/long.md" || h.Version != "v1" {
			t.Fatalf("%s: a long source is listed by its head (cut=%v bytes=%d head=%d)", where, h.ContentCut, h.ContentBytes, len(h.Content))
		}
		if s := heads[small.ID]; s.ContentCut || s.Content != "PASS" || s.ContentBytes != 4 {
			t.Fatalf("%s: a short source is listed whole: %+v", where, s)
		}
	}
	v, err := reflectQuery(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	check("reflect query", v.Observations)
	w, err := book.QueryPage("", "")
	if err != nil {
		t.Fatal(err)
	}
	check("wiki query", w.Observations)
	got, err := book.ReadSources([]string{big.ID})
	if obs, _ := got["observations"].([]notebook.Observation); err != nil || len(obs) != 1 || obs[0].Content != long {
		t.Fatalf("wiki query <source id> still returns the whole body: %v", err)
	}
}
