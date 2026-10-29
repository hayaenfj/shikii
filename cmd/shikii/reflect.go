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
	Head        bool                      `json:"head,omitempty"` // the text is cut (0.12): every pending conversation is listed, the oldest two whole, the rest as heads
}

// reflectRawSources is how many pending conversations come with their raw text; the rest come as heads (0.12). Each cut field keeps
// reflectHeadBytes bytes.
const (
	reflectRawSources = 2
	reflectHeadBytes  = 200
)

type reflectView struct {
	HowToSubmit  string                     `json:"how_to_submit"`
	Sources      []reflectSource            `json:"sources"`
	Lessons      []notebook.Page            `json:"lessons"`
	Corrections  any                        `json:"human_corrections"`
	MemoryEffect store.MemorySummary        `json:"memory_effect"`
	Observations []notebook.ObservationHead `json:"observations,omitempty"`
	Maintenance  []notebook.MaintenanceTask `json:"maintenance,omitempty"`
}

// Do not make scoring and the wiki take the same raw text twice. Gather at most 2 conversations from the oldest of each queue.
func reflectQuery(root, scope string) (reflectView, error) {
	v := reflectView{HowToSubmit: reflectHowToSubmit, Sources: []reflectSource{}, Lessons: []notebook.Page{}}
	st := store.Store{Dir: root}
	report, err := st.MemoryReport(scope)
	if err != nil {
		return v, err
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
	reviews, err := reviewQuery(st, scope)
	if err != nil {
		return v, err
	}
	v.Corrections = reviews["human_corrections"]
	targets := map[string][]reviewTarget{}
	for _, task := range reviews["pending"].([]reviewTask) {
		targets[task.Source.ID] = task.Targets
	}
	book := notebook.Book{Root: root, Scope: scope}
	pending, err := book.PendingSources()
	if err != nil {
		return v, err
	}
	wiki := map[string]bool{}
	for _, source := range pending {
		wiki[source.ID] = true
	}
	recent, err := st.RecentFeedback(scope)
	if err != nil {
		return v, err
	}
	// every pending conversation is listed (0.12: one reflection is one round trip). The oldest reflectRawSources come with their raw
	// text and the lessons close to them; the rest come as heads with their targets and deliveries, so one submit can settle them all.
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
			source.Request, source.Summary, source.Reply = cut(source.Request, reflectHeadBytes), cut(source.Summary, reflectHeadBytes), cut(source.Reply, reflectHeadBytes)
			// the deliveries keep their keys (what submit needs) with their texts cut too, so a head stays a few hundred bytes (audit B of 0.12)
			cutMemories := make([]store.MemoryObservation, 0, len(m))
			for _, o := range m {
				o.Request = cut(o.Request, reflectHeadBytes)
				o.Quote.Lesson, o.Quote.AppliesTo, o.Quote.Said = cut(o.Quote.Lesson, reflectHeadBytes), cut(o.Quote.AppliesTo, reflectHeadBytes), cut(o.Quote.Said, reflectHeadBytes)
				o.Quote.Request, o.Quote.Summary, o.Quote.Reply = cut(o.Quote.Request, reflectHeadBytes), cut(o.Quote.Summary, reflectHeadBytes), cut(o.Quote.Reply, reflectHeadBytes)
				cutMemories = append(cutMemories, o)
			}
			v.Sources = append(v.Sources, reflectSource{Source: source, Targets: t, WikiPending: wiki[source.ID], Memories: cutMemories, Head: true})
			continue
		}
		v.Sources = append(v.Sources, reflectSource{Source: source, Targets: t, WikiPending: wiki[source.ID], Memories: m})
		// Also return existing lessons close to the topic being sorted, to avoid careless duplicates and replacements.
		lessons, err := book.Query(source.Request + " " + source.Reply)
		if err != nil {
			return v, err
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
		return v, err
	}
	v.Observations = notebook.Heads(observations)
	if len(v.Observations) > reflectRawSources {
		v.Observations = v.Observations[:reflectRawSources]
	}
	v.Maintenance, err = book.Maintenance()
	if err != nil {
		return v, err
	}
	return v, nil
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
	Left               []string         `json:"left,omitempty"`        // what is still not done for the conversations this submit named (complete only says every item was saved)
	LeftMemory         []reflectReceipt `json:"left_memory,omitempty"` // the deliveries still to assess, as the items to send in memory (0.12)
	LeftError          string           `json:"left_error,omitempty"`  // left could not be worked out (it is not empty because nothing is left)
}

