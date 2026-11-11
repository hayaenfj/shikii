package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The second round of the audit of 0.31.0 (D-A7, D-U5, D-U6, D-U7, D-U8): the texts an agent and a person read name one flow,
// work prepare and then work record, and say what the commands do now. Each check below is a function over a text, run on the
// texts as they are and on the text it replaced (the negative example).

// agentCLI runs a command an agent calls with its input on standard input, and returns the reply.
func agentCLI(t *testing.T, input string, args ...string) (map[string]any, error) {
	t.Helper()
	var out string
	var err error
	withStdin(t, input, func() { out, err = captureErr(func() error { return cliCall(args) }) })
	var reply map[string]any
	_ = json.Unmarshal([]byte(out), &reply)
	return reply, err
}

// noticeIDs is the line of a notice that carries this turn's ids.
func noticeIDs(t *testing.T, notice string) string {
	t.Helper()
	for _, line := range strings.Split(notice, "\n") {
		if strings.HasPrefix(line, `{"session_id":`) {
			return line
		}
	}
	t.Fatalf("the notice carries the ids: %s", notice)
	return ""
}

// planningExampleCommands is the command each section of planning.md writes its JSON examples for. An example in any other
// section has no command the reader could tell, and is refused.
var planningExampleCommands = map[string][]string{
	"Retrieve relevant wiki evidence":  {"work", "prepare"},
	"The older entry: hook plan query": {"hook", "plan", "query"},
	"Submit grounded interpretations":  {"hook", "plan", "submit"},
}

const planningOlderEntry = "The older entry: hook plan query"

type planningExample struct{ section, text string }

// planningParts splits the reference at its "## " headings and collects its JSON examples with the section each is in.
func planningParts(text string) (sections map[string]string, examples []planningExample) {
	sections = map[string]string{}
	section, fence := "", -1
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		switch {
		case fence < 0 && strings.HasPrefix(line, "## "):
			section = strings.TrimPrefix(line, "## ")
		case fence < 0 && strings.HasPrefix(line, "```json"):
			fence = i + 1
		case fence >= 0 && strings.HasPrefix(line, "```"):
			examples = append(examples, planningExample{section, strings.Join(lines[fence:i], "\n")})
			fence = -1
		}
		sections[section] += line + "\n"
	}
	return sections, examples
}

// planningReferenceProblems says where planning.md would lead an agent wrong: the part about work prepare tells it to read a
// field only hook plan query returns, an example searches again in a way that replaces the receipt, or the section about hook
// plan query does not say what running it does to the receipt.
func planningReferenceProblems(text string) []string {
	var problems []string
	sections, examples := planningParts(text)
	at := strings.Index(text, "\n## "+planningOlderEntry+"\n")
	if at < 0 {
		return []string{"there is no section " + planningOlderEntry}
	}
	if strings.Contains(text[:at], "source_material") {
		problems = append(problems, "the part about work prepare names source_material, a field of the reply of hook plan query")
	}
	if !strings.Contains(sections[planningOlderEntry], "replaces the receipt") {
		problems = append(problems, "the section about hook plan query does not say that it replaces the receipt")
	}
	seen := map[string]bool{}
	for _, example := range examples {
		command, known := planningExampleCommands[example.section]
		if !known {
			problems = append(problems, "an example is in a section without a command of its own: "+example.section)
			continue
		}
		seen[example.section] = true
		var object map[string]any
		if json.Unmarshal([]byte(example.text), &object) != nil {
			problems = append(problems, "an example in "+example.section+" is not one JSON object")
			continue
		}
		_, receipt := object["receipt"]
		_, ids := object["session_id"]
		switch {
		case command[0] == "work" && (!receipt || ids):
			problems = append(problems, "the example for work prepare searches again without the receipt (sent with the ids it replaces the receipt)")
		case command[0] == "hook" && (receipt || !ids):
			problems = append(problems, "the example for "+strings.Join(command, " ")+" does not carry the ids")
		}
	}
	for section := range planningExampleCommands {
		if !seen[section] {
			problems = append(problems, "there is no example in "+section)
		}
	}
	sort.Strings(problems)
	return problems
}

