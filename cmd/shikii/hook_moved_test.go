package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.2: after the folder was renamed, a wiki store left under the old path's name made the wiki silently empty.
// The hook before a request tells the agent about the leftover store once per session.
func TestHookTellsWikiLeftUnderOldPath(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	// a store written before 0.9.7 under the old path's name, next to the current one (half copied by hand): a single old store is adopted
	// by itself, so only a leftover beside the current store is told
	if _, err := (notebook.Book{Root: agent, Scope: feedbackScope(repo)}).Capture(notebook.Observation{Kind: "test", Title: "now", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(2, 0)}); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if _, err := (notebook.Book{Root: elsewhere, Scope: "/work/old-name"}).Capture(notebook.Observation{Kind: "test", Title: "確かめた", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("/work/old-name"))
	place := "records/wiki-sources/" + hex.EncodeToString(sum[:])
	if err := os.Rename(filepath.Join(elsewhere, "records", "wiki-sources", store.StoreName(elsewhere, "/work/old-name")), filepath.Join(agent, place)); err != nil {
		t.Fatal(err)
	}
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	first := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	if !strings.Contains(first, "wiki store under another name") || !strings.Contains(first, place) {
		t.Fatalf("tells about the leftover store: %q", first)
	}
	prompt["prompt"] = "次は CHANGELOG の誤字を直してください。"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "under the previous path's name") {
		t.Fatalf("only once in the same session: %q", again)
	}
}

// 0.9.7 audit B-1: cp -R A B, then mv A C. B carries the same marker and store id as the original. If B's hook ran first, B took the
// registration and the real project C stayed stopped without a word. Only the store that was registered (the same folder, moved) is
// reconnected; the copy is told that shikii cannot tell, and stays inactive.
func TestAuditCopyThenMoveOriginal(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	original := feedbackScope(repo)
	base := t.TempDir()
	copied := filepath.Join(base, "copy")
	if out, err := exec.Command("cp", "-R", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	moved := filepath.Join(base, "moved")
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	copied, moved = feedbackScope(copied), feedbackScope(moved)
	if n := relinkMovedProject(copied, false); n != "" {
		t.Fatalf("the copy is not reconnected as the project: %q", n)
	}
	if n := copiedNotice(copied); !strings.Contains(n, "shikii --init") {
		t.Fatalf("the copy is told how to make it a project of its own: %q", n)
	}
	if n := relinkMovedProject(moved, false); !strings.Contains(n, "moved or renamed") {
		t.Fatalf("the real project is reconnected: %q", n)
	}
	if active, _ := projectActive(filepath.Join(moved, ".shikii"), moved); !active {
		t.Fatal("the real project is active")
	}
	if active, _ := projectActive(filepath.Join(copied, ".shikii"), copied); active {
		t.Fatal("the copy stays inactive")
	}
	// shikii --init in the copy makes it a project of its own, with its own store id (so it never matches the original's entry)
	t.Chdir(copied)
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "a project of its own") {
		t.Fatalf("init says the copy became a project of its own: %s", out)
	}
	if a, b := storeID(filepath.Join(copied, ".shikii"), false), storeID(filepath.Join(moved, ".shikii"), false); a == "" || a == b {
		t.Fatalf("the copy has its own store id: %q %q", a, b)
	}
	if active, _ := projectActive(filepath.Join(moved, ".shikii"), moved); !active {
		t.Fatal("the original stays active")
	}
}

