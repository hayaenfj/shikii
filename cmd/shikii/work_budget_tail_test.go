package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A reply that fits today is the same bytes after the last fields were added to the bound: testdata/work_limit_golden.json is what
// the code before them made of four shapes (candidates, pending, coverage and everything at once) at six bounds, including the ones
// that make it lighten the articles and leave fields out. Only a reply that was refused changes.
func TestLimitWorkPreparationMakesTheSameReplyThatFitsToday(t *testing.T) {
	raw, err := os.ReadFile("testdata/work_limit_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	golden := map[string]limitShapeResult{}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if want := len(limitShapeNames) * len(limitShapeBounds); len(golden) != want {
		t.Fatalf("the golden file holds %d replies, want %d", len(golden), want)
	}
	// the same bytes whether or not the caller lets the bound leave out its last fields: none of these replies is refused without them
	for _, leaveOutLast := range []bool{false, true} {
		for _, name := range limitShapeNames {
			for _, bound := range limitShapeBounds {
				key := fmt.Sprintf("%s@%d", name, bound)
				t.Run(fmt.Sprintf("%s last fields %v", key, leaveOutLast), func(t *testing.T) {
					got, want := runLimitShape(name, bound, leaveOutLast), golden[key]
					if got.Err != want.Err || got.Reply != want.Reply || !slices.Equal(got.Quoted, want.Quoted) || fmt.Sprint(got.Read) != fmt.Sprint(want.Read) {
						t.Fatalf("a reply that fit before changed:\nwant %s\n got %s\nerr want %q got %q", want.Reply, got.Reply, want.Err, got.Err)
					}
				})
			}
		}
	}
}

// tailShape is a reply that does not fit any bound until the last fields go: forty articles, two maintenance tasks with their
// feedback, a long list of carried references, and a task of 3,000 bytes (a task is never cut by the bound).
func tailShape() (map[string]any, *workState, map[string]string) {
	w := &workState{Sources: map[string]string{}, leaveOutLast: true}
	quoted := map[string]string{}
	entries := []any{}
	for i := 1; i <= 40; i++ {
		e := limitShapeEntry(i, 400)
		quoted[e["ref"].(string)] = "quote"
		entries = append(entries, e)
	}
	maintenance := []any{}
	for i := 1; i <= 2; i++ {
		feedback := []store.MemoryAssessment{}
		for n := 0; n < 3; n++ {
			feedback = append(feedback, store.MemoryAssessment{SourceID: fmt.Sprintf("%016x", n), Key: "lesson:x:y", Result: "harmful", Decision: strings.Repeat("d", 700), Evidence: strings.Repeat("e", 900)})
		}
		task := notebook.MaintenanceTask{PageID: fmt.Sprintf("%016x", 0x1000+i), Revision: fmt.Sprintf("%016x", 0x2000+i), Key: fmt.Sprintf("%016x", 0x3000+i), Title: fmt.Sprintf("Export rule %d", i), Reasons: []string{"delivery-feedback", "unlinked"}, Dependencies: map[string]string{}, Feedback: feedback, FeedbackTotal: 4, NextAction: strings.Repeat("n", 500)}
		maintenance = append(maintenance, map[string]any{"ref": fmt.Sprintf("maintenance%d", i), "task": task})
	}
	articles, sources := []any{}, []any{}
	for i := 1; i <= 32; i++ {
		articles = append(articles, fmt.Sprintf("knowledge%d", i))
	}
	for i := 1; i <= 400; i++ {
		sources = append(sources, fmt.Sprintf("observed%d", i))
	}
	reply := map[string]any{"receipt": "r-" + strings.Repeat("c", 100), "task": strings.Repeat("t", 3000), "task_context": map[string]any{"role": "user"}, "how_to_record": compactWorkHowTo, "knowledge": entries, "maintenance": maintenance, "carried": map[string]any{"articles": articles, "sources": sources, "next_action": strings.Repeat("c", 540)}}
	return reply, w, quoted
}

// tailNames are the names output gives to the last fields, in the order they go.
func tailNames(names []string) []string {
	out := []string{}
	for _, name := range names {
		for _, tail := range []string{omittedMaintenanceDetails, omittedKnowledgeSentences, omittedCarried, omittedMaintenance, "knowledge[] after the first "} {
			if strings.HasPrefix(name, tail) {
				out = append(out, tail)
			}
		}
	}
	return out
}

