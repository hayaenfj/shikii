package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Noticing without being told: if this project's agent memory (~/.claude/projects/<slug>/memory) has files not moved to the wiki,
// the hook before a request tells the agent to move them with the skill's steps (once per session). Files with the moved marker (.shikii/wiki <page id>) are not counted. In a partly moved folder the rest is shown.
// Non-.md files are not counted. In Codex's shared memory only files that name this project (the path, a folder name of 4+ characters as a word, the origin's name) are counted, and MEMORY.md is not looked at.
func TestHookNoticesAgentMemoryNotYetInTheWiki(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	home, _ := os.UserHomeDir()
	// the folder path: check Claude Code's rule (non-alphanumerics → -) with a fixed example (not a mirror test copying the same expression as the code. audit R4)
	os.MkdirAll(filepath.Join(home, ".claude", "projects", "-tmp-x-y-z", "memory"), 0o755)
	if got := agentMemoryDir("/tmp/x.y_z"); got != filepath.Join(home, ".claude", "projects", "-tmp-x-y-z", "memory") {
		t.Fatalf("the memory folder path: %q", got)
	}
	repo := filepath.Join(t.TempDir(), "mywork1") // the folder name has 4+ characters (becomes a naming word)
	os.MkdirAll(repo, 0o755)
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	initAt(t, repo)
	if dir := agentMemoryDir(repo); dir != "" {
		t.Fatalf("empty if there is no memory folder: %q", dir)
	}
	exec.Command("git", "-C", repo, "remote", "add", "origin", "https://example.com/org/shikii-like.git").Run()
	dir := filepath.Join(home, ".claude", "projects", strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, repo), "memory")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# index\n- [a](feedback-a.md)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "feedback-a.md"), []byte("---\nname: feedback-a\n---\n\n日本語で書く。教訓は .shikii/wiki に入れる方針を決めた(移行はまだ)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "feedback-b.md"), []byte("---\nname: feedback-b\n---\n\n正本: .shikii/wiki 0123456789abcdef\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "feedback-old.md.bak"), []byte("古い\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.json"), []byte("{}\n"), 0o644)
	// Codex's shared memory
	base := filepath.Base(repo)
	codex := filepath.Join(home, ".codex", "memories")
	os.MkdirAll(codex, 0o755)
	os.WriteFile(filepath.Join(codex, "MEMORY.md"), []byte("# 共通\n\n"+base+" の記憶は wiki が正本\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "this-project.md"), []byte("# "+base+" の決まり\n\nテストを先に書く\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "by-repo-name.md"), []byte("# shikii-like の決まり\n\nリポジトリの名で名指し\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "by-path.md"), []byte("# 経路で名指し\n\n"+repo+" は古い\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "other-project.md"), []byte("# 別のプロジェクトの決まり\n\n~/git/"+base+"-old/ は古い。関係ない\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "perf-"+base+".json"), []byte("{}\n"), 0o644)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "m1", "cwd": repo, "prompt": "README を直して"})
	if !strings.Contains(out, "files not yet moved to the wiki: 1 in ") || !strings.Contains(out, "/memory (feedback-a.md)") || strings.Contains(out, "feedback-b.md") || strings.Contains(out, ".bak") || strings.Contains(out, "notes.json") || !strings.Contains(out, "import-memory.md") {
		t.Fatalf("counts only unmoved .md files (text that merely mentions the wiki is not a marker) and shows the skill's steps:\n%s", out)
	}
	if !strings.Contains(out, "; 3 in Codex's ") || !strings.Contains(out, "that name this project (by-path.md, by-repo-name.md, this-project.md)") || strings.Contains(out, "other-project.md") || strings.Contains(out, "perf-") {
		t.Fatalf("in Codex's shared memory, only .md files naming the project by path, folder name (as a word) or origin name are counted, not MEMORY.md or other projects' files:\n%s", out)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "m1", "cwd": repo, "prompt": "次は画面を直して"})
	if strings.Contains(out, "not yet moved to the wiki") {
		t.Fatalf("not said twice in the same session:\n%s", out)
	}
	os.WriteFile(filepath.Join(dir, "feedback-a.md"), []byte("---\nname: feedback-a\n---\n\n正本: .shikii/wiki fedcba9876543210\n"), 0o644)
	for _, name := range []string{"this-project.md", "by-repo-name.md", "by-path.md"} {
		b, _ := os.ReadFile(filepath.Join(codex, name))
		os.WriteFile(filepath.Join(codex, name), append(b, []byte("\n"+base+" では .shikii/wiki 0123456789abcdef に写した\n")...), 0o644)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "m2", "cwd": repo, "prompt": "次は画面を直して"})
	if strings.Contains(out, "not yet moved to the wiki") {
		t.Fatalf("not shown once everything is moved:\n%s", out)
	}
	// naming word boundaries: a short folder name (app) is not counted even as a word, only the path. "." and "/" are not naming words
	if names := projectNames(""); names != nil {
		t.Fatalf("an empty scope names nothing: %v", names)
	}
	if names := projectNames("/work/app"); len(names) != 1 || names[0] != "/work/app" {
		t.Fatalf("a short folder name names the project only by path: %v", names)
	}
	if namesProject("myapp の決まり", []string{"app"}) || !namesProject("app の決まり", []string{"app"}) || !namesProject("/work/app は古い", []string{"/work/app"}) {
		t.Fatal("naming matches at word boundaries; a path matches as a substring")
	}
}

