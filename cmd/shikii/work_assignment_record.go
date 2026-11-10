package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func runAssignmentWork(root, scope, operation string, req workRequest) (any, error) {
	a := *req.Assignment
	if !validAssignment(a) || req.SessionID != "" || req.TurnID != "" || req.Agreed != nil || req.Open != nil || req.ContinuesPrevious != nil {
		return nil, errors.New("assignment needs bounded host context, without hook identifiers or human agreement")
	}
	dir, err := assignmentDir(root, scope)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if operation == "prepare" {
		return prepareAssignment(root, scope, dir, a, req)
	}
	if operation == "record" {
		return recordAssignment(root, scope, dir, a, req)
	}
	return nil, errors.New("assignment operation must be prepare or record")
}

// Deterministic preparation boundary for concurrency and recovery tests.
var assignmentPrepareWork = prepareWork

func prepareAssignment(root, scope string, dir *os.Root, a assignmentContext, req workRequest) (reply any, retErr error) {
	if req.Event != nil {
		return nil, errors.New("prepare an assignment checkpoint without an event")
	}
	// Match record's lock order: serialize one host, then inspect the inventory.
	release, err := workLock(dir, assignmentName(a.ID))
	if err != nil {
		return nil, err
	}
	defer release()
	unlock, err := workLock(dir, "assignments")
	if err != nil {
		return nil, err
	}
	inventoryHeld := true
	releaseInventory := func() {
		if inventoryHeld {
			unlock()
			inventoryHeld = false
		}
	}
	defer releaseInventory()
	records, err := assignmentRecords(dir)
	if err != nil {
		return nil, err
	}
	r, err := readAssignment(dir, assignmentName(a.ID))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		if len(records) >= 512 {
			return nil, errors.New("assignment capacity reached")
		}
		r = assignmentRecord{Context: a, At: time.Now().UTC(), Bindings: map[string]assignmentBinding{}, Used: map[string]notebook.CollectiveBaseline{}, Events: map[string]assignmentSavedEvent{}}
	} else if workHash(r.Context) != workHash(a) {
		return nil, errors.New("assignment identity already has different conditions; use a new execution id")
	}
	if req.Receipt != "" {
		if _, ok := r.Bindings[workHash(req.Receipt)]; !ok {
			return nil, errors.New("the receipt belongs to a different assignment")
		}
	}
	if req.Receipt == "" && len(r.Bindings) >= 8 {
		return nil, errors.New("assignment checkpoint capacity reached")
	}
	base := req
	base.Event = nil
	if req.Receipt == "" {
		base.Request = a.Purpose + "\n" + a.Role + " / " + a.Stage + "\n" + a.Question + "\n" + a.Conditions
	}
	if req.Receipt == "" && base.Query == "" {
		base.Query = a.Question
		if base.Query == "" {
			base.Query = a.Purpose
		}
		base.Query = assignmentClip(base.Query, 1500)
	}
	// Reserve the mandatory assignment identity in the same transport budget
	// before any knowledge is registered as delivered. Optional peer context
	// is reduced afterward without changing those delivered article bodies.
	minimumTeam := map[string]any{"assignment": a, "omitted": 999999999, "notice": assignmentNotice}
	size := workPreparationSizer([]func(any) int{req.responseSize})
	base.responseSize = func(value any) int {
		original := value.(map[string]any)
		copy := make(map[string]any, len(original)+1)
		for key, v := range original {
			copy[key] = v
		}
		copy["teamwork"] = minimumTeam
		return size(copy)
	}
	// Only a new preparation can be retired without losing previously delivered
	// context. Extensions retain their existing publication boundary.
	if req.Receipt == "" {
		releaseInventory()
	}
	value, err := assignmentPrepareWork(root, scope, base)
	if err != nil {
		return nil, err
	}
	v := value.(map[string]any)
	receipt := v["receipt"].(string)
	published := false
	defer func() {
		if retErr != nil && req.Receipt == "" && !published {
			if err := retireAssignmentPreparation(root, scope, receipt); err != nil {
				retErr = fmt.Errorf("%w; failed preparation recovery: %v", retErr, err)
			}
		}
	}()
	if req.Receipt == "" {
		unlock, err = workLock(dir, "assignments")
		if err != nil {
			return nil, err
		}
		inventoryHeld = true
		records, err = assignmentRecords(dir)
		if err != nil {
			return nil, err
		}
		current, err := readAssignment(dir, assignmentName(a.ID))
		if os.IsNotExist(err) {
			if len(records) >= 512 {
				return nil, errors.New("assignment capacity reached")
			}
		} else if err != nil {
			return nil, err
		} else {
			if workHash(current.Context) != workHash(a) {
				return nil, errors.New("assignment identity already has different conditions; use a new execution id")
			}
			if len(current.Bindings) >= 8 {
				return nil, errors.New("assignment checkpoint capacity reached")
			}
			r = current
		}
	}
	binding, team, err := prepareAssignmentView(root, scope, receipt, v, records, r, a, req, size)
	if err != nil {
		return nil, err
	}
	r.Bindings[workHash(receipt)] = binding
	if err = saveAssignment(dir, r); err != nil {
		return nil, err
	}
	published = true
	v["teamwork"] = team
	return v, nil
}

