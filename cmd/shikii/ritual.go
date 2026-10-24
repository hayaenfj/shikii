package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// 作法の検査(0.8.0): hook が頼んだ作法(作業の前の計画の整理、終える前の振り返りの保存)をエージェントがやらないまま終えようとしたら、
// 止める検査と同じ仕組みで Stop を一度だけ止めて理由を返す。頼むだけでは長い作業の中で後回しにされ、誰も気付かなかった
// (haya1 では needs-plan の記録が 0 件、届いた教訓の評価が 26 件中 6 件)。組み込みで常に有効、外す設定は無い。
// 記録は止める検査と同じ records/guards.jsonl に guard = plan-before-work / reflect-before-end で残り、status と画面に出る。

const (
	ritualPlan    = "plan-before-work"
	ritualReflect = "reflect-before-end"
)

// builtinGuards は組み込みの作法の検査(一覧・集計・画面用。評価は ritualStop が行う)。
func builtinGuards() []guard {
	return []guard{
		{ID: ritualPlan, Kind: "ritual", Title: "作業の前に計画を整理する(hook plan query → submit)", Reason: "hook が頼んだ依頼でファイルを変えたなら、整理を submit してから終える", Enabled: true},
		{ID: ritualReflect, Kind: "ritual", Title: "終える前に振り返りを保存する(reflect query → submit)", Reason: "hook が対象の会話を挙げたなら、仮採点・wiki・届いた教訓の評価を submit してから終える", Enabled: true},
	}
}

func isBuiltinGuard(id string) bool { return id == ritualPlan || id == ritualReflect }

// ritualStop は、このターンで頼んだ作法が済んでいるかを見る。recheck(止めた後の Stop)では記録だけして止めない。
func ritualStop(root, scope string, in *hookInput, snapshot *hookSession, changed []string, known bool, recheck bool) (hookOut, error) {
	if snapshot.RitualStopped == snapshot.TurnID && snapshot.TurnID != "" {
		recheck = true // このターンで一度止めた(通知の後の Stop は stop_hook_active が付かない): 記録だけ、もう止めない(監査 R5)
	}
	now := time.Now().UTC()
	var recs []guardRecord
	var failed []string
	// 計画: 頼んだ(controls: Planning がこのターンの物)のに submit が無く、ファイルを変えた
	if p := snapshot.Planning; p != nil && p.TurnID == snapshot.TurnID && known && len(changed) > 0 {
		pass := p.Result != nil
		reason := ""
		if !pass {
			reason = fmt.Sprintf("計画の整理(hook plan query → submit)が無いまま、%d 件のファイルを変えて終えようとしています。%s hook plan query に %s を渡して今回と直近の会話を読み、agreed・open を submit してから終えてください", len(changed), hookExe(), planJSON(in.SessionID, snapshot.TurnID))
			failed = append(failed, reason)
		}
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualPlan, Pass: pass, Reason: reason, Recheck: recheck})
	}
	// 振り返り: 依頼前に挙げた会話が、まだ振り返りの対象に残っている(仮採点・wiki・届いた教訓の評価のどれかが未)
	if len(snapshot.ReflectAsked) > 0 && snapshot.ReflectAskedTurn == snapshot.TurnID {
		left, err := reflectLeft(root, scope, snapshot.ReflectAsked)
		if err != nil {
			return hookOut{}, err
		}
		pass := len(left) == 0
		reason := ""
		if !pass {
			ids := make([]string, 0, len(left))
			for _, l := range left {
				ids = append(ids, l.id)
			}
			data, _ := json.Marshal(map[string]any{"sources": ids})
			reason = "振り返りの保存が無いまま終えようとしています: " + strings.Join(leftLines(left), "、") + "。" + hookExe() + " reflect query(または shikii_reflect の query。対象 " + string(data) + ")で原文・届いた記憶を読み、reviews・wiki(教訓なしは sources だけ)・memory(届いた教訓ごとの helpful / unused / harmful / unknown)を submit してから終えてください"
			failed = append(failed, reason)
		}
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualReflect, Pass: pass, Reason: reason, Recheck: recheck})
	}
	if len(recs) > 0 {
		if err := appendGuardRecords(root, recs); err != nil {
			return hookOut{}, err
		}
	}
	if len(failed) == 0 || recheck {
		return hookOut{}, nil
	}
	var b strings.Builder
	b.WriteString("shikii: 作法が済んでいません。済ませてから終えてください(組み込みの検査。二度目の Stop は止めません)。済ませた後の最後の返答では、作法の報告ではなく、本人が頼んだ作業の結果をもう一度短く伝えます。\n")
	for _, f := range failed {
		b.WriteString("- " + f + "\n")
	}
	return hookOut{Block: true, Reason: strings.TrimRight(b.String(), "\n")}, nil
}

// reflectLeftItem は、頼んだ会話のうちまだ振り返りの対象に残っている物と、何が未か。
type reflectLeftItem struct {
	id                string
	targets, memories int
	wiki              bool
}

func reflectLeft(root, scope string, asked []string) ([]reflectLeftItem, error) {
	view, err := reflectQuery(root, scope)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range asked {
		want[id] = true
	}
	var out []reflectLeftItem
	for _, s := range view.Sources {
		if !want[s.Source.ID] {
			continue
		}
		out = append(out, reflectLeftItem{id: s.Source.ID, targets: len(s.Targets), memories: len(s.Memories), wiki: s.WikiPending})
	}
	for _, o := range view.Observations {
		if want[o.ID] {
			out = append(out, reflectLeftItem{id: o.ID, wiki: true})
		}
	}
	return out, nil
}

func leftLines(left []reflectLeftItem) []string {
	var out []string
	for _, l := range left {
		var parts []string
		if l.targets > 0 {
			parts = append(parts, fmt.Sprintf("仮採点 %d", l.targets))
		}
		if l.wiki {
			parts = append(parts, "wiki 未整理")
		}
		if l.memories > 0 {
			parts = append(parts, fmt.Sprintf("届いた教訓の評価 %d 件", l.memories))
		}
		out = append(out, l.id+"("+strings.Join(parts, "・")+")")
	}
	return out
}

func hookExe() string {
	exe, err := os.Executable()
	if err != nil {
		return "shikii"
	}
	return "'" + strings.ReplaceAll(exe, "'", "'\\''") + "'"
}

func planJSON(session, turn string) string {
	b, _ := json.Marshal(map[string]string{"session_id": session, "turn_id": turn})
	return string(b)
}