// G2: start 2 children of the same type side by side. SubagentStart chooses from the same copy read outside the lock, so both pick the same note and one note remains
func TestAudit091ParallelSubagentsPickDistinctTasks(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g2", "cwd": scope, "prompt": "画面の色を直して"})
	for _, p := range []string{"README の導入手順を更新してください ONE", "README の導入手順を更新してください TWO"} {
		runHook(t, "agent-task", map[string]any{"session_id": "g2", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": p, "subagent_type": "Explore"}})
	}
	agent := st.Dir
	snapshot := loadSession(agent, "g2") // the copy the 2 hooks read at the same time
	o1, m1, err1 := hookSubagentStart(agent, &hookInput{SessionID: "g2", CWD: scope, AgentID: "c1", AgentType: "Explore"}, snapshot)
	o2, m2, err2 := hookSubagentStart(agent, &hookInput{SessionID: "g2", CWD: scope, AgentID: "c2", AgentType: "Explore"}, snapshot)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	for _, ms := range [][]func(*hookSession){m1, m2} {
		if err := updateSession(agent, "g2", func(s *hookSession) {
			for _, m := range ms {
				m(s)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	h1, h2 := strings.SplitN(o1.Context, "\n", 2)[0], strings.SplitN(o2.Context, "\n", 2)[0]
	left := loadSession(agent, "g2").SubagentTasks
	if h1 == h2 || len(left) != 0 || !strings.Contains(h1, "ONE") || !strings.Contains(h2, "TWO") { // c1 is ONE, c2 is TWO (so it does not pass even if one has no note; recheck GC)
		t.Fatalf("2 children started side by side use separate notes, and no note remains:\n c1=%q\n c2=%q\n remaining=%+v", h1, h2, left)
	}
}

// F2 control: answering what query returned from the short reflection request alone, everything is saved and left is empty.
func TestAuditFinalF2ShortReflectionAloneSubmits(t *testing.T) {
	_, scope, h, _, _ := memoryEffectFixture(t)
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	stop := func() {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		stop()
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	stop()
	short := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if !strings.Contains(short, "wrap up the reflection") {
		t.Fatalf("precondition: the second ask: %q", short)
	}
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	submit := map[string]any{"operation": "submit"}
	var reviews, memory []map[string]string
	var wiki []map[string]any
	for _, raw := range anySlice(q["sources"]) {
		s := raw.(map[string]any)
		id := s["source_material"].(map[string]any)["id"].(string)
		for _, tr := range anySlice(s["targets"]) {
			reviews = append(reviews, map[string]string{"source_id": id, "trace_id": tr.(map[string]any)["trace_id"].(string), "truth": "unknown", "reason": "\u4f1a\u8a71\u304b\u3089\u306f\u6c7a\u3081\u3089\u308c\u306a\u3044"})
		}
		if s["wiki_pending"] == true {
			wiki = append(wiki, map[string]any{"sources": []string{id}})
		}
		for _, m := range anySlice(s["memories"]) {
			item := m.(map[string]any)
			memory = append(memory, map[string]string{"source_id": item["source_id"].(string), "key": item["quote"].(map[string]any)["key"].(string), "result": "unknown", "decision": "", "evidence": "\u78ba\u304b\u3081\u305f\u7d50\u679c\u304c\u7121\u3044"})
		}
	}
	for _, raw := range anySlice(q["observations"]) {
		wiki = append(wiki, map[string]any{"sources": []string{raw.(map[string]any)["id"].(string)}})
	}
	submit["reviews"], submit["wiki"], submit["memory"] = reviews, wiki, memory
	r := reflectCall(t, h, submit)
	t.Logf("sent: reviews=%d wiki=%d memory=%d reply=%v", len(reviews), len(wiki), len(memory), r)
	if r["complete"] != true || r["left"] != nil {
		t.Fatalf("F2: a reflection sent from the short form alone was not saved: %v", r)
	}
}

// re3-2 (F4, new by 3509108): the short reflection form lists correction_result as a field of every memory item, next to the values of
// result, without "for a delivered correction". An agent that only has the short form (after compaction) and fills every field it lists
// sends correction_result for an ordinary lesson; that item is refused. The full text qualifies it.
func TestRe3ShortFormCorrectionResultOnlyForCorrections(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "cr")
	id := memoryEffectComplete(t, st, scope, "cr")
	key := "lesson:" + draft.ID + ":" + revision
	submit := func(withCR bool) map[string]any {
		item := map[string]string{"source_id": id, "key": key, "result": "unknown", "decision": "", "evidence": "\u4e0d\u660e"}
		if withCR {
			item["correction_result"] = "unknown" // the short form lists the field with no condition
		}
		return reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{item}})
	}
	qualified := strings.Contains(reflectHowToSubmit, "delivered correction")
	refused := submit(true)
	t.Logf("with correction_result: %v", refused)
	if refused["complete"] == true {
		t.Skip("an ordinary lesson accepts correction_result: no trap")
	}
	if ok := submit(false); ok["complete"] != true {
		t.Fatalf("control: without the field the same item is saved: %v", ok)
	}
	if !qualified {
		t.Fatalf("F4: how_to_submit lists correction_result for every memory item, but an ordinary lesson refuses it (%v); it must say it is for a delivered correction\nhow_to_submit: %q", refused["errors"], reflectHowToSubmit)
	}
}

func TestCorrectionTracksBehaviorSeparatelyFromHelpfulness(t *testing.T) {
	for _, outcome := range []string{"followed", "repeated", "not_applicable", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			st, scope, h, key := correctionSeed(t)
			// Even after moving on to the next request after completion, what is evaluated is the request at the time of delivery.
			items, _ := st.MemoryReport(scope)
			id := items.All[0].SourceID
			entry := map[string]string{"source_id": id, "key": key, "result": "unknown", "correction_result": outcome, "decision": "案内全文の確認を行った", "evidence": "合成の変更差分と確認記録を照合した"}
			body := map[string]any{"operation": "submit", "memory": []map[string]string{entry}}
			for range 2 {
				if reflectCall(t, h, body)["complete"] != true {
					t.Fatal("save the observation of the action only once")
				}
			}
			q := reflectCall(t, h, map[string]any{"operation": "query"})
			summary := q["memory_effect"].(map[string]any)
			counts := summary["corrections"].(map[string]any)
			if counts["delivered"] != float64(1) || counts[outcome] != float64(1) || counts["pending"] != float64(0) || summary["stats"].(map[string]any)["helpful"] != float64(0) {
				t.Fatal("do not confuse an observation that followed the correction with one that helped, and do not increase the count on resend")
			}
			entry["correction_result"] = "changed"
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("refuse unknown categories and later rewrites")
			}
			entry["correction_result"] = map[string]string{"followed": "repeated", "repeated": "followed", "unknown": "followed", "not_applicable": "followed"}[outcome]
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("even if other existing fields are the same, do not repaint only the correction's evaluation later")
			}
			_, labels, _ := st.Counts("done-as-asked")
			if len(labels) != 0 {
				t.Fatal("do not mix observations of corrections into calibration")
			}
		})
	}
}

