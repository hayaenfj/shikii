package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 作法の検査(0.7.6): hook が計画の整理を頼んだ依頼でファイルを変え、submit しないまま終えようとしたら Stop を一度だけ止める。
// 止めた後の Stop は止めないが記録は残る。submit してあれば止めず、通った記録が残る。
func TestStopStopsOnceWhenPlanningWasAskedButNotSubmitted(t *testing.T) {
	repo, root, _ := planningFixture(t)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if !strings.Contains(feedbackContext(t, out), "hook plan query") {
		t.Fatalf("計画の整理を頼む: %s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
		t.Fatalf("計画の整理が無いままファイルを変えて終える Stop は止める: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "hook plan query") || !strings.Contains(reason, `"session_id":"planning"`) || !strings.Contains(reason, "作法が済んでいません") {
		t.Fatalf("理由に何をすればよいか(コマンドと id)を書く: %s", reason)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 1 || recs[0].Pass || recs[0].Recheck {
		t.Fatalf("止めた記録が残る: %+v", recs)
	}
	// 止められた直後に整理を submit できる(ターンの判定が済んでいても、整理がまだなら受ける)
	if _, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID}); err != nil {
		t.Fatalf("止められた後の query: %v", err)
	}
	if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
		t.Fatalf("止められた後の submit: %v", err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "整理した", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("二度目の Stop は止めない: %s", out)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 2 || !recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("止めてから済ませた記録(直った)が残る: %+v", recs)
	}
	// 次の依頼: 整理を submit してからファイルを変えて終えれば止めない
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest + " (again)"})
	if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if strings.Contains(out, "block") {
		t.Fatalf("整理を submit した依頼は止めない: %s", out)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 3 || !recs[2].Pass {
		t.Fatalf("通った記録が残る: %+v", recs)
	}
	if sOut := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(sOut, "rituals: plan-before-work done 1 of 2 asked (after a stop 1)") {
		t.Fatalf("status に作法の実施の数(止めてから済ませた 1):\n%s", sOut)
	}
	// ファイルを変えない依頼(説明など)では、整理が無くても止めない
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "explain the parser"})
	if out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "It parses."}); strings.Contains(out, "計画の整理") {
		t.Fatalf("ファイルを変えない依頼は計画の整理で止めない(振り返りの検査は別): %s", out)
	}
	// 組み込みの検査は一覧に出て、外せない
	list := capture(t, func() error { t.Chdir(repo); return cmdGuard([]string{"list"}) })
	if !strings.Contains(list, ritualPlan) || !strings.Contains(list, ritualReflect) {
		t.Fatalf("一覧に組み込みの作法の検査が出る:\n%s", list)
	}
	if err := cmdGuard([]string{"disable", ritualPlan}); err == nil || !strings.Contains(err.Error(), "組み込み") {
		t.Fatalf("組み込みの検査は外せない: %v", err)
	}
}

// 作法の検査(0.7.6): 依頼前に振り返りの対象(会話)を挙げたのに、保存しないまま終えようとしたら Stop を一度だけ止める。保存すれば通る。
func TestStopStopsOnceWhenReflectionWasAskedButNotSaved(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	// 2 つの会話を終えておく(振り返りの依頼は未整理が 2 件以上で出る)
	for i, req := range []string{"README の導入手順を更新してください", "画面の色を直してください"} {
		sid := []string{"a", "b"}[i]
		runHook(t, "user-prompt-submit", map[string]any{"session_id": sid, "cwd": repo, "prompt": req})
		runHook(t, "stop", map[string]any{"session_id": sid, "cwd": repo, "last_assistant_message": "やりました。"})
		runHook(t, "user-prompt-submit", map[string]any{"session_id": sid, "cwd": repo, "prompt": "ありがとう、次は別の件です。"})
	}
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "ガイドの quick start を直して"})
	ctx := feedbackContext(t, out)
	if !strings.Contains(ctx, "振り返りをまとめます") {
		t.Fatalf("未整理の会話が 2 件あれば振り返りを頼む: %s", ctx)
	}
	asked := loadSession(root, "c").ReflectAsked
	if len(asked) == 0 {
		t.Fatal("頼んだ会話を控えに書く")
	}
	out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "直しました。"})
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
		t.Fatalf("振り返りを保存しないまま終える Stop は止める: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "reflect query") || !strings.Contains(reason, asked[0]) || !strings.Contains(reason, "振り返りの保存が無い") {
		t.Fatalf("理由に対象の会話と手順を書く: %s", reason)
	}
	if recs := guardRecordsFor(root, ritualReflect); len(recs) != 1 || recs[0].Pass {
		t.Fatalf("止めた記録が残る: %+v", recs)
	}
	// 振り返りを保存する(仮採点と wiki の出典)
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	q := reflectCall(t, h, map[string]any{"operation": "query"}) // 道具の query は対象を自分で選ぶ(hook が挙げた会話と同じ)
	var reviews []map[string]any
	var wiki []map[string]any
	for _, s := range q["sources"].([]any) {
		src := s.(map[string]any)
		sm := src["source_material"].(map[string]any)
		id := sm["id"].(string)
		for _, tg := range src["targets"].([]any) {
			target := tg.(map[string]any)
			reviews = append(reviews, map[string]any{"source_id": id, "trace_id": target["trace_id"], "truth": "yes", "reason": "次の依頼にやり直しの指示が無い"})
		}
		wiki = append(wiki, map[string]any{"sources": []string{id}})
	}
	res := reflectCall(t, h, map[string]any{"operation": "submit", "reviews": reviews, "wiki": wiki})
	if res["complete"] != true {
		t.Fatalf("振り返りの保存: %v", res)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "直しました。", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("二度目の Stop は止めない: %s", out)
	}
	if recs := guardRecordsFor(root, ritualReflect); len(recs) != 2 || !recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("保存した後の再検査は通る(直った記録): %+v", recs)
	}
}
