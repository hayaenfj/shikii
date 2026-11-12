package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

type reflectRequest struct {
	Operation   string                       `json:"operation"`
	Sources     []string                     `json:"sources,omitempty"` // a copy of query's targets (same shape as the hook's reason text; ignored, since query picks its own targets; audit R5)
	Reviews     []reviewRequest              `json:"reviews,omitempty"`
	Wiki        []notebook.Draft             `json:"wiki,omitempty"`
	Memory      []memoryAssessmentRequest    `json:"memory,omitempty"`
	Maintenance []notebook.MaintenanceReview `json:"maintenance,omitempty"`
	Discoveries []notebook.Discovery         `json:"discoveries,omitempty"`
}

type memoryAssessmentRequest struct {
	CorrectionResult string `json:"correction_result,omitempty"`
	SourceID         string `json:"source_id"`
	Key              string `json:"key"`
	Result           string `json:"result"`
	Decision         string `json:"decision"`
	Evidence         string `json:"evidence"`
}

type reflectSource struct {
	Source      store.Feedback            `json:"source_material"`
	Targets     []reviewTarget            `json:"targets"`
	WikiPending bool                      `json:"wiki_pending"`
	Memories    []store.MemoryObservation `json:"memories"`
	Head        bool                      `json:"head,omitempty"` // the text is cut (0.12): the oldest two conversations come whole, the next as heads, and the ones that do not fit are counted (more_pending)
}

// reflectRawSources is how many pending conversations come with their raw text; the rest come as heads (0.12). Each cut field keeps
// reflectHeadBytes bytes.
const (
	reflectRawSources = 2
	reflectHeadBytes  = 200
	reflectHeadsMax   = 40 // heads listed at most in one reply; the rest are counted in more_pending (an MCP reply has a size limit on the agent's side)
)

type reflectView struct {
	HowToSubmit  string                     `json:"how_to_submit"`
	Sources      []reflectSource            `json:"sources"`
	Lessons      []notebook.Page            `json:"lessons"`
	Corrections  any                        `json:"human_corrections"`
	MemoryEffect store.MemorySummary        `json:"memory_effect"`
	Observations []notebook.ObservationHead `json:"observations,omitempty"`
	Maintenance  []notebook.MaintenanceTask `json:"maintenance,omitempty"`
	MorePending  int                        `json:"more_pending,omitempty"` // pending conversations beyond the heads listed (0.12): they come in the next query once these are settled
}

// Do not make scoring and the wiki take the same raw text twice. The oldest reflectRawSources conversations come with their raw text,
// up to reflectHeadsMax more as heads; the rest are counted (more_pending).
func reflectQuery(root, scope string) (reflectView, error) {
	return reflectQueryWith(root, scope, reflectHeadsMax)
}

// reflectQueryWith is reflectQuery with the number of heads bounded by headsMax (0: every pending conversation, for working out what is
// left of the ones a submit named).
func reflectQueryWith(root, scope string, headsMax int) (reflectView, error) {
	if eventPages != nil {
		return reflectQueryBook(wikiBook(root, scope), headsMax)
	}
	return reflectQueryBook((notebook.Book{Root: root, Scope: scope}).ReadView(), headsMax)
}

func reflectQueryBook(book notebook.Book, headsMax int) (reflectView, error) {
	v, _, err := reflectQueryParts(book, headsMax, false)
	return v, err
}

// reflectPending is the reflection query for the hooks, which read only the pending conversations and observations (who to ask
// about before the request, what is left at Stop). It does not search the lessons near the oldest conversations or work out the
// maintenance tasks; rest runs exactly those steps and returns their error. A hook calls rest before it asks for the reflection or
// stops the turn on it, so those answers still fail when the full query would. When nothing is asked and nothing is left, the hook
// skips them: a wiki whose maintenance cannot be read no longer turns a finished reflection into an error.
func reflectPending(root, scope string, headsMax int) (v reflectView, rest func() error, err error) {
	book := wikiBook(root, scope)
	if eventPages == nil {
		book = book.ReadView()
	}
	return reflectQueryParts(book, headsMax, true)
}

