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
}

func reviewQuestion(spec string) string {
	if spec == "changes-in-scope" {
		return "変えたものは頼まれた範囲の中だったか"
	}
	return "頼まれたことを終えたか"
}

// reviewQueueState は全件の仮採点と、人が確認する列を分ける。
func reviewQueueState(rec store.Record, labels map[string]store.Label, reviews map[string]store.Review) (show, audit bool, reason string) {
	l, labeled := labels[rec.TraceID]
	if labeled && store.HumanSource(l.Source) {
		return false, false, ""
	}
	grade, ok := reviews[rec.TraceID]
	if !ok {
		if rec.ReviewPending || labeled {
			return false, false, ""
		}
		return true, rec.Band == "act" && store.AuditSample(rec.TraceID), ""
	}
	if grade.Truth == "unknown" {
		return true, true, "uncertain"
	}
	return grade.Sampled, grade.Sampled, "random"
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
		out = append(out, reviewTarget{Spec: target.Spec, TraceID: target.TraceID, Question: reviewQuestion(target.Spec), Files: rec.Inputs["files"], OriginalInputs: originals, TextStatus: status})
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
	// 古い未処理の会話を先に返し、新しい会話に押し出され続けるのを防ぐ。
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
	// 人が訂正した例を、次の採点役へ戻す。原文や過去の AI の採点は保持する。
	for _, id := range hookSpecs[2:] {
		records, err := st.Records(id, 0)
		if err != nil {
			return nil, err
		}
		for _, rec := range records {
			grade, ok := grades[id][rec.TraceID]
			l, human := labels[id][rec.TraceID]
			if !ok || !human || !store.HumanSource(l.Source) || grade.Scope != scope || l.At.Before(grade.At) || store.Partial(l.Truth) || grade.Truth == l.Truth {
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
		return nil, errors.New("採点するプロジェクトを確認できません")
	}
	st := store.Store{Dir: root}
	if req.Operation == "query" {
		return reviewQuery(st, scope)
	}
	if req.Operation != "submit" {
		return nil, errors.New("採点の操作は query または submit です")
	}
	if !utf8.ValidString(req.Reason) || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 1500 || (req.Truth != "yes" && req.Truth != "no" && req.Truth != "unknown") {
		return nil, errors.New("採点には yes / no / unknown と 1500 バイト以内の根拠が必要です")
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
			return map[string]any{"review": grade}, err
		}
	}
	return nil, errors.New("この仕事の会話に結び付いた採点対象がありません")
}

func decodeReview(input io.Reader) (reviewRequest, error) {
	var req reviewRequest
	data, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return req, errors.New("採点の入力は 64 KB 以内の JSON です")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, errors.New("採点の JSON を確認してください")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return req, errors.New("採点の JSON は 1 件ずつ渡します")
	}
	return req, nil
}

func cmdReview(args []string) error {
	if len(args) != 1 || (args[0] != "query" && args[0] != "submit") {
		return errors.New("使い方: shikii review query | submit（JSON を標準入力）")
	}
	req := reviewRequest{Operation: args[0]}
	var err error
	if args[0] == "submit" {
		req, err = decodeReview(stdin)
		if err != nil {
			return err
		}
		if req.Operation != "" && req.Operation != "submit" {
			return errors.New("submit では仮採点だけを保存します")
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

// waitsForPersonRecord は、列に出る記録のうち人の答えを待つもの: 抜き取り(audit)か、結果で正解が付かない判定の「迷い」(ask)。
// 結果で正解が付く判定(prompt-kind など)の迷いは次の依頼の結果が答える。AI だけが答える判定(wiki の矛盾など)は人を待たない(0.8.0)。
func waitsForPersonRecord(specID string, rec store.Record, audit bool) bool {
	if machineOnlySpecs[specID] {
		return false
	}
	return audit || (rec.Band == "ask" && !outcomeSpecs[specID])
}