// D-A7: planning.md mixed the reply of work prepare with the reply of hook plan query. Followed as written, the agent looked for
// source_material in a reply that has none, and searched again with the ids, which replaced its receipt. Every JSON example of
// the reference now runs, as written, through the command its section names, and the receipt of work prepare stays valid.
func TestPlanningReferenceKeepsTheReceiptOfWorkPrepare(t *testing.T) {
	text := string(mustRead(t, "../../skills/shikii/references/planning.md"))
	if problems := planningReferenceProblems(text); len(problems) > 0 {
		t.Errorf("planning.md: %s", strings.Join(problems, "; "))
	}
	// the negative examples: the reference as the audit read it
	for want, old := range map[string]string{
		"the example for work prepare searches again without the receipt": strings.Replace(text, `{"receipt":"<receipt>","query":"CSV export columns and filters"}`, `{"session_id":"the supplied ID","turn_id":"0123456789abcdef","query":"CSV export columns and filters"}`, 1),
		"the part about work prepare names source_material":               strings.Replace(text, "Read `task` and `conversation` with the active conversation.", "Read `source_material` with the active conversation.", 1),
		"does not say that it replaces the receipt":                       strings.Replace(text, "it replaces the receipt of a `work prepare` for the same request", "it is the older entry", 1),
		"an example is in a section without a command of its own":         strings.Replace(text, "## Retrieve relevant wiki evidence", "## Find evidence", 1),
	} {
		if old == text {
			t.Errorf("the negative example for %q no longer changes the reference", want)
		}
		if problems := strings.Join(planningReferenceProblems(old), "; "); !strings.Contains(problems, want) {
			t.Errorf("the reference as it was is not reported (%q): %q", want, problems)
		}
	}
	_, examples := planningParts(text)

	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	// the request as "Recover the current task" says: the ids of the notice, with topic words, to work prepare
	ids := noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})))
	first, err := agentCLI(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(first["receipt"])
	for _, field := range []string{"task", "conversation", "knowledge"} { // the fields that section tells the agent to read
		if _, has := first[field]; !has {
			t.Errorf("planning.md tells the agent to read %s, and the reply of work prepare has no such field", field)
		}
	}
	if _, has := first["source_material"]; has {
		t.Errorf("the reply of work prepare has source_material now: planning.md says it has none")
	}
	saved, err := agentCLI(t, `{"receipt":`+string(receipt)+`,"observations":[{"kind":"test","title":"Totals","content":"The totals match."}]}`, "work", "record")
	if refused, _ := saved["errors"].([]any); err != nil || len(refused) > 0 {
		t.Fatalf("an observation is saved with the receipt: %v %v", err, saved["errors"])
	}
	// the older entry runs on a request of its own, which also holds a receipt of work prepare (to see what the reference warns of)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "older", "cwd": repo, "prompt": planningTestRequest})
	olderTurn := loadSession(root, "older").TurnID
	olderPrepared, err := agentCLI(t, `{"session_id":"older","turn_id":"`+olderTurn+`","query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	fill := strings.NewReplacer("<receipt>", strings.Trim(string(receipt), `"`), "the supplied ID", "older", "0123456789abcdef", olderTurn, "an exact short quote from the returned text", "keep the existing filters")
	ran := map[string]int{}
	for _, example := range examples {
		command := planningExampleCommands[example.section]
		if command == nil {
			continue // reported above
		}
		reply, err := agentCLI(t, fill.Replace(example.text), command...)
		if err != nil {
			t.Errorf("an example of planning.md (%s) is refused by shikii %s as written: %v\n%s", example.section, strings.Join(command, " "), err, example.text)
			continue
		}
		ran[strings.Join(command, " ")]++
		switch strings.Join(command, " ") {
		case "work prepare":
			if again, _ := json.Marshal(reply["receipt"]); string(again) != string(receipt) {
				t.Errorf("searching again as planning.md writes it changes the receipt: %s, was %s", again, receipt)
			}
		case "hook plan query":
			if reply["source_material"] == nil || reply["how_to_submit"] == nil || reply["receipt"] != nil {
				t.Errorf("the reply of hook plan query has source_material and how_to_submit, and no receipt, as planning.md says: %v", reply)
			}
		}
	}
	for _, command := range planningExampleCommands {
		if ran[strings.Join(command, " ")] == 0 {
			t.Errorf("no example of planning.md ran through shikii %s", strings.Join(command, " "))
		}
	}
	// after searching again as the reference writes it, the receipt still records, and cites what it saved before
	saved, err = agentCLI(t, `{"receipt":`+string(receipt)+`,"result":{"changed":"Nothing.","checked":"Read the export.","evidence":["observed1"]}}`, "work", "record")
	if err != nil || saved["complete"] != true {
		raw, _ := json.Marshal(saved["errors"])
		t.Errorf("after searching again as planning.md writes it, the receipt records a result that cites its observation: %v complete=%v %s", err, saved["complete"], raw)
	}
	// what the reference warns of is what happens: hook plan query replaced the receipt of work prepare for its request
	olderReceipt, _ := json.Marshal(olderPrepared["receipt"])
	if _, err := agentCLI(t, `{"receipt":`+string(olderReceipt)+`,"result":{"changed":"Nothing.","checked":"Read the export."}}`, "work", "record"); err == nil || !strings.Contains(err.Error(), "replaced it") {
		t.Errorf("planning.md says hook plan query replaces the receipt of work prepare, and that receipt still records: %v", err)
	}
	// and so does a second preparation with the ids (the search the reference used to show)
	ids = noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "twice", "cwd": repo, "prompt": planningTestRequest})))
	once, err := agentCLI(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentCLI(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export columns and filters"}`, "work", "prepare"); err != nil {
		t.Fatal(err)
	}
	onceReceipt, _ := json.Marshal(once["receipt"])
	if _, err := agentCLI(t, `{"receipt":`+string(onceReceipt)+`,"result":{"changed":"Nothing.","checked":"Read the export."}}`, "work", "record"); err == nil || !strings.Contains(err.Error(), "replaced it") {
		t.Errorf("planning.md says a preparation without the receipt replaces the receipt, and the earlier receipt still records: %v", err)
	}
}

