package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 2026-11-28: Codex 0.172.0 shows a hook's additionalContext whole up to 2,500 tokens, which it counts as ceil(bytes / 4), so 10,000
// bytes; Claude Code shows 10,000 characters. Of 51 texts of the request hook that Codex cut in its records of that day, 38 had a line of
// JSON that no longer parsed, and 5 had lost part of how_to_record. Japanese is 3 bytes a character: the same 10,000 characters are three
// times what Codex shows. These tests make Codex the agent by the process table (the hook asks it which agent runs it) and check that
// what the hook says to it is within its limit, whole, with the closing instruction of the prepared work in it.

// codexSeesWhole is whether Codex 0.172.0 shows s whole: its estimate of the tokens is bytes / 4, rounded up, against 2,500.
func codexSeesWhole(s string) bool { return (len(s)+3)/4 <= 2500 }

// actAs makes the hook find an agent of that name above it.
func actAs(t *testing.T, name string) {
	t.Helper()
	useProcessTable(t, agentFamily(name))
	useParent(t, 500)
}

// japaneseArticles adds n articles of Japanese lessons that a request about them takes in full: more than the agents show at once.
func japaneseArticles(t *testing.T, b notebook.Book, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		interimAddArticle(t, b, fmt.Sprintf("\u30a2\u30fc\u30ab\u30a4\u30d6\u306e\u627f\u8a8d %d", i), strings.Repeat("\u672c\u756a\u306e\u5fa9\u5143\u306b\u306f\u6240\u6709\u8005\u306e\u627f\u8a8d\u304c\u8981\u308b\u3002\u30a2\u30fc\u30ab\u30a4\u30d6\u3092\u623b\u3059\u524d\u306b\u627f\u8a8d\u3092\u5f97\u308b\u3053\u3068\u3002", 60))
	}
}

const japaneseRequest = "\u30a2\u30fc\u30ab\u30a4\u30d6\u306e\u627f\u8a8d\u3068\u672c\u756a\u306e\u5fa9\u5143\u306e\u6761\u4ef6\u3092\u8aac\u660e\u3057\u3066\u3002"

// preparedOf is the prepared work in the context of a request hook, parsed (nil when there is none).
func preparedOf(t *testing.T, context string) map[string]any {
	t.Helper()
	const prefix = "shikii prepared work:\n"
	i := strings.Index(context, prefix)
	if i < 0 {
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(context[i+len(prefix):]), &v); err != nil {
		t.Fatalf("the prepared work is not whole JSON: %v: %.200s", err, context[i+len(prefix):])
	}
	return v
}

// The same request, in Japanese lessons the agents show only in part: Claude Code is given what fits 10,000 characters, which is far more
// than Codex shows; Codex is given what fits 9,800 bytes, whole: the JSON parses, it starts with its receipt, and how_to_record, the
// instruction of how to record the result, is in it. Cut by Codex, as it was, none of that held.
func TestCodexIsGivenThePreparedWorkWithinItsLimitWhereClaudeCodeIsGivenMore(t *testing.T) {
	repo, root, _ := planningFixture(t)
	japaneseArticles(t, notebook.Book{Root: root, Scope: feedbackScope(repo)}, 6)
	request := japaneseRequest + strings.Repeat("\u5fa9\u5143\u306e\u6761\u4ef6\u3068\u6240\u6709\u8005\u306e\u627f\u8a8d\u3092\u4fdd\u3064\u3053\u3068\u3002", 20)

	actAs(t, "claude")
	claude := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning-claude", "cwd": repo, "prompt": request}))
	if preparedOf(t, claude) == nil || codexSeesWhole(claude) || hookContextLength(claude) > hookContextMax {
		t.Fatalf("precondition: Claude Code is given a preparation of up to 10,000 characters, which Codex would cut (%d bytes, %d characters)", len(claude), hookContextLength(claude))
	}

	actAs(t, "codex")
	codex := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning-codex", "cwd": repo, "prompt": request}))
	v := preparedOf(t, codex)
	if v == nil {
		t.Fatalf("Codex is given the prepared work too: %.400s", codex)
	}
	if len(codex) > codexHookContextMax || !codexSeesWhole(codex) || strings.Contains(codex, "tokens truncated") {
		t.Fatalf("what Codex is given is %d bytes, within its %d: Codex counts %d tokens against 2,500", len(codex), codexHookContextMax, (len(codex)+3)/4)
	}
	if how, _ := v["how_to_record"].(string); how == "" || v["receipt"] == nil {
		t.Fatalf("the instruction of how to record the result and the receipt reach Codex: %v", v["how_to_record"])
	}
	if i := strings.Index(codex, "shikii prepared work:\n"); !strings.HasPrefix(codex[i+len("shikii prepared work:\n"):], `{"receipt":`) {
		t.Fatalf("the receipt comes first, before anything a cut could take")
	}
	if output, _ := v["output"].(map[string]any); output == nil || output["limited"] != true {
		t.Fatal("precondition: the lessons do not all fit, so that the limit decides where the prepared work stops")
	}
}