// reflectQueryParts is the reflection query. With pendingOnly, the lessons near the conversations that come with their raw text and
// the maintenance tasks are left out of the view, and rest computes them for their error (rest is nil otherwise).
func reflectQueryParts(book notebook.Book, headsMax int, pendingOnly bool) (reflectView, func() error, error) {
	root, scope := book.Root, book.Scope
	var lessonQueries []string // the searches pendingOnly leaves to rest, in the order the full query runs them
	v := reflectView{HowToSubmit: reflectHowToSubmit, Sources: []reflectSource{}, Lessons: []notebook.Page{}}
	st := store.Store{Dir: root}
	var recent []store.Feedback
	report, err := st.MemoryReportWithFeedback(scope, func() ([]store.Feedback, error) {
		var err error
		recent, err = book.RecentFeedback()
		return recent, err
	})
	if err != nil {
		return v, nil, err
	}
	v.MemoryEffect = report.MemorySummary
	memories := map[string][]store.MemoryObservation{}
	for _, item := range report.All {
		if item.Assessment == nil {
			key := item.SourceID
			if item.Parent != "" { // deliveries to subagents are assessed alongside the parent's conversation (source_id stays the delivery id)
				key = item.Parent
			}
			memories[key] = append(memories[key], item)
		}
	}
	// With the first 2 items of each queue, every target of the first 2 conversations combined is present.
	reviews, err := reviewQueryFeedback(st, scope, recent)
	if err != nil {
		return v, nil, err
	}
	v.Corrections = reviews["human_corrections"]
	targets := map[string][]reviewTarget{}
	for _, task := range reviews["pending"].([]reviewTask) {
		targets[task.Source.ID] = task.Targets
	}
	pending, err := book.PendingSourcesFromFeedback(recent)
	if err != nil {
		return v, nil, err
	}
	wiki := map[string]bool{}
	for _, source := range pending {
		wiki[source.ID] = true
	}
	// the pending conversations are listed oldest first (0.12: one reflection is one round trip). The oldest reflectRawSources come
	// with their raw text and the lessons close to them; the next reflectHeadsMax come as heads with their targets and deliveries, so
	// one submit can settle all that is listed. The ones beyond that, and the ones the bound of the reply makes wait
	// (limitReflectQuery), are counted in more_pending and listed by the next query.
	seen := map[string]bool{}
	for i := len(recent) - 1; i >= 0; i-- {
		source := recent[i]
		if len(targets[source.ID]) == 0 && !wiki[source.ID] && len(memories[source.ID]) == 0 {
			continue
		}
		t := targets[source.ID]
		if t == nil {
			t = []reviewTarget{}
		}
		m := memories[source.ID]
		if m == nil {
			m = []store.MemoryObservation{}
		}
		if len(v.Sources) >= reflectRawSources {
			if headsMax > 0 && len(v.Sources) >= reflectRawSources+headsMax {
				v.MorePending++ // beyond the heads one reply carries: counted, listed by the next query once these are settled
				continue
			}
			v.Sources = append(v.Sources, reflectHead(reflectSource{Source: source, Targets: t, WikiPending: wiki[source.ID], Memories: m}))
			continue
		}
		v.Sources = append(v.Sources, reflectSource{Source: source, Targets: t, WikiPending: wiki[source.ID], Memories: m})
		// Also return existing lessons close to the topic being sorted, to avoid careless duplicates and replacements.
		if len(t) == 0 && len(m) == 0 {
			continue
		} // Acknowledgements need no unrelated article search; earlier scoring corrections remain available.
		if pendingOnly {
			lessonQueries = append(lessonQueries, source.Request+" "+source.Reply)
			continue
		}
		lessons, err := book.Query(source.Request + " " + source.Reply)
		if err != nil {
			return v, nil, err
		}
		for _, lesson := range lessons.Lessons {
			if !seen[lesson.ID] {
				seen[lesson.ID] = true
				v.Lessons = append(v.Lessons, lesson)
			}
		}
	}
	observations, err := book.PendingObservations()
	if err != nil {
		return v, nil, err
	}
	v.Observations = notebook.Heads(observations)
	if len(v.Observations) > reflectRawSources {
		v.Observations = v.Observations[:reflectRawSources]
	}
	if pendingOnly {
		return v, func() error {
			for _, query := range lessonQueries {
				if _, err := book.Query(query); err != nil {
					return err
				}
			}
			_, err := book.Maintenance()
			return err
		}, nil
	}
	v.Maintenance, err = book.Maintenance()
	if err != nil {
		return v, nil, err
	}
	return v, nil, nil
}

