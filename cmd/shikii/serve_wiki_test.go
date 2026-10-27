package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Growing the wiki from the page: the list of drafts and claims (evidence and the confirmable mark), generalizing a draft into a claim, changing state, deleting.
// The line that only people change states holds in the API as well (the machine only says confirmable).
func TestServeWikiListsClaimsAndLetsAPersonPromoteAndDecide(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "false")
	pdir := filepath.Join(dir, ".shikii", "wiki", "patterns")
	if err := os.MkdirAll(filepath.Join(pdir, "drafts"), 0o755); err != nil {
		t.Fatal(err)
	}
	draft := filepath.Join(pdir, "drafts", "intent-strict.md")
	if err := os.WriteFile(draft, []byte("---\nclaim: intent では候補 strict が current より当たる\nstatus: draft\napplies_to:\n  type: choice\n  models: m\nevidence:\n  - date: \"2026-10-18\"\n    model: m\n    result: strict 1.000 vs current 0.500\n    verdict: supports\n    run: r1\n  - date: \"2026-10-18\"\n    model: other\n    result: strict 0.9 vs current 0.6\n    verdict: supports\n    run: r2\nretest:\n  spec: decisions/intent.yaml\n  better: strict\n  than: current\n---\n# 下書き\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "old-prose.md"), []byte("# 前書きの無い古いページ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ef endpointFlags
	ef.model, ef.local = "m", "http://127.0.0.1:1"
	s := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef}))
	t.Cleanup(s.Close)

	var list struct {
		Claims []map[string]any `json:"claims"`
	}
	if getJSON(t, s.URL+"/v1/wiki", &list) != 200 || len(list.Claims) != 2 {
		t.Fatalf("every page is listed, with or without front matter: %+v", list.Claims)
	}
	var d map[string]any
	for _, c := range list.Claims {
		if c["status"] == "draft" {
			d = c
		}
	}
	if d == nil || d["confirmable"] != true || d["supporting_models"] != 2.0 || len(d["evidence"].([]any)) != 2 {
		t.Fatalf("a draft with 2 supporting models is confirmable: %+v", d)
	}

	// HTTP evidence returns values with the lowercase keys the page reads.
	evidence := d["evidence"].([]any)[0].(map[string]any)
	for key, want := range map[string]string{
		"date": "2026-10-18", "model": "m", "result": "strict 1.000 vs current 0.500", "verdict": "supports", "run": "r1",
	} {
		if evidence[key] != want {
			t.Fatalf("evidence JSON field %s: got %v, want %s", key, evidence[key], want)
		}
	}

	// Generalize a draft into a claim: it moves to patterns/<slug>.md, status is tentative, and the body and evidence remain
	code, out := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": d["path"], "action": "promote", "slug": "strict-wording",
		"claim": "厳しく問う文言は迷いを減らす", "applies_to": map[string]string{"type": "choice", "models": "小さいモデル"}})
	if code != 200 || out["path"] != "patterns/strict-wording.md" {
		t.Fatalf("promote: %d %v", code, out)
	}
	if _, err := os.Stat(draft); !os.IsNotExist(err) {
		t.Fatalf("the draft must move, not copy")
	}
	pg, body, err := readClaimPage(filepath.Join(pdir, "strict-wording.md"))
	if err != nil || pg.Status != "tentative" || pg.Claim != "厳しく問う文言は迷いを減らす" || pg.AppliesTo["models"] != "小さいモデル" || len(pg.Evidence) != 2 || pg.Retest == nil || !strings.Contains(body, "下書き") {
		t.Fatalf("promoted page: %v %+v\n%s", err, pg, body)
	}
	// Change state (only confirmed / rejected). An unknown state is 400. A page without front matter is 409
	code, _ = postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/strict-wording.md", "action": "status", "status": "confirmed"})
	pg, _, _ = readClaimPage(filepath.Join(pdir, "strict-wording.md"))
	if code != 200 || pg.Status != "confirmed" {
		t.Fatalf("a person confirms: %d %+v", code, pg)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/strict-wording.md", "action": "status", "status": "maybe"}); code != 400 {
		t.Fatalf("unknown status must be 400, got %d", code)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/old-prose.md", "action": "status", "status": "confirmed"}); code != 409 {
		t.Fatalf("a page without front matter cannot change status, got %d", code)
	}
	// A path outside is 400. Only drafts are deleted (claims are rejected instead)
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "../etc/passwd", "action": "delete"}); code != 400 {
		t.Fatalf("paths outside wiki/patterns must be refused, got %d", code)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/strict-wording.md", "action": "delete"}); code != 409 {
		t.Fatalf("only drafts may be deleted; claims are rejected instead, got %d", code)
	}
	if err := os.WriteFile(filepath.Join(pdir, "drafts", "intent-worse.md"), []byte("---\nclaim: x\nstatus: draft\n---\n# d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/drafts/intent-worse.md", "action": "delete"}); code != 200 {
		t.Fatalf("a draft may be deleted, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(pdir, "drafts", "intent-worse.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted draft must be gone")
	}
	body2 := uiText(t, s.URL)
	for _, must := range []string{"v1/wiki", "主張にする", "promote"} {
		if !strings.Contains(body2, must) {
			t.Fatalf("the page must have the knowledge block; missing %q", must)
		}
	}
}