// The last fields go in one order and each only as far as the bound needs: maintenance tasks to their heads, the knowledge entries
// without the two sentences each repeats, carried, maintenance whole, and then the entries from the end of the ranking. Across the
// bounds a reply is made for, the more it needs left out the longer the list of what went, always from the start of that order, and
// every step is the last one at some bound.
func TestLeaveOutLastFieldsGoInOrderAndOnlyAsFarAsTheBoundNeeds(t *testing.T) {
	order := []string{omittedMaintenanceDetails, omittedKnowledgeSentences, omittedCarried, omittedMaintenance, "knowledge[] after the first "}
	levels := map[int]bool{}
	previous := -1
	for bound := 40000; bound >= workMinBudget; bound -= 250 {
		reply, w, quoted := tailShape()
		if _, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, bound); err != nil {
			t.Fatalf("bound %d: %v", bound, err)
		}
		if n := workContextSize(reply); n > bound-1 {
			t.Fatalf("bound %d: the reply is %d bytes", bound, n)
		}
		output, _ := reply["output"].(map[string]any)
		var names []string
		if output != nil {
			names = tailNames(output["omitted"].([]string))
		}
		if !slices.Equal(names, order[:len(names)]) {
			t.Fatalf("bound %d: the last fields went in another order: %q", bound, names)
		}
		if len(names) < previous {
			t.Fatalf("bound %d: a smaller bound left fewer of the last fields out (%d after %d)", bound, len(names), previous)
		}
		previous = len(names)
		levels[len(names)] = true
		// each field named is gone or reduced, and each one not named is whole
		maintenance, _ := reply["maintenance"].([]any)
		carriedPresent := reply["carried"] != nil
		heads := len(names) >= 1 && len(maintenance) > 0 && maintenance[0].(map[string]any)["incomplete"] == true
		switch {
		case slices.Contains(names, omittedCarried) != !carriedPresent:
			t.Fatalf("bound %d: carried is named exactly when it is gone: %q present %v", bound, names, carriedPresent)
		case slices.Contains(names, omittedMaintenance) != (len(maintenance) == 0):
			t.Fatalf("bound %d: maintenance is named exactly when it is gone: %q", bound, names)
		case len(names) >= 1 && len(maintenance) > 0 && !heads:
			t.Fatalf("bound %d: a task that is left has its head only", bound)
		case len(names) == 0 && len(maintenance) == 2 && maintenance[0].(map[string]any)["incomplete"] != nil:
			t.Fatalf("bound %d: nothing is left out of a reply that needs nothing left out", bound)
		}
	}
	for level := 0; level <= len(order); level++ {
		if !levels[level] {
			t.Fatalf("no bound leaves out exactly %d of the last fields: %v", level, levels)
		}
	}
}

// The way back is in the reply that left a field out: the names output gives, and output.next_action. The head of a maintenance task
// says where its details are, and the knowledge entries that stay keep what the named query needs.
func TestLeaveOutLastFieldsNameTheWayBackInTheReply(t *testing.T) {
	reply, w, quoted := tailShape()
	if _, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, workMinBudget); err != nil {
		t.Fatal(err)
	}
	output := reply["output"].(map[string]any)
	names := tailNames(output["omitted"].([]string))
	if len(names) != 5 {
		t.Fatalf("the smallest bound needs every last field left out: %q", names)
	}
	haystack := fmt.Sprint(output["omitted"]) + " " + output["next_action"].(string)
	for field, spec := range workReplyFields {
		if !spec.Kept && !strings.Contains(haystack+" "+workPreparationHint(map[string]any{})["next_action"].(string), spec.Back) {
			t.Errorf("%s: the words of its way back (%q) are in no reply that leaves it out", field, spec.Back)
		}
	}
	for _, raw := range reply["knowledge"].([]any) {
		e := raw.(map[string]any)
		page := limitShapePage(0, 0)
		if _, ok := e["id"].(string); !ok || len(e["id"].(string)) != len(page.ID) || e["revision"] == nil {
			t.Fatalf("an entry that stays names its article by id and exact revision: %v", e)
		}
	}
	// the query the names give is the form the entries had: lesson:<id>@<revision>
	entry := limitShapeEntry(1, 100)
	if want := "lesson:" + entry["id"].(string) + "@" + entry["revision"].(string); entry["query"].(map[string]any)["query"] != want {
		t.Fatalf("the query that reads an entry is %q", want)
	}
}