func TestCorrectionRequiresExactHumanReplyAndActualDelivery(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	for _, correction := range []map[string]string{
		{"source_id": draft.Sources[0], "quote": "人が言っていない訂正", "action": "全文を確認する"},
		{"source_id": "ffffffffffffffff", "quote": "まだ英語が残っています", "action": "全文を確認する"},
		{"source_id": draft.Sources[0], "quote": "まだ英語が残っています", "action": ""},
	} {
		data, _ := json.Marshal(map[string]any{"sources": draft.Sources, "title": draft.Title, "lesson": draft.Lesson, "applies_to": draft.AppliesTo, "correction": correction})
		var d notebook.Draft
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
		if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(d); err == nil {
			t.Fatal("a correction needs the original text of the person's reply in the same work and a concrete action")
		}
	}
	// Do not let an ordinary lesson be scored as an example of a person's correction taking effect.
	memoryEffectPrompt(t, scope, "ordinary")
	id := memoryEffectComplete(t, st, scope, "ordinary")
	key := memoryEffectItem(t, h, id)["quote"].(map[string]any)["key"].(string)
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "helpful", "correction_result": "followed", "decision": "確認した", "evidence": "合成の確認結果"}}}
	if reflectCall(t, h, body)["complete"] != false {
		t.Fatal("do not evaluate an action for a correction that was never delivered")
	}
}