// mcpFormOfNotice is the one way a notice names the MCP call: the JSON is the value of work.
var mcpFormOfNotice = regexp.MustCompile(`as work to the MCP tool shikii_wiki, operation (prepare|record)`)

// mcpFormProblem says whether a text names the tool shikii_wiki in another way than "as work to the MCP tool shikii_wiki".
func mcpFormProblem(text string) string {
	if named, allowed := strings.Count(text, "shikii_wiki"), len(mcpFormOfNotice.FindAllString(text, -1)); named == 0 || named != allowed {
		return fmt.Sprintf("names shikii_wiki %d times, and %d of them say that the JSON goes in work", named, allowed)
	}
	return ""
}

// D-U5: the notice of a request that needs no planning judgment named the MCP tool without saying that the ids go inside work. An
// agent that put them beside operation was refused. Both notices name the MCP call in one way, and the call runs as they say.
func TestBothNoticesNameTheMCPCallWithTheIdsInsideWork(t *testing.T) {
	for name, text := range map[string]string{"the notice with a planning judgment": planningNotice("s", "0123456789abcdef"), "the notice without one": reasoningNotice("s", "0123456789abcdef")} {
		if problem := mcpFormProblem(text); problem != "" || !strings.Contains(text, "("+noticeMCPForm+")") {
			t.Errorf("%s %s: %s", name, problem, text)
		}
	}
	// the negative example: the notice as the audit read it
	if mcpFormProblem("on standard input of 'shikii' work prepare (MCP: shikii_wiki, operation prepare); it registers the start.") == "" {
		t.Errorf("a notice that names shikii_wiki without saying the JSON goes in work is not reported")
	}
	for _, c := range []struct {
		name    string
		fixture func(t *testing.T) (repo, root string)
		notice  string // a sentence only this notice has
	}{
		{"with a planning judgment", func(t *testing.T) (string, string) {
			repo, root, _ := planningFixture(t)
			return repo, root
		}, "prepare this task once"},
		{"without a planning judgment", func(t *testing.T) (string, string) {
			repo := gitRepo(t)
			return repo, hookEnv(t, fakeAgentModel(t, func(string) float64 { return 0.1 }), repo)
		}, "it registers the start"},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, root := c.fixture(t)
			t.Chdir(repo)
			h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
			notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "fix the typo in a.go"}))
			if !strings.Contains(notice, c.notice) || !strings.Contains(notice, "work prepare ("+noticeMCPForm+")") {
				t.Fatalf("the request gets the notice %s, with the MCP form beside the command: %s", c.name, notice)
			}
			ids := noticeIDs(t, notice)
			work := strings.TrimSuffix(ids, "}") + `,"query":"typo in a.go"}`
			call := func(arguments string) (map[string]any, string) {
				params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(arguments)})
				result, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)["result"].(map[string]any)
				raw, _ := json.Marshal(result)
				return result, string(raw)
			}
			// the ids beside operation, as "(MCP: shikii_wiki, operation prepare)" was read: refused
			if result, raw := call(`{"operation":"prepare",` + strings.TrimPrefix(work, "{")); result["isError"] != true || !strings.Contains(raw, "is a field of work") {
				t.Errorf("the ids beside operation are refused, and the refusal names work: %s", raw)
			}
			// as the notice says: the JSON as work
			result, raw := call(`{"operation":"prepare","work":` + work + `}`)
			prepared, _ := result["structuredContent"].(map[string]any)
			if result["isError"] == true || prepared["receipt"] == nil {
				t.Fatalf("the notice's JSON with query added, as work, prepares the task through the MCP tool: %s", raw)
			}
			receipt, _ := json.Marshal(prepared["receipt"])
			result, raw = call(`{"operation":"record","work":{"receipt":` + string(receipt) + `,"result":{"changed":"Nothing.","checked":"Read a.go."}}}`)
			if saved, _ := result["structuredContent"].(map[string]any); result["isError"] == true || saved["complete"] != true {
				t.Errorf("work record the same way, with the receipt and the result, records the task: %s", raw)
			}
		})
	}
}

