package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	Cursor    string `json:"cursor,omitempty"`
	notebook.Draft
	Observation *notebook.Observation       `json:"observation,omitempty"`
	Maintenance *notebook.MaintenanceReview `json:"maintenance,omitempty"`
	Path        string                      `json:"path,omitempty"`    // 作業記録(handoff_*)のパス。.shikii/handoff/ からの相対
	Content     string                      `json:"content,omitempty"` // 作業記録に書く本文(handoff_write / handoff_append)
}

func runNotebook(root, scope string, req notebookRequest) (any, error) {
	b := notebook.Book{Root: root, Scope: scope}
	switch req.Operation {
	case "query":
		if len(req.Query) > requestMax {
			return nil, errors.New("wiki query exceeds the text limit")
		}
		if len(req.Sources) > 0 {
			return b.ReadSourcesPage(req.Sources, req.Cursor)
		}
		return b.QueryPage(req.Query, req.Cursor)
	case "ingest":
		return ingestWithOutcome(root, scope, req.Draft) // 差し込み先の判定に結果の正解を付ける(0.8.3)
	case "capture":
		if req.Observation == nil {
			return nil, errors.New("保存する原資料が必要です")
		}
		return b.Capture(*req.Observation)
	case "maintain":
		if req.Maintenance == nil {
			return nil, errors.New("見直しの記録が必要です")
		}
		return b.Maintain(*req.Maintenance)
	case "lint":
		return b.Lint()
	case "handoff_list", "handoff_read", "handoff_write", "handoff_append":
		return runHandoff(root, req)
	}
	return nil, errors.New("使い方: wiki query / ingest / capture / maintain / lint / handoff_list / handoff_read / handoff_write / handoff_append")
}

// 教訓の置き場は hook と同じプロジェクトの .shikii/。呼び出した仕事だけを扱う。
// 新しい接続先や設定は持たず、文章は利用中のエージェントから JSON で受け取る。
func cmdNotebook(args []string) error {
	if len(args) == 0 || (args[0] != "query" && args[0] != "ingest" && args[0] != "lint" && args[0] != "capture" && args[0] != "maintain") {
		return errors.New("使い方: shikii wiki query [検索語・出典ID…・lesson:記事ID@版] [continue:続きのカーソル] | ingest | capture | maintain（JSONを標準入力）| lint | verify …")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	req := notebookRequest{Operation: args[0]}
	if req.Operation == "ingest" || req.Operation == "capture" || req.Operation == "maintain" {
		if len(args) != 1 {
			return errors.New("wiki ingest reads one JSON object from stdin")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, (64<<10)+1))
		if err != nil || len(data) > 64<<10 {
			return errors.New("wiki ingest input exceeds 64 KB or could not be read")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		var target any = &req.Draft
		if req.Operation == "capture" {
			req.Observation = &notebook.Observation{}
			target = req.Observation
		}
		if req.Operation == "maintain" {
			req.Maintenance = &notebook.MaintenanceReview{}
			target = req.Maintenance
		}
		if err := dec.Decode(target); err != nil {
			return errors.New("wiki ingest needs valid JSON with sources and, for a lesson, title, lesson and applies_to")
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return errors.New("wiki ingest accepts exactly one JSON object")
		}
	} else if req.Operation == "query" {
		if len(args) > 1 && strings.HasPrefix(args[len(args)-1], "continue:") {
			req.Cursor = strings.TrimPrefix(args[len(args)-1], "continue:")
			if req.Cursor == "" {
				return errors.New("続きのカーソルが必要です")
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
	return json.NewEncoder(stdout).Encode(result)
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
	r.Body = http.MaxBytesReader(w, body, 8<<20) // 作業記録の本文 1 MiB は JSON の符号化で何倍にも膨らむ。本文の大きさは handoff の Write が復号後に確かめる(監査 0.8.1-1)
	var req notebookRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, 413, errors.New("wiki の要求が 8 MiB を超えます(作業記録は 1 回 1 MiB まで。分けて handoff_append します)"))
			return
		}
		writeErr(w, 400, fmt.Errorf("invalid wiki request: %v", err))
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