// reflectHead is a pending conversation as a head: its texts cut to reflectHeadBytes, with its targets and the keys of its
// deliveries (what submit needs) kept, so a head stays a few hundred bytes (audit B of 0.12).
func reflectHead(s reflectSource) reflectSource {
	source := s.Source
	source.Request, source.Summary, source.Reply = cut(source.Request, reflectHeadBytes), cut(source.Summary, reflectHeadBytes), cut(source.Reply, reflectHeadBytes)
	cutMemories := make([]store.MemoryObservation, 0, len(s.Memories))
	for _, o := range s.Memories {
		o.Request = cut(o.Request, reflectHeadBytes)
		o.Quote.Lesson, o.Quote.AppliesTo, o.Quote.Said = cut(o.Quote.Lesson, reflectHeadBytes), cut(o.Quote.AppliesTo, reflectHeadBytes), cut(o.Quote.Said, reflectHeadBytes)
		o.Quote.Request, o.Quote.Summary, o.Quote.Reply = cut(o.Quote.Request, reflectHeadBytes), cut(o.Quote.Summary, reflectHeadBytes), cut(o.Quote.Reply, reflectHeadBytes)
		o.Quote.Rationale, o.Quote.DiscoveredFor = cut(o.Quote.Rationale, reflectHeadBytes), cut(o.Quote.DiscoveredFor, reflectHeadBytes)
		if c := o.Quote.Correction; c != nil { // a copy: the delivery's record keeps the correction whole
			shortened := *c
			shortened.Quote, shortened.Action = cut(c.Quote, reflectHeadBytes), cut(c.Action, reflectHeadBytes)
			o.Quote.Correction = &shortened
		}
		cutMemories = append(cutMemories, o)
	}
	// The targets keep what submit needs (spec, trace_id, the question) and the list of changed files, which is what a target
	// about the scope of the changes is scored on (30 paths at most). The texts of the conversation they carry are cut like the
	// conversation's own: a target holds the whole request in original_inputs, so a head of a conversation with a pasted log was
	// as large as the conversation itself and made every conversation after it wait (audit of 0.31.0).
	cutTargets := make([]reviewTarget, 0, len(s.Targets))
	for _, target := range s.Targets {
		if len(target.OriginalInputs) > 0 {
			inputs := make(map[string]string, len(target.OriginalInputs))
			for key, text := range target.OriginalInputs {
				inputs[key] = cut(text, reflectHeadBytes)
			}
			target.OriginalInputs = inputs
		}
		cutTargets = append(cutTargets, target)
	}
	return reflectSource{Source: source, Targets: cutTargets, WikiPending: s.WikiPending, Memories: cutMemories, Head: true}
}

// reflectBare is a conversation whose delivered lessons keep only what submit needs: the source, the key, the page and its
// revision, who it went to, and a correction cut like a head's. A turn that started many subagents has a delivery for each (61 in
// the audit of 0.31.0), and even its head was over the CLI's bound; wiki query lesson:<id> reads a lesson whole.
func reflectBare(s reflectSource) reflectSource {
	bare := make([]store.MemoryObservation, 0, len(s.Memories))
	for _, o := range s.Memories {
		q := store.MemoryQuote{Key: o.Quote.Key, ID: o.Quote.ID, Revision: o.Quote.Revision}
		if c := o.Quote.Correction; c != nil { // a correction is answered with correction_result: the delivery says it is one, and
			// keeps only that (its words are read with wiki query lesson:<id>; with them, 61 deliveries of a corrected lesson were
			// over the CLI's bound: audit of 0.31.0)
			q.Correction = &store.Correction{SourceID: c.SourceID}
		}
		bare = append(bare, store.MemoryObservation{SourceID: o.SourceID, Parent: o.Parent, Agent: o.Agent, At: o.At, Quote: q})
	}
	s.Memories = bare
	return s
}