func recordAssignment(root, scope string, dir *os.Root, a assignmentContext, req workRequest) (any, error) {
	base := req
	base.Assignment = nil
	base.Event = nil
	if req.Event != nil {
		if !validAssignmentEvent(*req.Event) {
			return nil, errors.New("invalid bounded assignment event")
		}
		content, _ := json.Marshal(map[string]any{"assignment": a, "event": req.Event})
		if len(content) > 64<<10 {
			return nil, errors.New("the encoded event observation must fit 64 KiB; narrow the report before recording")
		}
		version := a.Commit
		if req.Event.Commit != "" {
			version = req.Event.Commit
		}
		if version == "" {
			version = "host-observation"
		}
		base.Observations = append([]workObservation{{Kind: "agent", Title: assignmentClip(a.Role+" "+a.Stage+": "+req.Event.State, 200), Content: string(content), Reference: "assignment:" + a.ID + "/" + req.Event.ID, Version: version}}, req.Observations...)
	}
	if err := validateWorkRecording(base); err != nil {
		return nil, err
	}
	// Serialize one host execution while allowing other assignments to record.
	release, err := workLock(dir, assignmentName(a.ID))
	if err != nil {
		return nil, err
	}
	defer release()
	unlock, err := workLock(dir, "assignments")
	if err != nil {
		return nil, err
	}
	r, err := readAssignment(dir, assignmentName(a.ID))
	if err != nil {
		unlock()
		return nil, err
	}
	binding, ok := r.Bindings[workHash(req.Receipt)]
	if !ok || workHash(r.Context) != workHash(a) {
		unlock()
		return nil, errors.New("assignment receipt or conditions changed")
	}
	for _, ref := range req.Reviewed {
		if strings.HasPrefix(ref, "knowledge") {
			if _, ok := binding.Knowledge[ref]; !ok {
				unlock()
				return nil, errors.New("knowledge was not supplied to this assignment")
			}
		}
	}
	var saved assignmentSavedEvent
	if req.Event != nil {
		e := *req.Event
		if !validAssignmentEvent(e) {
			unlock()
			return nil, errors.New("invalid bounded assignment event")
		}
		saved, ok = r.Events[e.ID]
		if ok {
			if saved.Hash != workHash(e) {
				unlock()
				return nil, errors.New("a recorded event cannot change")
			}
			if saved.ReceiptHash != "" && saved.ReceiptHash != workHash(req.Receipt) {
				unlock()
				return nil, errors.New("an event must retain its original receipt")
			}
			if saved.ReceiptHash == "" {
				// Earlier records can establish ownership only from the unchanged
				// original input, whose hash already includes the receipt.
				if saved.InputHash != workHash(req) {
					unlock()
					return nil, errors.New("an earlier event needs its unchanged original recording once before corrections")
				}
				if len(saved.Result) == 0 {
					saved.ReceiptHash = workHash(req.Receipt)
					r.Events[e.ID] = saved
					if err = saveAssignment(dir, r); err != nil {
						unlock()
						return nil, err
					}
				}
			}
			if len(saved.Result) > 0 {
				if saved.InputHash != workHash(req) {
					unlock()
					return nil, errors.New("a completed event retry must retain its recording content")
				}
				unlock()
				var v any
				err = json.Unmarshal(saved.Result, &v)
				return v, err
			}
		} else {
			for _, pending := range r.Events {
				if pending.Source == "" {
					unlock()
					return nil, errors.New("an earlier event needs its identical retry before another transition")
				}
			}
			state, _ := assignmentState(r)
			if len(r.Order) >= 64 || !assignmentTransition(state, e.State) {
				unlock()
				return nil, errors.New("assignment event capacity or lifecycle conflict")
			}
			if _, err = assignmentObservationSource(root, scope, req.Receipt, base.Observations[0]); err != nil {
				unlock()
				return nil, err
			}
			saved = assignmentSavedEvent{Hash: workHash(e), InputHash: workHash(req), ReceiptHash: workHash(req.Receipt), Event: e, At: time.Now().UTC()}
			r.Events[e.ID] = saved
			r.Order = append(r.Order, e.ID)
			if err = saveAssignment(dir, r); err != nil {
				unlock()
				return nil, err
			}
		}
	}
	unlock()
	value, err := recordWork(root, scope, base)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result workResult
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	var eventSource string
	if req.Event != nil {
		eventSource, err = assignmentObservationSource(root, scope, req.Receipt, base.Observations[0])
		if err != nil {
			return nil, fmt.Errorf("work saved, event source needs the same retry: %w", err)
		}
	}
	unlock, err = workLock(dir, "assignments")
	if err != nil {
		return nil, err
	}
	defer unlock()
	r, err = readAssignment(dir, assignmentName(a.ID))
	if err != nil {
		return nil, err
	}
	// Only an inspection the recording saved counts: a batch that stopped before it, or a review that failed, leaves the article
	// uninspected (the next execution must not get changes measured from a revision nobody confirmed).
	inspected := map[string]bool{}
	for _, item := range result.Saved {
		if item.Kind == "collective_review" {
			inspected[item.Ref] = true
		}
	}
	for _, ref := range req.Reviewed {
		if k, ok := binding.Knowledge[ref]; ok && inspected[ref] {
			r.Used[k.ID] = k
		}
	}
	if req.Event != nil {
		saved = r.Events[req.Event.ID]
		saved.Source = eventSource
		if result.Complete {
			saved.InputHash = workHash(req)
			saved.Result = data
		}
		r.Events[req.Event.ID] = saved
	}
	r.At = time.Now().UTC()
	if err = saveAssignment(dir, r); err != nil {
		return nil, fmt.Errorf("work saved, assignment checkpoint needs the same retry: %w", err)
	}
	return value, nil
}

func assignmentClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func assignmentSupplied(root, scope, receipt string) (map[string]notebook.CollectiveBaseline, error) {
	token, err := workDecode(receipt)
	if err != nil {
		return nil, err
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	w, _, err := loadWorkState(dir, scope, token)
	if err != nil {
		return nil, err
	}
	if w.Collective == nil {
		return nil, errors.New("the prepared exposure is unavailable")
	}
	return w.Collective.Exposures, nil
}

// Capture keys identify the event observation even if a different observation
// succeeded first. Read under the session lock, as ordinary record does.
func assignmentObservationSource(root, scope, receipt string, observation workObservation) (string, error) {
	token, err := workDecode(receipt)
	if err != nil {
		return "", err
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, token.Session))
	unlock, err := workLock(dir, name)
	if err != nil {
		return "", err
	}
	defer unlock()
	w, _, err := loadWorkState(dir, scope, token)
	if err != nil {
		return "", err
	}
	_, key, err := prepareWorkObservation(w, observation)
	if err != nil {
		return "", err
	}
	return w.Captures[key].ID, nil
}