// 11, 12, 7: the page's wiki endpoint. Layout variants on the same base count as one model. A draft cannot be made confirmed. Unknown front matter fields are not lost.
func TestWikiClaimRulesFromAudit4(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "false")
	pdir := filepath.Join(dir, ".shikii", "wiki", "patterns")
	if err := os.MkdirAll(filepath.Join(pdir, "drafts"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := "---\nclaim: c\nstatus: draft\nauthor: someone\nevidence:\n  - date: \"2026-10-18\"\n    model: m+state-last\n    result: r\n    verdict: supports\n  - date: \"2026-10-18\"\n    model: m+plain\n    result: r\n    verdict: supports\nretest:\n  spec: decisions/intent.yaml\n  better: plain\n  than: current\n---\n# d\n"
	draft := filepath.Join(pdir, "drafts", "intent-plain.md")
	if err := os.WriteFile(draft, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	var ef endpointFlags
	ef.model, ef.local = "m", "http://127.0.0.1:1"
	s := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef}))
	t.Cleanup(s.Close)
	var list struct {
		Claims []map[string]any `json:"claims"`
	}
	getJSON(t, s.URL+"/v1/wiki", &list)
	if len(list.Claims) != 1 || list.Claims[0]["confirmable"] != false || list.Claims[0]["supporting_models"] != 1.0 {
		t.Fatalf("two layouts of one base model are one model: %+v", list.Claims)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/drafts/intent-plain.md", "action": "status", "status": "confirmed"}); code != 409 {
		t.Fatalf("a draft must be promoted before it can be confirmed, got %d", code)
	}
	pg, body, err := readClaimPage(draft)
	if err != nil {
		t.Fatal(err)
	}
	pg.Evidence = append(pg.Evidence, claimEvidence{Date: "2026-10-18", Model: "x", Result: "r", Verdict: "supports"})
	if err := writeClaimPage(draft, pg, body); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(draft)
	if !strings.Contains(string(b), "author: someone") {
		t.Fatalf("a key the machine does not know must survive a machine write:\n%s", b)
	}
}

// E1: the inbox lists harmful evaluations of the same lesson once per count, and keeps showing those of replaced (no longer live) pages.
func TestAudit090InboxDedupesAndDropsDeadLessons(t *testing.T) {
	agent, scope, _, src := audit083Setup(t)
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "害のあった記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "置き換えた記事", Lesson: "新しい教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時", Replaces: []string{a.Page.ID}}); err != nil {
		t.Fatal(err)
	}
	obs := []store.MemoryObservation{}
	for i := 0; i < 3; i++ {
		obs = append(obs, store.MemoryObservation{Request: "r", Quote: store.MemoryQuote{ID: a.Page.ID, Lesson: "教訓。"}, Assessment: &store.MemoryAssessment{Result: "harmful"}})
	}
	_, inbox := knowledgeMeta(agent, scope, obs)
	if len(inbox) != 0 {
		t.Fatalf("%d harmful evaluations of a replaced page are listed in the inbox", len(inbox))
	}
}

