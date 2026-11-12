package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Tests of audit F of 0.31.0 (third round; the audit stopped before its report, these are its findings as tests). Each failed on
// the candidate 4b231ef.

const auditFSession = "0f4c1e2a-7b3d-4c5e-8f90-123456789abc" // 36 bytes, as a real session id

func auditFStop(t *testing.T, repo, session string, active bool) (string, bool) {
	t.Helper()
	in := map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."}
	if active {
		in["stop_hook_active"] = true
	}
	return stopReason(t, runUnreviewedHook(t, "stop", in))
}

// auditFSecondShort runs a first request to its end and then sends a short message that is not a bare acknowledgement. It returns
// the ids line of the second notice.
func auditFSecondShort(t *testing.T, repo string) string {
	t.Helper()
	ids := noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditFSession, "cwd": repo, "prompt": planningTestRequest})))
	prepared, err := agentCLI(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(prepared["receipt"])
	if saved, err := agentCLI(t, `{"receipt":`+string(receipt)+`,"result":{"changed":"Nothing.","checked":"Read."}}`, "work", "record"); err != nil || saved["complete"] != true {
		t.Fatal(err, saved)
	}
	if reason, blocked := auditFStop(t, repo, auditFSession, false); blocked {
		t.Fatal(reason)
	}
	return noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditFSession, "cwd": repo, "prompt": "Use tabs there."})))
}

func auditFLessons(t *testing.T, b notebook.Book, n int, bodyRepeat int) []notebook.Page {
	t.Helper()
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	var pages []notebook.Page
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", bodyRepeat)
		applies := fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 6)
		page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("CSV export filters %d", i), Lesson: "Keep the filters when exporting CSV.", AppliesTo: applies, Kind: "rule", Scene: "when exporting CSV", Body: &body})
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, *page.Page)
	}
	return pages
}

// INVARIANT 5, guide.md "reflect query keeps its reply within what an agent's tool shows": the first pending conversation was a
// turn that started many subagents, and each delivery to a subagent is assessed under the parent conversation. On 4b231ef a head
// kept the cut texts of every delivery and the reply was over the CLI's bound; the deliveries now keep their keys only, and the
// reply fits.
func TestAuditFReflectQueryWithManySubagentDeliveries(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Kind, always.Scene, always.AppliesTo = "", "Visible text in Japanese", "Write every visible sentence, reports included, in Japanese.", "rule", "\u3044\u3064\u3082", "every request"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always); err != nil {
		t.Fatal(err)
	}
	const prompt = "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": prompt})
	const subagents = 60
	for i := 0; i < subagents; i++ {
		startSubagent(t, "s1", scope, fmt.Sprintf("agent-%d", i), "general-purpose", prompt)
	}
	memoryEffectComplete(t, st, scope, "s1")
	view, err := reflectQueryWith(st.Dir, scope, reflectHeadsMax)
	if err != nil {
		t.Fatal(err)
	}
	heavy := -1
	for i, s := range view.Sources {
		t.Logf("pending conversation %d: %d deliveries to assess, %d bytes whole, %d bytes as a head", i, len(s.Memories), workContextSize(s), workContextSize(reflectHead(s)))
		if len(s.Memories) >= subagents {
			heavy = i
		}
	}
	if heavy < 0 {
		t.Fatalf("precondition: the turn that started %d subagents has their deliveries to assess", subagents)
	}
	// (1) as the hooks leave it: an older conversation is still pending, so the heavy one can wait
	t.Chdir(scope)
	out := capture(t, func() error { return cmdReflect([]string{"query"}) })
	var reply map[string]any
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("%v: %.200s", err, out)
	}
	sources, _ := reply["sources"].([]any)
	output, _ := reply["output"].(map[string]any)
	t.Logf("with an older conversation pending: shikii reflect query is %d bytes of %d, %d listed, more_pending=%v, omitted=%v", len(out), cliPrepareBytes, len(sources), reply["more_pending"], output["omitted"])
	if len(out) > cliPrepareBytes || len(sources) == 0 {
		t.Errorf("with an older conversation pending, the CLI's reflect query is %d bytes of %d with %d listed", len(out), cliPrepareBytes, len(sources))
	}
	// (2) once the conversations before it are settled, it is the first: the shape the shipped test of the long request uses
	first := view
	first.Sources = append([]reflectSource{}, view.Sources[heavy:]...)
	for name, c := range map[string]struct {
		bound int
		size  func(any) int
	}{
		"the CLI (28672 bytes)": {cliPrepareBytes, func(v any) int { return workContextSize(v) + 1 }},
	} {
		raw, _ := json.Marshal(limitReflectQuery(first, c.bound, c.size))
		var limited map[string]any
		_ = json.Unmarshal(raw, &limited)
		listed, _ := limited["sources"].([]any)
		output, _ := limited["output"].(map[string]any)
		memories := 0
		if len(listed) > 0 {
			list, _ := listed[0].(map[string]any)["memories"].([]any)
			memories = len(list)
		}
		t.Logf("with the conversation of %d subagents first, %s: %d bytes, %d listed, the first with %d deliveries (head=%v)\n   output.omitted=%v", subagents, name, len(raw)+1, len(listed), memories, len(listed) > 0 && listed[0].(map[string]any)["head"] == true, output["omitted"])
		if len(raw)+1 > c.bound {
			t.Errorf("INVARIANT 5: %s: the reply of reflect query is %d bytes, over the bound: the %d deliveries of the first conversation alone do not fit, and nothing more is taken off", name, len(raw)+1, memories)
		}
	}
}

