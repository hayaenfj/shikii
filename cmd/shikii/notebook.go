package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func currentProjectScope() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return projectScopeAt(cwd)
}

type notebookRequest struct {
	Operation  string       `json:"operation"`
	Query      string       `json:"query,omitempty"`
	Cursor     string       `json:"cursor,omitempty"`
	Request    string       `json:"request,omitempty"`
	Proposal   string       `json:"proposal,omitempty"`
	Conditions string       `json:"conditions,omitempty"`
	Work       *workRequest `json:"work,omitempty"`
	notebook.Draft
	Organization *notebook.Organization      `json:"organization,omitempty"`
	Patch        *notebook.Patch             `json:"patch,omitempty"`
	CaseID       string                      `json:"case_id,omitempty"`
	Cases        []string                    `json:"cases,omitempty"`
	Observation  *notebook.Observation       `json:"observation,omitempty"`
	Maintenance  *notebook.MaintenanceReview `json:"maintenance,omitempty"`
	Path         string                      `json:"path,omitempty"`    // path of a work record (handoff_*), relative to .shikii/handoff/
	Content      string                      `json:"content,omitempty"` // body to write to a work record (handoff_write / handoff_append)
}

func runNotebook(root, scope string, req notebookRequest) (any, error) {
	b := notebook.Book{Root: root, Scope: scope}
	switch req.Operation {
	case "prepare", "record":
		if req.Work == nil {
			return nil, errors.New("prepare/record needs a work object")
		}
		return runWork(root, scope, req.Operation, *req.Work)
	case "organize":
		if req.Organization == nil {
			return nil, errors.New("wiki organize needs an organization object")
		}
		return b.Organize(*req.Organization)
	case "reconcile":
		if req.Patch == nil {
			return nil, errors.New("wiki reconcile needs a pinned patch object")
		}
		return b.Reconcile(*req.Patch)
	case "consult", "reuse":
		return consultExperience(b, req)
	case "query":
		if len(req.Query) > requestMax {
			return nil, errors.New("wiki query exceeds the text limit")
		}
		if len(req.Sources) > 0 {
			return b.ReadSourcesPage(req.Sources, req.Cursor)
		}
		view, err := b.QueryPage(req.Query, req.Cursor)
		if err != nil {
			return nil, err
		}
		view.Lookup, err = b.RecordLookup(req.Query, view.Lessons)
		return view, err
	case "diagnose":
		return diagnoseKnowledge(b, req.ID, req.CaseID)
	case "compare":
		if req.Patch == nil {
			return nil, errors.New("wiki compare needs a patch object")
		}
		return compareKnowledge(b, *req.Patch, req.Cases)
	case "patch":
		if req.Patch == nil {
			return nil, errors.New("wiki patch needs a patch object")
		}
		return b.Patch(*req.Patch)
	case "ingest":
		res, err := ingestWithOutcome(root, scope, req.Draft) // attach the truth from the result to the insertion-point decision (0.8.3)
		if err == nil {
			res.Remaining = b.RemainingUnclassified() // remaining unclassified pages (0.9.0; also reaches agents without hooks)
		}
		return res, err
	case "classify":
		res, err := b.Classify(req.ID, req.Kind, req.Scene) // give only a kind and a scene to a page from before the version bump (0.9.0)
		if err == nil {
			res.Remaining = b.RemainingUnclassified()
		}
		return res, err
	case "capture":
		if req.Observation == nil {
			return nil, errors.New("a source to save is required")
		}
		return b.CaptureObserved(*req.Observation) // a title shikii gives its own sources is refused there
	case "maintain":
		if req.Maintenance == nil {
			return nil, errors.New("a review record is required")
		}
		return b.Maintain(*req.Maintenance)
	case "lint":
		return b.Lint()
	case "handoff_list", "handoff_read", "handoff_write", "handoff_append":
		return runHandoff(root, req)
	}
	return nil, errors.New("usage: wiki query / consult / reuse / organize / reconcile / ingest / patch / diagnose / compare / capture / maintain / lint / handoff_list / handoff_read / handoff_write / handoff_append")
}