// D-U6: "Include continues_previous (true or false) for this short message." did not say which call takes it. It followed "the
// work prepare below sorts it out", an agent sent it with prepare, and prepare refused it. The sentence names the call.
func TestTheStopBlockSaysWhichCallTakesContinuesPrevious(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	first := noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})))
	prepared, err := agentCLI(t, strings.TrimSuffix(first, "}")+`,"query":"CSV export filters"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(prepared["receipt"])
	if saved, err := agentCLI(t, `{"receipt":`+string(receipt)+`,"result":{"changed":"Nothing.","checked":"Read the export."}}`, "work", "record"); err != nil || saved["complete"] != true {
		t.Fatalf("the first request is recorded: %v %v", err, saved)
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("the first request ends: %s", reason)
	}
	// a short message that is not a bare acknowledgement, and a file changed with nothing prepared
	second := noticeIDs(t, feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Use tabs there."})))
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}))
	const sentence = "Include continues_previous (true or false) in work record for this short message."
	if !blocked || !strings.Contains(reason, "the work prepare below sorts it out") || !strings.Contains(reason, sentence) || strings.Contains(reason, "hook plan submit") {
		t.Fatalf("the block says that continues_previous goes in work record:\n%s", reason)
	}
	// the call the sentence used to leave open: with prepare it is refused
	if _, err := agentCLI(t, strings.TrimSuffix(second, "}")+`,"query":"tabs","continues_previous":true}`, "work", "prepare"); err == nil || !strings.Contains(err.Error(), "prepare was sent continues_previous") {
		t.Errorf("continues_previous sent with work prepare is refused: %v", err)
	}
	// as the block says: prepare, then continues_previous in work record
	prepared, err = agentCLI(t, strings.TrimSuffix(second, "}")+`,"query":"tabs"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ = json.Marshal(prepared["receipt"])
	if saved, err := agentCLI(t, `{"receipt":`+string(receipt)+`,"continues_previous":true,"result":{"changed":"README.md","checked":"Read it."}}`, "work", "record"); err != nil || saved["complete"] != true {
		t.Fatalf("continues_previous in work record, as the block says, is recorded: %v %v", err, saved)
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done.", "stop_hook_active": true})); blocked {
		t.Errorf("the turn ends after that record: %s", reason)
	}
	// the older line (no review asked for in the same block) asks for hook plan submit, and the sentence names that call
	turn := "0123456789abcdef"
	out, err := ritualStop(root, feedbackScope(repo), &hookInput{SessionID: "older", late: true},
		&hookSession{TurnID: turn, Scope: feedbackScope(repo), Planning: &planningFrame{TurnID: turn, Grouping: &requestGrouping{Reply: "Use tabs there."}}}, []string{"README.md"}, true, false)
	if err != nil || !out.Block || !strings.Contains(out.Reason, "hook plan query, read this and the recent conversations, submit agreed and open") || !strings.Contains(out.Reason, "Include continues_previous (true or false) in the submit for this short message.") || strings.Contains(out.Reason, "in work record for this short message") {
		t.Errorf("the block that asks for hook plan submit says that continues_previous goes in the submit: %v\n%s", err, out.Reason)
	}
}