type reflectReceipt struct {
	PageID   string   `json:"page_id,omitempty"`
	TraceID  string   `json:"trace_id,omitempty"`
	Sources  []string `json:"sources,omitempty"`
	Error    string   `json:"error,omitempty"`
	SourceID string   `json:"source_id,omitempty"`
	Key      string   `json:"key,omitempty"`
}

type reflectResult struct {
	notebook.Remaining                  // remaining unclassified pages (0.9.0). Delivered in the save reply even to agents without hooks
	Complete           bool             `json:"complete"`
	Reviews            []reflectReceipt `json:"reviews"`
	Wiki               []reflectReceipt `json:"wiki"`
	Memory             []reflectReceipt `json:"memory"`
	Maintenance        []reflectReceipt `json:"maintenance,omitempty"`
	Discoveries        []reflectReceipt `json:"discoveries,omitempty"`
	Left               []string         `json:"left,omitempty"`        // what is still not done for the conversations this submit named (complete only says every item was saved)
	LeftMemory         []reflectReceipt `json:"left_memory,omitempty"` // the deliveries still to assess, as the items to send in memory (0.12)
	LeftError          string           `json:"left_error,omitempty"`  // left could not be worked out (it is not empty because nothing is left)
}

func runReflect(root, scope string, req reflectRequest) (any, error) {
	return runReflectWithCommit(root, scope, req, nil)
}

