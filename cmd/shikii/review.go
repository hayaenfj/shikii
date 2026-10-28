package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

type reviewRequest struct {
	Operation string `json:"operation"`
	SourceID  string `json:"source_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	Truth     string `json:"truth,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type reviewTask struct {
	Source  store.Feedback `json:"source_material"`
	Targets []reviewTarget `json:"targets"`
}

type reviewTarget struct {
	Spec           string            `json:"spec"`
	TraceID        string            `json:"trace_id"`
	Question       string            `json:"question"`
	Files          string            `json:"files"`
	OriginalInputs map[string]string `json:"original_inputs,omitempty"`
	TextStatus     map[string]string `json:"text_status,omitempty"`
	// ModelAnswer is the decision model's answer to the same question, shown before the agent scores (0.11): the agent decides having seen it.
	// When the saved score agrees, it becomes an agreed machine truth; when it differs or is unknown, the person is asked
	ModelAnswer modelAnswer `json:"decision_model_answer"`
}

type modelAnswer struct {
	Answer     string  `json:"answer"`     // yes / no
	Confidence float64 `json:"confidence"` // the model's probability of that answer
	Note       string  `json:"note"`
}

const modelAnswerNote = "The decision model's answer, not a truth. Score from the conversation; if your score agrees it is kept as an agreed machine truth (never the person's), if it differs or is unknown the person is asked to choose between the two."

func reviewQuestion(spec string) string {
	if spec == "changes-in-scope" {
		return "Were the changes within the requested scope?"
	}
	return "Was the requested work finished?"
}

// aiPair is the decision model's answer and the agent's answer to the same yes/no question (0.11). The person is asked only when they differ:
// the agent answered after seeing the model's answer (needs-plan: its organizing before the start; changes-in-scope / done-as-asked: its
// provisional score in reflection).
type aiPair struct {
	Model           string  `json:"model"`            // yes when the model's p(yes) is 0.5 or more
	ModelConfidence float64 `json:"model_confidence"` // the model's probability of its answer
	Agent           string  `json:"agent"`            // yes / no / unknown
	AgentReason     string  `json:"agent_reason,omitempty"`
}

func (p aiPair) agree() bool { return p.Agent == p.Model }

// planningOpenUnknown is the open input when the agent set open unknown in its organizing (renderPlanningFact): its answer is "no, nothing
// for the person to decide". Records from before 0.9.2 kept the Japanese text.
const planningOpenUnknown = "unknown (cannot be confirmed from the current conversation)"

const planningOpenUnknownJA = "\u4e0d\u660e\uff08\u73fe\u5728\u306e\u4f1a\u8a71\u304b\u3089\u78ba\u304b\u3081\u3089\u308c\u306a\u3044\uff09"

// modelYes is the decision model's p(yes) of a noul record.
func modelYes(rec store.Record) float64 {
	if p, ok := rec.Calibrated["yes"]; ok {
		return p
	}
	if p, ok := rec.Raw["yes"]; ok {
		return p
	}
	return rec.P
}

func modelAnswerOf(rec store.Record) modelAnswer {
	p := modelYes(rec)
	if p >= 0.5 {
		return modelAnswer{Answer: "yes", Confidence: p, Note: modelAnswerNote}
	}
	return modelAnswer{Answer: "no", Confidence: 1 - p, Note: modelAnswerNote}
}

// pairOf returns both answers of a decision when the agent gave one: the needs-plan organizing (open filled: yes; open unknown: no), or the
// provisional score of changes-in-scope / done-as-asked. Old needs-plan records without the organizing, and unscored decisions, have none.
func pairOf(specID string, rec store.Record, reviews map[string]store.Review) (aiPair, bool) {
	m := modelAnswerOf(rec)
	pair := aiPair{Model: m.Answer, ModelConfidence: m.Confidence}
	switch {
	case specID == "needs-plan":
		open := strings.TrimSpace(rec.Inputs["open"])
		if open == "" || strings.TrimSpace(rec.Inputs["agreed"]) == "" {
			return pair, false
		}
		if open == planningOpenUnknown || open == planningOpenUnknownJA {
			pair.Agent = "no"
		} else {
			pair.Agent, pair.AgentReason = "yes", strings.TrimPrefix(open, planningPreface)
		}
		return pair, true
	case store.ReviewSpec(specID):
		grade, ok := reviews[rec.TraceID]
		if !ok {
			return pair, false
		}
		pair.Agent, pair.AgentReason = grade.Truth, grade.Reason
		return pair, true
	}
	return pair, false
}