// 0.9.7 audit B-2: an index entry written before 0.9.7 has no store id. A .shikii brought in from elsewhere without a store id, naming the
// same path, matched it ("" equals "") and switched itself on. An entry without a store id never reconnects by itself.
func TestAuditForeignLegacyStoreActivatesItself(t *testing.T) {
	_, old, moved := movedProject(t)
	common, _ := agentHome()
	// the index as 0.9.6 left it: the entry named by the path, with no store id
	own := filepath.Join(common, "projects", storeID(filepath.Join(moved, ".shikii"), false)+".json")
	data, err := os.ReadFile(own)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(own)
	entry := filepath.Join(common, "projects", projectID(old)+".json")
	var p map[string]any
	_ = json.Unmarshal(data, &p)
	delete(p, "store_id") // the shape 0.9.6 wrote
	delete(p, "store_dir")
	data, _ = json.Marshal(p)
	if err := os.WriteFile(entry, data, 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := feedbackScope(gitRepo(t)) // a 0.9.6 .shikii from another machine, naming the same path
	root := filepath.Join(foreign, ".shikii")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(projectActivation{Version: 2, Scope: old})
	if err := os.WriteFile(filepath.Join(root, "project.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	notice := relinkMovedProject(foreign, false)
	if active, _ := projectActive(root, foreign); active || notice != "" {
		t.Fatalf("a .shikii brought in switched itself on without shikii --init: notice=%q active=%v", notice, active)
	}
	if n := copiedNotice(foreign); !strings.Contains(n, "cannot confirm") || !strings.Contains(n, "shikii --init") {
		t.Fatalf("the agent is told that shikii is not running and why: %q", n)
	}
}

// Final audit BC-2 (C1, decided 2026-10-27): a store 0.9.6 initialized on this machine, moved before the update converted the index (G1
// covers a move after it). The index entry 0.9.6 wrote has no store id or store folder, so the move cannot be told from a copy whose
// original is gone: the hook does not take it over, and says shikii is not running there. The person's --init then keeps the wiki 0.9.6
// wrote.
func TestFinalBC2MovedBy096StoreIsNotReconnected(t *testing.T) {
	common, old, moved := movedProject(t)
	asIf096(t, common, old, moved)
	if note := relinkMovedProject(moved, false); note != "" {
		t.Fatalf("not taken over by itself: %q", note)
	}
	if notice := copiedNotice(moved); !strings.Contains(notice, "--init") {
		t.Fatalf("the person is told how to keep using it: %q", notice)
	}
	t.Chdir(moved)
	capture(t, func() error { return cmdInit(nil) })
	if active, _ := projectActive(filepath.Join(moved, ".shikii"), moved); !active {
		t.Fatal("--init activates it")
	}
	if live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live(); err != nil || len(live) != 1 {
		t.Fatalf("and keeps the wiki 0.9.6 wrote: %d pages %v", len(live), err)
	}
}

// C1 "say it once": two hooks of the same moved folder at the same time (a prompt and a subagent, two agents, two sessions). Checked in
// one run instead of waiting for two hooks to race: right after this hook saw the old path and before it takes the lock, another hook
// reconnects the folder all the way; this hook then says nothing.
func TestFull1RelinkAfterAnotherHookReconnectedSaysNothing(t *testing.T) {
	R := full1Moved(t)
	other, ran := "", false
	old := beforeRelinkLock
	t.Cleanup(func() { beforeRelinkLock = old })
	beforeRelinkLock = func() {
		if !ran {
			ran = true
			other = relinkMovedProject(R, false)
		}
	}
	this := relinkMovedProject(R, false)
	if !strings.HasPrefix(other, "shikii:") {
		t.Fatalf("precondition: the other hook reconnects the moved folder: %q", other)
	}
	if this != "" {
		t.Fatalf("C1: the move was told twice; the second hook said %q", this)
	}
}

// The reconnection is written while its lock is held, so two hooks never both get past the second look at project.json: right after that
// look, the lock cannot be taken from outside.
func TestFull1RelinkWritesUnderTheLock(t *testing.T) {
	R := full1Moved(t)
	common, _ := agentHome()
	lock := filepath.Join(common, "relink-"+storeID(filepath.Join(R, ".shikii"), false)+".lock")
	tried := errors.New("whileRelinking did not run")
	old := whileRelinking
	t.Cleanup(func() { whileRelinking = old })
	whileRelinking = func() { tried = tryLockFromOutside(lock) }
	if n := relinkMovedProject(R, false); !strings.HasPrefix(n, "shikii:") {
		t.Fatalf("precondition: the moved folder is reconnected: %q", n)
	}
	if tried != syscall.EWOULDBLOCK {
		t.Fatalf("the reconnection lock is held while reconnecting: taking it from outside gave %v", tried)
	}
}

// full1Moved makes a project in a Git repository, moves its folder (mv) and returns the new path.
func full1Moved(t *testing.T) string {
	t.Helper()
	p := gitRepo(t)
	hookEnv(t, nil, p)
	r := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(feedbackScope(p), r); err != nil {
		t.Fatal(err)
	}
	return feedbackScope(r)
}

// Docs: "The next request's hook reconnects the project at its new path by itself". mv to another disk is a copy and a removal; the
// .shikii folder gets a new inode.
func TestFull1MoveToAnotherDiskIsLeftToThePerson(t *testing.T) {
	p := gitRepo(t)
	hookEnv(t, nil, p)
	common, _ := agentHome()
	P := feedbackScope(p)
	r := filepath.Join(t.TempDir(), "moved")
	full1Copy(t, P, r) // what mv does across disks
	if err := os.RemoveAll(P); err != nil {
		t.Fatal(err)
	}
	R := feedbackScope(r)
	// the hook cannot tell this from a copy whose original moved elsewhere (another folder), so it does not take it over by itself
	if n := relinkMovedProject(R, false); n != "" {
		t.Fatalf("a folder whose store folder changed is not reconnected by the hook: %q", n)
	}
	if n := copiedNotice(R); !strings.Contains(n, "cannot confirm") {
		t.Fatalf("the hook says why shikii is not running: %q", n)
	}
	// the person's --init: shikii cannot tell this from a copy of a moved original, so it becomes a project of its own (new id), its wiki
	// and records kept, and the old entry stays for the original (decided in 0.9.7 after the property test found a takeover)
	before := storeID(filepath.Join(R, ".shikii"), false)
	t.Chdir(R)
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "a project of its own") || storeID(filepath.Join(R, ".shikii"), false) == before {
		t.Fatalf("--init makes it a project of its own with a new id:\n%s", out)
	}
	if active, _ := projectActive(filepath.Join(R, ".shikii"), R); !active {
		t.Fatal("and it is active")
	}
	_ = common
}

// C1/C5 (two sessions send their first request in the moved folder at the same time; the move is told once) is checked in one run by
// TestFull1RelinkAfterAnotherHookReconnectedSaysNothing and TestFull1RelinkWritesUnderTheLock.

// controls: the same F2 scenario without the other session (the missed conversations still wait) gives the full text; the move told by one
// request alone is told once and a second request says nothing; after the concurrent relink the project is active at the new path only.
func TestAuditControls(t *testing.T) {
	t.Run("C5 sequential", func(t *testing.T) {
		_, _, moved := movedProject(t)
		a, b := relinkMovedProject(moved, false), relinkMovedProject(moved, false)
		if a == "" || b != "" {
			t.Fatalf("control: %q / %q", a, b)
		}
	})
	t.Run("C5 concurrent state", func(t *testing.T) {
		common, old, moved := movedProject(t)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); relinkMovedProject(moved, false) }()
		}
		wg.Wait()
		active, err := projectActive(moved+"/.shikii", moved)
		paths := registeredPaths(common)
		t.Logf("active=%v err=%v paths=%v old=%s", active, err, paths, old)
		for _, p := range paths {
			if p == old {
				t.Fatalf("the old path stays in the index: %v", paths)
			}
		}
	})
}

