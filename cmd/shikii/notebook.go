package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func currentProjectScope() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return projectScopeAt(cwd)
}

type notebookRequest struct {
	Operation string `json:"operation"`
	Query     string `json:"query,omitempty"`
	notebook.Draft
}

func runNotebook(root, scope string, req notebookRequest) (any, error) {
	b := notebook.Book{Root: root, Scope: scope}
	switch req.Operation {
	case "query":
		if len(req.Query) > requestMax {
			return nil, errors.New("wiki query exceeds the text limit")
		}
		if len(req.Sources) > 0 {
			return b.ReadSources(req.Sources)
		}
		return b.Query(req.Query)
	case "ingest":
		return b.Ingest(req.Draft)
	case "lint":
		return b.Lint()
	}
	return nil, errors.New("wiki operation must be ingest, query or lint")
}

// 教訓の置き場は hook と同じプロジェクトの .shikii/。呼び出した仕事だけを扱う。
// 新しい接続先や設定は持たず、文章は利用中のエージェントから JSON で受け取る。
func cmdNotebook(args []string) error {
	if len(args) == 0 || (args[0] != "query" && args[0] != "ingest" && args[0] != "lint") {
		return errors.New("usage: shikii wiki query [query] | ingest (JSON on stdin) | lint | verify …")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	req := notebookRequest{Operation: args[0]}
	if req.Operation == "ingest" {
		if len(args) != 1 {
			return errors.New("wiki ingest reads one JSON object from stdin")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, (64<<10)+1))
		if err != nil || len(data) > 64<<10 {
			return errors.New("wiki ingest input exceeds 64 KB or could not be read")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req.Draft); err != nil {
			return errors.New("wiki ingest needs valid JSON with sources and, for a lesson, title, lesson and applies_to")
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return errors.New("wiki ingest accepts exactly one JSON object")
		}
	} else if req.Operation == "query" {
		req.Query = strings.Join(args[1:], " ")
		if len(args) == 2 && len(args[1]) == 16 {
			if _, err := hex.DecodeString(args[1]); err == nil {
				req.Sources, req.Query = []string{args[1]}, ""
			}
		}
	} else if len(args) != 1 {
		return errors.New("wiki lint takes no arguments")
	}
	result, err := runNotebook(root, scope, req)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func notebookMaintenance(agent, scope string) error {
	if scope == "" {
		return nil
	}
	return (notebook.Book{Root: agent, Scope: scope}).MaybeLint()
}

func (s *server) handleNotebook(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req notebookRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, 400, errors.New("invalid wiki request"))
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeErr(w, 400, errors.New("wiki accepts exactly one JSON object"))
		return
	}
	result, err := runNotebook(s.o.storeDir, s.o.projectScope, req)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, result)
}
