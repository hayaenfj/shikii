package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.11 item H (end condition 7-1): right before an edit or a command, one lesson whose page names that file or command arrives
// (PreToolUse, hook tool-lesson); nothing arrives for a file or command no lesson names.

const (
	versionLessonText = "When cmd/shikii/version.go changes, update the version badges in README.md and docs/readme/README_ja.md too."
	// Japanese around the command words, with no space on one side, as the project's own lessons are written
	commitLessonText = "検査は git commit の前に && でつなぎ、| tail を付けない(終了コードが消える)。後でgh releaseを出す。"
	toolPrompt       = "make the colours of the page softer"
)

// toolLessonFixture is a project with a lesson that names version.go, one that names git commit, and the README lesson of
// memoryEffectFixture that names neither.
func toolLessonFixture(t *testing.T) (store.Store, string, http.Handler, map[string]string) {
	t.Helper()
	st, scope, h, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	ids := map[string]string{}
	for name, d := range map[string]notebook.Draft{
		"version": {Title: "Bump the README badges with the version", Lesson: versionLessonText, AppliesTo: "when bumping shikii's version", Kind: "rule", Scene: "when bumping the version"},
		"commit":  {Title: "Check the exit code before committing", Lesson: commitLessonText, AppliesTo: "when committing in shikii", Kind: "rule", Scene: "when committing"},
	} {
		d.Sources = draft.Sources
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = "lesson:" + r.Page.ID + ":" + r.Page.Revision
	}
	return st, scope, h, ids
}