// writeAgreed keeps an agreement of the decision model and the agent as a machine truth (source agreed; never the person's). Once per record.
func writeAgreed(st store.Store, rec *store.Record, truth string) error {
	labels, err := store.LabelsFor(rec, truth, 1, store.SourceAgreed)
	if err != nil {
		return err
	}
	return st.AppendLabelsOnce(labels)
}

// reviewQueueState separates the provisional scoring of every item from the queue a person checks. Where the agent gave an answer too
// (pairOf), only a disagreement or an undecided answer waits for the person (0.11); the 1-in-20 spot check of act stays.
func reviewQueueState(specID string, rec store.Record, labels map[string]store.Label, reviews map[string]store.Review) (show, audit bool, reason string) {
	l, labeled := labels[rec.TraceID]
	if labeled && store.HumanSource(l.Source) {
		return false, false, ""
	}
	spot := rec.Band == "act" && store.AuditSample(rec.TraceID)
	if pair, ok := pairOf(specID, rec, reviews); ok {
		switch {
		case pair.Agent == "unknown":
			return true, true, "uncertain"
		case !pair.agree():
			return true, true, "disagree"
		case store.ReviewSpec(specID): // a score that agrees: settled without the person
			return false, false, ""
		case spot:
			return true, true, ""
		}
		return !labeled, false, ""
	}
	if rec.ReviewPending || labeled {
		return false, false, ""
	}
	return true, spot, ""
}

// queueItem is where one decision stands on the page: shown in the queue, asked as a check (a spot check, a disagreement, an undecided
// score or an outcome check), why, and whether it waits for the person. The queue (handleQueue) and the status count (handleStatus) use
// this one rule, so the Answer screen, which loads only questions whose status says something waits, never misses what the queue shows.
type queueItem struct {
	show, audit, waits bool
	reason             string
	pair               *aiPair
}

func queueItemOf(specID string, rec store.Record, labels map[string]store.Label, reviews map[string]store.Review, checks *outcomeChecks) queueItem {
	var q queueItem
	q.show, q.audit, q.reason = reviewQueueState(specID, rec, labels, reviews)
	if !q.show && outcomeCheckShown(checks, rec, labels) {
		q.show, q.audit, q.reason = true, true, "outcome"
	}
	if !q.show {
		return q
	}
	pair, paired := pairOf(specID, rec, reviews)
	if paired {
		q.pair = &pair
	}
	// decisions only the AI answers (wiki contradictions etc.) never wait for people (0.8.0). Otherwise the checks wait, and an unsure decision
	// waits when no outcome gives its truth and the agent gave no answer of its own (an agreement settles it without the person, 0.11)
	q.waits = !machineOnlySpecs[specID] && (q.audit || (rec.Band == "ask" && !outcomeSpecs[specID] && !paired))
	return q
}

func reviewTargets(st store.Store, source store.Feedback) []reviewTarget {
	out := []reviewTarget{}
	for _, target := range source.Targets {
		if !store.ReviewSpec(target.Spec) {
			continue
		}
		rec, err := st.FindRecord(target.Spec, target.TraceID)
		if err != nil || !rec.ReviewPending || rec.Inputs["request"] != source.Request {
			continue
		}
		originals, status := st.RecordText(*rec)
		out = append(out, reviewTarget{Spec: target.Spec, TraceID: target.TraceID, Question: reviewQuestion(target.Spec), Files: rec.Inputs["files"], OriginalInputs: originals, TextStatus: status, ModelAnswer: modelAnswerOf(*rec)})
	}
	return out
}