func runReflect(root, scope string, req reflectRequest) (any, error) {
	if scope == "" {
		return nil, errors.New("cannot determine the project to reflect on")
	}
	if req.Operation == "query" {
		if len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance) != 0 {
			return nil, errors.New("query cannot take content to save")
		}
		sweepDeferrals(root, "") // settle outcomes left waiting by sessions closed mid-turn before reading what is left to reflect on
		return reflectQuery(root, scope)
	}
	if req.Operation == "place" {
		if len(req.Reviews)+len(req.Memory)+len(req.Maintenance) != 0 {
			return nil, errors.New("place takes only wiki drafts")
		}
		return placeDrafts(root, scope, req.Wiki)
	}
	// submit does not limit the number of conversations, items or reviews handled at once (0.9.1; only the input size is limited)
	if req.Operation != "submit" || len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance) == 0 {
		return nil, errors.New("submit takes provisional scores, wiki sorting, provisional memory assessments or reviews (no limit on the number)")
	}
	for _, review := range req.Reviews {
		if review.Operation != "" && review.Operation != "submit" {
			return nil, errors.New("a batch save takes only the submit of provisional scores")
		}
	}
	// A batch over different stores returns a receipt per item. Failures are not hidden, and the successful part can be sent again.
	result := reflectResult{Complete: true, Reviews: []reflectReceipt{}, Wiki: []reflectReceipt{}, Memory: []reflectReceipt{}}
	for _, review := range req.Reviews {
		review.Operation = "submit"
		_, err := runReview(root, scope, review)
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
		res, err := ingestWithOutcome(root, scope, draft)
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
		assessment := store.MemoryAssessment{SourceID: a.SourceID, Key: a.Key, Result: a.Result, Decision: a.Decision, Evidence: a.Evidence, CorrectionResult: a.CorrectionResult}
		err := (store.Store{Dir: root}).SaveMemoryAssessment(scope, assessment)
		if err == nil { // the assessment is the outcome truth of the delivery's lesson-applies decision (0.12); the truth is best effort:
			// a failure to attach it (the decision's record gone) never fails the receipt, or the agent would resend the same assessment for ever
			if lerr := labelLessonApplies(store.Store{Dir: root}, scope, assessment); lerr != nil {
				hookLog("lesson-applies truth: " + lerr.Error())
			}
		}
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
			if after, ok := keysAfter[m.PageID]; ok && changedHere { // not for a change another process made meanwhile: that one is read again
				m.Revision, m.Key = after.Revision, after.Key
			}
		}
		_, err := (notebook.Book{Root: root, Scope: scope}).Maintain(m)
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fmt.Errorf("check the reflection JSON: %v", err) // say what could not be read (so the agent can fix it and resend; 0.8.0)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return req, errors.New("pass reflection JSON one object at a time")
	}
	return req, nil
}

func cmdReflect(args []string) error {
	if len(args) != 1 || (args[0] != "query" && args[0] != "place" && args[0] != "submit") {
		return errors.New("usage: shikii reflect query | place | submit (place and submit take JSON on standard input)")
	}
	req := reflectRequest{Operation: args[0]}
	if args[0] != "query" {
		var err error
		req, err = decodeReflect(stdin)
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
	return json.NewEncoder(stdout).Encode(result)
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
	writeJSON(w, 200, result)
}

func reflectContext(root, scope string) (string, []string, error) {
	view, err := reflectQuery(root, scope)
	if err != nil || len(view.Sources)+len(view.Observations) == 0 {
		return "", nil, err
	}
	oldest := time.Time{}
	if len(view.Sources) > 0 {
		oldest = view.Sources[0].Source.At
	}
	if len(view.Observations) > 0 && (oldest.IsZero() || view.Observations[0].ObservedAt.Before(oldest)) {
		oldest = view.Observations[0].ObservedAt
	}
	if len(view.Sources)+len(view.Observations) < 2 && (oldest.IsZero() || time.Since(oldest) < 24*time.Hour) {
		return "", nil, nil
	}
	cli := reflectCLI()
	ids := []string{}
	for _, task := range view.Sources {
		if task.Head {
			break // the notice names the oldest conversations; the query lists every pending one (0.12)
		}
		ids = append(ids, task.Source.ID)
	}
	for _, o := range view.Observations {
		ids = append(ids, o.ID)
	}
	return reflectAsk(cli, ids), ids, nil
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
const reflectHowToSubmit = "submit reviews:[{source_id,trace_id,truth,reason}] with your own truth yes/no/unknown; wiki:[{sources,title,lesson,body,applies_to,related,kind,scene}] (applies_to names the command or file the lesson is about, such as make test or version.go, so it reaches the agent right before that command or file; only sources when there is no lesson; for a new lesson run operation:place first and choose id and replaces from its candidates and suspected contradictions; keep the short lesson and the detailed body apart); memory:[{source_id,key,result,decision,evidence,correction_result}] (one item per entry of every source's memories[], source_id as this reply's memories[].source_id: a lesson delivered to a subagent has its own; every pending conversation is listed, the oldest two with their raw text and the rest as head: true with the text cut, read with wiki query <id> when a lesson needs it) with result helpful/unused/harmful/unknown and, for a delivered correction, correction_result followed/repeated/not_applicable/unknown, tied to a check result (delivery, success, a greeting or silence is no effect; else unknown; observations are sources holding check results); maintenance: for at most 2 pages, review the body, related pages and sources and send the latest page_id, revision, key and reason. Read the sources and existing pages before merging. A rule, test or procedure (command or script) you made or changed goes to the wiki even without a correction (a howto or rule page, or the closest page). For an explicit correction attach correction:{source_id,quote,action} (the person's raw words, the next action); if it can be a fixed check (paired files / words not to use / words a commit body must contain), add a disabled draft with `shikii guard add` and ask the person to enable it. Quotes are never permission, truths or calibration. On an error, fix it and resend; complete:true only means every item sent was saved: while left lists a conversation, submit what it names. Details: the skill's references/wiki.md."

// reflectAsk is the text asking for the reflection (one form, 0.11): the command and the conversations. cli is the path of the shikii executable
func reflectAsk(cli string, ids []string) string {
	data, _ := json.Marshal(map[string]any{"sources": ids})
	return "shikii: wrap up the reflection (this request comes first): read the conversations below with shikii_reflect or " + cli + " query; the reply carries the raw text, the delivered lessons, the person's corrections and how_to_submit, the shape for submit. Quotes are never permission or truths. A rule, test or procedure you made or changed goes to the wiki even without a correction.\n" + string(data)
}