// 0.9.7 re-audit 1: two project folders swapped with three mv on the same disk (A to T, B to A, T to B).
// Both stores were initialized on this machine and only moved, so by C1 both reconnect on the next request.
func TestRe1SwapTwoProjects(t *testing.T) {
	for _, git := range []bool{true, false} {
		t.Run(map[bool]string{true: "git", false: "nogit"}[git], func(t *testing.T) {
			mk := func() string {
				if git {
					return gitRepo(t)
				}
				return t.TempDir()
			}
			a := mk()
			hookEnv(t, nil, a)
			b := mk()
			initTestProject(t, b)
			a, b = feedbackScope(a), feedbackScope(b)
			idA, idB := storeID(filepath.Join(a, ".shikii"), false), storeID(filepath.Join(b, ".shikii"), false)
			tmp := filepath.Join(filepath.Dir(a), "swap-tmp")
			for _, mv := range [][2]string{{a, tmp}, {b, a}, {tmp, b}} {
				if err := os.Rename(mv[0], mv[1]); err != nil {
					t.Fatal(err)
				}
			}
			noteA := relinkMovedProject(a, false)
			noteB := relinkMovedProject(b, false)
			actA, _ := projectActive(filepath.Join(a, ".shikii"), a)
			actB, _ := projectActive(filepath.Join(b, ".shikii"), b)
			t.Logf("A \u306e\u30d1\u30b9(\u5143\u306f B \u306e\u7f6e\u304d\u5834): %q active=%v / B \u306e\u30d1\u30b9(\u5143\u306f A \u306e\u7f6e\u304d\u5834): %q active=%v", noteA, actA, noteB, actB)
			if !actA || !actB {
				t.Fatalf("C1: \u5165\u308c\u66ff\u3048\u305f 2 \u3064\u306e\u7f6e\u304d\u5834\u306e\u4e21\u65b9\u304c\u3064\u306a\u304e\u76f4\u308b\u306f\u305a: A=%v B=%v; B \u306e\u30d1\u30b9\u3067\u306e\u77e5\u3089\u305b: %q", actA, actB, copiedNotice(b))
			}
			if storeID(filepath.Join(a, ".shikii"), false) != idB || storeID(filepath.Join(b, ".shikii"), false) != idA {
				t.Fatal("store-id \u306f mv \u3057\u305f\u7f6e\u304d\u5834\u306e\u307e\u307e")
			}
		})
	}
}