// The entries that go from the end of knowledge are named, each by the query that reads it (lesson:<id>@<revision>), as many as the
// bound leaves room for and at most omittedLessonsListed, and the rest are counted: the reply says which lessons it did not carry,
// not only how many. Across the bounds that drop entries, the lessons named are exactly those that are gone (the first ones that are
// gone, in the order they were), and none that stays is named.
func TestLeaveOutLastFieldsNameTheLessonsOfTheEntriesThatWent(t *testing.T) {
	counted, listedAll := false, false
	keptAt := func(bound int) int {
		reply, w, quoted := tailShape()
		if _, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, bound); err != nil {
			return -1
		}
		return len(reply["knowledge"].([]any))
	}
	// the largest bound that leaves an entry out, found by halving; the bounds tried are the few below it, where one or two entries
	// go, and a few steps down to the smallest
	low, high := workMinBudget, 20000
	for low < high {
		if middle := (low + high + 1) / 2; keptAt(middle) < 40 {
			low = middle
		} else {
			high = middle - 1
		}
	}
	bounds := []int{}
	for bound := low; bound > low-120; bound -= 3 {
		bounds = append(bounds, bound)
	}
	for bound := low - 400; bound >= workMinBudget; bound -= 700 {
		bounds = append(bounds, bound)
	}
	bounds = append(bounds, workMinBudget)
	for _, bound := range bounds {
		reply, w, quoted := tailShape()
		before := slices.Clone(reply["knowledge"].([]any))
		if _, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, bound); err != nil {
			continue
		}
		output, _ := reply["output"].(map[string]any)
		if output == nil {
			continue
		}
		var name string
		for _, n := range output["omitted"].([]string) {
			if strings.HasPrefix(n, "knowledge[] after the first ") {
				name = n
			}
		}
		if name == "" {
			continue
		}
		kept := len(reply["knowledge"].([]any))
		gone := before[kept:]
		named := regexp.MustCompile(`lesson:[0-9a-f]+@[0-9a-f]+`).FindAllString(name, -1)
		named = slices.DeleteFunc(named, func(s string) bool { return s == "lesson:<id>@<revision>" })
		want := []string{}
		for _, raw := range gone[:min(len(named), len(gone))] {
			e := raw.(map[string]any)
			want = append(want, "lesson:"+e["id"].(string)+"@"+e["revision"].(string))
		}
		if len(named) > omittedLessonsListed || !slices.Equal(named, want) {
			t.Fatalf("bound %d: the name lists the lessons that went, first first: %q, want %q", bound, named, want)
		}
		for _, raw := range reply["knowledge"].([]any) {
			e := raw.(map[string]any)
			if strings.Contains(name, "lesson:"+e["id"].(string)+"@"+e["revision"].(string)) {
				t.Fatalf("bound %d: a lesson that stays is named as gone: %s", bound, name)
			}
		}
		if rest := len(gone) - len(named); rest > 0 && len(named) > 0 && !strings.HasSuffix(name, fmt.Sprintf(" and %d more", rest)) {
			t.Fatalf("bound %d: the others are counted (%d): %s", bound, rest, name)
		}
		if len(named) == 0 && !strings.HasSuffix(name, fmt.Sprintf(": %d lessons", len(gone))) {
			t.Fatalf("bound %d: with no room to name them, the lessons are counted (%d): %s", bound, len(gone), name)
		}
		counted = counted || len(named) < len(gone)
		listedAll = listedAll || len(named) == len(gone)
	}
	if !counted || !listedAll {
		t.Fatalf("the bounds tried name all the lessons that went at one and count the others at another: counted %v, all named %v", counted, listedAll)
	}
}

