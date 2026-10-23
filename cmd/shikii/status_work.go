package main

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// personWork は「人の手間」の数: 今の規則で人の答えを待つ判定と、正解を誰が決めたか(人か、結果か)。
// 判定の段の数(rungs の left to a person)は迷った数で、結果から正解が付く判定の迷いは人を待たない。こちらは人が実際に答える数
// (迷い・抜き取り・AI の仮採点の抜き取りと保留)。
type personWork struct {
	Records, Waits             int // 全体
	RecentRecords, RecentWaits int // 直近 7 日
	ByPerson, ByResult         int // 正解の付いた判定(trace)の数。機械の正解(context / judge)は数えない
}

const personWorkWindow = 7 * 24 * time.Hour

func countPersonWork(specID string, discardUnsure bool, recs []store.Record, labs []store.Label, reviews map[string]store.Review, now time.Time) personWork {
	var w personWork
	person, result := map[string]bool{}, map[string]bool{}
	for _, l := range labs {
		switch {
		case l.Source == "outcome":
			result[l.TraceID] = true
		case store.HumanSource(l.Source):
			person[l.TraceID] = true
		}
	}
	w.ByPerson, w.ByResult = len(person), len(result)
	for _, r := range recs {
		waits := false
		switch r.Band {
		case "ask":
			waits = !outcomeSpecs[specID]
		case "discard":
			waits = discardUnsure && !outcomeSpecs[specID]
		case "act":
			waits = r.IsCalibrated && store.AuditSample(r.TraceID)
		}
		// AI の仮採点のうち、抜き取りと保留(unknown)は人に見せる。結果の正解が先に付いた判定は見せない(答えが出ている)
		if rv, ok := reviews[r.TraceID]; ok && (rv.Sampled || rv.Truth == "unknown") && !result[r.TraceID] {
			waits = true
		}
		recent := now.Sub(r.At) <= personWorkWindow
		w.Records++
		if recent {
			w.RecentRecords++
		}
		if waits {
			w.Waits++
			if recent {
				w.RecentWaits++
			}
		}
	}
	return w
}

func perThousand(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) * 1000 / float64(of)
}

func (w personWork) print(out io.Writer, indent string) {
	fmt.Fprintf(out, "%sperson's work: %d of %d decisions wait for a person under the current rules (%.0f per 1000; last 7 days %.0f per 1000 of %d); truths decided by a person %d, by results %d\n",
		indent, w.Waits, w.Records, perThousand(w.Waits, w.Records), perThousand(w.RecentWaits, w.RecentRecords), w.RecentRecords, w.ByPerson, w.ByResult)
}

func (w *personWork) add(o personWork) {
	w.Records += o.Records
	w.Waits += o.Waits
	w.RecentRecords += o.RecentRecords
	w.RecentWaits += o.RecentWaits
	w.ByPerson += o.ByPerson
	w.ByResult += o.ByResult
}

// printMemoryEffect は「届いた教訓が行動を変えたか」を本番の観察で数える(0.7.3。合成実験では差が出なかったので、実使用で数える):
// 届けた引用の数(サブエージェントへの配送も含む)と、振り返りが付けた helpful / unused / harmful / unknown、まだ観察の無い数。
// 訂正の配送(followed / repeated)は別に出す。直近 7 日は配送の時刻で数える。
func printMemoryEffect(out io.Writer, st store.Store, scope string, now time.Time) {
	m, err := memoryEffectOf(st, scope, now)
	if err != nil || m.Delivered == 0 {
		fmt.Fprintln(out, "memory: no lessons delivered yet (lessons reach the agent before similar requests, and subagents when they start)")
		return
	}
	fmt.Fprintf(out, "memory: %d quotes delivered; helpful %d, unused %d, harmful %d, unknown %d, not yet assessed %d (%d waiting for the next request)\n", m.Delivered, m.Helpful, m.Unused, m.Harmful, m.Unknown, m.Pending, m.AwaitingFeedback)
	fmt.Fprintf(out, "  last 7 days: delivered %d, helpful %d\n", m.RecentDelivered, m.RecentHelpful)
	if c := m.Corrections; c.Delivered > 0 {
		fmt.Fprintf(out, "  corrections delivered %d: followed %d, repeated %d, not applicable %d, unknown %d, pending %d\n", c.Delivered, c.Followed, c.Repeated, c.NotApplicable, c.Unknown, c.Pending)
	}
}