// reflectListingProblem says what a text that tells which conversations reflect query lists leaves out: the ones that did not fit
// are counted in more_pending, and the next query lists them. next is how the text's language writes "next query".
func reflectListingProblem(text, next string) string {
	switch {
	case strings.Contains(text, "every pending conversation"):
		return "says that every pending conversation is listed"
	case !strings.Contains(text, "more_pending"):
		return "does not name more_pending, which counts the conversations that did not fit"
	case !strings.Contains(text, next):
		return "does not say that the next query lists the conversations that did not fit"
	}
	return ""
}

// lineWith is the one line of a file that holds the given text.
func lineWith(t *testing.T, path, needle string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(string(mustRead(t, path)), "\n") {
		if strings.Contains(line, needle) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s has %d lines with %q, want 1", path, len(found), needle)
	}
	return found[0]
}

// D-U7: the texts said that reflect query lists every pending conversation. Since the reply has a bound, the conversations that
// do not fit wait: in one reply output said they were counted in more_pending while how_to_submit said every one was listed.
func TestTextsAboutReflectQuerySayWhereTheRestGoes(t *testing.T) {
	const next = "next query"
	nextJA := string([]rune{0x6b21, 0x306e}) + " query" // "the next query" as docs/guide.ja.md writes it
	help := ""
	for _, form := range agentUsages["reflect"].Forms {
		if form.Call == "reflect query" {
			help = form.When
		}
	}
	tool := ""
	for _, definition := range mcpTools {
		if definition["name"] == "shikii_reflect" {
			tool, _ = definition["description"].(string)
		}
	}
	// review.md: what sources holds, up to where the same step goes on to the bound of the reply (which names more_pending too)
	review, _, _ := strings.Cut(lineWith(t, "../../skills/shikii/references/review.md", "1. Call `shikii_reflect` with `operation: query`"), "The reply is kept within")
	for _, c := range []struct{ name, text, next string }{
		{"the help of shikii reflect", help, next},
		{"the description of the MCP tool shikii_reflect", tool, next},
		{"how_to_submit of reflect query", reflectHowToSubmit, next},
		{"skills/shikii/references/wiki.md", lineWith(t, "../../skills/shikii/references/wiki.md", "`reflect query` lists"), next},
		{"skills/shikii/references/review.md", review, next},
		{"docs/guide.md", lineWith(t, "../../docs/guide.md", "| `POST /v1/reflect` |"), next},
		{"docs/guide.ja.md", lineWith(t, "../../docs/guide.ja.md", "| `POST /v1/reflect` |"), nextJA},
	} {
		if c.text == "" {
			t.Errorf("%s: the text was not found", c.name)
		} else if problem := reflectListingProblem(c.text, c.next); problem != "" {
			t.Errorf("%s %s: %.400s", c.name, problem, c.text)
		}
	}
	// the negative examples: the texts as the audit read them
	for _, old := range []string{
		"every pending conversation with its sources, and how_to_submit (the shape of submit)",
		"Reflection in one place (query lists every pending conversation, the oldest 2 with their text and the rest as heads; submit takes any number).",
		"every pending conversation is listed, the oldest two with their raw text and the rest as head: true with the text cut, read with wiki query <id> when a lesson needs it",
		"`sources` holds every pending conversation, oldest first: the oldest 2 with their raw `source_material`, up to 40 more as `head: true` with the text cut (the rest are counted in `more_pending` and come once these are settled)",
		"query lists the conversations (the oldest 2 with their text, up to 40 more as heads, the rest as a count in more_pending)",
		"query lists the pending conversations, the oldest 2 with their text and the rest as heads",
	} {
		if reflectListingProblem(old, next) == "" {
			t.Errorf("a text that does not say where the rest goes is not reported: %s", old)
		}
	}
	// a reply the bound shortened until conversations wait says the same thing in output and in how_to_submit
	view := reflectView{HowToSubmit: reflectHowToSubmit}
	for i := 0; i < 60; i++ {
		source := reflectSource{Targets: []reviewTarget{}, WikiPending: true}
		source.Source.ID = strings.Repeat("0", 14) + string(rune('a'+i/26)) + string(rune('a'+i%26))
		source.Source.Request = strings.Repeat("Check the CSV export. ", 40)
		view.Sources = append(view.Sources, source)
	}
	size := func(v any) int { data, _ := json.Marshal(v); return len(data) }
	limited, _ := limitReflectQuery(view, 16<<10, size).(map[string]any)
	waiting, _ := limited["more_pending"].(float64)
	how, _ := limited["how_to_submit"].(string)
	if limited == nil || waiting == 0 || size(limited) > 16<<10 {
		t.Fatalf("precondition: the bound makes conversations wait: more_pending=%v, %d bytes", limited["more_pending"], size(limited))
	}
	if problem := reflectListingProblem(how, next); problem != "" {
		t.Errorf("a reply with more_pending %v carries a how_to_submit that %s", waiting, problem)
	}
}