func TestCorrectionKeepsDeliveredActionAfterLessonUpdate(t *testing.T) {
	st, scope, h, _ := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	book := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := book.Query("")
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(err)
	}
	page := v.Lessons[0]
	// Even an ordinary update that omits the correction does not lose the existing correction and source.
	source := page.Sources[0].ID
	updated, err := book.Ingest(notebook.Draft{ID: page.ID, Sources: []string{source}, Title: page.Title, Lesson: "新しい教訓に更新した。", Kind: "rule", Scene: "テストの時", AppliesTo: page.AppliesTo})
	if err != nil || updated.Page.Correction == nil || updated.Page.Correction.Action != page.Correction.Action {
		t.Fatal("an ordinary update does not delete the correction", err)
	}
	data, _ := json.Marshal(map[string]any{"id": page.ID, "sources": []string{source}, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo, "correction": map[string]string{"source_id": source, "quote": page.Correction.Quote, "action": "今後の別の行動"}})
	var d notebook.Draft
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	updated, err = book.Ingest(d)
	if err != nil || updated.Page.Revision == page.Revision {
		t.Fatal("an update to the action also makes a new version", err)
	}
	item := memoryEffectItem(t, h, id)
	correction := item["quote"].(map[string]any)["correction"].(map[string]any)
	if correction["action"] != page.Correction.Action {
		t.Fatal("a later action does not re-explain the correction delivered back then")
	}
}

func TestCorrectionPendingAndUnknownDoNotClaimImprovement(t *testing.T) {
	st, scope, h, key := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	counts := q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["pending"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("being delivered alone does not mean the correction took effect")
	}
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "unknown", "evidence": "行動の記録が無い"}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("the older input format is also accepted as unknown")
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	counts = q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["unknown"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("do not treat omission or silence as evidence of improvement")
	}
}

// a reflect query (the CLI, MCP shikii_reflect and /v1/reflect all go through runReflect) settles a stale deferral too
func TestSweepStaleDeferralByReflectQuery(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	if _, err := runReflect(root, loadSession(root, "planning").Scope, reflectRequest{Operation: "query"}); err != nil {
		t.Fatal(err)
	}
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("a reflect query must settle a stale deferral: %+v %v", l, ok)
	}
}

// A subagent's (Claude Code's Agent tool) task text arrives in neither UserPromptSubmit nor SubagentStart. Since 0.7.3 lessons are delivered at SubagentStart.
// 0.9.1: the task text the parent passed to the Agent tool is kept at PreToolUse and lessons are picked with it (picking by the parent's request delivered lessons unrelated to the child's work).
// The parent session's record (request, delivered memory versions) is not changed, and the delivery is recorded under an ID separate from the parent's (so reflection can mark it helpful / unused). Nothing is output without a parent request.
func TestSubagentStartDeliversLessonsForItsOwnTask(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	if out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"}); out != "" {
		t.Fatalf("delivers nothing if there is no parent request yet: %s", out)
	}
	// the parent's request is unrelated to README. The child is given the README work
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "画面の色を直して、監査も頼みます"})
	before := loadSession(st.Dir, "sub")
	ctx := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください"))
	if !strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "close to its task") {
		t.Fatalf("delivers lessons close to the task text given to the child: %s", ctx)
	}
	after := loadSession(st.Dir, "sub")
	if after.Request != before.Request || after.TurnID != before.TurnID || len(after.Memories) != len(before.Memories) || len(after.SubagentTasks) != 0 {
		t.Fatalf("the parent's record is unchanged except for dropping the used child task: before=%+v after=%+v", before, after)
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.Parent == before.TurnID {
			sub = d
		}
	}
	if sub.Agent != "agent-1" || sub.Request != "README の導入手順を更新してください" || len(sub.Quotes) == 0 || sub.Quotes[0].ID != draft.ID {
		t.Fatalf("records the delivery to the child under an ID separate from the parent's, with the child's task text and the quoted versions: %+v", deliveries)
	}
	// the same child starting again does not fail (if the task is passed again, the same ID and the same quotes are not saved again)
	if ctx2 := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください")); !strings.Contains(ctx2, draft.Lesson) {
		t.Fatalf("delivers the second time too: %s", ctx2)
	}
}