// RED: work record called without a receipt (the ids of the notice sent to record, the help's <receipt> left as it is, or the
// field left out). The refusal is "invalid work receipt": it does not say that record takes the receipt of work prepare, or what
// to send next. The changelog says a call shikii cannot accept is answered with the cause and what to send instead.
func TestAuditFRecordWithoutAReceiptIsToldWhatToSend(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	ids := noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditFSession, "cwd": repo, "prompt": planningTestRequest})))
	for name, input := range map[string]string{
		"the ids of the notice sent to work record":     strings.TrimSuffix(ids, "}") + `,"result":{"changed":"Nothing.","checked":"Read."}}`,
		"no receipt at all":                             `{"result":{"changed":"Nothing.","checked":"Read."}}`,
		"the help's form with <receipt> left as it is":  `{"receipt":"<receipt>","result":{"changed":"<what changed>","checked":"<what you checked and how>"}}`,
		"the help's third form sent before any prepare": `{"receipt":"<receipt>","query":"<other topic words>"}`,
	} {
		operation := "record"
		if strings.Contains(input, `"query"`) {
			operation = "prepare"
		}
		_, _, err := auditDCLI(t, input, "work", operation)
		t.Logf("%s -> %v", name, err)
		if err == nil {
			t.Errorf("%s: the call is taken", name)
			continue
		}
		if !strings.Contains(err.Error(), "prepare") {
			t.Errorf("%s: the refusal %q does not say what to send (the receipt of the reply of work prepare)", name, err)
		}
	}
}

// RED: the Stop block says "add agreed and open to work record when the person agreed on something or a choice is open". Neither
// that line nor how_to_record gives their shape, and the refusal of a quote whose reference is not one of the reply's does not
// list the references that are (the refusals of memory, review and source references do).
func TestAuditFAgreedRefusalsNameTheShapeAndTheReferences(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	ids := noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditFSession, "cwd": repo, "prompt": planningTestRequest})))
	prepared, _, err := auditDCLI(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt := auditJSON(prepared["receipt"])
	saved, _, err := auditDCLI(t, `{"receipt":`+receipt+`,"agreed":{"summary":"Keep the filters."}}`, "work", "record")
	first := auditJSON(saved["errors"])
	t.Logf("agreed with a summary alone: err=%v %s", err, first)
	if !strings.Contains(first, "quotes") {
		t.Errorf("the refusal of agreed without quotes does not name the field quotes or its shape {ref,text}: %s", first)
	}
	// the source key of the older entry (hook plan submit: current_user), which the reply of work prepare also shows in conversation_context
	saved, _, err = auditDCLI(t, `{"receipt":`+receipt+`,"agreed":{"summary":"Keep the filters.","quotes":[{"ref":"current_user","text":"keep the existing filters"}]}}`, "work", "record")
	second := auditJSON(saved["errors"])
	t.Logf("a quote with ref current_user: err=%v %s", err, second)
	if !strings.Contains(second, "unknown prepared quotation reference") {
		t.Fatalf("precondition: the quote reference is refused: %s", second)
	}
	if !strings.Contains(second, "the references here: current") {
		t.Errorf("the refusal of a quote reference does not list the references the reply has (current, previous, knowledgeN): %s", second)
	}
	saved, _, err = auditDCLI(t, `{"receipt":`+receipt+`,"agreed":{"summary":"Keep the filters.","quotes":[{"ref":"current","text":"keep the existing filters"}]}}`, "work", "record")
	if raw := auditJSON(saved["errors"]); err != nil || strings.Contains(raw, "kind\":\"plan") {
		t.Errorf("negative: the quote the refusal names (ref current) is taken: %v %s", err, raw)
	}
}