// planFlowProblem says what a text that names the flow of the check plan-before-work gets wrong: the flow is work prepare, then
// work record, and a text that names hook plan beside it says that it is the older entry or what it does to the receipt.
func planFlowProblem(text string) string {
	older := string([]rune{0x53e4, 0x3044}) // "older" as docs/guide.ja.md writes it
	switch {
	case !strings.Contains(text, "work prepare") || !strings.Contains(text, "work record"):
		return "does not name work prepare and work record"
	case strings.Contains(text, "hook plan") && !strings.Contains(text, "older") && !strings.Contains(text, older) && !strings.Contains(text, "receipt"):
		return "names hook plan without saying that it is the older entry, or that it replaces the receipt"
	}
	return ""
}

// D-U8: the older flow was still the one named where a person reads about the check plan-before-work: shikii guard list
// ("hook plan query -> submit") and the guides ("Explicit hook plan query/submit remains available for deeper planning"), while
// the notice of every request says not to run it.
func TestThePlanCheckNamesTheFlowAnAgentRuns(t *testing.T) {
	guides := map[string]string{}
	for _, path := range []string{"../../docs/guide.md", "../../docs/guide.ja.md"} { // read before the fixture moves the working directory
		guides[path+", the check plan-before-work"] = lineWith(t, path, "(plan-before-work)**")
		guides[path+", the row of shikii hook plan"] = lineWith(t, path, "| `shikii hook plan query` / `submit` |")
		lineWith(t, path, "| `shikii work prepare` / `record` |") // the table of commands has the row of the flow itself
	}
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	t.Chdir(repo)
	listed := ""
	for _, line := range strings.Split(capture(t, func() error { return cmdGuard([]string{"list"}) }), "\n") {
		if strings.HasPrefix(line, ritualPlan+" ") {
			listed = line
		}
	}
	if !strings.Contains(listed, "built in (always enabled)") || strings.Contains(listed, "hook plan") {
		t.Errorf("shikii guard list shows the check %s without the older flow: %q", ritualPlan, listed)
	}
	guides["shikii guard list"] = listed
	for name, text := range guides {
		if problem := planFlowProblem(text); problem != "" {
			t.Errorf("%s %s: %s", name, problem, text)
		}
	}
	// the negative examples: the texts as the audit read them
	for _, old := range []string{
		"plan-before-work  ritual  built in (always enabled)  Sort out the plan before work (hook plan query, then submit)",
		"- **Planning before work (plan-before-work)**: prepare once with the host hook identifiers and topic words. Explicit hook plan query/submit remains available for deeper planning.",
		"| `shikii hook plan query` / `submit` | Planning before work (used by the agent) |",
		"prepare once (`work prepare`), record at the end (`work record`). Explicit hook plan query/submit remains available for deeper planning.",
	} {
		if planFlowProblem(old) == "" {
			t.Errorf("a text that names the older flow as the one to run is not reported: %s", old)
		}
	}
}