// toolHook starts a request in session (unless prompt is empty) and runs the PreToolUse hook for one tool call.
func toolHook(t *testing.T, scope, session, prompt, tool string, input map[string]any) string {
	t.Helper()
	if prompt != "" {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": prompt})
	}
	out := runHook(t, "tool-lesson", map[string]any{"session_id": session, "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
	assertOnlyContext(t, out)
	return out
}

// assertOnlyContext is an allowlist on the hook's output: nothing, or exactly {"hookSpecificOutput": {"hookEventName": "PreToolUse",
// "additionalContext": ...}}. Any other key (permissionDecision, decision, continue, systemMessage, updatedInput...) fails.
func assertOnlyContext(t *testing.T, out string) {
	t.Helper()
	if out == "" {
		return
	}
	var v map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("the output is one JSON object with only hookSpecificOutput: %v\n%s", err, out)
	}
	spec, ok := v["hookSpecificOutput"]
	if len(v) != 1 || !ok || len(spec) != 2 || spec["hookEventName"] != "PreToolUse" {
		t.Fatalf("the output only adds context; it never allows, denies, asks or changes the tool call:\n%s", out)
	}
	if c, ok := spec["additionalContext"].(string); !ok || c == "" {
		t.Fatalf("the only other field is a non-empty additionalContext:\n%s", out)
	}
}

func lessonKeys(ctx string) int { return strings.Count(ctx, `"key":"lesson:`) }

func TestToolLessonBeforeEditingNamedFile(t *testing.T) {
	_, scope, _, ids := toolLessonFixture(t)
	ctx := feedbackContext(t, toolHook(t, scope, "edit", toolPrompt, "Edit", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go"), "old_string": "0.10.1", "new_string": "0.11.0"}))
	if !strings.Contains(ctx, ids["version"]) || !strings.Contains(ctx, "README.md") || lessonKeys(ctx) != 1 || !strings.Contains(ctx, "version.go") {
		t.Fatalf("an edit of version.go gets the one lesson that names it:\n%s", ctx)
	}
	ctx = feedbackContext(t, toolHook(t, scope, "write", toolPrompt, "Write", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go"), "content": "package main"}))
	if !strings.Contains(ctx, ids["version"]) || lessonKeys(ctx) != 1 {
		t.Fatalf("writing the whole file is the same moment:\n%s", ctx)
	}
}

func TestToolLessonBeforeGitCommit(t *testing.T) {
	_, scope, _, ids := toolLessonFixture(t)
	for i, command := range []string{
		`test -z "$(gofmt -l .)" && scripts/check-readme.sh && git -c user.name=a -c user.email=b commit -m "fix: x"`,
		"/usr/bin/git commit -q -F msg.txt",
		"GIT_AUTHOR_NAME=a git commit --amend",
	} {
		session := "commit" + string(rune('a'+i))
		ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Bash", map[string]any{"command": command, "description": "commit"}))
		if !strings.Contains(ctx, ids["commit"]) || lessonKeys(ctx) != 1 || !strings.Contains(ctx, "git commit") {
			t.Fatalf("%q gets the lesson that names git commit:\n%s", command, ctx)
		}
	}
	ctx := feedbackContext(t, toolHook(t, scope, "release", toolPrompt, "Bash", map[string]any{"command": "gh release create v0.11.0 --draft"}))
	if !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("a lesson naming gh release with Japanese right next to it arrives before gh release:\n%s", ctx)
	}
}

func TestToolLessonNothingForUnrelatedTargets(t *testing.T) {
	_, scope, _, _ := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "none", "cwd": scope, "prompt": toolPrompt})
	cases := []struct {
		tool  string
		input map[string]any
	}{
		{"Edit", map[string]any{"file_path": filepath.Join(scope, "internal", "version.go")}},          // another version.go than the one the lesson names
		{"Edit", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "myversion.go")}},   // the name only ends the same
		{"Edit", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go.bak")}}, // the name only starts the same
		{"MultiEdit", map[string]any{"file_path": filepath.Join(scope, "web", "src", "app.js")}},
		{"NotebookEdit", map[string]any{"notebook_path": filepath.Join(scope, "notes.ipynb")}},
		{"Read", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}}, // reading is not about to change it
		{"Bash", map[string]any{"command": "git status --short"}},
		{"Bash", map[string]any{"command": `echo "git commit" && ls cmd/shikii/version.go`}}, // named only as arguments
		{"Bash", map[string]any{"command": "gofmt -l ."}},
		{"Bash", map[string]any{"command": "git log --oneline -3"}},
	}
	for _, c := range cases {
		if out := toolHook(t, scope, "none", "", c.tool, c.input); out != "" {
			t.Fatalf("%s %v names no lesson's file or command, and gets nothing:\n%s", c.tool, c.input, out)
		}
	}
	// before any request in the session there is no turn to assess the delivery with: nothing
	if out := toolHook(t, scope, "fresh", "", "Bash", map[string]any{"command": "git commit -m x"}); out != "" {
		t.Fatalf("without a request in the session nothing is delivered:\n%s", out)
	}
}

