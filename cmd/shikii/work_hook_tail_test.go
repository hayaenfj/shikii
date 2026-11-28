package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The hook's own preparation, driven the way hookWorkPreparation drives it: the hook's context before the prepared work (the
// notice the preparation replaces, and filler for what the hook wrote before it: the lessons it quoted, the one-time notices)
// goes in, and what comes out is the context the agent is shown. The filler is the one thing that stands in for the rest of the hook;
// every field of the prepared work is made by the real preparation from a real wiki and a real session.
type hookTailFixture struct {
	t     *testing.T
	repo  string
	root  string
	book  notebook.Book
	turn  string
	input string // the person's request
}

const hookTailRequest = "Check the CSV export filters and totals."

func newHookTailFixture(t *testing.T) *hookTailFixture {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	return &hookTailFixture{t: t, repo: repo, root: root, book: notebook.Book{Root: root, Scope: feedbackScope(repo)}, input: hookTailRequest}
}

// ask sends a request through the real hook (which prepares it once) and keeps its turn.
func (f *hookTailFixture) ask(prompt string) {
	f.t.Helper()
	runUnreviewedHook(f.t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": prompt})
	f.turn, f.input = loadSession(f.root, "planning").TurnID, prompt
}

// prepare runs the hook's preparation of the current request again, with filler chars of context before it. A refused preparation
// leaves the context as it was: the manual recovery notice. It returns the context and the prepared work when there is one.
func (f *hookTailFixture) prepare(filler int) (context string, prepared map[string]any) {
	f.t.Helper()
	if err := updateSession(f.root, "planning", func(s *hookSession) { s.Planning.Work = nil }); err != nil {
		f.t.Fatal(err)
	}
	in := hookInput{SessionID: "planning", CWD: f.repo, Prompt: f.input, workTurn: f.turn}
	out := hookOut{Context: planningNotice("planning", f.turn) + strings.Repeat("x", filler)}
	prepareHookWork(f.root, &in, &out)
	const prefix = "shikii prepared work:\n"
	i := strings.Index(out.Context, prefix)
	if i < 0 {
		return out.Context, nil
	}
	if err := json.Unmarshal([]byte(out.Context[i+len(prefix):]), &prepared); err != nil {
		f.t.Fatal("the prepared work must reach the agent as JSON", err)
	}
	return out.Context, prepared
}

// capacity is the most filler the hook's preparation of the current request still delivers with, found by halving.
func (f *hookTailFixture) capacity() int {
	f.t.Helper()
	if _, p := f.prepare(0); p == nil {
		return -1
	}
	low, high := 0, hookContextMax
	for low < high {
		if middle := (low + high + 1) / 2; func() bool { _, p := f.prepare(middle); return p != nil }() {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return low
}

func (f *hookTailFixture) article(title string) notebook.Page {
	return interimAddArticle(f.t, f.book, title, strings.Repeat("The export filters stay on until the totals are checked.\n\n", 60))
}

// addFeedback gives the article n harmful assessments of a delivery, each with the decision and evidence the agent wrote: the review
// of the article is due with them, and the maintenance task of the article carries three of them whole.
func (f *hookTailFixture) addFeedback(p notebook.Page, n int) {
	f.t.Helper()
	st := store.Store{Dir: f.root}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("%016x", 0x900+i)
		if err := st.AppendFeedback(f.book.Scope, store.Feedback{ID: id, Request: "CSV export", Summary: "Checked the export.", Reply: "The export is checked."}); err != nil {
			f.t.Fatal(err)
		}
		key := "lesson:" + p.ID + ":" + p.Revision
		d := store.MemoryDelivery{ID: id, Scope: f.book.Scope, Request: "CSV export", Phrase: "git diff", Quotes: []store.MemoryQuote{{Key: key, ID: p.ID, Revision: p.Revision, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}}}}
		if err := st.SaveMemoryDelivery(d); err != nil {
			f.t.Fatal(err)
		}
		a := store.MemoryAssessment{SourceID: id, Key: key, Result: "harmful", Decision: strings.Repeat("d", 700), Evidence: strings.Repeat("e", 900), At: time.Now()}
		if err := st.SaveMemoryAssessment(f.book.Scope, a); err != nil {
			f.t.Fatal(err)
		}
	}
}

func replyOmitted(t *testing.T, prepared map[string]any) []string {
	t.Helper()
	output, _ := prepared["output"].(map[string]any)
	names := []string{}
	for _, raw := range output["omitted"].([]any) {
		names = append(names, raw.(string))
	}
	return names
}

func containsName(names []string, want string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return strings.HasPrefix(name, want) })
}