// What the last fields leave behind still holds the state of what was read: a knowledge entry that lost its text keeps body_included
// false and attention_limited true, and no quote of it stays registered, so nothing unseen is scored as read.
func TestLeaveOutLastFieldsKeepTheStateOfWhatWasRead(t *testing.T) {
	reply, w, quoted := tailShape()
	read, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, workMinBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 0 || len(quoted) != 0 {
		t.Fatalf("an article whose text was not delivered is not read or quoted: %v %v", read, quoted)
	}
	for _, raw := range reply["knowledge"].([]any) {
		if e := raw.(map[string]any); e["body_included"] != false || e["attention_limited"] != true {
			t.Fatalf("%v", e)
		}
	}
}

// When even the last fields do not fit, the preparation is refused as before, with the same words.
func TestLeaveOutLastFieldsStillRefuseWhatTheFixedFieldsAloneDoNotFit(t *testing.T) {
	reply, w, quoted := tailShape()
	reply["task"] = strings.Repeat("t", workMinBudget)
	_, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, workMinBudget)
	if err == nil || !strings.Contains(err.Error(), "the task and reference inventory exceed max_bytes (8192); prepare again with a larger max_bytes (up to 393216)") {
		t.Fatalf("the refusal is the one it always was: %v", err)
	}
}

// replyVariables are the names the code gives to a map that becomes a reply of prepare: reply in the code that builds it, and v and
// copy in the code that adds the assignment's context to it (work_assignment_*.go). A reply that is handed on under another name is
// not seen here, and TestRepliesOfRealPreparationsCarryOnlyRegisteredFields is what sees it: it reads the fields of the replies that
// every real way of preparing makes.
var replyVariables = []string{"reply", "v", "copy"}

// scanReplyFields returns the names a source gives to the fields of a reply: reply["name"] = ... (and v and copy, replyVariables) and
// the keys of reply := map[string]any{...}.
func scanReplyFields(name string, src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		return nil, err
	}
	found := []string{}
	key := func(e ast.Expr) {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			s, _ := strconv.Unquote(lit.Value)
			found = append(found, s)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			switch l := lhs.(type) {
			case *ast.IndexExpr:
				if id, ok := l.X.(*ast.Ident); ok && slices.Contains(replyVariables, id.Name) {
					key(l.Index)
				}
			case *ast.Ident:
				if cl, ok := as.Rhs[min(i, len(as.Rhs)-1)].(*ast.CompositeLit); ok && l.Name == "reply" {
					for _, el := range cl.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							key(kv.Key)
						}
					}
				}
			}
		}
		return true
	})
	return found, nil
}

// unregisteredReplyFields returns, sorted, the fields of reply (or the names given) that workReplyFields does not register.
func unregisteredReplyFields(fields []string) []string {
	out := []string{}
	for _, field := range fields {
		if _, ok := workReplyFields[field]; !ok && !slices.Contains(out, field) {
			out = append(out, field)
		}
	}
	slices.Sort(out)
	return out
}

func replyFieldNames(reply map[string]any) []string {
	names := []string{}
	for field := range reply {
		names = append(names, field)
	}
	return names
}

// workReplyFields is an allow list of the fields a reply of prepare can have. The code that builds one (every work*.go file) is read
// for the names it gives to the fields of reply: one that is not registered fails, and so does a registered one nothing builds.
func TestEveryFieldBuiltIntoAPreparationReplyIsRegistered(t *testing.T) {
	files, err := filepath.Glob("work*.go")
	if err != nil || len(files) == 0 {
		t.Fatal(err)
	}
	built := []string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		names, err := scanReplyFields(file, src)
		if err != nil {
			t.Fatal(err)
		}
		built = append(built, names...)
	}
	if missing := unregisteredReplyFields(built); len(missing) > 0 {
		t.Fatalf("the code builds fields of a preparation reply that workReplyFields does not register (add each as kept with a cap, or as one that may go with its way back): %q", missing)
	}
	for field := range workReplyFields {
		if !slices.Contains(built, field) {
			t.Errorf("%s is registered and no code builds it: take it out", field)
		}
	}
}