// RED (documents): integrations/claude-code.md and its Japanese version say the hook points the agent to hook plan query /
// submit. The notice of every request says "Do not run the legacy hook plan query: it replaces the receipt".
func TestAuditFIntegrationDocumentNamesWorkPrepare(t *testing.T) {
	notice := planningNotice("s", "0123456789abcdef")
	if !strings.Contains(notice, "Do not run the legacy hook plan query") || !strings.Contains(notice, "work prepare") {
		t.Fatalf("precondition: the notice asks for work prepare and keeps hook plan query out: %s", notice)
	}
	// every document that tells what the hook asks the agent to run (the guides, the integrations, the architecture)
	docs, _ := filepath.Glob("../../integrations/*.md")
	more, _ := filepath.Glob("../../docs/*.md")
	for _, path := range append(docs, more...) {
		for i, line := range strings.Split(string(mustRead(t, path)), "\n") {
			if strings.Contains(line, "hook plan query / submit`") && !strings.Contains(line, "work prepare") {
				t.Errorf("%s:%d says the hook points the agent to hook plan query / submit, and does not name work prepare: %.260s", path, i+1, line)
			}
		}
	}
	for _, c := range []struct{ path, needle string }{
		{"../../docs/guide.md", "`hook plan query` before work can pick the bodies you need with `pages`"},
		{"../../skills/shikii/references/review.md", "`hook plan query` may include this same reflection view. Reuse it and attach the normal submit object under `reflection` in `hook plan submit`"},
	} {
		for i, line := range strings.Split(string(mustRead(t, c.path)), "\n") {
			if strings.Contains(line, c.needle) && !strings.Contains(line, "older") && !strings.Contains(line, "receipt") {
				t.Errorf("%s:%d names hook plan query as a call to run before or beside the work, without saying it is the older entry or what it does to the receipt: %.200s", c.path, i+1, line)
			}
		}
	}
}

// GREEN (README, Updates): "Without hooks, rerun shikii --init in an initialized project instead: it adds the same line and
// leaves the hooks off."
func TestAuditFInitWithoutHooksAddsTheCodexLineAndLeavesTheHooksOff(t *testing.T) {
	home := freshMachine(t)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	capture(t, func() error { return cmdInit(nil) }) // the first project: hooks, the registration with the line, the note
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if got := agentsWithShikiiHook(home); len(got) != 0 {
		t.Fatalf("precondition: the hooks are off: %v", got)
	}
	// the machine as 0.30.8 left it: the three lines shikii wrote, no approval line, no note
	path := filepath.Join(home, ".codex", "config.toml")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), codexToolsAllowedLine) {
		t.Fatalf("precondition: the registration with the line: %v\n%s", err, b)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), codexToolsAllowedLine+"\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(codexToolsAllowedMark()); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	after, _ := os.ReadFile(path)
	t.Logf("after shikii --init in the initialized project: the line is there: %v; hooks: %v", strings.Contains(string(after), codexToolsAllowedLine), agentsWithShikiiHook(home))
	if !strings.Contains(string(after), codexToolsAllowedLine) || len(agentsWithShikiiHook(home)) != 0 || !strings.Contains(out, "hook install --yes") {
		t.Errorf("README: init without hooks adds the line, leaves the hooks off and names the command:\n%s\n%s", after, out)
	}
}

