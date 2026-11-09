package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

type workQuote struct {
	Ref  string `json:"ref"`
	Text string `json:"text"`
}
type workFact struct {
	Unknown bool        `json:"unknown,omitempty"`
	Summary string      `json:"summary,omitempty"`
	Quotes  []workQuote `json:"quotes,omitempty"`
}
type workObservation struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Reference string `json:"reference,omitempty"`
	Version   string `json:"version,omitempty"`
}
type workNote struct {
	notebook.Draft
	Ref    string              `json:"ref,omitempty"`
	Target string              `json:"target,omitempty"`
	Edits  []notebook.TextEdit `json:"edits,omitempty"`
}
type workReview struct {
	Ref    string `json:"ref"`
	Answer string `json:"answer"`
	Reason string `json:"reason"`
}
type workMemory struct {
	Ref              string `json:"ref"`
	Result           string `json:"result"`
	Decision         string `json:"decision"`
	Evidence         string `json:"evidence"`
	CorrectionResult string `json:"correction_result,omitempty"`
}
type workMaintenance struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}
type workDiscovery struct {
	Ref      string `json:"ref"`
	Context  string `json:"context"`
	Decision string `json:"decision"`
	Evidence string `json:"evidence"`
	CaseID   string `json:"case_id,omitempty"`
}
type workOutcome struct {
	Ref        string   `json:"ref"`
	Conditions string   `json:"conditions"`
	Action     string   `json:"action"`
	Failure    string   `json:"failure,omitempty"`
	Correction string   `json:"correction,omitempty"`
	Result     string   `json:"result"`
	Sources    []string `json:"sources"`
}
type workCompletion struct {
	Ref        string   `json:"ref,omitempty"`
	Sufficient *bool    `json:"sufficient,omitempty"`
	Changed    string   `json:"changed"`
	Checked    string   `json:"checked,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
	Remaining  string   `json:"remaining,omitempty"`
}
type workCompletionRef struct {
	ReviewGeneration uint64   `json:"review_generation,omitempty"`
	Unresolved       bool     `json:"unresolved,omitempty"`
	Signature        string   `json:"signature"`
	Ready            bool     `json:"ready"`
	Source           string   `json:"source"`
	Evidence         []string `json:"evidence,omitempty"`
}
type workRequest struct {
	responseSize      func(any) int          // Host transport sizing; never supplied by the caller.
	MaxBytes          int                    `json:"max_bytes,omitempty"`
	Result            *workCompletion        `json:"result,omitempty"`
	Reading           *workReadingFeedback   `json:"reading,omitempty"`
	Trial             *workTrialRequest      `json:"trial,omitempty"`
	Comparison        *workComparisonRequest `json:"comparison,omitempty"`
	Reasoning         *workReasoning         `json:"reasoning,omitempty"`
	Assignment        *assignmentContext     `json:"assignment,omitempty"`
	Event             *assignmentEvent       `json:"event,omitempty"`
	SessionID         string                 `json:"session_id,omitempty"`
	TurnID            string                 `json:"turn_id,omitempty"`
	Query             string                 `json:"query,omitempty"`
	Request           string                 `json:"request,omitempty"`
	Proposal          string                 `json:"proposal,omitempty"`
	Conditions        string                 `json:"conditions,omitempty"`
	Cursor            string                 `json:"cursor,omitempty"`
	Receipt           string                 `json:"receipt,omitempty"`
	Agreed            *workFact              `json:"agreed,omitempty"`
	Open              *workFact              `json:"open,omitempty"`
	ContinuesPrevious *bool                  `json:"continues_previous,omitempty"`
	Observations      []workObservation      `json:"observations,omitempty"`
	Notes             []workNote             `json:"notes,omitempty"`
	Reviews           []workReview           `json:"reviews,omitempty"`
	Memory            []workMemory           `json:"memory,omitempty"`
	Reviewed          []string               `json:"reviewed,omitempty"`
	Maintenance       []workMaintenance      `json:"maintenance,omitempty"`
	Discoveries       []workDiscovery        `json:"discoveries,omitempty"`
	Outcomes          []workOutcome          `json:"outcomes,omitempty"`
}
type workReviewRef struct {
	Source string `json:"source"`
	Trace  string `json:"trace"`
}
type workMemoryRef struct {
	Source string `json:"source"`
	Key    string `json:"key"`
}
type workCapture struct {
	Archive  string           `json:"archive,omitempty"`
	Original *workObservation `json:"original,omitempty"`
	Ref      string           `json:"ref"`
	ID       string           `json:"id,omitempty"`
	At       time.Time        `json:"at"`
	Version  string           `json:"version"`
}
type workItem struct {
	Ref      string   `json:"ref"`
	Kind     string   `json:"kind"`
	ID       string   `json:"id,omitempty"`
	Revision string   `json:"revision,omitempty"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
type workReply struct {
	Plan       *planningReceipt `json:"plan,omitempty"`
	Reflection *reflectResult   `json:"reflection,omitempty"`
}
type workState struct {
	reviewingResult bool                                  // Trusted ending persistence; never decoded from input.
	Loop            *workLoopState                        `json:"loop,omitempty"`
	Delivery        *workDeliveryState                    `json:"delivery,omitempty"`
	Result          *workCompletionRef                    `json:"result,omitempty"`
	NextPending     uint64                                `json:"next_pending,omitempty"`
	ActiveNote      string                                `json:"-"`
	Reasoning       *reasoningProgress                    `json:"reasoning,omitempty"`
	Collective      *workCollective                       `json:"collective,omitempty"`
	Replies         map[string]workReply                  `json:"replies"`
	PendingOrigins  map[string]string                     `json:"pending_origins"`
	Nonce           string                                `json:"nonce"`
	At              time.Time                             `json:"at"`
	Standalone      bool                                  `json:"standalone,omitempty"`
	Query           string                                `json:"query"`
	Task            string                                `json:"task,omitempty"`
	Current         string                                `json:"current,omitempty"`
	Pages           []string                              `json:"pages,omitempty"`
	Keys            map[string]string                     `json:"keys"`
	Sources         map[string]string                     `json:"sources"`
	Articles        map[string]store.KnowledgeReference   `json:"articles"`
	Reviews         map[string]workReviewRef              `json:"reviews"`
	Memory          map[string]workMemoryRef              `json:"memory"`
	Maintenance     map[string]notebook.MaintenanceReview `json:"maintenance"`
	Captures        map[string]workCapture                `json:"captures"`
	Saved           map[string]workItem                   `json:"saved"`
	Pending         map[string]notebook.Draft             `json:"pending"`
	PendingKeys     map[string]string                     `json:"pending_keys"`
	Expected        map[string]store.KnowledgeReference   `json:"expected"`
}
type workToken struct {
	Session string `json:"s"`
	Turn    string `json:"t"`
	Nonce   string `json:"n"`
}
type workResult struct {
	Procedure        *notebook.ProcedureStatus `json:"procedure,omitempty"`
	Storage          map[string]any            `json:"storage,omitempty"`
	NextAction       string                    `json:"next_action,omitempty"`
	Comparison       *workComparisonView       `json:"comparison,omitempty"`
	Reasoning        reasoningStatus           `json:"reasoning"`
	Complete         bool                      `json:"complete"`
	Saved            []workItem                `json:"saved"`
	Errors           []workItem                `json:"errors,omitempty"`
	Pending          []any                     `json:"pending"`
	Plan             any                       `json:"plan,omitempty"`
	Reflection       any                       `json:"reflection,omitempty"`
	ProcedureReviews []workProcedureReview     `json:"procedure_reviews,omitempty"`
}

type workProcedureReview struct {
	notebook.MaintenanceTask
	Ref string `json:"ref"`
}

func workHash(v any) string {
	data, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func workEncode(t workToken) string {
	b, _ := json.Marshal(t)
	return base64.RawURLEncoding.EncodeToString(b)
}
func workDecode(token string) (workToken, error) {
	var t workToken
	if len(token) > 1000 {
		return t, errors.New("invalid work receipt")
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || json.Unmarshal(data, &t) != nil || t.Session == "" || len(t.Session) > 200 || len(t.Turn) != 16 || len(t.Nonce) != 32 {
		return t, errors.New("invalid work receipt")
	}
	if workEncode(t) != token {
		return t, errors.New("the work receipt changed")
	}
	return t, nil
}

func openWorkSessions(root string) (*os.Root, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err = r.Mkdir("sessions", 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return planningSessionRoot(root)
}
func workLock(dir *os.Root, name string) (func(), error) {
	f, err := dir.OpenFile(name+".work.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid work lock")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("this work is being saved; retry the same request")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func writeWorkSession(dir *os.Root, name string, s *hookSession) error {
	data, err := encodeHookSession(s)
	if err != nil {
		return err
	}
	temp := ".work-" + store.NewTraceID() + ".tmp"
	f, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	_, err = f.Write(data)
	if err == nil {
		err = store.SyncTemp(f)
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return dir.Rename(temp, name)
}
func saveWorkState(dir *os.Root, name, scope, turn string, w *workState, install bool) error {
	return saveWorkContext(dir, name, scope, turn, w, install, nil)
}

func saveWorkContext(dir *os.Root, name, scope, turn string, w *workState, install bool, update func(*hookSession) error) error {
	f, err := dir.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("invalid session lock")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("the session changed while saving; retry")
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return err
	}
	if err = currentPlanningFrame(s, turn, scope); err != nil {
		return err
	}
	if !install && (s.Planning.Work == nil || s.Planning.Work.Nonce != w.Nonce) {
		return errors.New("the work receipt is no longer current; prepare again")
	}
	if update != nil {
		if err = update(s); err != nil {
			return err
		}
	}
	if w.Reasoning != nil {
		syncReasoningCaptures(w)
		if w.Reasoning.TurnID != turn {
			return errors.New("reasoning belongs to another turn")
		}
		if !install && reasoningMaterial(s.Planning.Work) != reasoningMaterial(w) {
			w.Reasoning.Generation++
			if s.RitualRecovery != nil && !s.RitualRecovery.Pending {
				s.RitualRecovery = nil
			}
		}
		if w.reviewingResult && w.Result != nil && w.Result.Ready {
			w.Result.ReviewGeneration = w.Reasoning.Generation
		}
		s.Reasoning = w.Reasoning
	}
	retainCollectiveSeen(s, w)
	s.Planning.Work = w
	if err := writeWorkSession(dir, name, s); err != nil {
		if errors.Is(err, errHookSessionCapacity) && w.Reasoning != nil && (len(w.Reasoning.RequiredNotes) > 0 || len(w.Reasoning.PendingCaptures) > 0 || trialPendingCount(w.Reasoning) > 0 || pendingResultCount(w.Reasoning) > 0 || pendingWorkProcedure(w)) {
			book := notebook.Book{Root: filepath.Dir(dir.Name()), Scope: scope}
			if recoveryErr := archiveWorkCapacity(book, s); recoveryErr != nil {
				// Keep original note/capture payloads in the compact turn state
				// when the source store itself cannot accept the archive. A new
				// preparation must recover them before completion is available.
				s.Reasoning, s.Planning.Work = w.Reasoning, nil
				if saveErr := writeWorkSession(dir, name, s); saveErr != nil {
					return fmt.Errorf("%w; recovery could not archive or save the required work: %v; %v", err, recoveryErr, saveErr)
				}
				return fmt.Errorf("%w; the receipt was retired with required work retained, but its archive failed: %v", err, recoveryErr)
			}
			if recoveryErr := writeWorkSession(dir, name, s); recoveryErr != nil {
				return fmt.Errorf("%w; recovery could not save the required work: %v", err, recoveryErr)
			}
		}
		return err
	}
	return nil
}
func loadWorkState(dir *os.Root, scope string, t workToken) (*workState, string, error) {
	name := filepath.Base(sessionPath("", t.Session))
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return nil, name, err
	}
	if err = currentPlanningFrame(s, t.Turn, scope); err != nil {
		return nil, name, err
	}
	w := s.Planning.Work
	if w == nil || w.Nonce != t.Nonce || time.Since(w.At) > hookSessionTTL || time.Since(w.At) < -time.Minute {
		return nil, name, errors.New("the work receipt is expired or no longer current")
	}
	return w, name, nil
}
func runWork(root, scope, operation string, req workRequest) (any, error) {
	encoded, _ := json.Marshal(req)
	if len(encoded) > 1<<20 || scope == "" {
		return nil, errors.New("work input must fit 1 MiB in this project")
	}
	if req.Assignment != nil {
		return runAssignmentWork(root, scope, operation, req)
	}
	if req.Event != nil {
		return nil, errors.New("an event needs its prepared assignment")
	}
	switch operation {
	case "prepare":
		return prepareWork(root, scope, req)
	case "record":
		return recordWork(root, scope, req)
	}
	return nil, errors.New("work operation must be prepare or record")
}

func prepareWork(root, scope string, req workRequest) (any, error) {
	if _, e := workOutputBudget(req.MaxBytes); e != nil {
		return nil, e
	}
	depth, err := preparationDepth(req.Reasoning)
	if err != nil {
		return nil, err
	}
	if req.Result != nil || req.Reading != nil || req.Trial != nil || req.Comparison != nil || req.Agreed != nil || req.Open != nil || req.ContinuesPrevious != nil || len(req.Observations)+len(req.Notes)+len(req.Reviews)+len(req.Memory)+len(req.Reviewed)+len(req.Maintenance)+len(req.Discoveries)+len(req.Outcomes) > 0 {
		return nil, errors.New("prepare reads context; send content with record")
	}
	if req.Receipt != "" {
		return extendWork(root, scope, req)
	}
	if req.Proposal != "" || req.Conditions != "" || req.Cursor != "" {
		return nil, errors.New("prepare once, then pass its receipt to add a search, proposal or current conditions")
	}
	if strings.TrimSpace(req.Query) == "" || len(req.Query) > requestMax || len(req.Request) > 16000 || len(req.SessionID) > 200 {
		return nil, errors.New("prepare needs topic words (at most 1500 bytes) and bounded task text")
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	standalone := req.SessionID == "" && req.TurnID == ""
	if !standalone && (req.SessionID == "" || len(req.TurnID) != 16) {
		return nil, errors.New("pass both session_id and turn_id from this request's hook")
	}
	if standalone {
		req.SessionID = "work-" + store.NewTraceID()
		req.TurnID = store.NewTraceID()
	}
	name := filepath.Base(sessionPath(root, req.SessionID))
	unlock, err := workLock(dir, name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	var planningContext any
	var s *hookSession
	var contextRevision string
	if standalone {
		s = &hookSession{Scope: scope, TurnID: req.TurnID, At: time.Now(), Planning: &planningFrame{TurnID: req.TurnID, At: time.Now(), Sources: map[string]string{}}}
		if err = writeWorkSession(dir, name, s); err != nil {
			return nil, err
		}
		contextRevision = workHash(s.Planning)
	} else {
		value, e := runPlanning(root, scope, "query", planningRequest{SessionID: req.SessionID, TurnID: req.TurnID, Query: req.Query, workPrepare: true})
		if e != nil {
			return nil, e
		}
		staged := value.(stagedPlanningQuery)
		s, contextRevision, planningContext = staged.Session, staged.Revision, staged.Notice
		if err := archiveReasoningPending(notebook.Book{Root: root, Scope: scope}, s); err != nil {
			return nil, err
		}
	}
	if previous := s.Planning.Work; previous != nil {
		previous.Reasoning = cloneReasoning(s.Reasoning)
		if previous.Reasoning != nil {
			if err := bindReasoningOwner(previous.Reasoning, req.SessionID); err != nil {
				return nil, err
			}
			token := workToken{Session: req.SessionID, Turn: req.TurnID, Nonce: previous.Nonce}
			commit := workCommitter(func(action func() error) error { return commitWorkTurn(dir, name, scope, token, action) })
			if err := preserveReasoningHistory(notebook.Book{Root: root, Scope: scope}, previous, true, commit); err != nil {
				return nil, err
			}
		}
	}
	w := &workState{Replies: map[string]workReply{}, Nonce: store.NewTraceID() + store.NewTraceID(), At: time.Now().UTC(), Standalone: standalone, Query: req.Query, Current: s.Planning.Sources["current_user"], Pages: s.Planning.KnowledgePages, Keys: map[string]string{}, Sources: map[string]string{}, Articles: map[string]store.KnowledgeReference{}, Reviews: map[string]workReviewRef{}, Memory: map[string]workMemoryRef{}, Maintenance: map[string]notebook.MaintenanceReview{}, Captures: map[string]workCapture{}, Saved: map[string]workItem{}, Pending: map[string]notebook.Draft{}, PendingKeys: map[string]string{}, PendingOrigins: map[string]string{}, Expected: map[string]store.KnowledgeReference{}}
	w.Reasoning = cloneReasoning(s.Reasoning)
	if w.Reasoning == nil {
		w.Reasoning = &reasoningProgress{TurnID: req.TurnID, Required: !standalone}
	}
	if depth != "" {
		w.Reasoning.AttentionDepth = depth
	}
	if w.Reasoning.PreparedAt.IsZero() {
		w.Reasoning.PreparedAt = w.At
	}
	w.Result = w.Reasoning.Result
	if w.Result != nil && w.Result.Source != "" {
		w.Sources["result"] = w.Result.Source
	}
	restoreReasoningCaptures(w)
	if err := bindReasoningOwner(w.Reasoning, req.SessionID); err != nil {
		return nil, err
	}
	conversation, conversationContext := preparedWorkConversation(s, w, req.TurnID)
	knowledge := []any{}
	keys := []string{}
	for key := range s.Planning.Sources {
		if strings.HasPrefix(key, "article:") || strings.HasPrefix(key, "lesson:") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, key := range keys {
		parts := strings.Split(key, ":")
		if len(parts) != 3 || seen[parts[1]] {
			continue
		}
		seen[parts[1]] = true
		ref := "knowledge" + strconv.Itoa(len(knowledge)+1)
		w.Keys[ref] = key
		w.Articles[ref] = store.KnowledgeReference{ID: parts[1], Revision: parts[2]}
		knowledge = append(knowledge, map[string]any{"ref": ref, "id": parts[1], "revision": parts[2], "text": s.Planning.Sources[key], "body_included": strings.HasPrefix(key, "article:"), "query": map[string]any{"operation": "query", "query": "lesson:" + parts[1] + "@" + parts[2]}})
	}
	book, err := (notebook.Book{Root: root, Scope: scope}).PrepareReadSnapshot()
	if err != nil {
		return nil, err
	}
	var knowledgeContext map[string]any
	var knowledgeQuery map[string]any
	if standalone {
		view, e := book.QueryPage(req.Query, "")
		if e != nil {
			return nil, e
		}
		view.Lookup, e = book.RecordLookup(req.Query, view.Lessons)
		if e != nil {
			return nil, e
		}
		knowledgeContext, e = preparedKnowledgeContext(view, req.Query)
		if e != nil {
			return nil, e
		}
		knowledgeQuery = map[string]any{"operation": "query", "query": req.Query}
		if view.NextCursor != "" {
			knowledgeQuery["cursor"] = view.NextCursor
		}
		for _, p := range view.Lessons {
			ref := "knowledge" + strconv.Itoa(len(knowledge)+1)
			w.Articles[ref] = store.KnowledgeReference{ID: p.ID, Revision: p.Revision}
			knowledge = append(knowledge, map[string]any{"ref": ref, "id": p.ID, "revision": p.Revision, "article": p, "body_included": true, "query": map[string]any{"operation": "query", "query": "lesson:" + p.ID + "@" + p.Revision}})
		}
	}
	reflection, err := reflectQueryBook(book, reflectHeadsMax)
	if err != nil {
		return nil, err
	}
	if req.Assignment != nil && req.Assignment.EvidenceScope != "" {
		// Scoped hosts receive bounded peer reports through teamwork. Raw
		// reflection observations have no execution scope, so require an
		// explicit source read instead of automatically mixing their originals.
		reflection.Observations = nil
	}
	pending := []any{}
	reviewN, memoryN := 0, 0
	for i, source := range reflection.Sources {
		ref := "source" + strconv.Itoa(i+1)
		if source.Head {
			pending = append(pending, map[string]any{"request": source.Source.Request, "head": true, "next_action": "Prepare again after the complete sources are settled; do not judge a shortened source."})
			continue
		}
		material := prepareWorkSource(book, source.Source.ID)
		if !material.Complete {
			pending = append(pending, map[string]any{"request": source.Source.Request, "head": true, "incomplete": true, "wiki_pending": source.WikiPending, "source": material.Material, "source_query": material.Query, "next_action": material.Notice})
			continue
		}
		w.Sources[ref] = source.Source.ID
		reviews := []any{}
		memories := []any{}
		for _, target := range source.Targets {
			reviewN++
			r := "review" + strconv.Itoa(reviewN)
			w.Reviews[r] = workReviewRef{source.Source.ID, target.TraceID}
			reviews = append(reviews, map[string]any{"ref": r, "question": target.Question, "files": target.Files, "decision_model_answer": target.ModelAnswer, "original_inputs": target.OriginalInputs, "text_status": target.TextStatus})
		}
		for _, m := range source.Memories {
			memoryN++
			r := "memory" + strconv.Itoa(memoryN)
			w.Memory[r] = workMemoryRef{m.SourceID, m.Quote.Key}
			memories = append(memories, map[string]any{"ref": r, "quote": m.Quote, "request": m.Request, "parent": m.Parent, "agent": m.Agent, "tool": m.Tool, "phrase": m.Phrase, "at": m.At})
		}
		pending = append(pending, map[string]any{"ref": ref, "request": source.Source.Request, "answer": source.Source.Summary, "reply": source.Source.Reply, "head": source.Head, "wiki_pending": source.WikiPending, "source": material.Material, "reviews": reviews, "memory": memories})
	}
	evidence := []any{}
	for i, o := range reflection.Observations {
		ref := "evidence" + strconv.Itoa(i+1)
		material := prepareWorkSource(book, o.ID)
		if !material.Complete {
			evidence = append(evidence, map[string]any{"head": o, "incomplete": true, "source": material.Material, "source_query": material.Query, "next_action": material.Notice})
			continue
		}
		w.Sources[ref] = o.ID
		evidence = append(evidence, map[string]any{"ref": ref, "source": material.Material})
	}
	maintenance := []any{}
	for _, m := range reflection.Maintenance {
		relevant := false
		for _, article := range w.Articles {
			relevant = relevant || article.ID == m.PageID
		}
		if !relevant {
			continue
		}
		ref := "maintenance" + strconv.Itoa(len(maintenance)+1)
		w.Maintenance[ref] = notebook.MaintenanceReview{PageID: m.PageID, Revision: m.Revision, Key: m.Key}
		maintenance = append(maintenance, map[string]any{"ref": ref, "task": m})
	}
	task, taskContext, err := preparedWorkTask(s, w, req)
	if err != nil {
		return nil, err
	}
	if standalone {
		w.Task = task
	}
	quoted := map[string]string{}
	stage := ""
	if req.Assignment != nil {
		stage = req.Assignment.Stage
	}
	attention, err := prepareReadingAttention(book, w, knowledge, req.Query, stage)
	if err != nil {
		return nil, err
	}
	if err = focusWorkKnowledge(book, knowledge, req.Query, req.Query+" "+task, quoted, &attention); err != nil {
		return nil, err
	}
	var read []store.KnowledgeReference
	attention.Loop = prepareWorkLoop(book, w, req, knowledge, quoted, task, stage)
	attention.Procedure = procedureAttention(w)
	trackWorkReading(w, knowledge, attention, req.Query, task, stage)
	var memoryUpgrade map[string]any
	if req.Assignment == nil {
		var upgradeRead []store.KnowledgeReference
		memoryUpgrade, upgradeRead, err = prepareMemoryUpgrade(book, w, quoted, req.Query == "memory-upgrade")
		if err != nil {
			return nil, err
		}
		read = append(read, upgradeRead...)
	}
	reflectionContext, err := preparedReflectionContext(reflection, attention.Depth == "brief")
	if err != nil {
		return nil, err
	}
	if workContextSize(map[string]any{"conversation": conversation, "conversation_context": conversationContext}) > workSourceBudget {
		return nil, errors.New("the conversation context exceeds the preparation budget; read the original host conversation and use the individual planning operations")
	}
	collective, err := preparedCollective(book, w, s.KnowledgeSeen, knowledge)
	if err != nil {
		return nil, err
	}
	reply := map[string]any{"memory_upgrade": memoryUpgrade, "reading": w.Reasoning.Reading, "attention": attention, "reasoning": currentReasoningView(book, w.Reasoning), "planning_required": !standalone && !s.Planning.ReasoningOnly, "collective": collective, "receipt": workEncode(workToken{req.SessionID, req.TurnID, w.Nonce}), "task": task, "task_context": taskContext, "agent_supplied_task": standalone, "conversation": conversation, "conversation_context": conversationContext, "context_notice": planningContext, "knowledge": knowledge, "knowledge_context": knowledgeContext, "knowledge_query": knowledgeQuery, "pending": pending, "reflection_context": reflectionContext, "more_pending": reflection.MorePending, "observations": evidence, "maintenance": maintenance, "how_to_record": compactWorkHowTo}
	read, err = limitWorkPreparation(book, w, reply, quoted, req.MaxBytes, req.responseSize)
	if err != nil {
		return nil, err
	}
	initialWorkReading(s, w, knowledge, quoted)
	rememberWorkMetadata(w, reply)
	if err = saveWorkContext(dir, name, scope, req.TurnID, w, true, func(current *hookSession) error {
		if err := book.ValidateRead(); err != nil {
			return err
		}
		if workHash(current.Planning) != contextRevision {
			return errors.New("the planning context changed while preparing; prepare again")
		}
		current.Planning.Sources, current.Planning.Knowledge, current.Planning.KnowledgePages = s.Planning.Sources, s.Planning.Knowledge, s.Planning.KnowledgePages
		return retainWorkKnowledge(current, w, quoted, read)
	}); err != nil {
		return nil, err
	}
	recoverPreparedProcedure(root, scope, dir, name, w, workToken{req.SessionID, req.TurnID, w.Nonce}, reply)
	if err = limitRecoveredPreparation(reply, req.MaxBytes, req.responseSize); err != nil {
		return nil, err
	}
	return reply, nil
}

const workHowToRecord = "Optional reading:{check,sufficient,evidence:[captured refs]} reports checked reading quality against automatically measured knowledge bytes and full recoveries. Inspect attention.learning for a candidate or adopted depth; compare with focused on another task using the same check. An omission retracts that default. Recover an interrupted reading with reading:{ref} only. Never invent quality reports. Prepare may use reasoning:{depth:brief|focused|deep} alone as a reading intent, never a saved start. Optional trial:{plan:{case,question,expected,method,conditions,decision,effort:small|medium|large,evidence}} then trial:{ref,result:{assessment:supports|challenges|inconclusive,observed,evidence,measurements?}} records an actual check. Cite challenged results by trial ref in the next judgment. comparison:{before,after,reason} compares matched saved executions; ref-only retries recover pending captures. reasoning.depth:brief|focused|deep and next_trial refine reading and the next check. No trial is required without a decision-changing uncertainty. reasoning:{phase:start|update|finish,focus,assessment:revise|retain|unresolved,reason,evidence:[current|knowledgeN|observedN|newN],next_action,result?}: save a concise evidence-linked judgment at start, update it when work changes the premise, and finish with the checked result. Do not report private deliberation. Retaining an approach and evidence gaps are valid; do not invent discoveries or experiments. New saved work evidence reopens finish. Resolve failed/pending notes first. This is not permission, success or a human truth. Use record with this receipt and the content you judged. agreed/open: {summary,quotes:[{ref,text}]} or {unknown:true}; open defaults to unknown; continues_previous: true only for a continuation with no new request, false for an addition, omit when unsure. Keep summaries within 600 bytes, with 1-3 verbatim quotes of at most 240 bytes each. Quote current/previous human words for agreement. reviews:[{ref,answer:yes|no|unknown,reason}], memory:[{ref,result,decision,evidence,correction_result?}], reviewed:[source/evidence refs] when no lasting lesson; knowledgeN confirms inspected initial evidence, changeN acknowledges only the inspected change, never usefulness. observations:[{kind,title,content,reference?,version?}] become immutable sources with time and recording version supplied; new1 names the first observation in this call. notes:[{title,lesson,body,applies_to,kind,scene,sources:[refs],target?,edits?}]. For a read article use target knowledgeN and exact edits and/or passages:[{role,quote,conditions,reason}]; metadata and revisions are retained. Read orientation before mixed chronology; unclassified text and changed premises remain unresolved. For a new note placement runs internally; if uncertain, read the returned candidates and resubmit that note with target new or a returned article ref and edits. Add knowledge mid-work with prepare:{receipt,query} (optional cursor), or pass proposal for consult or conditions for reuse. Keep the returned article refs and the same receipt. Record evidenced discoveries:[{ref,context,decision,evidence,case_id?}]. Record procedure executions with outcomes:[{ref,conditions,action,failure?,correction?,result,sources:[observation refs]}]; this reports an outcome, never success or a truth. Handle procedure_reviews by checking current text, correction and evidence, then save a supported note. Notes return updated procedure_reviews; check the change and complete review with maintenance:[{ref,reason}] using that latest ref. An edit alone does not complete review; old refs are refused. With no edit, save the checked reason with the original ref. Check complete, errors and pending; resend corrected content with the same receipt. Read/saved is never helpfulness, permission or human truth."

func cmdWork(args []string) error {
	if len(args) != 1 || (args[0] != "prepare" && args[0] != "record") {
		return errors.New("usage: shikii work prepare | record (JSON on stdin)")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return errors.New("work JSON transport exceeds 8 MiB")
	}
	var req workRequest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&req); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("work takes exactly one JSON object")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	result, err := runWork(root, scope, args[0], req)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}