func reviewQuery(st store.Store, scope string) (map[string]any, error) {
	recent, err := st.RecentFeedback(scope)
	if err != nil {
		return nil, err
	}
	pending := []reviewTask{}
	examples := []map[string]any{}
	qualities := map[string]store.ReviewStats{}
	grades := map[string]map[string]store.Review{}
	labels := map[string]map[string]store.Label{}
	for _, id := range hookSpecs[2:] {
		grades[id], err = st.Reviews(id)
		if err != nil {
			return nil, err
		}
		labels[id], err = st.LabeledTraces(id)
		if err != nil {
			return nil, err
		}
		qualities[id] = store.ReviewQuality(grades[id], labels[id])
	}
	// Return old unprocessed conversations first, so they are not pushed out forever by new ones.
	for i := len(recent) - 1; i >= 0; i-- {
		source := recent[i]
		needed := false
		for _, target := range source.Targets {
			if !store.ReviewSpec(target.Spec) {
				continue
			}
			if _, ok := grades[target.Spec][target.TraceID]; ok {
				continue
			}
			if l, ok := labels[target.Spec][target.TraceID]; ok && store.HumanSource(l.Source) {
				continue
			}
			needed = true
		}
		if !needed {
			continue
		}
		task := reviewTask{Source: source, Targets: []reviewTarget{}}
		for _, target := range reviewTargets(st, source) {
			if _, ok := grades[target.Spec][target.TraceID]; ok {
				continue
			}
			if l, ok := labels[target.Spec][target.TraceID]; ok && store.HumanSource(l.Source) {
				continue
			}
			task.Targets = append(task.Targets, target)
		}
		if len(task.Targets) > 0 {
			pending = append(pending, task)
		}
		if len(pending) == 2 {
			break
		}
	}
	// Return examples a person corrected to the next scorer. The raw text and the earlier AI scores are kept.
	// The scores are in this project's own store, so a score's path is not compared: one given before the folder was moved counts too (0.9.7).
	for _, id := range hookSpecs[2:] {
		records, err := st.Records(id, 0)
		if err != nil {
			return nil, err
		}
		for _, rec := range records {
			grade, ok := grades[id][rec.TraceID]
			l, human := labels[id][rec.TraceID]
			if !ok || !human || !store.HumanSource(l.Source) || l.At.Before(grade.At) || store.Partial(l.Truth) || grade.Truth == l.Truth {
				continue
			}
			examples = append(examples, map[string]any{"spec": id, "source_material": grade.Source, "ai_truth": grade.Truth, "ai_reason": grade.Reason, "human_truth": l.Truth})
			break
		}
	}
	return map[string]any{"pending": pending, "human_corrections": examples, "quality": qualities}, nil
}

func runReview(root, scope string, req reviewRequest) (any, error) {
	if scope == "" {
		return nil, errors.New("cannot determine the project to score")
	}
	st := store.Store{Dir: root}
	if req.Operation == "query" {
		return reviewQuery(st, scope)
	}
	if req.Operation != "submit" {
		return nil, errors.New("the scoring operation is query or submit")
	}
	if !utf8.ValidString(req.Reason) || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 1500 || (req.Truth != "yes" && req.Truth != "no" && req.Truth != "unknown") {
		return nil, errors.New("a score needs yes / no / unknown and a reason of at most 1500 bytes")
	}
	recent, err := st.RecentFeedback(scope)
	if err != nil {
		return nil, err
	}
	for _, source := range recent {
		if source.ID != req.SourceID {
			continue
		}
		for _, target := range reviewTargets(st, source) {
			if target.TraceID != req.TraceID {
				continue
			}
			rec, err := st.FindRecord(target.Spec, target.TraceID)
			if err != nil {
				return nil, err
			}
			grade, err := st.SaveReview(store.Review{TraceID: req.TraceID, Spec: target.Spec, Scope: scope, InputsHash: rec.InputsHash, Truth: req.Truth, Reason: strings.TrimSpace(req.Reason), Source: source})
			if err != nil {
				return nil, err
			}
			// the score agrees with the decision model: settled without the person, as a machine truth (0.11). A disagreement or unknown waits
			if pair, _ := pairOf(target.Spec, *rec, map[string]store.Review{grade.TraceID: grade}); pair.agree() {
				if err := writeAgreed(st, rec, grade.Truth); err != nil {
					return nil, err
				}
			}
			return map[string]any{"review": grade}, nil
		}
	}
	return nil, errors.New("no scoring target is tied to a conversation in this project")
}

func decodeReview(input io.Reader) (reviewRequest, error) {
	var req reviewRequest
	data, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return req, errors.New("scoring input is JSON of at most 64 KB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, errors.New("check the scoring JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return req, errors.New("pass scoring JSON one object at a time")
	}
	return req, nil
}

func cmdReview(args []string) error {
	if len(args) != 1 || (args[0] != "query" && args[0] != "submit") {
		return errors.New("usage: shikii review query | submit (JSON on standard input)")
	}
	req := reviewRequest{Operation: args[0]}
	var err error
	if args[0] == "submit" {
		req, err = decodeReview(stdin)
		if err != nil {
			return err
		}
		if req.Operation != "" && req.Operation != "submit" {
			return errors.New("submit saves only provisional scores")
		}
		req.Operation = "submit"
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	result, err := runReview(root, scope, req)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func (s *server) handleReview(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	req, err := decodeReview(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	result, err := runReview(s.o.storeDir, s.o.projectScope, req)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, result)
}
