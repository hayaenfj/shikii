package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Found by sending one small request to a real agent (Claude Code) with shikii's hooks, against the binary before and after the
// other changes of this series (2026-11-12). In both runs the agent ran work prepare and hook plan query in one command, because
// the notice named both; the second replaces the receipt of the first. It then tried to finish without recording, and the Stop
// block asked for both flows at once (hook plan query, then submit, for the plan; work prepare, then work record, for the review),
// so the agent ran hook plan submit, had its work record refused three times (the request's ids beside the receipt, the receipt no
// longer current, a sentence in evidence) and prepared a second time. One flow is asked for now, and those refusals are gone or say
// what to send.
func TestTheNoticeAndTheStopBlockAskForOneFlow(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	cli := func(input string, args ...string) (map[string]any, error) {
		t.Helper()
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cliCall(args) }) })
		var reply map[string]any
		if err == nil && json.Unmarshal([]byte(out), &reply) != nil {
			t.Fatalf("not JSON: %s", out)
		}
		return reply, err
	}
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest}))
	for _, w := range []string{"Do not run the legacy hook plan query or hook plan submit (how_to_submit)", "they replace the receipt", "result:{changed,checked?,evidence?:[refs],remaining?}"} {
		if !strings.Contains(notice, w) {
			t.Errorf("the notice lacks %q: %s", w, notice)
		}
	}
	if strings.Contains(notice, "Legacy hook plan query returns") {
		t.Errorf("the notice still invites the second flow: %s", notice)
	}
	turn := loadSession(root, "planning").TurnID
	ids := `"session_id":"planning","turn_id":"` + turn + `"`

	// finishing with a changed file and nothing prepared: the block asks for work prepare and work record, and for nothing else
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}))
	if !blocked {
		t.Fatalf("finishing without the plan and the review is blocked: %s", reason)
	}
	for _, w := range []string{"The plan has not been sorted out, yet 1 file was changed", "the work prepare below sorts it out", "do not also run hook plan query or hook plan submit", "{" + ids + "}",
		"with topic words in query on standard input to ", " work prepare", "result:{changed,checked?,evidence?:[refs],remaining?}", "the same way to work record"} {
		if !strings.Contains(reason, w) {
			t.Errorf("the block lacks %q:\n%s", w, reason)
		}
	}
	for _, w := range []string{"hook plan query, read this", "submit agreed and open, then finish"} {
		if strings.Contains(reason, w) {
			t.Errorf("the block still asks for the second flow (%q):\n%s", w, reason)
		}
	}

	// the agent prepares as told, and finishes again without recording: only the ending is asked for, with the receipt it holds
	prepared, err := cli(`{`+ids+`,"query":"CSV export filters"}`, "work", "prepare")
	if err != nil || prepared["receipt"] == nil {
		t.Fatal(prepared, err)
	}
	reason, blocked = stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done.", "stop_hook_active": true}))
	if !blocked {
		t.Fatalf("finishing without the ending result is blocked: %s", reason)
	}
	for _, w := range []string{"has its prepared start and needs the saved ending result", "the receipt of your work prepare", "on standard input to ", " work record"} {
		if !strings.Contains(reason, w) {
			t.Errorf("the block after preparing lacks %q:\n%s", w, reason)
		}
	}
	for _, w := range []string{"hook plan", "plan has not been sorted out", "with topic words in query on standard input"} {
		if strings.Contains(reason, w) {
			t.Errorf("the block after preparing asks again for what is done (%q):\n%s", w, reason)
		}
	}

	// the request's own ids beside the receipt change nothing, so they are accepted; anything else beside it is named
	receipt, _ := json.Marshal(prepared["receipt"])
	if _, err := cli(`{`+ids+`,"receipt":`+string(receipt)+`,"query":"CSV","result":{"changed":"README.md","checked":"Read it."}}`, "work", "record"); err == nil || !strings.Contains(err.Error(), "leave out query") {
		t.Errorf("a record with query beside the receipt names the field: %v", err)
	}
	if _, err := cli(`{"session_id":"another","turn_id":"`+turn+`","receipt":`+string(receipt)+`,"result":{"changed":"README.md","checked":"Read it."}}`, "work", "record"); err == nil || !strings.Contains(err.Error(), "session_id") || !strings.Contains(err.Error(), "not those of the receipt") {
		t.Errorf("a record with another request's ids is refused with the reason: %v", err)
	}
	// a wrong type is said in the words of the JSON, not of the program
	_, err = cli(`{"receipt":`+string(receipt)+`,"result":{"changed":"README.md","evidence":"I read it"}}`, "work", "record")
	if err == nil || !strings.Contains(err.Error(), "result.evidence") || !strings.Contains(err.Error(), "an array") || !strings.Contains(err.Error(), "got string") || strings.Contains(err.Error(), "Go struct") {
		t.Errorf("a string where a list goes is named with its field and both shapes: %v", err)
	}
	// a sentence in evidence: the refusal says what evidence takes and where the sentence goes
	saved, err := cli(`{`+ids+`,"receipt":`+string(receipt)+`,"result":{"changed":"README.md","checked":"Read it.","evidence":["I read the file back"]}}`, "work", "record")
	if err != nil {
		t.Fatalf("the request's own ids beside the receipt are accepted: %v", err)
	}
	text, _ := json.Marshal(saved["errors"])
	for _, w := range []string{`unknown reasoning evidence \"I read the file back\"`, "evidence takes references, not text", "what you checked goes in checked", "new1"} {
		if !strings.Contains(string(text), w) {
			t.Errorf("the refusal of a sentence in evidence lacks %q: %s", w, text)
		}
	}
	// the report itself was saved and waits for its evidence: sending the whole result again is answered with the short form
	pending := ""
	for _, raw := range saved["errors"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "result" {
			pending, _ = item["id"].(string)
		}
	}
	saved, err = cli(`{"receipt":`+string(receipt)+`,"result":{"changed":"README.md","checked":"Read it.","evidence":["current"]}}`, "work", "record")
	text, _ = json.Marshal(saved["errors"])
	if err != nil || pending == "" || saved["complete"] == true || !strings.Contains(string(text), `result:{ref:\"`+pending+`\",evidence:[refs]}`) {
		t.Errorf("a second whole result while one waits for evidence names the short form with its ref %q: %v %s", pending, err, text)
	}
	// the short form completes the request, and finishing passes
	saved, err = cli(`{"receipt":`+string(receipt)+`,"result":{"ref":"`+pending+`","evidence":["current"]}}`, "work", "record")
	if err != nil || saved["complete"] != true {
		t.Fatalf("the saved result with its evidence completes: %v %v", saved, err)
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done.", "stop_hook_active": true})); blocked {
		t.Errorf("finishing after the record passes: %s", reason)
	}

	// a receipt replaced by a later preparation says so, and what to use
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest + " Also the totals."})
	next := `"session_id":"planning","turn_id":"` + loadSession(root, "planning").TurnID + `"`
	first, err := cli(`{`+next+`,"query":"CSV export filters"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli(`{`+next+`,"query":"CSV export totals"}`, "work", "prepare"); err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(first["receipt"])
	_, err = cli(`{"receipt":`+string(old)+`,"result":{"changed":"README.md","checked":"Read it."}}`, "work", "record")
	if err == nil || !strings.Contains(err.Error(), "the work receipt is expired or no longer current") || !strings.Contains(err.Error(), "a later work prepare or hook plan query for this request replaced it") || !strings.Contains(err.Error(), "use the receipt of the latest work prepare") {
		t.Errorf("a replaced receipt says what replaced it and what to use: %v", err)
	}
}

// The short shape of result says that evidence is a list of references wherever an agent meets it: the notice, the reply of
// prepare, the MCP guidance, the help, the skill and the guide. (Written as "evidence?" alone, it was filled with a sentence in
// both real runs.)
func TestTheShapeOfResultSaysEvidenceIsReferences(t *testing.T) {
	skill := string(mustRead(t, "../../skills/shikii/SKILL.md"))
	guide := string(mustRead(t, "../../docs/guide.md"))
	bare := regexp.MustCompile(`evidence\?[,}]`)
	for name, text := range map[string]string{"the notice": planningNotice("s", "0123456789abcdef"), "how_to_record": compactWorkHowTo, "the MCP guidance": mcpInstructions, "SKILL.md": skill, "the guide": guide} {
		if !strings.Contains(text, "result:{changed,checked?,evidence?:[refs],remaining?}") || bare.MatchString(text) {
			t.Errorf("%s writes the shape of result without saying evidence is a list of references", name)
		}
	}
	for name, text := range map[string]string{"how_to_record": compactWorkHowTo, "the help of work": agentUsages["work"].text()} {
		if !strings.Contains(text, "evidence takes references") || !strings.Contains(text, "current") || !strings.Contains(text, "newN") {
			t.Errorf("%s does not say which references evidence takes", name)
		}
	}
}

// Found by sending the same request to a real agent that could reach shikii only through its MCP tools (the shell asked for an
// approval nobody could give, 2026-11-12): the notice and the Stop block named only the command line, so the agent tried the
// command, was refused, never called shikii_wiki although it was connected, and the turn ended with nothing recorded. Both now
// name the MCP form beside the command, and that form runs as they say: the notice's JSON as work to shikii_wiki prepare, the
// receipt and the result as work to shikii_wiki record.
func TestTheNoticeAndTheStopBlockNameTheMCPForm(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest}))
	if !strings.Contains(notice, "work prepare (or as work to the MCP tool shikii_wiki, operation prepare)") {
		t.Errorf("the notice names the MCP form of prepare: %s", notice)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}))
	if !blocked || !strings.Contains(reason, "work prepare (or as work to the MCP tool shikii_wiki, operation prepare)") || !strings.Contains(reason, "the same way to work record (MCP: operation record)") {
		t.Errorf("the block for a turn that prepared nothing names the MCP form of both calls:\n%s", reason)
	}
	var ids string
	for _, line := range strings.Split(notice, "\n") {
		if strings.HasPrefix(line, `{"session_id":`) {
			ids = line
		}
	}
	work := strings.TrimSuffix(ids, "}") + `,"query":"CSV export filters"}`
	prepared, _ := mcpToolCall(t, h, "shikii_wiki", `{"operation":"prepare","work":`+work+`}`)
	receipt, _ := json.Marshal(prepared["structuredContent"].(map[string]any)["receipt"])
	if string(receipt) == "null" {
		t.Fatalf("the notice's JSON with query added, as work, prepares the task through MCP: %v", prepared)
	}
	reason, blocked = stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done.", "stop_hook_active": true}))
	if !blocked || !strings.Contains(reason, "work record (or as work to the MCP tool shikii_wiki, operation record)") {
		t.Errorf("the block for a prepared turn names the MCP form of record:\n%s", reason)
	}
	saved, _ := mcpToolCall(t, h, "shikii_wiki", `{"operation":"record","work":{"receipt":`+string(receipt)+`,"result":{"changed":"README.md","checked":"Read it."}}}`)
	if saved["structuredContent"].(map[string]any)["complete"] != true {
		t.Fatalf("the receipt and the result, as work, record the task through MCP: %v", saved["structuredContent"])
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done.", "stop_hook_active": true})); blocked {
		t.Errorf("finishing after the record through MCP passes: %s", reason)
	}
}

// A message the person sends while the agent is still working starts a new request, and the receipt of the interrupted one stops
// working. In one piece of work this happened three times (2026-11-12): each time the agent prepared again, its references to the
// pages it had read and to the sources it had saved were gone, and it read the same pages again. The preparation of the new
// request now takes over what the interrupted work held (the pages it read, under the same references; the sources it saved; the
// pages it wrote) and says so in carried, and the refusal of the old receipt says where they went. A request the agent finished
// (its Stop came) hands nothing over: the next request starts with its own references.
func TestAMessageDuringWorkHandsItsReferencesToTheNextPreparation(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	ask := func(prompt string) string { // the person's message; returns the ids of its notice
		t.Helper()
		notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
		for _, line := range strings.Split(notice, "\n") {
			if strings.HasPrefix(line, `{"session_id":`) {
				return line
			}
		}
		t.Fatalf("the notice carries the ids: %s", notice)
		return ""
	}
	call := func(operation, work string) (map[string]any, string) { // the structured reply, or the text of a refusal
		t.Helper()
		params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(`{"operation":"` + operation + `","work":` + work + `}`)})
		result, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)["result"].(map[string]any)
		if result == nil || result["isError"] == true {
			raw, _ := json.Marshal(result)
			return nil, string(raw)
		}
		return result["structuredContent"].(map[string]any), ""
	}
	prepare := func(ids, query string) map[string]any {
		t.Helper()
		reply, refusal := call("prepare", strings.TrimSuffix(ids, "}")+`,"query":"`+query+`"}`)
		if reply == nil {
			t.Fatalf("prepare: %s", refusal)
		}
		return reply
	}
	record := func(prepared map[string]any, fields string) (map[string]any, string) {
		t.Helper()
		receipt, _ := json.Marshal(prepared["receipt"])
		return call("record", `{"receipt":`+string(receipt)+`,`+fields+`}`)
	}
	errorsOf := func(saved map[string]any) string {
		raw, _ := json.Marshal(saved["errors"])
		if string(raw) == "null" {
			return ""
		}
		return string(raw)
	}
	refOf := func(prepared map[string]any) string {
		for _, raw := range prepared["knowledge"].([]any) {
			if entry := raw.(map[string]any); entry["id"] == page.Page.ID {
				return entry["ref"].(string)
			}
		}
		return ""
	}

	// the first request: the agent reads the page, saves an observation and edits the page
	first := prepare(ask(planningTestRequest), "CSV export filters")
	ref := refOf(first)
	if ref == "" || first["carried"] != nil {
		t.Fatalf("the first preparation reads the page and takes nothing over: %v %v", first["knowledge"], first["carried"])
	}
	saved, refusal := record(first, `"observations":[{"kind":"test","title":"Totals","content":"The totals match."}],"notes":[{"target":"`+ref+`","sources":["new1"],"change_reason":"Add the totals.","edits":[{"field":"body","before":"The export keeps the filters.","after":"The export keeps the filters and the totals."}]}]`)
	if saved == nil || errorsOf(saved) != "" {
		t.Fatalf("the first work saves its observation and its edit: %s %s", refusal, errorsOf(saved))
	}
	firstObserved := ""
	for _, raw := range saved["saved"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "observation" {
			firstObserved, _ = item["id"].(string)
		}
	}

	// the person adds a request before the agent has finished
	ids := ask("Also check the totals of the CSV export.")
	if _, refusal = record(first, `"result":{"changed":"The article says the totals."}`); !strings.Contains(refusal, "a newer request has started") || !strings.Contains(refusal, "under the same references") {
		t.Errorf("the refusal of the interrupted receipt says where its references went: %s", refusal)
	}
	second := prepare(ids, "CSV export filters")
	if again := refOf(second); again == "" || again == ref {
		t.Fatalf("the page as it is now (edited) gets a reference of its own, after the ones taken over: %q beside %q", again, ref)
	}
	carried, _ := second["carried"].(map[string]any)
	if carried == nil || fmt.Sprint(carried["articles"]) != "["+ref+"]" || fmt.Sprint(carried["sources"]) != "[observed1]" || !strings.Contains(fmt.Sprint(carried["next_action"]), "without reading") {
		t.Fatalf("the preparation of the new request names what it took over: %v", second["carried"])
	}
	// the new receipt cites the page the interrupted work read and edited, and a new page names the source that work saved
	saved, refusal = record(second, `"observations":[{"kind":"test","title":"Totals again","content":"The totals still match."}],"notes":[{"target":"new","title":"CSV totals","lesson":"Check the totals of the CSV export.","applies_to":"CSV export totals","kind":"rule","scene":"when exporting CSV","body":"Checked the totals.","sources":["observed1"]}],"result":{"changed":"Checked the totals.","checked":"Read the export.","evidence":["`+ref+`","new1"]}`)
	if saved == nil || errorsOf(saved) != "" {
		t.Fatalf("the new receipt cites what the interrupted work read, edited and saved: %s %s", refusal, errorsOf(saved))
	}
	observed, written := "", ""
	for _, raw := range saved["saved"].([]any) {
		switch item := raw.(map[string]any); item["kind"] {
		case "observation":
			observed, _ = item["ref"].(string)
		case "note":
			written, _ = item["id"].(string)
		}
	}
	if observed != "observed2" {
		t.Errorf("a new observation continues the numbering after the ones taken over (observed1 keeps naming the first): %q", observed)
	}
	if pending, _ := saved["pending"].([]any); len(pending) == 1 { // the page is close to the existing one: the agent chooses to keep it new
		placed, refusal := record(second, `"notes":[{"ref":"`+pending[0].(map[string]any)["ref"].(string)+`","target":"new"}]`)
		if placed == nil || errorsOf(placed) != "" {
			t.Fatalf("the pending page is placed as new: %s %s", refusal, errorsOf(placed))
		}
		for _, raw := range placed["saved"].([]any) {
			if item := raw.(map[string]any); item["kind"] == "note" {
				written, _ = item["id"].(string)
			}
		}
	}
	live, err := b.LiveWhere(func(p notebook.Page) bool { return p.ID == written })
	if err != nil || len(live) != 1 {
		raw, _ := json.Marshal(saved)
		t.Fatalf("the new page is saved: %v %d %.600s", err, len(live), raw)
	}
	if raw, _ := json.Marshal(live[0].Sources); !strings.Contains(string(raw), firstObserved) {
		t.Errorf("observed1 names the source the interrupted work saved (%s), not a later one: %s", firstObserved, raw)
	}

	// the agent finishes (its Stop comes): the next request takes nothing over
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	third := prepare(ask("Now rename the export button."), "invoice numbering")
	if third["carried"] != nil {
		t.Errorf("a request that follows a finished one takes nothing over: %v", third["carried"])
	}
	if saved, refusal = record(third, `"notes":[{"target":"new","title":"Export button","lesson":"Name the export button Export.","applies_to":"export button","kind":"rule","scene":"when naming buttons","body":"Renamed.","sources":["observed1"]}]`); !strings.Contains(refusal+errorsOf(saved), `unknown source reference \"observed1\"`) {
		t.Errorf("a source of a finished request is not valid in the next one: %s %s", refusal, errorsOf(saved))
	}
	if saved, refusal = record(third, `"result":{"changed":"Renamed.","evidence":["`+ref+`"]}`); saved != nil && errorsOf(saved) == "" {
		t.Errorf("a reference of a finished request is not valid in the next one: %v", saved)
	}
}