// 0.9.1: a child with no kept task (no PreToolUse, as in Codex; a different type; too old) does not get lessons picked by the parent's request,
// only the rules that apply to every request (rules with the scene "いつも"). Kept tasks of the same type are used one at a time, oldest first
func TestSubagentWithoutItsTaskGetsOnlyAlwaysRules(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Kind, always.Scene, always.AppliesTo = "", "見える文は日本語", "報告も含めて見える文は日本語で書く。", "rule", "いつも", "どの依頼でも"
	page, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always)
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s4", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a1", "agent_type": "Explore"}))
	if !strings.Contains(ctx, always.Lesson) || strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "the rules that apply to every request") {
		t.Fatalf("a child with no kept task gets only the \"いつも\" rules, not lessons close to the parent's request: %s", ctx)
	}
	// a kept task for a child of another type is not used. Kept tasks of the same type are used oldest first
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください", "subagent_type": "Plan"}})
	// WebFetch also has a prompt in its input. Tools other than Agent / Task are not kept even with a prompt
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "WebFetch", "tool_input": map[string]any{"url": "https://example.com", "prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a2", "agent_type": "Explore"})); strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a task for a child of another type (Plan) is not used for an Explore child: %s", ctx)
	}
	if got := loadSession(st.Dir, "s4").SubagentTasks; len(got) != 1 || got[0].Type != "Plan" {
		t.Fatalf("only Agent tool tasks are kept (not WebFetch), and unused records stay: %+v", got)
	}
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a3", "agent_type": "Plan"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a child of the same type is picked with the kept task text: %s", ctx)
	}
	// a child started without a type starts as general-purpose
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Task", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a4", "agent_type": "general-purpose"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a record without a type is used for a general-purpose child: %s", ctx)
	}
	_ = page
}

// Audit (2026-10-23): deliveries to subagents can be assessed in reflection alongside the parent's conversation, and are not silent on a restart after the lesson's version changed.
func TestSubagentDeliveriesAreAssessedWithTheParentAndSurviveRevisions(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub2").TurnID
	startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	deliveries, _ := st.MemoryDeliveries(scope)
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.Parent != "" {
			sub = d
		}
	}
	if sub.Parent != parent || sub.Agent != "agent-x" {
		t.Fatalf("a delivery to a subagent carries the parent's turn and the agent: %+v", deliveries)
	}
	// after the lesson's version changed, it is delivered even when the same subagent starts again (it used to be refused as another quote for the same id, and stayed silent)
	draft.Lesson = draft.Lesson + " 変更後も確認する。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	out := startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	if !strings.Contains(out, "変更後も確認する") {
		t.Fatalf("delivered on a restart after the version changed: %q", out)
	}
	// reflection: the parent's conversation (once the reply and the next message are there) also lists the delivery to the subagent
	runHook(t, "stop", map[string]any{"session_id": "sub2", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "次は画面の色を直して。"})
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	sources, _ := q["sources"].([]any)
	found := 0
	for _, s := range sources {
		src := s.(map[string]any)
		if src["source_material"].(map[string]any)["id"] != parent {
			continue
		}
		for _, m := range src["memories"].([]any) {
			if m.(map[string]any)["parent"] == parent {
				found++
			}
		}
	}
	if found < 1 {
		t.Fatalf("the delivery to the subagent shows up for assessment alongside the parent's conversation: %v", q["sources"])
	}
}

