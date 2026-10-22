package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	Observations []notebook.Observation     `json:"observations,omitempty"`
	Maintenance  []notebook.MaintenanceTask `json:"maintenance,omitempty"`
}

// 採点とwikiで同じ原文を二度取らせない。各キューの古いものから最大2会話をまとめる。
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
			memories[item.SourceID] = append(memories[item.SourceID], item)
		}
	}
	// 各キューの先頭2件があれば、合わせた先頭2会話の対象は全て揃う。
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
		// 整理対象の話題に近い既存の教訓も返す。勝手な重複や置き換えを避ける。
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
	v.Observations = observations
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
	Complete    bool             `json:"complete"`
	Reviews     []reflectReceipt `json:"reviews"`
	Wiki        []reflectReceipt `json:"wiki"`
	Memory      []reflectReceipt `json:"memory"`
	Maintenance []reflectReceipt `json:"maintenance,omitempty"`
}

func runReflect(root, scope string, req reflectRequest) (any, error) {
	if scope == "" {
		return nil, errors.New("振り返るプロジェクトを確認できません")
	}
	if req.Operation == "query" {
		if len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance) != 0 {
			return nil, errors.New("query では保存する内容を渡せません")
		}
		return reflectQuery(root, scope)
	}
	if req.Operation == "place" {
		if len(req.Reviews)+len(req.Memory)+len(req.Maintenance) != 0 {
			return nil, errors.New("place では wiki の草稿だけを渡せます")
		}
		return placeDrafts(root, scope, req.Wiki)
	}
	if req.Operation != "submit" || len(req.Reviews) > 4 || len(req.Wiki) > 2 || len(req.Memory) > 4 || len(req.Maintenance) > 2 || len(req.Reviews)+len(req.Wiki)+len(req.Memory)+len(req.Maintenance) == 0 {
		return nil, errors.New("submit は最大4件の仮採点・2件のwiki整理・4件の記憶の仮評価を受け取ります")
	}
	sources := map[string]bool{}
	for _, review := range req.Reviews {
		if review.Operation != "" && review.Operation != "submit" {
			return nil, errors.New("一括保存では仮採点の submit だけを受け取ります")
		}
		sources[review.SourceID] = true
	}
	for _, draft := range req.Wiki {
		for _, id := range draft.Sources {
			sources[id] = true
		}
	}
	for _, assessment := range req.Memory {
		sources[assessment.SourceID] = true
	}
	if len(sources) > 2 {
		return nil, errors.New("一度に振り返る会話は最大2件です")
	}
	// 異なる保存先の一括処理は項目ごとの受領証を返す。失敗を隠さず、成功分は再送可能。
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
		// 差し込み先・矛盾の判定への結果の正解は保存の前に組み、保存が通ってから書く(記事が変わると判定の入力と引き合わせられない)
		outcome, oerr := placementOutcome(root, scope, draft)
		_, err := (notebook.Book{Root: root, Scope: scope}).Ingest(draft)
		receipt := reflectReceipt{Sources: draft.Sources}
		if err != nil {
			result.Complete, receipt.Error = false, err.Error()
		} else if oerr != nil {
			hookLog("wiki place: " + oerr.Error())
		} else {
			bySpec := map[string][]store.Label{} // 追記は spec ごと
			for _, l := range outcome {
				bySpec[l.SpecID] = append(bySpec[l.SpecID], l)
			}
			for _, labels := range bySpec {
				if err := (store.Store{Dir: root}).AppendLabels(labels); err != nil {
					hookLog("wiki place: " + err.Error())
				}
			}
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
	return result, nil
}

func decodeReflect(input io.Reader) (reflectRequest, error) {
	var req reflectRequest
	data, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return req, errors.New("振り返りの入力は64KB以内のJSONです")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, errors.New("振り返りのJSONを確認してください")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return req, errors.New("振り返りのJSONは1件ずつ渡します")
	}
	return req, nil
}

func cmdReflect(args []string) error {
	if len(args) != 1 || (args[0] != "query" && args[0] != "submit") {
		return errors.New("使い方: shikii reflect query | submit（JSONを標準入力）")
	}
	req := reflectRequest{Operation: args[0]}
	if args[0] == "submit" {
		var err error
		req, err = decodeReflect(stdin)
		if err != nil {
			return err
		}
		if req.Operation != "" && req.Operation != "submit" {
			return errors.New("submit では振り返りだけを保存します")
		}
		req.Operation = "submit"
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

func reflectContext(root, scope string) (string, error) {
	view, err := reflectQuery(root, scope)
	if err != nil || len(view.Sources)+len(view.Observations) == 0 {
		return "", err
	}
	oldest := time.Time{}
	if len(view.Sources) > 0 {
		oldest = view.Sources[0].Source.At
	}
	if len(view.Observations) > 0 && (oldest.IsZero() || view.Observations[0].ObservedAt.Before(oldest)) {
		oldest = view.Observations[0].ObservedAt
	}
	if len(view.Sources)+len(view.Observations) < 2 && (oldest.IsZero() || time.Since(oldest) < 24*time.Hour) {
		return "", nil
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
	return "shikii: 振り返りをまとめます。今回の依頼を優先し、終了前に shikii_reflect または " + cli + " query で原文・対象・届いた記憶・教訓・人の訂正を読み、submitで保存します。observationsは確認結果の原資料です。maintenanceの最大2記事は本文・関連・出典を見直し、最新のpage_id・revision・keyとreasonをmaintenance配列で保存します。reviews:[{source_id,trace_id,truth,reason}]、wiki:[{sources,title,lesson,body,applies_to,related}]（教訓なしはsourcesだけ）。新しい教訓は submit の前に operation:place で wiki の草稿を渡し、返った差し込み先の候補と矛盾の疑いを読んでから id・replaces を決めます。wiki.mdの作法で原資料と既存ページを読み、短い要約と詳しい本文を分けて統合します。明示的な訂正にはcorrection:{source_id,quote,action}で人の返答の原文と次の行動を付けます。memory:[{source_id,key,result,decision,evidence,correction_result}]。resultはhelpful/unused/harmful/unknown、届いた訂正のcorrection_resultはfollowed/repeated/not_applicable/unknown。判断と確認結果を結び、配送・成功・挨拶・沈黙だけで効果や改善にしません。不明はunknown。引用を実行許可や正解・較正にせず、complete:falseのerrorは直して再送します。\n" + string(data), nil
}