func TestMemoryEffectCountsOnlyDeliveredQuotesAndKeepsUnfinishedVisible(t *testing.T) {
	_, scope, h, _, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "unfinished")
	memoryEffectPrompt(t, scope, "unfinished") // the same revision is not actually delivered again.
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "unrelated", "cwd": scope, "prompt": "決済障害の監視を説明してください。"})
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["pending"] != float64(1) || stats["awaiting_feedback"] != float64(1) {
		t.Fatal("suppressed duplicates and requests that did not match are not added to the delivery count, and unfinished ones stay in the denominator")
	}
	other := newServer(serveOpts{storeDir: t.TempDir(), projectScope: feedbackScope(gitRepo(t))}) // another project has its own store
	if memoryEffectStats(t, other)["delivered"] != float64(0) {
		t.Fatal("another project is not shown the memory quotes or assessments")
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/wiki", nil))
	var view map[string]any
	if json.Unmarshal(r.Body.Bytes(), &view) != nil || view["memory_effect"] == nil {
		t.Fatal("the knowledge page gets the same totals")
	}
}

func TestNotebookContinuationAcrossCLIHTTPAndMCP(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	scope := feedbackScope(repo)
	book := notebook.Book{Root: root, Scope: scope}
	ids := []string{}
	for i := range 12 {
		o, err := book.Capture(notebook.Observation{Kind: "test", Title: fmt.Sprint("Fixture ", i), Content: strings.Repeat("x", 9000), Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	args := append([]string{"query"}, ids...)
	first := capture(t, func() error { return cmdWiki(args) })
	var one struct {
		Next         string                 `json:"next_cursor"`
		Observations []notebook.Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(first), &one); err != nil || one.Next == "" || len(one.Observations) >= 12 {
		t.Fatal("the CLI splits the result and returns a continuation", err)
	}
	cli := capture(t, func() error { return cmdWiki(append(args, "continue:"+one.Next)) })
	data, _ := json.Marshal(map[string]any{"operation": "query", "sources": ids, "cursor": one.Next})
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal("the same continuation can be read over HTTP", rec.Code, rec.Body.String())
	}
	var a, b any
	if json.Unmarshal([]byte(cli), &a) != nil || json.Unmarshal(rec.Body.Bytes(), &b) != nil {
		t.Fatal("the continuation is JSON")
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("CLI and HTTP return the same continuation")
	}
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": .9, "question": .1, "other": 0}, root)
	m := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": map[string]any{"operation": "query", "sources": ids, "cursor": one.Next}}}))
	if m["isError"] == true || m["structuredContent"] == nil {
		t.Fatal("MCP handles the continuation input too", m)
	}
	jm, _ := json.Marshal(m["structuredContent"])
	if string(jm) != string(ja) {
		t.Fatal("MCP returns the same sources too")
	}
	for i, id := range ids {
		body := strings.Repeat("Detailed evidence. ", 550)
		if _, err := book.Ingest(notebook.Draft{Title: fmt.Sprint("Topic ", i), Lesson: "Checked", Kind: "rule", Scene: "テストの時", AppliesTo: "Topic maintenance", Sources: []string{id}, Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	seen, cursor := map[string]bool{}, ""
	for calls := 0; ; calls++ {
		if calls > len(ids) {
			t.Fatal("the continuation of the page's list does not move forward")
		}
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/wiki?cursor="+cursor, nil))
		if r.Code != 200 {
			t.Fatal("fetching the page's continuation", r.Body.String())
		}
		var page struct {
			Lessons []notebook.Page `json:"lessons"`
			Next    string          `json:"next_cursor"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, p := range page.Lessons {
			if seen[p.ID] {
				t.Fatal("a page appeared twice in the page's list")
			}
			seen[p.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != len(ids) {
		t.Fatal("the page reaches every page too", len(seen))
	}
}

// 0.9.0: for the knowledge page, /v1/wiki returns each page's last update and helpful / harmful counts (lesson_meta), an inbox of only the exceptions a person should look at
// (inbox: harmful lessons, repeated corrections, guards not yet enabled), and the number of pages with no kind or scene.
func TestWikiListCarriesLessonMetaAndInbox(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "inbox")
	id := memoryEffectComplete(t, st, scope, "inbox")
	key := "lesson:" + draft.ID + ":" + revision
	if out := memoryEffectSubmit(t, h, id, key, "harmful", "この教訓に従って古い手順を書いた", "本人が直した"); out["complete"] != true {
		t.Fatalf("save the harmful evaluation: %v", out)
	}
	if err := saveGuard(st.Dir, guard{ID: "draft-one", Kind: guardKindForbiddenWords, Title: "草稿の検査", Words: []string{"x"}}); err != nil { // a draft (disabled)
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/wiki", nil))
	var body struct {
		Meta map[string]struct {
			Updated string `json:"updated"`
			Helpful int    `json:"helpful"`
			Harmful int    `json:"harmful"`
		} `json:"lesson_meta"`
		Inbox []struct {
			Kind   string `json:"kind"`
			Lesson string `json:"lesson_id"`
			Guard  string `json:"guard_id"`
		} `json:"inbox"`
		Unclassified *int `json:"unclassified_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if m := body.Meta[draft.ID]; m.Updated == "" || m.Harmful != 1 {
		t.Fatalf("each page's last update and harmful count: %+v", body.Meta)
	}
	var harmful, guardDraft bool
	for _, it := range body.Inbox {
		harmful = harmful || it.Kind == "harmful" && it.Lesson == draft.ID
		guardDraft = guardDraft || it.Kind == "guard_draft" && it.Guard == "draft-one"
	}
	if !harmful || !guardDraft {
		t.Fatalf("the inbox shows the harmful lesson and the guard not yet enabled: %+v", body.Inbox)
	}
	if body.Unclassified == nil || *body.Unclassified != 0 {
		t.Fatalf("number of pages with no kind or scene (returned even when 0): %v", body.Unclassified)
	}
}

// Audit 0.9.0 E1 and M8: the inbox groups items into one per live page, with a count. Repeated corrections show too.
func TestInboxGroupsByLessonWithCount(t *testing.T) {
	agent, scope, _, src := audit083Setup(t)
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "害のあった記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "訂正の記事", Lesson: "訂正の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	obs := []store.MemoryObservation{}
	for i := 0; i < 3; i++ {
		obs = append(obs, store.MemoryObservation{Request: "r", Quote: store.MemoryQuote{ID: a.Page.ID, Lesson: "教訓。"}, Assessment: &store.MemoryAssessment{Result: "harmful"}})
	}
	obs = append(obs, store.MemoryObservation{Request: "r", Quote: store.MemoryQuote{ID: c.Page.ID, Lesson: "訂正の教訓。"}, Assessment: &store.MemoryAssessment{Result: "unused", CorrectionResult: "repeated"}})
	_, inbox := knowledgeMeta(agent, scope, obs)
	var harmful, repeated int
	for _, it := range inbox {
		switch it["kind"] {
		case "harmful":
			harmful++
			if it["lesson_id"] != a.Page.ID || it["count"] != 3 {
				t.Fatalf("the 3 harmful items are grouped into 1 with count 3: %v", it)
			}
		case "repeated":
			repeated++
		}
	}
	if harmful != 1 || repeated != 1 {
		t.Fatalf("1 harmful item and 1 repeated correction: %v", inbox)
	}
}

// 0.9.5: a cursor made before a page changed is answered with 409, not 400, so the page can tell it apart from a bad request and read the
// knowledge list again from the start instead of showing "the wiki cursor does not match ...".
func TestStaleWikiCursorIs409(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	var last notebook.Page
	for i := range 40 {
		d := draft
		d.ID, d.Title = "", fmt.Sprintf("Knowledge %03d", i)
		d.AppliesTo = strings.Repeat("Knowledge scope ", 60)
		body := strings.Repeat("Detailed evidence. ", 200)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		last = *r.Page
	}
	get := func(target string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, first := get("/v1/wiki")
	cursor, _ := first["next_cursor"].(string)
	if code != 200 || cursor == "" {
		t.Fatalf("the list comes in parts: %d %q", code, cursor)
	}
	d := draft
	d.ID, d.Title, d.Lesson = last.ID, last.Title, "Revised knowledge"
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if code, out := get("/v1/wiki?cursor=" + url.QueryEscape(cursor)); code != http.StatusConflict {
		t.Fatalf("/v1/wiki: a cursor from before a page changed is 409: %d %v", code, out["error"])
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/wiki/notebook", strings.NewReader(`{"operation":"query","cursor":"`+cursor+`"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("/v1/wiki/notebook: a cursor from before a page changed is 409: %d %s", rec.Code, rec.Body.String())
	}
}