// Audit R2: a delivery to a subagent can be assessed by the source_id (delivery id) in the reflection output and leaves the observation queue.
// If the parent's conversation (reply and next message) is complete, the delivery id need not be a conversation id. Submit's conversation count also folds into the parent.
func TestSubagentDeliveryCanBeAssessedByItsOwnSourceID(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub3", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub3").TurnID
	startSubagent(t, "sub3", scope, "agent-y", "Explore", "README の導入手順を更新してください")
	runHook(t, "stop", map[string]any{"session_id": "sub3", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub3", "cwd": scope, "prompt": "次は画面の色を直して。"})
	deliveries, _ := st.MemoryDeliveries(scope)
	var subID string
	for _, d := range deliveries {
		if d.Parent == parent {
			subID = d.ID
		}
	}
	if subID == "" {
		t.Fatal("there is a delivery to the subagent")
	}
	key := "lesson:" + draft.ID + ":" + revision
	// before assessment: the parent's conversation is complete, so the delivery to the subagent is waiting for assessment, not for the "next request" (audit R3)
	if rep, err := st.MemoryReport(scope); err != nil || rep.Stats.Pending != 2 || rep.Stats.AwaitingFeedback != 0 {
		t.Fatalf("once the parent's conversation is complete, the delivery to the subagent is not waiting for the next request either: %+v %v", rep.Stats, err)
	}
	// even with one more delivery from another session, the parent's and the subagent's deliveries can be assessed at once (up to 0.9.0 this checked that parent and subagent fold into "at most 2 conversations". audit R3)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	other := loadSession(st.Dir, "other").TurnID
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "次は画面の色を直して。"})
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]any{
		{"source_id": parent, "key": key, "result": "helpful", "decision": "手順に確認を足した", "evidence": "返答に確認の手順が入った"},
		{"source_id": subID, "key": key, "result": "unused", "decision": "サブエージェントは手順を読まなかった", "evidence": "返答に手順への言及が無い"},
		{"source_id": other, "key": key, "result": "helpful", "decision": "別のセッションでも手順に確認を足した", "evidence": "返答に確認の手順が入った"},
	}})
	if out["complete"] != true {
		t.Fatalf("the parent's and the subagent's deliveries can be assessed at once as the same conversation (2 conversations with one other added): %v", out)
	}
	rep, err := st.MemoryReport(scope)
	if err != nil || rep.Stats.Pending != 0 || rep.Stats.AwaitingFeedback != 0 || rep.Stats.Helpful != 2 || rep.Stats.Unused != 1 {
		t.Fatalf("once assessed they leave the observation queue: %+v %v", rep.Stats, err)
	}
}

// Audit 0.9.1 (test gaps GM2-4, 14): kept child tasks are used oldest first within the same type, and ones older than 10 minutes are not used.
// When adding, expired ones are dropped and at most 8 are kept
func TestSubagentTasksAreOldestFirstWithinLimits(t *testing.T) {
	now := time.Now()
	tasks := []subagentTask{
		{Type: "Explore", Prompt: "古すぎる", At: now.Add(-11 * time.Minute)},
		{Type: "Explore", Prompt: "古い", At: now.Add(-2 * time.Second)},
		{Type: "Explore", Prompt: "新しい", At: now.Add(-1 * time.Second)},
	}
	if got, ok := pickSubagentTask(tasks, "Explore", now); !ok || got.Prompt != "古い" {
		t.Fatalf("uses the oldest record within the time limit (not ones older than 10 minutes): %+v %v", got, ok)
	}
	add := func(s *hookSession, prompt string) {
		in := &hookInput{ToolName: "Agent", ToolInput: json.RawMessage(`{"prompt":"` + prompt + `","subagent_type":"Plan"}`)}
		for _, m := range hookAgentTask(in) {
			m(s)
		}
	}
	// when adding, expired ones (even of another type) are dropped
	expired := &hookSession{SubagentTasks: []subagentTask{{Type: "Explore", Prompt: "期限切れ", At: now.Add(-11 * time.Minute)}}}
	add(expired, "仕事 X")
	if len(expired.SubagentTasks) != 1 || expired.SubagentTasks[0].Prompt != "仕事 X" {
		t.Fatalf("drops expired records when adding: %+v", expired.SubagentTasks)
	}
	// dropping records older than 30 seconds applies only to the same type (records of other types stay. re-audit GB)
	other := &hookSession{SubagentTasks: []subagentTask{{Type: "Explore", Prompt: "1 分前の Explore", At: now.Add(-time.Minute)}}}
	add(other, "仕事 Y")
	if len(other.SubagentTasks) != 2 || other.SubagentTasks[0].Prompt != "1 分前の Explore" {
		t.Fatalf("records of another type are not dropped by the 30-second rule: %+v", other.SubagentTasks)
	}
	s := &hookSession{SubagentTasks: []subagentTask{{Type: "Plan", Prompt: "期限切れ", At: now.Add(-11 * time.Minute)}}}
	for i := 0; i < 10; i++ {
		in := &hookInput{ToolName: "Agent", ToolInput: json.RawMessage(`{"prompt":"仕事 ` + string(rune('A'+i)) + `","subagent_type":"Plan"}`)}
		for _, m := range hookAgentTask(in) {
			m(s)
		}
	}
	if len(s.SubagentTasks) != subagentTaskMax || s.SubagentTasks[0].Prompt != "仕事 C" || s.SubagentTasks[len(s.SubagentTasks)-1].Prompt != "仕事 J" {
		t.Fatalf("drops expired ones when adding and keeps only the newest 8: %+v", s.SubagentTasks)
	}
}