// planning.md's minimal record for a short message, with the receipt of the prepare its Stop names, settles the Stop. Audit F
// found the minimal form written with the hook's ids: work record refused it, and hook plan submit left the Stop blocked.
func TestAuditFPlanningMinimalRecordSettlesAShortMessage(t *testing.T) {
	planning := string(mustRead(t, "../../skills/shikii/references/planning.md"))
	at := strings.Index(planning, "the minimal `")
	if at < 0 || !strings.Contains(planning[at:at+400], "is enough") {
		t.Fatal("planning.md no longer has the minimal record")
	}
	minimal := planning[at+len("the minimal `"):]
	minimal = minimal[:strings.Index(minimal, "`")]
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	ids := auditFSecondShort(t, repo)
	if reason, blocked := auditFStop(t, repo, auditFSession, false); !blocked || !strings.Contains(reason, "work record") {
		t.Fatalf("precondition: the Stop of the short message asks for work record: %s", reason)
	}
	prepared, err := agentCLI(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export tabs"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := prepared["receipt"].(string)
	body := strings.ReplaceAll(minimal, "<receipt>", receipt)
	if !json.Valid([]byte(body)) || !strings.Contains(body, receipt) {
		t.Fatalf("the minimal record with the receipt filled in is not JSON with the receipt: %s", body)
	}
	saved, err := agentCLI(t, body, "work", "record")
	if err != nil {
		t.Fatalf("planning.md's minimal record is refused by work record: %v", err)
	}
	t.Logf("saved: %.200s", auditJSON(saved["saved"]))
	if reason, blocked := auditFStop(t, repo, auditFSession, true); blocked {
		t.Errorf("planning.md says the minimal record is enough; after it the Stop is still blocked: %s", reason)
	}
	_ = root
}

// recording.md says a receipt without hook ids records the same review citing newN or knowledgeN, and has no current. Its
// example of an explicit judgment, with current taken out, is taken on such a receipt; with current it is refused and the
// refusal does not offer current (audit F of 0.31.0 found the sentence without the exception).
func TestAuditFRecordingReviewOnAReceiptWithoutHookIDs(t *testing.T) {
	text := string(mustRead(t, "../../skills/shikii/references/recording.md"))
	if !strings.Contains(text, "agent-supplied preparation can record the same review, citing `newN` or\n`knowledgeN` (it has no `current`)") {
		t.Fatal("recording.md no longer says what a receipt without hook ids cites")
	}
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, `{"receipt":"<receipt>","reasoning":{`) {
			start = i
		}
	}
	if start < 0 {
		t.Fatal("recording.md no longer has the example of an explicit judgment")
	}
	var example []string
	for i := start; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
		example = append(example, lines[i])
	}
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	auditFLessons(t, notebook.Book{Root: root, Scope: feedbackScope(repo)}, 1, 2)
	prepared, err := agentCLI(t, `{"query":"CSV export filters","request":"Check the CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := prepared["receipt"].(string)
	body := strings.ReplaceAll(strings.Join(example, "\n"), "<receipt>", receipt)
	refused, err := agentCLI(t, body, "work", "record")
	if raw := auditJSON(refused["errors"]); err != nil || !strings.Contains(raw, `\"current\"`) || strings.Contains(raw, "usable here: current") {
		t.Errorf("with current, the example is refused on a receipt without hook ids, and the refusal does not offer current: %v %s", err, raw)
	}
	saved, err := agentCLI(t, strings.Replace(body, `"current",`, "", 1), "work", "record")
	if err != nil || saved["complete"] != true {
		t.Errorf("the example citing knowledge1 alone, as recording.md says such a receipt does, is refused: %v %s", err, auditJSON(saved["errors"]))
	}
}

// The help says a request longer than the CLI's bound is not cut, and how much of a request the hook keeps. A request over that
// comes cut to it, and task_context says so (audit F of 0.31.0 found the sentence without the hook's limit).
func TestAuditFTheHelpNamesWhatTheHookKeepsOfARequest(t *testing.T) {
	help := agentUsages["work"].text()
	if !strings.Contains(help, "A request longer than that is not cut: the reply grows to hold it (the hook keeps 64 KiB of a request; task_context says when it kept less).") {
		t.Fatalf("the help does not name what the hook keeps of a request: %s", help)
	}
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prompt := "Use the agreed CSV columns and keep the existing filters. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", (70<<10)/56) + "LAST LINE: also rename the button."
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditFSession, "cwd": repo, "prompt": prompt}))
	prepared, err := agentCLI(t, strings.TrimSuffix(noticeIDs(t, notice), "}")+`,"query":"CSV export filters"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := prepared["task"].(string)
	context, _ := prepared["task_context"].(map[string]any)
	if len(task) != 64<<10 || !strings.HasPrefix(prompt, task) || context["text_status"] != "stored_request_truncated" || context["next_action"] == nil {
		t.Errorf("a request of %d bytes comes as its first 64 KiB, and task_context says it was cut: task %d bytes, status %v", len(prompt), len(task), context["text_status"])
	}
}