// A delivered article whose review is due with harmful feedback brings a maintenance task of several KB, and that field was not
// shortened or left out: the hook's preparation was refused at any filler over 553 characters (measured), with no other field
// large. The hook fell back to the manual prepare. Now the details go, the task keeps its head and its ref, and output names it.
func TestHookPreparationDeliversWhenMaintenanceIsLarge(t *testing.T) {
	f := newHookTailFixture(t)
	p := f.article("CSV export filters and totals")
	f.ask(hookTailRequest)
	f.addFeedback(p, 4)
	f.ask(hookTailRequest)
	filler := 5200 // the baseline (no article at all) takes 7118; a quarter of the bound is what the hook wrote before
	context, prepared := f.prepare(filler)
	if prepared == nil || hookContextLength(context) > hookContextMax {
		t.Fatalf("the preparation must be delivered, not refused to the manual recovery (%d characters): %.300s", hookContextLength(context), context)
	}
	if strings.Contains(context, "shikii: prepare this task once") {
		t.Fatal("the manual recovery notice must not come back")
	}
	items, _ := prepared["maintenance"].([]any)
	if len(items) != 1 {
		t.Fatalf("the task keeps one head: %v", prepared["maintenance"])
	}
	item := items[0].(map[string]any)
	task := item["task"].(map[string]any)
	if item["ref"] != "maintenance1" || item["incomplete"] != true || task["page_id"] != p.ID || task["title"] != p.Title || task["feedback"] != nil || task["feedback_total"] != float64(4) || !strings.Contains(task["next_action"].(string), "reflect query") {
		t.Fatalf("the head keeps the ref, the page, the title and how many pieces of feedback, and says where the rest is: %v", item)
	}
	omitted := replyOmitted(t, prepared)
	if !containsName(omitted, omittedMaintenanceDetails) {
		t.Fatalf("output names what went: %q", omitted)
	}
	output := prepared["output"].(map[string]any)
	if !strings.Contains(output["next_action"].(string), "reflect query") {
		t.Fatalf("the way back is in the reply: %v", output["next_action"])
	}
	// the head's ref is the one the work recorded: the review can still be recorded with this receipt
	s := loadSession(f.root, "planning")
	if review := s.Planning.Work.Maintenance["maintenance1"]; review.PageID != p.ID {
		t.Fatalf("the work still holds the review the ref names: %v", s.Planning.Work.Maintenance)
	}
}

// What the maintenance task carries whole is still read with the way back the reply names: reflect query lists it.
func TestHookPreparationMaintenanceHeadIsAnswerableByReflectQuery(t *testing.T) {
	f := newHookTailFixture(t)
	p := f.article("CSV export filters and totals")
	f.ask(hookTailRequest)
	f.addFeedback(p, 4)
	f.ask(hookTailRequest)
	_, prepared := f.prepare(5200)
	if prepared == nil {
		t.Fatal("not delivered")
	}
	head := prepared["maintenance"].([]any)[0].(map[string]any)["task"].(map[string]any)
	view, err := reflectQueryAs(f.root, f.book.Scope, "planning")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range view.Maintenance {
		found = found || task.PageID == head["page_id"] && task.Key == head["key"] && len(task.Feedback) == 3
	}
	if !found {
		t.Fatalf("reflect query lists the task the head names, whole: %+v", view.Maintenance)
	}
}