// storeScope は置き場(.shikii/)の親をプロジェクトとして、会話・記憶の区切り(実パス)にする。
func storeScope(storeDir string) string {
	abs, err := filepath.Abs(storeDir)
	if err != nil {
		return ""
	}
	return feedbackScope(filepath.Dir(abs))
}

// memoryEffectSummary は「届いた教訓の効果」の数(CLI の status と画面の /v1/status で同じ)。
type memoryEffectSummary struct {
	Delivered, Helpful, Unused, Harmful, Unknown, Pending, AwaitingFeedback int
	RecentDelivered, RecentHelpful                                          int
	Corrections                                                             store.CorrectionStats
}

func memoryEffectOf(st store.Store, scope string, now time.Time) (memoryEffectSummary, error) {
	var m memoryEffectSummary
	r, err := st.MemoryReport(scope)
	if err != nil {
		return m, err
	}
	s := r.Stats
	m.Delivered, m.Helpful, m.Unused, m.Harmful, m.Unknown, m.Pending, m.AwaitingFeedback = s.Delivered, s.Helpful, s.Unused, s.Harmful, s.Unknown, s.Pending, s.AwaitingFeedback
	m.Corrections = r.Corrections
	for _, o := range r.All {
		if now.Sub(o.At) > personWorkWindow {
			continue
		}
		m.RecentDelivered++
		if o.Assessment != nil && o.Assessment.Result == "helpful" {
			m.RecentHelpful++
		}
	}
	return m, nil
}

func (m memoryEffectSummary) json() map[string]any {
	return map[string]any{"delivered": m.Delivered, "helpful": m.Helpful, "unused": m.Unused, "harmful": m.Harmful, "unknown": m.Unknown, "pending": m.Pending, "awaiting_feedback": m.AwaitingFeedback,
		"recent_delivered": m.RecentDelivered, "recent_helpful": m.RecentHelpful,
		"corrections": map[string]any{"delivered": m.Corrections.Delivered, "followed": m.Corrections.Followed, "repeated": m.Corrections.Repeated, "not_applicable": m.Corrections.NotApplicable, "unknown": m.Corrections.Unknown, "pending": m.Corrections.Pending}}
}

func (w personWork) json() map[string]any {
	return map[string]any{"records": w.Records, "waits": w.Waits, "recent_records": w.RecentRecords, "recent_waits": w.RecentWaits, "by_person": w.ByPerson, "by_result": w.ByResult, "per_thousand": perThousand(w.Waits, w.Records), "recent_per_thousand": perThousand(w.RecentWaits, w.RecentRecords)}
}

// statusSummary は画面の先頭に出す要約(0.7.4): 人の手間、届いた教訓の効果、止める検査。
func statusSummary(st store.Store, root, scope string, person personWork, now time.Time) map[string]any {
	out := map[string]any{"person": person.json()}
	if m, err := memoryEffectOf(st, scope, now); err == nil {
		out["memory"] = m.json()
	} else {
		out["memory"] = memoryEffectSummary{}.json()
	}
	out["guards"] = guardSummaryJSON(root)
	return out
}

// storeSpecPaths は置き場の decisions/*.yaml(基本構成)。status が spec のパスを省かれた時に読む。
func storeSpecPaths(storeDir string) []string {
	paths, _ := filepath.Glob(filepath.Join(specsDirOf(storeDir), "*.yaml"))
	return paths
}