// 0.9.7 re-audit 1: a usual rename: proj moved to proj-old, another project proj-new moved to proj. When the new proj asks first,
// proj-old (the original proj, only moved) was taken for a copy and not reconnected.
func TestRe1RenameThenReusePath(t *testing.T) {
	p := t.TempDir()
	hookEnv(t, nil, p)
	q := t.TempDir()
	initTestProject(t, q)
	p, q = feedbackScope(p), feedbackScope(q)
	old := filepath.Join(filepath.Dir(p), "proj-old")
	if err := os.Rename(p, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(q, p); err != nil {
		t.Fatal(err)
	}
	old = feedbackScope(old)
	t.Logf("\u65b0\u3057\u3044 proj: %q", relinkMovedProject(p, false))
	t.Logf("proj-old: %q", relinkMovedProject(old, false))
	if act, _ := projectActive(filepath.Join(old, ".shikii"), old); !act {
		t.Fatalf("C1: mv \u3057\u305f\u3060\u3051\u306e proj-old \u304c\u3064\u306a\u304e\u76f4\u3089\u306a\u3044; \u77e5\u3089\u305b: %q", copiedNotice(old))
	}
}

// re3-5 (docs, redone with the index keyed by the store's id): an older shikii rewrote the old path's entry in the shape 0.9.6 writes (no
// store id). The store's own entry is named by its id and is untouched, so the hook still reconnects the move by itself, once; the old
// path's entry is not read (G2: an older shikii's entry is left as it is).
func TestRe3InitAfterOldShikiiRewroteTheEntry(t *testing.T) {
	guides := map[string]string{}
	for _, guide := range []string{"../../docs/guide.md", "../../docs/guide.ja.md"} {
		b, err := os.ReadFile(guide) // before movedProject changes the working folder
		if err != nil {
			t.Fatal(err)
		}
		guides[guide] = string(b)
	}
	common, old, moved := movedProject(t)
	legacy, _ := json.Marshal(map[string]any{"id": projectID(old), "name": filepath.Base(old), "path": old, "url": "/p/" + projectID(old) + "/"})
	if err := os.WriteFile(filepath.Join(common, "projects", projectID(old)+".json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	before := storeID(filepath.Join(moved, ".shikii"), false)
	if n := relinkMovedProject(moved, false); !strings.Contains(n, "moved or renamed") {
		t.Fatalf("the hook reconnects it: %q", n)
	}
	if n := relinkMovedProject(moved, false); n != "" {
		t.Fatalf("and says so once: %q", n)
	}
	if after := storeID(filepath.Join(moved, ".shikii"), false); after != before {
		t.Fatalf("the store keeps its id: %s -> %s", before, after)
	}
	for _, p := range registeredPaths(common) {
		if p == old {
			t.Fatalf("the old path's entry is not read: %v", registeredPaths(common))
		}
	}
	for guide, b := range guides {
		if strings.Contains(b, "`shikii --init` reconnects it") || strings.Contains(b, "`shikii --init` \u3067\u3064\u306a\u304e\u76f4\u305b\u307e\u3059") || strings.Contains(b, "fixed names") {
			t.Fatalf("%s describes the old design", guide)
		}
	}
}

// A copy (cp -r) leaves the original where it is, still a shikii project. The copy is not taken over: it stays inactive (records keep going to
// the original) and the agent is told how to make it a project of its own.
func TestCopiedProjectIsNotTakenOver(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	original := feedbackScope(repo)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	copied = feedbackScope(copied)
	t.Chdir(copied)
	notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": copied, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}))
	if !strings.Contains(notice, "copied from "+original) || !strings.Contains(notice, "shikii --init") {
		t.Fatalf("tells that this is a copy and how to make it a project of its own: %q", notice)
	}
	if active, _ := projectActive(filepath.Join(copied, ".shikii"), copied); active {
		t.Fatal("the copy is not taken over")
	}
	if active, _ := projectActive(filepath.Join(original, ".shikii"), original); !active {
		t.Fatal("the original stays a project")
	}
}

// The page shikii started for the old path is stopped when the project is reconnected (a page for a folder that is gone would stay as an
// orphan), and only that page: a process whose command line is not that serve is never stopped. No real signal is sent (stopProcess records).
func TestReconnectingStopsOnlyTheOldPathsPage(t *testing.T) {
	common, old, moved := movedProject(t)
	page := os.Getppid() // a live pid stands in for the page; its command line comes from the table
	oldCommand, oldStop := processCommand, stopProcess
	t.Cleanup(func() { processCommand, stopProcess = oldCommand, oldStop })
	serve := "/usr/local/bin/shikii serve --store " + filepath.Join(old, ".shikii") + " --port 0"
	commands := map[int]string{page: serve}
	processCommand = func(pid int) string { return commands[pid] }
	var stopped []int
	stopProcess = func(pid int) error { stopped = append(stopped, pid); return nil }
	if err := writePageRecord(common, old, 1, page); err != nil {
		t.Fatal(err)
	}
	relinkMovedProject(moved, false)
	if len(stopped) != 1 || stopped[0] != page {
		t.Fatalf("the old path's page is stopped: %v", stopped)
	}
	if _, ok := readPageRecord(common, old); ok {
		t.Fatal("the old path's page record is removed")
	}
	// negative example: the recorded pid now runs something else (pid reuse), so it is not stopped
	stopped = nil
	commands[page] = "/bin/sleep 30"
	if err := writePageRecord(common, old, 1, page); err != nil {
		t.Fatal(err)
	}
	forgetProject(old)
	if len(stopped) != 0 {
		t.Fatalf("a process that is not the old path's page is never stopped: %v", stopped)
	}
}

// The conversations, memory deliveries and memory reviews written before 0.9.7 under the old path's name move with the project when it is
// reconnected, even when an older file from a still earlier path sits beside them (so "the only old file" cannot tell which is this project's).
func TestReconnectingKeepsConversationsWrittenBefore097(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	old := feedbackScope(repo)
	dir := filepath.Join(old, ".shikii", "records", "feedback")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"id":"0000000000000abc","request":"fix the readme","summary":"fixed","reply":"thanks","at":"2026-10-01T00:00:00Z"}` + "\n"
	for _, scope := range []string{old, "/an/even/older/path"} {
		if err := os.WriteFile(filepath.Join(dir, projectID(scope)+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	moved := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	if relinkMovedProject(moved, false) == "" {
		t.Fatal("precondition: reconnected")
	}
	if got, err := (store.Store{Dir: filepath.Join(moved, ".shikii")}).RecentFeedback(moved); err != nil || len(got) != 1 {
		t.Fatalf("the conversations written under the old path are kept: %+v %v", got, err)
	}
}

// 0.9.7 real-agent check: Codex told the person about the move only in a message during the turn, and neither Claude Code nor Codex told
// the person about a copy at all (the copy notice read as background). Both notices ask for the final reply; the copy notice comes once per
// session (the copy is not a project, so nothing is written into its store).
func TestMoveAndCopyNoticesReachThePerson(t *testing.T) {
	_, _, moved := movedProject(t)
	if n := relinkMovedProject(moved, false); !strings.Contains(n, "in your final reply") {
		t.Fatalf("the move notice asks for the final reply: %q", n)
	}
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", feedbackScope(repo), copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	copied = feedbackScope(copied)
	t.Chdir(copied)
	ask := func(session string) string {
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": copied, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}))
	}
	if n := ask("s1"); !strings.Contains(n, "copied from") || !strings.Contains(n, "in your final reply") {
		t.Fatalf("the copy notice asks for the final reply: %q", n)
	}
	if n := ask("s1"); n != "" {
		t.Fatalf("once per session: %q", n)
	}
	if n := ask("s2"); !strings.Contains(n, "copied from") {
		t.Fatalf("again in another session: %q", n)
	}
	if entries, _ := os.ReadDir(filepath.Join(copied, ".shikii", "sessions")); len(entries) != 0 {
		t.Fatalf("nothing is written into the copy's store: %d session files", len(entries))
	}
}

// 0.9.7 second real-agent check: after the copy notice, both agents asked the person again at the end of every later reply while no answer
// came. The notice asks once.
func TestCopyNoticeAsksOnce(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", feedbackScope(repo), copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	if n := copiedNotice(copied); !strings.Contains(n, "ask once") {
		t.Fatalf("the copy notice asks the person once: %q", n)
	}
}

// A reconnection that stopped after writing project.json and before the index (finishProjectInit) leaves the index at the path the folder
// had before, with that path's page still running. The next reconnection reads the old path from project.json, so only the index's
// path names that page: it is stopped too.
func TestReconnectingAfterAStoppedReconnectionStopsTheIndexPathsPage(t *testing.T) {
	common, old, moved := movedProject(t)
	page := os.Getppid()
	oldCommand, oldStop := processCommand, stopProcess
	t.Cleanup(func() { processCommand, stopProcess = oldCommand, oldStop })
	processCommand = func(pid int) string {
		return "/usr/local/bin/shikii serve --store " + filepath.Join(old, ".shikii") + " --port 0"
	}
	var stopped []int
	stopProcess = func(pid int) error { stopped = append(stopped, pid); return nil }
	if err := writePageRecord(common, old, 1, page); err != nil {
		t.Fatal(err)
	}
	// the stopped reconnection: project.json names the folder's middle path, the index still names old
	marker := filepath.Join(moved, ".shikii", "project.json")
	var a projectActivation
	b, err := os.ReadFile(marker)
	if err != nil || json.Unmarshal(b, &a) != nil {
		t.Fatal(err)
	}
	a.Scope = filepath.Join(t.TempDir(), "middle")
	if b, err = json.Marshal(a); err != nil || os.WriteFile(marker, b, 0o644) != nil {
		t.Fatal(err)
	}
	if notice := relinkMovedProject(moved, false); notice == "" {
		t.Fatal("the folder is reconnected")
	}
	if len(stopped) != 1 || stopped[0] != page {
		t.Fatalf("the index path's page is stopped: %v", stopped)
	}
	if _, ok := readPageRecord(common, old); ok {
		t.Fatal("the index path's page record is removed")
	}
}

// C BC-1: proj is moved to proj-old, another project is moved to proj and reconnected first, then a request comes at proj-old. The relink
// forgot proj and stopped the page recorded under proj, which now serves the other project. A path where a .shikii is again is someone's
// project: its page is left alone.
func TestPatch099ForgettingAPathDoesNotStopTheProjectNowThere(t *testing.T) {
	b := newUninstallBox(t)
	old := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(old, ".shikii"), 0o700); err != nil { // the other project now at the old path
		t.Fatal(err)
	}
	if err := writePageRecord(b.agent, old, 40003, os.Getppid()); err != nil {
		t.Fatal(err)
	}
	processCommand = func(pid int) string {
		return "/bin/shikii serve --store " + filepath.Join(old, ".shikii") + " --port 0"
	}
	forgetProject(old)
	if len(b.stopped) != 0 {
		t.Fatalf("the page of the project now at the old path is not stopped: %v", b.stopped)
	}
	if _, ok := readPageRecord(b.agent, old); !ok {
		t.Fatal("and its record stays")
	}
	// control: after a real move nothing is at the old path, and the old page is stopped
	if err := os.RemoveAll(filepath.Join(old, ".shikii")); err != nil {
		t.Fatal(err)
	}
	forgetProject(old)
	if len(b.stopped) != 1 {
		t.Fatalf("control: the moved project's old page is stopped: %v", b.stopped)
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

// C1: mv, then a request in the new folder: the hook reconnects it and it is active there.
func TestRelinkMovedFolderIsActiveAfterARequest(t *testing.T) {
	relinkEachPlace(t, func(t *testing.T, common, p string) {
		r := relinkMove(t, p)
		if n := relinkMovedProject(r, false); !strings.HasPrefix(n, "shikii:") {
			t.Fatalf("C1: the request does not say it reconnected the moved folder: %q", n)
		}
		if !relinkActive(r) {
			t.Fatal("C1: the moved folder, initialized here and asked for, is not active")
		}
		relinkIndexNamesItsStores(t, common)
	})
}

// C2: cp -R, then a request in the copy while the original is still in place: the copy stays inactive and the original keeps its entry.
func TestRelinkCopyStaysInactiveAfterARequest(t *testing.T) {
	relinkEachPlace(t, func(t *testing.T, common, p string) {
		q := relinkCopy(t, p, "copy")
		if n := relinkMovedProject(q, false); n != "" {
			t.Fatalf("C2: the request reconnected a copy: %q", n)
		}
		if relinkActive(q) {
			t.Fatal("C2: the copy became active without the person's shikii --init")
		}
		if !relinkActive(p) {
			t.Fatal("C2: the original is not active any more")
		}
		relinkIndexNamesItsStores(t, common)
	})
}