// Negative example of the allow list: a field the registry does not know is named, whether it is a field of a reply or a name found in code.
func TestAnUnregisteredFieldOfAReplyIsCaught(t *testing.T) {
	src := []byte("package main\nfunc f() {\n\treply := map[string]any{\"receipt\": 1, \"stray_in_literal\": 2}\n\treply[\"stray_by_assignment\"] = 3\n\treply[\"output\"] = 4\n\tv[\"stray_under_v\"] = 5\n\tcopy[\"teamwork\"] = 6\n}\n")
	names, err := scanReplyFields("x.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if got := unregisteredReplyFields(names); !slices.Equal(got, []string{"stray_by_assignment", "stray_in_literal", "stray_under_v"}) {
		t.Fatalf("the scan names the three fields that are not registered: %q from %q", got, names)
	}
	if got := unregisteredReplyFields(replyFieldNames(map[string]any{"receipt": 1, "stray": 2})); !slices.Equal(got, []string{"stray"}) {
		t.Fatalf("a reply with a field that is not registered fails: %q", got)
	}
}

// The replies real preparations make carry only registered fields, through every way of making one: a standalone prepare (whole, and
// under a named bound), the hook's (whole, and as its third try with its last fields left out), a receipt that adds to them (a lesson
// read, the memory upgrade, a search, a consult, a reuse and a source read), and the assignment a host gives (new, and on a receipt),
// which adds its context to the reply under a name of its own. The reading of the code (TestEveryFieldBuiltIntoAPreparationReplyIsRegistered)
// follows the variables called reply, v and copy; this is what sees a field that goes by any other name. The union of what the
// replies carried is held to a floor, so that a way of preparing that stops being reached cannot make the check empty.
func TestRepliesOfRealPreparationsCarryOnlyRegisteredFields(t *testing.T) {
	f := newHookTailFixture(t)
	p := f.article("CSV export filters and totals")
	f.ask(hookTailRequest)
	f.addFeedback(p, 4)
	f.ask(hookTailRequest)
	seen := map[string]bool{}
	check := func(label string, reply map[string]any) {
		t.Helper()
		for field := range reply {
			seen[field] = true
		}
		if missing := unregisteredReplyFields(replyFieldNames(reply)); len(missing) > 0 {
			t.Errorf("%s: fields that are not registered: %q", label, missing)
		}
	}
	_, hook := f.prepare(0)
	check("hook", hook)
	// the hook's third try: the filler takes what the last fields need
	if _, tail := f.prepare(f.capacity()); tail == nil || tail["output"] == nil {
		t.Fatalf("the hook's preparation at its capacity is delivered with its output: %v", tail)
	} else {
		check("hook, last fields left out", tail)
	}
	standalone, err := workCall(t, f.book, "prepare", map[string]any{"request": "Check the export.", "query": "CSV export filters"})
	if err != nil {
		t.Fatal(err)
	}
	check("standalone", standalone)
	bounded, err := workCall(t, f.book, "prepare", map[string]any{"request": "Check the export.", "query": "CSV export filters", "max_bytes": 12288})
	if err != nil {
		t.Fatal(err)
	}
	check("standalone under a named bound", bounded)
	original, err := f.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "Long original", Content: strings.Repeat("<routing condition>", 2048), Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for label, extra := range map[string]map[string]any{
		"lesson":         {"query": "lesson:" + p.ID + "@" + p.Revision},
		"memory-upgrade": {"query": "memory-upgrade"},
		"search":         {"query": "CSV export"},
		"consult":        {"proposal": "Keep the export filters on while the totals are checked."},
		"reuse":          {"conditions": "The export is for the finance team."},
		"source":         {"query": original.ID, "max_bytes": 8192},
	} {
		extra["receipt"] = standalone["receipt"]
		reply, err := workCall(t, f.book, "prepare", extra)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		check(label, reply)
	}
	a := assignmentInput("fields-job", "case-1", "reviewer", "review")
	assigned, err := workCall(t, f.book, "prepare", map[string]any{"query": "CSV export filters", "assignment": a})
	if err != nil {
		t.Fatal("an assignment's preparation:", err)
	}
	check("assignment", assigned)
	extended, err := workCall(t, f.book, "prepare", map[string]any{"query": "CSV export", "assignment": a, "receipt": assigned["receipt"]})
	if err != nil {
		t.Fatal("an assignment's preparation on its receipt:", err)
	}
	check("assignment on a receipt", extended)
	for _, field := range []string{"receipt", "task", "how_to_record", "knowledge", "maintenance", "carried", "output", "source", "source_query", "experience", "teamwork"} {
		if !seen[field] {
			t.Errorf("no real preparation of this test carried %s: the check is not looking at it", field)
		}
	}
}