func TestMemoryEffectPreservesDeliveredRevisionAndIndependentPending(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	ctx := memoryEffectPrompt(t, scope, "effect")
	if !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("the related lesson is actually delivered")
	}
	key := "lesson:" + draft.ID + ":" + revision
	id := memoryEffectComplete(t, st, scope, "effect")
	// Finish the wiki and the usual scoring first, so only the memory assessment is left.
	feedback, _ := st.RecentFeedback(scope)
	for _, f := range feedback {
		if f.ID != id {
			continue
		}
		for _, target := range f.Targets {
			if _, err := runReview(st.Dir, scope, reviewRequest{Operation: "submit", SourceID: id, TraceID: target.TraceID, Truth: "yes", Reason: "合成の実行結果を確認"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(notebook.Draft{Sources: []string{id}}); err != nil {
		t.Fatal(err)
	}
	draft.Lesson = "更新された別の教訓。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	item := memoryEffectItem(t, h, id)
	quote := item["quote"].(map[string]any)
	if quote["key"] != key || quote["lesson"] == draft.Lesson || !strings.Contains(ctx, quote["lesson"].(string)) {
		t.Fatal("assesses the revision and quote as delivered, not the lesson updated later")
	}
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["helpful"] != float64(0) || stats["pending"] != float64(1) {
		t.Fatal("scoring completion as yes alone does not make the memory's effect a success")
	}
	result := memoryEffectSubmit(t, h, id, key, "helpful", "未初期化の場所の検査を追加する判断に使った", "合成の隔離フォルダで出力と記録が無いことを検査した")
	if result["complete"] != true {
		t.Fatalf("saves a provisional assessment of the memory, backed by the decision and the result: %v", result)
	}
	stats = memoryEffectStats(t, h)
	if stats["helpful"] != float64(1) || stats["pending"] != float64(0) {
		t.Fatal("keeps the number of quotes delivered apart from the number of observations that they helped")
	}
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || personTruths(labels) != 0 {
		t.Fatal("provisional memory assessments are not mixed into truths or calibration")
	}
}

func TestMemoryEffectKeepsTheEscapedRawQuoteExactly(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	request := "README の導入手順を更新してください"
	if err := st.AppendFeedback(scope, store.Feedback{ID: "0000000000000001", Request: request, Summary: strings.Repeat("\x01", 600), Reply: "<引用>\n過去の発言" + strings.Repeat("\x02", 600)}); err != nil {
		t.Fatal(err)
	}
	ctx := memoryEffectPrompt(t, scope, "raw-effect")
	if len(ctx) > 1800 || !strings.Contains(ctx, "feedback:0000000000000001") {
		t.Fatal("unsorted raw text is also actually delivered within the limit")
	}
	var quotes []map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(ctx, "\n", 2)[1]), &quotes); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: scope})
	// A request with no next message yet cannot be assessed as a successful memory.
	id := loadSession(agent, "raw-effect").TurnID
	if memoryEffectSubmit(t, h, id, quotes[0]["key"].(string), "helpful", "仮の判断", "確認前")["complete"] != false {
		t.Fatal("no effect is attached to a request that has not ended")
	}
	id = memoryEffectComplete(t, st, scope, "raw-effect")
	quote := memoryEffectItem(t, h, id)["quote"].(map[string]any)
	actual, _ := json.Marshal(quotes[0])
	saved, _ := json.Marshal(quote)
	if string(actual) != string(saved) || len(quote["summary"].(string)) >= 600 {
		t.Fatal("what is saved is not the whole raw text but exactly the quote delivered after escaping and cutting")
	}
}

