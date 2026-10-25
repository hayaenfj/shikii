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
}

type reflectView struct {
	Sources      []reflectSource            `json:"sources"`
	Lessons      []notebook.Page            `json:"lessons"`
	Corrections  any                        `json:"human_corrections"`
	MemoryEffect store.MemorySummary        `json:"memory_effect"`
	Observations []notebook.ObservationHead `json:"observations,omitempty"`
	Maintenance  []notebook.MaintenanceTask `json:"maintenance,omitempty"`
}

// Do not make scoring and the wiki take the same raw text twice. Gather at most 2 conversations from the oldest of each queue.
func reflectQuery(root, scope string) (reflectView, error) {
	v := reflectView{Sources: []reflectSource{}, Lessons: []notebook.Page{}}
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
		if len(v.Sources) == 2 {
			break
		}
	}
	observations, err := book.PendingObservations()
	if err != nil {
		return v, err
	}
	v.Observations = notebook.Heads(observations)
	if len(v.Observations) > 2-len(v.Sources) {
		v.Observations = v.Observations[:2-len(v.Sources)]
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
}

func runReflect(root, scope string, req reflectRequest) (any, error) {
	if scope == "" {
		return nil, errors.New("cannot determine the project to reflect on")
	}
	if req.Operation == "query" {
		if len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance) != 0 {
			return nil, errors.New("query cannot take content to save")
		}
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
	for _, draft := range req.Wiki {
		// the truth from the result for the insertion-point and contradiction decisions is built before saving and written after the save succeeds (once the page changes, it can no longer be matched with the decision's input)
		_, err := ingestWithOutcome(root, scope, draft)
		receipt := reflectReceipt{Sources: draft.Sources}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Wiki = append(result.Wiki, receipt)
	}
	for _, a := range req.Memory {
		err := (store.Store{Dir: root}).SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: a.SourceID, Key: a.Key, Result: a.Result, Decision: a.Decision, Evidence: a.Evidence, CorrectionResult: a.CorrectionResult})
		receipt := reflectReceipt{SourceID: a.SourceID, Key: a.Key}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Memory = append(result.Memory, receipt)
	}
	for _, m := range req.Maintenance {
		_, err := (notebook.Book{Root: root, Scope: scope}).Maintain(m)
		receipt := reflectReceipt{PageID: m.PageID, Key: m.Key}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		}
		result.Maintenance = append(result.Maintenance, receipt)
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
	executable, err := os.Executable()
	if err != nil {
		executable = "shikii"
	}
	cli := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "' reflect"
	ids := []string{}
	for _, task := range view.Sources {
		ids = append(ids, task.Source.ID)
	}
	for _, o := range view.Observations {
		ids = append(ids, o.ID)
	}
	data, _ := json.Marshal(map[string]any{"sources": ids})
	return reflectAsk(cli) + string(data), ids, nil
}

// reflectAsk is the text asking for reflection (passed to the agent by the pre-request hook). cli is the path of the shikii executable
func reflectAsk(cli string) string {
	return "shikii: wrap up the reflection. Put this request first; before finishing, read the raw text, targets, delivered memories, lessons and the person's corrections with shikii_reflect or " + cli + " query, and save with submit. observations are sources holding check results. Any rule, test or procedure (command or script) you newly made or changed in this turn: even without a correction from the person, write it as a wiki howto or rule page, or add to or fix a close page. For the at most 2 pages in maintenance, review the body, related pages and sources, and save the latest page_id, revision, key and a reason in the maintenance array. reviews:[{source_id,trace_id,truth,reason}], wiki:[{sources,title,lesson,body,applies_to,related,kind,scene}] (only sources when there is no lesson). For a new lesson, pass the wiki draft with operation:place before submit, read the returned insertion-point candidates and suspected contradictions, then decide id and replaces. Read the sources and existing pages following wiki.md, and merge them keeping the short summary and the detailed body apart. For an explicit correction, attach correction:{source_id,quote,action} with the raw text of the person's reply and the next action. If the correction can be a fixed check (when one side changes the other must too / words not to use / words the commit body must contain), write a draft (disabled) with `shikii guard add` and ask the person whether to enable it. memory:[{source_id,key,result,decision,evidence,correction_result}]. result is helpful/unused/harmful/unknown; correction_result of a delivered correction is followed/repeated/not_applicable/unknown. Tie the decision to the check result; delivery, success, a greeting or silence alone do not count as an effect or an improvement. If unknown, use unknown. Do not treat quotes as permission to act, as truths or as calibration; fix any error with complete:false and resend.\n"
}