func TestToolLessonNotRepeatedInSession(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	if ctx := feedbackContext(t, toolHook(t, scope, "repeat", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("precondition: the first edit gets the lesson:\n%s", ctx)
	}
	if out := toolHook(t, scope, "repeat", "", "Edit", edit); out != "" {
		t.Fatalf("the same revision is not delivered again in the session within the hour:\n%s", out)
	}
	if out := toolHook(t, scope, "repeat", "now bump the version for real", "Write", edit); out != "" {
		t.Fatalf("not even on the next request or through another tool:\n%s", out)
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "other", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("another session gets it:\n%s", ctx)
	}
	// each lesson once per session, whichever tool call it comes before
	if out := toolHook(t, scope, "other", "", "Bash", map[string]any{"command": "git commit -m x"}); !strings.Contains(out, ids["commit"]) {
		t.Fatalf("precondition: the commit lesson arrives once:\n%s", out)
	}
	if out := toolHook(t, scope, "other", "", "Bash", map[string]any{"command": "git commit --amend"}); out != "" {
		t.Fatalf("and only once:\n%s", out)
	}
	sess := loadSession(st.Dir, "other")
	found := 0
	for _, m := range sess.Memories {
		if m.Key == ids["version"] || m.Key == ids["commit"] {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("the deliveries go to the session's record of delivered revisions: %+v", sess.Memories)
	}
}

// The hook is installed for Claude Code only, as its own PreToolUse group for the edit tools and Bash, and the output never decides.
func TestToolLessonInstalledForClaudeCodeOnly(t *testing.T) {
	_, scope, _, _ := toolLessonFixture(t)
	home, _ := os.UserHomeDir()
	var cfg map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(home, ".claude", "settings.json")), &cfg); err != nil {
		t.Fatal(err)
	}
	group := false
	for _, g := range cfg["hooks"].(map[string]any)["PreToolUse"].([]any) {
		gm := g.(map[string]any)
		for _, h := range gm["hooks"].([]any) {
			if strings.HasSuffix(h.(map[string]any)["command"].(string), " hook tool-lesson") {
				group = gm["matcher"] == "Edit|Write|MultiEdit|NotebookEdit|Bash" && len(gm["hooks"].([]any)) == 1
			}
		}
	}
	if !group {
		t.Fatalf("tool-lesson is its own PreToolUse group for Edit|Write|MultiEdit|NotebookEdit|Bash:\n%v", cfg["hooks"])
	}
	if codex := hookCommands(filepath.Join(home, ".codex", "hooks.json")); strings.Contains(strings.Join(codex, " "), "tool-lesson") {
		t.Fatalf("Codex does not get tool-lesson: %v", codex)
	}
	// broken input, an unknown tool, a tool without input: nothing, and never a decision
	for _, in := range []map[string]any{
		{"session_id": "odd", "cwd": scope, "tool_name": "Bash"},
		{"session_id": "odd", "cwd": scope, "tool_name": "Bash", "tool_input": "git commit"},
		{"session_id": "odd", "cwd": scope, "tool_name": "Unknown", "tool_input": map[string]any{"command": "git commit"}},
	} {
		if out := runHook(t, "tool-lesson", in); out != "" {
			t.Fatalf("odd input gets nothing: %v\n%s", in, out)
		}
	}
}

// The delivery is recorded like other quotes: the reflection sees it with the conversation it happened in, it can be assessed, and
// status counts it.
func TestToolLessonIsAssessedInReflection(t *testing.T) {
	st, scope, h, ids := toolLessonFixture(t)
	ctx := feedbackContext(t, toolHook(t, scope, "assess", toolPrompt, "Edit", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}))
	if !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("precondition: delivered:\n%s", ctx)
	}
	runHook(t, "stop", map[string]any{"session_id": "assess", "cwd": scope, "last_assistant_message": "Bumped the version and the README badges."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "assess", "cwd": scope, "prompt": "now look at the release notes"})
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	source := ""
	sources, _ := q["sources"].([]any)
	for _, s := range sources {
		memories, _ := s.(map[string]any)["memories"].([]any)
		for _, m := range memories {
			m := m.(map[string]any)
			quote, _ := m["quote"].(map[string]any)
			if tool, _ := m["tool"].(string); quote["key"] == ids["version"] && strings.Contains(tool, "version.go") {
				source, _ = m["source_id"].(string)
			}
		}
	}
	if source == "" {
		t.Fatalf("the reflection sees the delivery with the tool moment it was made at: %v", q)
	}
	result := memoryEffectSubmit(t, h, source, ids["version"], "helpful", "updated both README badges with version.go", "the diff changes the badges in README.md and README_ja.md")
	receipts, _ := result["memory"].([]any)
	if len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil {
		t.Fatalf("the delivery can be assessed: %v", result)
	}
	var b strings.Builder
	printMemoryEffect(&b, st, scope, time.Now())
	if !strings.Contains(b.String(), "right before an edit or a command 1 (helpful 1)") {
		t.Fatalf("status counts the deliveries made right before a tool:\n%s", b.String())
	}
}