func TestMemoryEffectSeparatesUsefulUnusedHarmfulAndUnknown(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	key := "lesson:" + draft.ID + ":" + revision
	for i, result := range []string{"helpful", "unused", "harmful", "unknown"} {
		session := fmt.Sprintf("case-%d", i)
		memoryEffectPrompt(t, scope, session)
		id := memoryEffectComplete(t, st, scope, session)
		if memoryEffectSubmit(t, h, id, key, result, "合成の判断記録", "合成の検査結果")["complete"] != true {
			t.Fatal("examples that did not help, and unknown ones, are saved in the same unit")
		}
	}
	stats := memoryEffectStats(t, h)
	for _, name := range []string{"helpful", "unused", "harmful", "unknown"} {
		if stats[name] != float64(1) {
			t.Fatalf("does not count only the good examples: %s=%v", name, stats[name])
		}
	}
	if stats["delivered"] != float64(4) || stats["pending"] != float64(0) {
		t.Fatal("the delivered count is the sum of all categories and the unassessed")
	}
	t.Run("two-quotes", func(t *testing.T) {
		st, scope, h, draft, _ := memoryEffectFixture(t)
		draft.ID, draft.Title, draft.Lesson = "", "README の導入手順の補足", "モデルの起動方法も確認する。"
		if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
			t.Fatal(err)
		}
		memoryEffectPrompt(t, scope, "two-quotes")
		id := memoryEffectComplete(t, st, scope, "two-quotes")
		q := reflectCall(t, h, map[string]any{"operation": "query"})
		entries := []map[string]string{}
		for _, raw := range q["sources"].([]any) {
			source := raw.(map[string]any)
			if source["source_material"].(map[string]any)["id"] != id {
				continue
			}
			for i, raw := range source["memories"].([]any) {
				key := raw.(map[string]any)["quote"].(map[string]any)["key"].(string)
				entries = append(entries, map[string]string{"source_id": id, "key": key, "result": []string{"helpful", "unused"}[i], "decision": "合成の判断", "evidence": "合成の結果"})
			}
		}
		if len(entries) != 2 || reflectCall(t, h, map[string]any{"operation": "submit", "memory": entries})["complete"] != true {
			t.Fatal("two memories delivered to one request are assessed separately")
		}
		stats := memoryEffectStats(t, h)
		if stats["delivered"] != float64(2) || stats["helpful"] != float64(1) || stats["unused"] != float64(1) {
			t.Fatal("the unit is memories actually delivered, not requests")
		}
	})
}

// 0.11: the reflection request is one short form (the command and the conversations); the shape of submit comes back in the reply of
// reflect query. A turn that ends without the reflection is told the steps again by the Stop check, and the next ask is the same form.
func TestReflectionNoticeIsOneFormAndTheStopCheckRepeatsTheSteps(t *testing.T) {
	_, scope, _, _, _ := memoryEffectFixture(t)
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	if !strings.Contains(first, "wrap up the reflection") || !strings.Contains(first, "reflect query") || !strings.Contains(first, `{"sources":[`) || strings.Contains(first, "reviews:[") {
		t.Fatalf("the ask names the command and the conversations and leaves the shape to the reply: %q", first)
	}
	// the turn ends without the reflection: the Stop check repeats the steps, and the next ask is the same form
	stop := runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	if !strings.Contains(string(stop), "reflect query") || !strings.Contains(string(stop), "submit reviews") {
		t.Fatalf("the Stop check repeats the steps of the missed reflection: %s", stop)
	}
	again := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if !strings.Contains(again, "wrap up the reflection") || !strings.Contains(again, `{"sources":[`) || !strings.Contains(again, "reflect query") {
		t.Fatalf("the next ask is the same form naming the command and the conversations: %q", again)
	}
}

// The text asking for reflection contains none of the conversation's raw text (request, summary, the person's reply) (checked by content, not size)
func TestReflectAskCarriesNoOriginalText(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	var ctx string
	var ids []string
	for n := 1; n <= 2; n++ {
		id, _ := reviewSeed(t, st, scope, n)
		ids = append(ids, id)
		ctx = feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": fmt.Sprint(n), "cwd": repo, "prompt": "合成の記録を調べてください。今回の内容を確認します。"}))
	}
	t.Logf("text size: %d bytes", len(ctx))
	// If the text does not appear, checking that no raw text is in it means nothing (final audit 0.9.1 T2). The asking text and the ids of the conversations to reflect on are there
	if !strings.Contains(ctx, "wrap up the reflection") {
		t.Fatalf("the text asking for reflection does not appear: %q", ctx)
	}
	for _, id := range ids {
		if !strings.Contains(ctx, id) {
			t.Errorf("the id %s of a conversation to reflect on is missing: %q", id, ctx)
		}
	}
	for _, raw := range []string{"README の起動手順を日本語にする", "起動手順を直しました", "まだ英語が残っています"} {
		if strings.Contains(ctx, raw) {
			t.Errorf("the conversation's raw text %q got into the text asking for reflection", raw)
		}
	}
}