// carriedFixture is the hook's session in which the first request read articleCount articles (each its own reference) and a second
// request came before the first was ended: the preparation of the second hands the references of the first over (carried).
func carriedFixture(t *testing.T, articleCount int, interrupted bool) *hookTailFixture {
	f := newHookTailFixture(t)
	pages := []notebook.Page{}
	for i := 1; i <= articleCount; i++ {
		pages = append(pages, f.article(fmt.Sprintf("CSV export filters and totals %d", i)))
	}
	f.ask(hookTailRequest)
	if !interrupted {
		return f
	}
	work := loadSession(f.root, "planning").Planning.Work
	token := workEncode(workToken{Session: "planning", Turn: f.turn, Nonce: work.Nonce})
	for _, p := range pages {
		if _, err := workCall(t, f.book, "prepare", map[string]any{"receipt": token, "query": "lesson:" + p.ID + "@" + p.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	f.ask("Also check the totals of the CSV export.")
	return f
}

// An earlier request that was still being worked on hands its references over (carried), and the reply says they stay valid. With
// many articles read in that request the list is long, and it was neither shortened nor left out. The same session without the
// interrupted request delivers at most twin characters of filler; with carried, a few hundred less would have been the limit, and the
// preparation is delivered at that filler without carried.
func TestHookPreparationDeliversWhenCarriedIsLarge(t *testing.T) {
	twin := carriedFixture(t, 28, false).capacity()
	f := carriedFixture(t, 28, true)
	f.input = "Also check the totals of the CSV export."
	if _, with := f.prepare(0); with == nil || with["carried"] == nil || workContextSize(with["carried"]) < 700 {
		t.Fatalf("the interrupted request hands its references over, and the list is long: %v", with["carried"])
	}
	context, prepared := f.prepare(twin - 300)
	if prepared == nil || hookContextLength(context) > hookContextMax || prepared["carried"] != nil {
		t.Fatalf("the preparation is delivered without carried (%d characters): %.200s", hookContextLength(context), context)
	}
	if omitted := replyOmitted(t, prepared); !containsName(omitted, omittedCarried) || !strings.Contains(omittedCarried, "stay valid with this receipt") {
		t.Fatalf("output names carried and says its references stay valid: %q", omitted)
	}
	// the way back the documents give (recording.md): preparing again with the hook's ids, which makes a new receipt, lists them again
	again, err := workCall(t, f.book, "prepare", map[string]any{"session_id": "planning", "turn_id": f.turn, "query": "CSV export filters"})
	if err != nil {
		t.Fatal(err)
	}
	listed, _ := again["carried"].(map[string]any)
	if articles, _ := listed["articles"].([]any); len(articles) < 28 {
		t.Fatalf("a new preparation with the hook's ids lists the references the hook's reply left out: %v", again["carried"])
	}
}

// The knowledge entries of a reply, down to their references, are what is left when the filler is large: the two sentences each
// repeats go before the entries do, and every entry keeps its reference, id and exact revision.
func TestHookPreparationKnowledgeEntriesLoseTheirRepeatedSentencesBeforeAnyEntryGoes(t *testing.T) {
	bare := newHookTailFixture(t)
	bare.ask(hookTailRequest)
	twin := bare.capacity()
	f := newHookTailFixture(t)
	for i := 1; i <= 3; i++ {
		f.article(fmt.Sprintf("CSV export filters and totals %d", i))
	}
	f.ask(hookTailRequest)
	_, whole := f.prepare(0)
	delivered := len(whole["knowledge"].([]any))
	context, prepared := f.prepare(twin - 1000)
	if prepared == nil || hookContextLength(context) > hookContextMax {
		t.Fatalf("the preparation is delivered (%d characters): %.200s", hookContextLength(context), context)
	}
	entries := prepared["knowledge"].([]any)
	if delivered < 2 || len(entries) != delivered {
		t.Fatalf("the hook delivers articles and no entry went: %d of %d", len(entries), delivered)
	}
	for _, raw := range entries {
		e := raw.(map[string]any)
		if e["id"] == nil || e["revision"] == nil || e["ref"] == nil || e["body_included"] != false || e["attention_limited"] != true || e["query"] != nil || e["reading_notice"] != nil {
			t.Fatalf("an entry keeps its reference, id and exact revision and the state of what was read, and nothing else: %v", e)
		}
	}
	if omitted := replyOmitted(t, prepared); !containsName(omitted, omittedKnowledgeSentences) || !strings.Contains(omittedKnowledgeSentences, "lesson:<id>@<revision>") {
		t.Fatalf("output says how to read each article: %q", omitted)
	}
}

// The CLI's, the MCP server's and the HTTP API's prepare go through the same bound but not through the last try that the hook has: a
// refusal is what tells the caller to ask again with a larger max_bytes, and a reply without its last fields would take that away.
// At the smallest max_bytes a caller can name, an article with feedback made the preparation refused ("the task and reference
// inventory exceed max_bytes"), and it still is, with the same words; the hook's third try, which does leave the last fields out, is
// what delivers the same preparation (TestHookPreparationDeliversWhenMaintenanceIsLarge).
func TestPrepareAtTheSmallestBoundIsRefusedAsBeforeWhenMaintenanceIsLarge(t *testing.T) {
	f := newHookTailFixture(t)
	p := f.article("CSV export filters and totals")
	f.ask(hookTailRequest)
	f.addFeedback(p, 4)
	_, err := workCall(t, f.book, "prepare", map[string]any{"request": "Check the CSV export filters and totals.", "query": "CSV export filters and totals", "max_bytes": workMinBudget})
	if err == nil || err.Error() != fmt.Sprintf("the task and reference inventory exceed max_bytes (%d); prepare again with a larger max_bytes (up to %d)", workMinBudget, workSourceBudget*3) {
		t.Fatalf("the preparation is refused with the words that name the larger max_bytes: %v", err)
	}
	reply, err := workCall(t, f.book, "prepare", map[string]any{"request": "Check the CSV export filters and totals.", "query": "CSV export filters and totals", "max_bytes": 28 << 10})
	if err != nil {
		t.Fatal("the larger max_bytes the refusal names delivers the preparation:", err)
	}
	if reply["output"] != nil {
		t.Fatalf("it delivers the fields whole: %v", reply["output"])
	}
}
