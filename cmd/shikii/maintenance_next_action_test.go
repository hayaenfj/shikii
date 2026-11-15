package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Confirmation audit of 0.34.0 (U-E): the next_action of a maintenance task with missed evidence said "diagnose the recorded
// case", and that task reaches MCP agents too, but MCP refuses diagnose. A next step an agent reads over MCP names every operation
// that MCP does not run as its CLI command, and that command runs on what the task gives.
func TestMaintenanceNextActionNamesTheCLIForOperationsMCPRefuses(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	tasks, _ := b.Maintenance()
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	if _, err := b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: tasks[0].Key, Reason: "Initial review."}); err != nil {
		t.Fatal(err)
	}
	_, c, err := b.RecallDetailed("quartz zephyr deployment")
	if err != nil {
		t.Fatal(err)
	}
	c.TurnID = "0000000000000190"
	if err := b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	cases, _ := b.DeliveryCases()
	v, err := improvementCall(t, b, map[string]any{"operation": "query", "query": "lesson:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	lookup, _ := loopObject(t, v)["lookup"].(map[string]any)["id"].(string)
	if err := b.Discover(notebook.Discovery{LookupID: lookup, PageID: p.ID, Revision: p.Revision, Context: c.Request, Decision: "Used the existing rule instead of adding forms.", Evidence: "The proposal has no extra form.", CaseID: cases[0].ID}); err != nil {
		t.Fatal(err)
	}

	// What an MCP agent reads: the maintenance of a query without a search text.
	h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
	result, _ := mcpToolCall(t, h, "shikii_wiki", `{"operation":"query"}`)
	raw, _ := json.Marshal(result)
	var reply struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || len(reply.Content) != 1 {
		t.Fatalf("%s %v", raw, err)
	}
	var view struct {
		Maintenance []struct {
			PageID     string `json:"page_id"`
			NextAction string `json:"next_action"`
			Missed     []struct {
				CaseID string `json:"case_id"`
			} `json:"missed"`
		} `json:"maintenance"`
	}
	if err := json.Unmarshal([]byte(reply.Content[0].Text), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Maintenance) != 1 || len(view.Maintenance[0].Missed) != 1 || view.Maintenance[0].Missed[0].CaseID != cases[0].ID {
		t.Fatalf("precondition: MCP delivers the task with its missed evidence: %+v", view.Maintenance)
	}
	task := view.Maintenance[0]
	if problem := nextActionCLIProblem(task.NextAction); problem != "" {
		t.Error(problem)
	}
	// The check catches the old wording, and lets an operation named as its CLI command, or one MCP runs, pass.
	if nextActionCLIProblem("Read the article and missed evidence; diagnose the recorded case, capture supporting evidence.") == "" ||
		nextActionCLIProblem("Run shikii wiki diagnose for the recorded case, then capture and compare.") != "" {
		t.Error("nextActionCLIProblem does not tell a bare operation MCP refuses from its CLI command")
	}
	// What the text says works: MCP refuses diagnose, and the CLI diagnoses the case with the ids the task gives.
	if refused := mcpRawCall(h, `{"operation":"diagnose","id":"`+task.PageID+`","case_id":"`+task.Missed[0].CaseID+`"}`); refused["isError"] != true {
		t.Fatalf("precondition: MCP refuses diagnose: %v", refused)
	}
	if _, err := improvementCall(t, b, map[string]any{"operation": "diagnose", "id": task.PageID, "case_id": task.Missed[0].CaseID}); err != nil {
		t.Fatalf("shikii wiki diagnose with the task's article and case: %v", err)
	}
}

// nextActionCLIProblem names an operation MCP refuses that text tells the agent to do without naming its CLI command.
func nextActionCLIProblem(text string) string {
	for op := range mcpWikiCLIOnly {
		for _, at := range regexp.MustCompile(`\b`+op+`\b`).FindAllStringIndex(text, -1) {
			if !strings.HasSuffix(text[:at[0]], "shikii wiki ") {
				return "the next action names " + op + ", which MCP refuses, without its CLI command shikii wiki " + op + ": " + text
			}
		}
	}
	return ""
}

// mcpRawCall returns the tools/call result of shikii_wiki, error or not.
func mcpRawCall(h http.Handler, arguments string) map[string]any {
	params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(arguments)})
	result, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)["result"].(map[string]any)
	return result
}