// A prepared work receipt guards each storage operation against a turn change.
// Ordinary reflection has no turn constraint and keeps its existing behavior.
func runReflectWithCommit(root, scope string, req reflectRequest, commit workCommitter) (any, error) {
	if commit == nil {
		commit = func(action func() error) error { return action() }
	}

	if scope == "" {
		return nil, errors.New("cannot determine the project to reflect on")
	}
	if req.Operation == "query" {
		if len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance)+len(req.Discoveries) != 0 {
			return nil, errors.New("query cannot take content to save")
		}
		sweepDeferrals(root, "") // settle outcomes left waiting by sessions closed mid-turn before reading what is left to reflect on
		return reflectQuery(root, scope)
	}
	if req.Operation == "place" {
		if len(req.Reviews)+len(req.Memory)+len(req.Maintenance)+len(req.Discoveries) != 0 {
			return nil, errors.New("place takes only wiki drafts")
		}
		return placeDrafts(root, scope, req.Wiki)
	}
	// submit does not limit the number of conversations, items or reviews handled at once (0.9.1; only the input size is limited)
	if req.Operation != "submit" || len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance)+len(req.Discoveries) == 0 {
		return nil, errors.New("submit takes provisional scores, wiki sorting, provisional memory assessments or reviews (no limit on the number)")
	}
	for _, review := range req.Reviews {
		if review.Operation != "" && review.Operation != "submit" {
			return nil, errors.New("a batch save takes only the submit of provisional scores")
		}
	}
	// A batch over different stores returns a receipt per item. Failures are not hidden, and the successful part can be sent again.
	result := reflectResult{Complete: true, Reviews: []reflectReceipt{}, Wiki: []reflectReceipt{}, Memory: []reflectReceipt{}}
	for _, d := range req.Discoveries {
		r := reflectReceipt{PageID: d.PageID, Key: d.LookupID}
		if err := commit(func() error { return (notebook.Book{Root: root, Scope: scope}).Discover(d) }); err != nil {
			result.Complete = false
			r.Error = err.Error()
		}
		result.Discoveries = append(result.Discoveries, r)
	}
	for _, review := range req.Reviews {
		review.Operation = "submit"
		err := commit(func() error { _, e := runReview(root, scope, review); return e })
		receipt := reflectReceipt{TraceID: review.TraceID}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Reviews = append(result.Reviews, receipt)
	}
	// the maintenance keys before and after this submit's own wiki saves (0.12): a review sent with the key read before the save stands
	var keysBefore, keysAfter map[string]notebook.MaintenanceTask
	if len(req.Maintenance) > 0 && len(req.Wiki) > 0 {
		keysBefore, _ = notebook.Book{Root: root, Scope: scope}.MaintenanceKeys()
	}
	touched := map[string]bool{}
	for _, draft := range req.Wiki {
		// the truth from the result for the insertion-point and contradiction decisions is built before saving and written after the save succeeds (once the page changes, it can no longer be matched with the decision's input)
		var res notebook.Result
		err := commit(func() error { var e error; res, e = ingestWithOutcome(root, scope, draft); return e })
		if err == nil && res.Page != nil && res.Page.ID != "" { // a sources-only draft saves no page
			touched[res.Page.ID] = true // the pages this submit's own saves changed: a review's key is remapped only for them (audit B of 0.12)
		}
		receipt := reflectReceipt{Sources: draft.Sources}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Wiki = append(result.Wiki, receipt)
	}
	if keysBefore != nil {
		keysAfter, _ = notebook.Book{Root: root, Scope: scope}.MaintenanceKeys()
	}
	for _, a := range req.Memory {
		neutral := a.CorrectionResult == "" || a.CorrectionResult == "unknown"
		if neutral && a.Evidence == "" && (a.Result == "unknown" || a.Result == "unused") {
			if a.Result == "unknown" {
				a.Evidence = "No effect assessment was supplied; usefulness remains unknown."
			} else {
				a.Evidence = "The agent reports that this memory was not used for this task."
			}
		}
		if neutral && a.Decision == "" && a.Result == "unused" {
			a.Decision = "No task decision used this memory."
		}
		assessment := store.MemoryAssessment{SourceID: a.SourceID, Key: a.Key, Result: a.Result, Decision: a.Decision, Evidence: a.Evidence, CorrectionResult: a.CorrectionResult}
		err := commit(func() error {
			if e := (store.Store{Dir: root}).SaveMemoryAssessment(scope, assessment); e != nil {
				return e
			}
			if e := labelLessonApplies(store.Store{Dir: root}, scope, assessment); e != nil {
				hookLog("lesson-applies truth: " + e.Error())
			}
			return nil
		})
		receipt := reflectReceipt{SourceID: a.SourceID, Key: a.Key}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Memory = append(result.Memory, receipt)
	}
	for _, m := range req.Maintenance {
		// a review sent with the key the page had before this submit's own saves stands for the page as saved (0.12): the agent read the
		// page, then changed it in the same call, so the review is recorded against the new revision and key
		if before, ok := keysBefore[m.PageID]; ok && before.Key == m.Key && before.Revision == m.Revision {
			changedHere := touched[m.PageID]
			for dep := range before.Dependencies {
				changedHere = changedHere || touched[dep]
			}
			after, ok := keysAfter[m.PageID]
			for dep := range after.Dependencies { // a page this submit created with related pointing here is a new dependency (confirmation audit 1)
				changedHere = changedHere || touched[dep]
			}
			if ok && changedHere { // not for a change another process made meanwhile: that one is read again
				m.Revision, m.Key = after.Revision, after.Key
			}
		}
		err := commit(func() error { _, e := (notebook.Book{Root: root, Scope: scope}).Maintain(m); return e })
		receipt := reflectReceipt{PageID: m.PageID, Key: m.Key}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Maintenance = append(result.Maintenance, receipt)
	}
	// A submit that sorted the wiki but left provisional scores or delivered lessons came back complete with nothing else,
	// so the agent stopped while the Stop hook kept asking (behaviour check 0.9.2). Say what is still left.
	named := []string{}
	for _, r := range req.Reviews {
		named = append(named, r.SourceID)
	}
	for _, d := range req.Wiki {
		named = append(named, d.Sources...)
	}
	for _, a := range req.Memory {
		named = append(named, a.SourceID)
	}
	// Do not let left vanish silently: a failure to work it out is said, so the agent runs query instead of stopping.
	if left, err := reflectLeft(root, scope, named); err != nil {
		result.LeftError = "could not work out what is left (" + err.Error() + "). Run query to see the conversations still pending"
	} else if len(left) > 0 {
		result.Left = leftLines(left)
		for _, l := range left {
			result.LeftMemory = append(result.LeftMemory, l.items...)
		}
	}
	result.Remaining = notebook.Book{Root: root, Scope: scope}.RemainingUnclassified()
	return result, nil
}