// Every field the registry says may go can go: with the fixed fields taking nearly all of the smallest bound, each of them, however
// small (600 bytes), is left out and named in output. The three that go in steps of their own (knowledge, maintenance, carried) are
// held to their order above.
func TestEveryMayGoFieldOfTheRegistryIsLeftOutAndNamed(t *testing.T) {
	reply := map[string]any{"receipt": "r", "how_to_record": compactWorkHowTo, "task": strings.Repeat("t", 5600), "knowledge": []any{}}
	mayGo := []string{}
	for field, spec := range workReplyFields {
		if !spec.Kept && field != "knowledge" && field != "maintenance" && field != "carried" {
			reply[field] = map[string]any{"p": strings.Repeat("x", 600)}
			mayGo = append(mayGo, field)
		}
	}
	if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, workMinBudget); err != nil {
		t.Fatal(err)
	}
	omitted := fmt.Sprint(reply["output"].(map[string]any)["omitted"])
	slices.Sort(mayGo)
	for _, field := range mayGo {
		if reply[field] != nil || !strings.Contains(omitted, field) {
			t.Errorf("%s is registered as one that may go, and it stayed (or went unnamed): %v", field, omitted)
		}
	}
	if len(mayGo) < 14 {
		t.Fatalf("the registry lists the fields the bound leaves out: %q", mayGo)
	}
}

// The fields a reply keeps are held to their caps: each test value is the largest the code can make of the field.
func TestKeptFieldsStayWithinTheirCaps(t *testing.T) {
	size := func(field string, value any) {
		t.Helper()
		spec, ok := workReplyFields[field]
		if !ok || !spec.Kept || spec.Cap <= 0 {
			t.Fatalf("%s is not registered as a kept field with a cap: %+v", field, spec)
		}
		if n := workContextSize(value); n > spec.Cap {
			t.Errorf("%s takes %d bytes, over its cap %d", field, n, spec.Cap)
		}
	}
	// the receipt of a session id as long as one may be, made of the characters JSON writes longest
	size("receipt", workEncode(workToken{Session: strings.Repeat("<", 200), Turn: "0123456789abcdef", Nonce: store.NewTraceID() + store.NewTraceID()}))
	// a request is cut only by the bound of the largest reply: preparedWorkTask is what holds it there
	huge := strings.Repeat("<", 60000)
	task, context, err := preparedWorkTask(&hookSession{}, &workState{Standalone: true}, workRequest{Request: huge})
	if err != nil {
		t.Fatal(err)
	}
	size("task", task)
	hookContext := map[string]any{"role": "user", "scope": "stored_request_including_joined_messages", "text_status": "stored_request", "quote_ref": "current", "provided_bytes": 1 << 20, "quote_notice": context["quote_notice"]}
	deliveredTaskHead(strings.Repeat("a ", 30000), hookContext, hookTaskHeadMax)
	size("task_context", hookContext)
	size("task_context", context)
	for _, scalar := range []any{true, uint64(1<<63 + 12345), 1 << 40} {
		for _, field := range []string{"agent_supplied_task", "planning_required", "more_pending", "delivery"} {
			size(field, scalar)
		}
	}
	for _, text := range []string{compactWorkHowTo, continuedWorkHowTo, compactHowToRecord(&workState{}), compactHowToRecord(&workState{Standalone: true}), strings.Replace(compactWorkHowTo, "with this receipt", "with the hook's session_id/turn_id (or this receipt)", 1)} {
		size("how_to_record", text)
	}
	size("knowledge_query", map[string]any{"operation": "query", "query": strings.Repeat("<", requestMax), "cursor": strings.Repeat("c", 200)})
	// output with every name the bound can give
	gone := []any{}
	for i := 1; i <= 64; i++ {
		gone = append(gone, limitShapeEntry(i, 100))
	}
	names := []string{omittedMaintenanceDetails, omittedKnowledgeSentences, omittedCarried, omittedMaintenance, omittedEntriesName(63, gone, omittedLessonsListed), "knowledge[] text and summaries, the last entries first (each query reads its lesson whole)", "knowledge[] titles, the last entries first"}
	for field, spec := range workReplyFields {
		if !spec.Kept && field != "knowledge" && field != "maintenance" && field != "carried" {
			names = append(names, field)
		}
	}
	size("output", workPreparationHint(workPreparationOutput(workSourceBudget*3, names)))
	// the pool of the notices that more than one entry shares: every notice the code writes, twice
	notices := readingNoticeLiterals(t)
	if len(notices) < 4 {
		t.Fatalf("the code writes at least four reading notices: %d", len(notices))
	}
	entries := []any{}
	for _, notice := range notices {
		for n := 0; n < 2; n++ {
			entries = append(entries, map[string]any{"ref": fmt.Sprintf("knowledge%d", len(entries)+1), "reading_notice": notice})
		}
	}
	pooled := map[string]any{"knowledge": entries}
	shareWorkExplanations(pooled, workContextSize)
	if pooled["reading_notices"] == nil {
		t.Fatal("the entries share their notices")
	}
	size("reading_notices", pooled["reading_notices"])
	size("procedure_recovery", pendingProcedureRecovery(errors.New(strings.Repeat("<", 5000))))
	size("procedure_recovery", map[string]any{"status": "saved", "result_source": "0123456789abcdef"})
	size("source_query", map[string]any{"operation": "prepare", "work": map[string]any{"receipt": strings.Repeat("r", 2048), "query": "0123456789abcdef", "cursor": strings.Repeat("c", 16000)}})
	// an answer to an explicit call is sized by the bound its caller names, which is never above the largest
	for _, field := range []string{"experience", "source", "task"} {
		if workReplyFields[field].Cap > workSourceBudget*3 {
			t.Errorf("%s: its cap is above the largest reply", field)
		}
	}
}