// A request longer than what Codex shows with the rest comes as its head, counted in bytes: a Japanese request that Claude Code is shown
// in the 2,000 characters of its head is, for Codex, 1,960 bytes of it, and the work still arrives whole.
func TestCodexIsGivenTheHeadOfALongJapaneseRequestWithinItsLimit(t *testing.T) {
	repo, root, _ := planningFixture(t)
	japaneseArticles(t, notebook.Book{Root: root, Scope: feedbackScope(repo)}, 3)
	request := japaneseRequest + strings.Repeat("\u5fa9\u5143\u306e\u6761\u4ef6\u3068\u6240\u6709\u8005\u306e\u627f\u8a8d\u3092\u4fdd\u3064\u3053\u3068\u3002", 120)
	actAs(t, "codex")
	codex := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning-codex", "cwd": repo, "prompt": request}))
	v := preparedOf(t, codex)
	if v == nil || len(codex) > codexHookContextMax || !codexSeesWhole(codex) {
		t.Fatalf("the preparation is delivered within Codex's limit (%d bytes): %.300s", len(codex), codex)
	}
	if how, _ := v["how_to_record"].(string); how == "" {
		t.Fatal("how_to_record reaches Codex")
	}
}

// addJapaneseAlwaysRules saves n rules for every request, each of lessonRunes Japanese characters ending with its own number.
func addJapaneseAlwaysRules(t *testing.T, book notebook.Book, draft notebook.Draft, n, lessonRunes int) {
	t.Helper()
	for i := 0; i < n; i++ {
		d := draft
		d.ID, d.Kind, d.Scene, d.AppliesTo = "", "rule", "\u3044\u3064\u3082", "every request"
		d.Title = fmt.Sprintf("Always rule %c", 'A'+i)
		d.Lesson = "cmd/shikii/version.go " + strings.Repeat("\u6c7a\u307e\u308a\u3092\u5b88\u308b\u3002", lessonRunes/6) + fmt.Sprintf("END%d", i)
		if _, err := book.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
}

// The brief of the session start, in Japanese rules of 500 characters each: Claude Code is shown all 8 whole (about 12,000 bytes, 4,000
// characters); Codex is shown the rules that fit its 9,800 bytes, whole, with the notice of the memory nap that comes after them, and the
// rest named by their ids to read with shikii wiki query. Cut by Codex, the rules in the middle were gone.
func TestCodexIsGivenTheSessionBriefAndTheNoticeWithinItsLimit(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	addJapaneseAlwaysRules(t, notebook.Book{Root: st.Dir, Scope: scope}, draft, 8, 500)
	saveWorkResults(t, st, scope, 0, 6, time.Now())

	rules, err := (notebook.Book{Root: st.Dir, Scope: scope}).AlwaysRules()
	if err != nil || len(rules) != 8 {
		t.Fatalf("precondition: 8 always rules: %d %v", len(rules), err)
	}
	if claude := briefOf(rules, "", "", hookBoundFor("claude"), 0); len(claude) < 11500 || strings.Contains(claude, "more rules") {
		t.Fatalf("precondition: Claude Code is given all 8 rules, %d bytes, which Codex would cut: %.200s", len(claude), claude)
	}

	actAs(t, "codex")
	ctx := noticeContext(t, startSession(t, scope, nil))
	if len(ctx) > codexHookContextMax || !codexSeesWhole(ctx) {
		t.Fatalf("the brief and the notice are %d bytes, over what Codex shows (%d)", len(ctx), codexHookContextMax)
	}
	if !strings.Contains(ctx, "shikii: a memory nap is due (6 work results so far).") {
		t.Fatalf("the notice comes with the brief within the limit:\n%.300s", ctx)
	}
	// the rules come in the order the wiki lists them: the first ones whole, the others only by their ids in the last line
	whole := 0
	for _, r := range rules {
		if strings.Contains(ctx, "(lesson:"+r.ID+":"+r.Revision+")") {
			if whole < len(rules) && !strings.Contains(ctx, r.Lesson) {
				t.Fatalf("rule %s is listed but not whole", r.ID)
			}
			whole++
			continue
		}
		break
	}
	if whole < 3 || whole == 8 || !strings.Contains(ctx, fmt.Sprintf("- %d more rules for every request, each read with shikii wiki query lesson:<id>: lesson:%s", 8-whole, rules[whole].ID)) {
		t.Fatalf("the first rules that fit come whole (%d of 8) and the rest are named by their ids:\n%.400s", whole, ctx[max(0, len(ctx)-600):])
	}
	for _, r := range rules[whole:] {
		if strings.Contains(ctx, "(lesson:"+r.ID+":"+r.Revision+")") || !strings.Contains(ctx, "lesson:"+r.ID) {
			t.Fatalf("rule %s is named by its id and not listed", r.ID)
		}
	}
}

// What a hook says to Claude Code is not changed by Codex's limit: where the brief is within Claude Code's 10,000 characters, as 8 rules
// of 500 Japanese characters are, it is all 8 whole.
func TestClaudeCodeIsStillGivenAllRulesWholeWhereCodexIsGivenFewer(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	addJapaneseAlwaysRules(t, notebook.Book{Root: st.Dir, Scope: scope}, draft, 8, 500)
	actAs(t, "claude")
	ctx := noticeContext(t, startSession(t, scope, nil))
	for i := 0; i < 8; i++ {
		if !strings.Contains(ctx, fmt.Sprintf("END%d (lesson:", i)) {
			t.Fatalf("Claude Code is shown rule %d whole:\n%.300s", i, ctx)
		}
	}
	if hookContextLength(ctx) > hookContextMax || strings.Contains(ctx, "more rules for every request") {
		t.Fatalf("within Claude Code's limit with every rule listed: %d characters", hookContextLength(ctx))
	}
}

// The other texts the hook says at an event are small beside Codex's limit even where the lessons are Japanese: the lessons a subagent is
// given at its start and the lessons right before a command are quotes the hook cuts to a few hundred characters each. This makes the
// largest that 8 rules of 500 Japanese characters, a request that names them and a command that names a lesson can make, and holds each
// under Codex's limit, so that a change that made one longer is found.
func TestTheOtherEventsStayWithinCodexsLimitInJapanese(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	addJapaneseAlwaysRules(t, book, draft, 8, 500)
	d := draft
	d.ID, d.Kind, d.Scene, d.AppliesTo, d.Title = "", "rule", "when cmd/shikii/version.go changes", "when cmd/shikii/version.go changes", "Bump the version files"
	d.Lesson = "cmd/shikii/version.go " + strings.Repeat("\u30c7\u30d7\u30ed\u30a4\u306e\u524d\u306b\u78ba\u8a8d\u3059\u308b\u3002", 50)
	if _, err := book.Ingest(d); err != nil {
		t.Fatal(err)
	}
	actAs(t, "codex")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s-other", "cwd": scope, "prompt": "cmd/shikii/version.go " + japaneseRequest})
	for name, out := range map[string]string{
		"subagent-start": runHook(t, "subagent-start", map[string]any{"session_id": "s-other", "cwd": scope, "hook_event_name": "SubagentStart", "agent_id": "child-1", "agent_type": "general-purpose"}),
		"tool-lesson":    runHook(t, "tool-lesson", map[string]any{"session_id": "s-other", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}}),
	} {
		ctx := feedbackContext(t, out)
		t.Logf("%s: %d bytes", name, len(ctx))
		if ctx == "" {
			t.Errorf("precondition: %s says something here", name)
		}
		if len(ctx) > codexHookContextMax || !codexSeesWhole(ctx) {
			t.Errorf("%s says %d bytes to Codex, over its %d", name, len(ctx), codexHookContextMax)
		}
	}
}
