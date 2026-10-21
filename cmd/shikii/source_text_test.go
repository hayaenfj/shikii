package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestHookKeepsFullClosingTextForPeople(t *testing.T) {
	var modelSawEnd atomic.Bool
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "END-OF-FULL-REPLY") || strings.Contains(state, "END-OF-FULL-REQUEST") {
			modelSawEnd.Store(true)
		}
		return .6
	})
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	request := strings.Repeat("README の日本語の起動手順を更新してください。", 100) + "END-OF-FULL-REQUEST"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "full-text", "cwd": repo, "prompt": request})
	closing := strings.Repeat("日本語の起動手順を確認しました。", 100) + "\nEND-OF-FULL-REPLY"
	runHook(t, "stop", map[string]any{"session_id": "full-text", "cwd": repo, "last_assistant_message": closing})
	st := store.Store{Dir: agent}
	records, _ := st.Records("done-as-asked", 0)
	if len(records) != 1 || len(records[0].Inputs["summary"]) > requestMax || modelSawEnd.Load() {
		t.Fatal("判定モデルに渡す入力の上限は保つ")
	}
	before := records[0]
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "full-text", "cwd": repo, "prompt": "画面を開いて"})
	feedback, _ := st.RecentFeedback(feedbackScope(repo))
	view, err := reviewQuery(st, feedbackScope(repo))
	if err != nil || len(view["pending"].([]reviewTask)) != 1 || view["pending"].([]reviewTask)[0].Targets[0].OriginalInputs["summary"] != closing {
		t.Fatal("仮採点をするエージェントも query で末尾まで読める")
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: feedbackScope(repo)})
	reviewCall(t, h, map[string]any{"operation": "submit", "source_id": feedback[0].ID, "trace_id": before.TraceID, "truth": "unknown", "reason": "次の発言は画面を開く追加依頼であり、完了の評価を含まない"})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/queue?spec=done-as-asked", nil))
	var queue struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &queue)
	if len(queue.Items) != 1 {
		t.Fatal("確認する判定が画面に届く")
	}
	originals, _ := queue.Items[0]["original_inputs"].(map[string]any)
	if originals["summary"] != closing || originals["request"] != request {
		t.Fatal("1,500 バイトを超えた返答も、末尾まで画面へ届ける")
	}
	after, _ := st.FindRecord(before.SpecID, before.TraceID)
	if after.InputsHash != before.InputsHash || after.SpecHash != before.SpecHash || after.Inputs["summary"] != before.Inputs["summary"] {
		t.Fatal("画面用の原文は元の判定・入力・ハッシュを変えない")
	}
}

// 件数を絞る前後で、優先順・やり直し元の除外・返す原文が変わらない。
func TestServeQueueLimitKeepsPriorityAndOriginals(t *testing.T) {
	url, path, dir := newServeFixture(t, "false", map[string]float64{"change_request": 1})
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: dir}
	ids := []string{"0000000000000001", "0000000000000002", "0000000000000003", "0000000000000004"}
	for i, id := range ids {
		inputs := map[string]string{"message": "fixture"}
		rec := store.Record{TraceID: id, SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Band: "ask", Inputs: inputs, InputsHash: store.HashInputs(inputs), At: time.Unix(int64(i), 0)}
		if i == 1 {
			rec.Band = "discard"
		}
		if i == 3 {
			rec.RedoOf = ids[2]
		}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveRecordText(rec, map[string]string{"message": "fixture original " + id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct {
		query string
		want  []string
	}{{"&limit=1", []string{ids[3]}}, {"&limit=2", []string{ids[3], ids[0]}}, {"", []string{ids[3], ids[0], ids[1]}}} {
		var result struct {
			Items []struct {
				TraceID   string            `json:"trace_id"`
				Originals map[string]string `json:"original_inputs"`
			} `json:"items"`
		}
		if code := getJSON(t, url+"/v1/queue?spec=intent"+check.query, &result); code != 200 || len(result.Items) != len(check.want) {
			t.Fatalf("queue %s: status=%d rows=%+v", check.query, code, result.Items)
		}
		for i, item := range result.Items {
			if item.TraceID != check.want[i] || item.Originals["message"] != "fixture original "+item.TraceID {
				t.Fatalf("queue %s row %d: got %+v, want %s with its original", check.query, i, item, check.want[i])
			}
		}
	}
}