// reflectInputMax is the input limit for reflection (0.9.1; 1 MiB, the same as writing a work record and the HTTP endpoint's maxBody).
// The number of conversations, items and reviews is not limited, so this is the only limit. The per-page limits (body 16 KB, whole file 64 KB) are checked separately by notebook
const reflectInputMax = 1 << 20

func decodeReflect(input io.Reader) (reflectRequest, error) {
	var req reflectRequest
	data, err := io.ReadAll(io.LimitReader(input, reflectInputMax+1))
	if err != nil || len(data) > reflectInputMax {
		return req, errors.New("reflection input is JSON of at most 1 MiB. Split the drafts and send the rest in the next submit")
	}
	err, extra := decodeStrictObject(data, &req)
	if err != nil {
		return req, fmt.Errorf("check the reflection JSON: %v", err)
	}
	if extra {
		return req, errors.New("pass reflection JSON one object at a time")
	}
	return req, nil
}

func cmdReflect(args []string) error {
	if helpAsked(args, 2) {
		return printAgentUsage("reflect")
	}
	if problem, ok := operationProblem("reflect", []string{"query", "place", "submit"}, args); !ok {
		return usageRefusal("reflect", problem)
	}
	req := reflectRequest{Operation: args[0]}
	if args[0] != "query" {
		// read once here, so an empty standard input is told apart from JSON that cannot be read; decodeReflect keeps its own limit
		data, err := io.ReadAll(io.LimitReader(stdin, reflectInputMax+1))
		if err == nil && noJSON(data) {
			return emptyInputRefusal("reflect", "reflect "+args[0])
		}
		var input io.Reader = bytes.NewReader(data)
		if err != nil {
			input = io.MultiReader(input, stdin) // the read failed: let decodeReflect meet the same failure and report it
		}
		req, err = decodeReflect(input)
		if err != nil {
			return err
		}
		if req.Operation != "" && req.Operation != args[0] {
			return fmt.Errorf("the JSON for %s cannot name a different operation", args[0])
		}
		req.Operation = args[0]
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	result, err := runReflect(root, scope, req)
	if err != nil {
		return err
	}
	if view, ok := result.(reflectView); ok { // query: within what the agent's shell shows whole, like work prepare
		result = limitReflectQuery(view, cliPrepareBytes, func(v any) int { return workContextSize(v) + 1 })
	}
	return writeReply(stdout, result)
}

func (s *server) handleReflect(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	req, err := decodeReflect(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	result, err := runReflect(s.o.storeDir, s.o.projectScope, req)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	// through MCP the whole tool result is measured, as for work prepare; a plain HTTP caller gets the reply whole
	if view, ok := result.(reflectView); ok {
		if size, _ := r.Context().Value(workResponseSizeKey{}).(func(any) int); size != nil {
			budget, _ := workOutputBudget(0)
			result = limitReflectQuery(view, budget, size)
		}
	}
	writeReplyJSON(w, 200, result)
}

func reflectContext(root, scope string) (string, []string, error) {
	view, rest, err := reflectPending(root, scope, reflectHeadsMax)
	if err != nil || len(view.Sources)+len(view.Observations) == 0 {
		return "", nil, err
	}
	oldest := time.Time{}
	if len(view.Sources) > 0 {
		oldest = view.Sources[0].Source.At
	}
	if o := oldestObservation(view.Observations); !o.IsZero() && (oldest.IsZero() || o.Before(oldest)) {
		oldest = o
	}
	if len(view.Sources)+len(view.Observations) < 2 && (oldest.IsZero() || time.Since(oldest) < 24*time.Hour) {
		return "", nil, nil
	}
	if err := rest(); err != nil { // asking: the query the agent is sent to must not fail (a turn where asking failed asks nothing)
		return "", nil, err
	}
	cli := reflectCLI()
	ids := []string{}
	for _, task := range view.Sources {
		if task.Head {
			break // the notice names the oldest conversations; the query lists more of them, and counts the ones that do not fit (0.12)
		}
		ids = append(ids, task.Source.ID)
	}
	for _, o := range view.Observations {
		ids = append(ids, o.ID)
	}
	return reflectAsk(cli, ids), ids, nil
}

// oldestObservation is the time the notice judges "waited a day" by among pending observations: the oldest observed, whatever the
// order they were captured in (zero when there are none).
func oldestObservation(observations []notebook.ObservationHead) time.Time {
	oldest := time.Time{}
	for _, o := range observations {
		if oldest.IsZero() || o.ObservedAt.Before(oldest) {
			oldest = o.ObservedAt
		}
	}
	return oldest
}

// reflectCLI is the reflect command of this shikii executable, quoted for the shell.
func reflectCLI() string {
	executable, err := os.Executable()
	if err != nil {
		executable = "shikii"
	}
	return "'" + strings.ReplaceAll(executable, "'", "'\\''") + "' reflect"
}

// reflectHowToSubmit comes back in the reply of reflect query (the CLI and shikii_reflect) as how_to_submit: the exact shape of submit,
// read at the moment it is needed (0.11). The text asking for the reflection (reflectAsk) names only the command and the conversations.
// Before, the shape came with every ask (about 1,300 bytes, 1,700 in the full form), and a full and a short form kept drifting apart.
const reflectHowToSubmit = "submit reviews:[{source_id,trace_id,truth,reason}] with your own truth yes/no/unknown; wiki:[{sources,title,lesson,body,applies_to,related,kind,scene}] (applies_to names the command or file the lesson is about, such as make test or version.go, so it reaches the agent right before that command or file; only sources when there is no lesson; for a new lesson run operation:place first and choose id and replaces from its candidates and suspected contradictions; keep the short lesson and the detailed body apart); memory:[{source_id,key,result,decision,evidence,correction_result}] (one item per entry of every source's memories[], source_id as this reply's memories[].source_id: a lesson delivered to a subagent has its own; the pending conversations are listed oldest first, the oldest two with their raw text and the next as head: true with the text cut, read with wiki query <id> when a lesson needs it; more_pending counts the ones that did not fit, and the next query lists them) with result helpful/unused/harmful/unknown and, for a delivered correction, correction_result followed/repeated/not_applicable/unknown, tied to a check result (delivery, success, a greeting or silence is no effect; else unknown; observations are sources holding check results); maintenance: for at most 2 pages, review the body, related pages, sources and any feedback (saved delivery decisions and evidence), then send the latest page_id, revision, key and reason. Unused does not mean incorrect: repair applicability or content only when supported; no-change reviews count. Never change a revision just to restore delivery. Read the sources and existing pages before merging. A rule, test or procedure (command or script) you made or changed goes to the wiki even without a correction (a howto or rule page, or the closest page). For an explicit correction attach correction:{source_id,quote,action} (the person's raw words, the next action); if it can be a fixed check (paired files / words not to use / words a commit body must contain), add a disabled draft with `shikii guard add` and ask the person to enable it. Quotes are never permission, truths or calibration. On an error, fix it and resend; complete:true only means every item sent was saved: while left lists a conversation, submit what it names. Manual searches: discoveries:[{lookup_id,page_id,revision,context,decision,evidence,case_id?}] records only evidenced use of a query result, never just reading; optional case_id links a complete diagnostic for that work and exact revision, reopening missed-delivery maintenance. Wiki rationale keeps source-backed choices, rejected alternatives and reasons; watch lists at most four relative project files whose changes require rechecking. Details: the skill's references/wiki.md."

// reflectAsk is the text asking for the reflection (one form, 0.11): the command and the conversations. cli is the path of the shikii executable
func reflectAsk(cli string, ids []string) string {
	data, _ := json.Marshal(map[string]any{"sources": ids})
	return "shikii: wrap up the reflection (this request comes first). work prepare includes the pending originals below; combine their reviews and memory with the ending work record. Do not repeat supplied text or invent useful effects. Deep recovery with shikii_reflect or " + cli + " query returns how_to_submit. A rule, test or procedure you made or changed goes to the wiki. Quotes are never permission or truths.\n" + string(data)
}