// readingNoticeLiterals returns every string the code writes as a reading_notice.
func readingNoticeLiterals(t *testing.T) []string {
	t.Helper()
	literal := regexp.MustCompile(`"reading_notice"(?:\]\s*=|:)\s*("(?:[^"\\]|\\.)*")`)
	out := []string{}
	files, _ := filepath.Glob("work*.go")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range literal.FindAllSubmatch(src, -1) {
			if s, err := strconv.Unquote(string(m[1])); err == nil && len(s) >= 128 {
				out = append(out, s)
			}
		}
	}
	return out
}

// A request that does not fit with the rest of what the hook wrote is delivered as its head, as before: the whole request stays in
// the person's conversation and task_context says so. The last fields never cut it any further.
func TestHookPreparationOfALargeRequestDeliversItsHeadAsBefore(t *testing.T) {
	f := newHookTailFixture(t)
	request := strings.Repeat("Check the export filters and the totals. ", 300) // 12,300 characters
	f.ask(request)
	context, prepared := f.prepare(0)
	if prepared == nil || hookContextLength(context) > hookContextMax {
		t.Fatalf("the preparation is delivered with the head of the request (%d characters)", hookContextLength(context))
	}
	task, _ := prepared["task"].(string)
	taskContext := prepared["task_context"].(map[string]any)
	if task == "" || !strings.HasPrefix(request, task) || len(task) > hookTaskHeadMax || len(task) < hookTaskHeadMax-60 || taskContext["text_status"] != "delivered_head" || taskContext["provided_bytes"] != float64(len(task)) {
		t.Fatalf("the head is the first %d characters or so of the request, and task_context says it is a head: %d bytes, %v", hookTaskHeadMax, len(task), taskContext)
	}
}

func TestLastFieldsNeverCutTheTask(t *testing.T) {
	f := newHookTailFixture(t)
	p := f.article("CSV export filters and totals")
	f.ask(hookTailRequest)
	f.addFeedback(p, 4)
	request := strings.Repeat("Check the CSV export filters and totals. ", 36) // 1,476 characters, kept whole by the hook
	f.ask(request)
	context, prepared := f.prepare(1500)
	if prepared == nil || hookContextLength(context) > hookContextMax {
		t.Fatalf("the preparation is delivered (%d characters)", hookContextLength(context))
	}
	if prepared["task"] != strings.TrimSpace(request) && prepared["task"] != request {
		t.Fatalf("the task is the whole request: %d bytes of %d", len(prepared["task"].(string)), len(request))
	}
	if omitted := replyOmitted(t, prepared); !containsName(omitted, omittedMaintenanceDetails) {
		t.Fatalf("the fixture must leave the maintenance details out: %q", omitted)
	}
}