// Lessons are kept in the project's .shikii/, the same as the hook. Only the calling project is handled.
// It has no new endpoints or settings; the text comes as JSON from the agent in use.
func cmdNotebook(args []string) error {
	// help: the word or a flag where the operation is expected, or a flag as an operation's only argument (search words and a
	// scene may hold the word help)
	if (len(args) > 0 && (helpFlag(args[0]) || args[0] == "help")) || (len(args) == 2 && helpFlag(args[1])) {
		return printAgentUsage("wiki")
	}
	if len(args) == 0 {
		return usageRefusal("wiki", "")
	}
	if args[0] != "query" && args[0] != "ingest" && args[0] != "lint" && args[0] != "capture" && args[0] != "maintain" && args[0] != "classify" && args[0] != "patch" && args[0] != "diagnose" && args[0] != "compare" && args[0] != "consult" && args[0] != "reuse" && args[0] != "organize" && args[0] != "reconcile" {
		if strings.HasPrefix(args[0], "-") {
			return usageRefusal("wiki", "wiki takes its operation first, and no flags")
		}
		return usageRefusal("wiki", fmt.Sprintf("wiki has no operation %q", args[0]))
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	req := notebookRequest{Operation: args[0]}
	if req.Operation == "ingest" || req.Operation == "capture" || req.Operation == "maintain" || req.Operation == "patch" || req.Operation == "diagnose" || req.Operation == "compare" || req.Operation == "consult" || req.Operation == "reuse" || req.Operation == "organize" || req.Operation == "reconcile" {
		if len(args) != 1 {
			return usageRefusal("wiki", "wiki "+req.Operation+" reads one JSON object from standard input, not from arguments", "wiki "+req.Operation)
		}
		limit := 64 << 10
		if req.Operation == "consult" || req.Operation == "reuse" || req.Operation == "organize" || req.Operation == "reconcile" || req.Operation == "ingest" || req.Operation == "patch" {
			limit = 1 << 20 // organization creates and guarded patches must also fit after JSON escaping
		}
		if req.Operation == "capture" {
			limit = store.MaxLineBytes // a source has no size limit of its own (a long memory file is captured whole)
		}
		data, err := io.ReadAll(io.LimitReader(stdin, int64(limit)+1))
		if err != nil || len(data) > limit {
			return fmt.Errorf("wiki %s input exceeds %d KiB or could not be read", req.Operation, limit>>10)
		}
		if noJSON(data) {
			return emptyInputRefusal("wiki", "wiki "+req.Operation)
		}
		var target any = &req.Draft
		if req.Operation == "diagnose" || req.Operation == "compare" || req.Operation == "consult" || req.Operation == "reuse" || req.Operation == "organize" || req.Operation == "reconcile" {
			target = &req
		}
		if req.Operation == "patch" {
			req.Patch = &notebook.Patch{}
			target = req.Patch
		}
		if req.Operation == "capture" {
			req.Observation = &notebook.Observation{}
			target = req.Observation
		}
		if req.Operation == "maintain" {
			req.Maintenance = &notebook.MaintenanceReview{}
			target = req.Maintenance
		}
		err, extra := decodeStrictObject(data, target)
		if err != nil {
			switch req.Operation {
			case "patch", "diagnose", "compare", "consult", "reuse", "organize", "reconcile":
				return fmt.Errorf("wiki %s needs valid JSON: %v", req.Operation, err)
			case "capture":
				return fmt.Errorf("wiki capture needs the source object itself (kind, title, content, reference, version, observed_at): %v", err)
			case "maintain":
				return fmt.Errorf("wiki maintain needs valid JSON with page_id, revision, key and reason: %v", err)
			}
			return fmt.Errorf("wiki ingest needs valid JSON with sources and, for a lesson, title, lesson and applies_to (all strings except sources): %v", err)
		}
		if extra {
			return fmt.Errorf("wiki %s accepts exactly one JSON object", req.Operation)
		}
		if req.Operation != args[0] {
			return errors.New("JSON operation must match the wiki command")
		}
	} else if req.Operation == "classify" {
		// give only a kind and a scene to a page from before the version bump (0.9.0). kind is rule|howto|criteria|fact|history; the scene is the rest of the arguments (spaces allowed)
		if len(args) < 4 {
			return errors.New("usage: shikii wiki classify <page ID> <rule|howto|criteria|fact|history> <scene (\"when ...\")>")
		}
		req.ID, req.Kind, req.Scene = args[1], args[2], strings.Join(args[3:], " ")
	} else if req.Operation == "query" {
		if len(args) > 1 && strings.HasPrefix(args[len(args)-1], "continue:") {
			req.Cursor = strings.TrimPrefix(args[len(args)-1], "continue:")
			if req.Cursor == "" {
				return errors.New("a cursor to continue from is required")
			}
			args = args[:len(args)-1]
		}
		req.Query = strings.Join(args[1:], " ")
		if len(args) > 1 {
			allIDs := true
			for _, id := range args[1:] {
				if _, err := hex.DecodeString(id); len(id) != 16 || err != nil {
					allIDs = false
					break
				}
			}
			if allIDs {
				req.Sources, req.Query = args[1:], ""
			}
		}
	} else if len(args) != 1 {
		return errors.New("wiki lint takes no arguments")
	}
	result, err := runNotebook(root, scope, req)
	if err != nil {
		return err
	}
	return writeReply(stdout, result)
}

func notebookMaintenance(agent, scope string) error {
	if scope == "" {
		return nil
	}
	return (notebook.Book{Root: agent, Scope: scope}).MaybeLint()
}

func (s *server) handleNotebook(w http.ResponseWriter, r *http.Request) {
	body := r.Body
	if !acceptJSONPost(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, body, 8<<20) // 1 MiB of work record body can grow several times when encoded as JSON. The body size is checked after decoding by handoff's Write (audit 0.8.1-1)
	var req notebookRequest
	data, err := io.ReadAll(r.Body)
	var extra bool
	if err == nil {
		err, extra = decodeStrictObject(data, &req)
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, 413, errors.New("the wiki request is over 8 MiB (a work record takes up to 1 MiB at a time; split it and use handoff_append)"))
			return
		}
		writeErr(w, 400, fmt.Errorf("invalid wiki request: %v", err))
		return
	}
	if extra {
		writeErr(w, 400, errors.New("wiki accepts exactly one JSON object"))
		return
	}
	if req.Work != nil {
		req.Work.responseSize, _ = r.Context().Value(workResponseSizeKey{}).(func(any) int)
	}
	result, err := runNotebook(s.o.storeDir, s.o.projectScope, req)
	if errors.Is(err, notebook.ErrStaleCursor) || errors.Is(err, notebook.ErrArticleConflict) { // the page reads again from the start
		writeErr(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Work != nil && (req.Operation == "prepare" || req.Operation == "record") {
		// Match the compact representation used by the work response budget.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = writeReply(w, result)
		return
	}
	writeReplyJSON(w, 200, result)
}
