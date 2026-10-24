package main

// 線の状態(0.8.3): 正解が eval.min_n に届いた後を、件数だけで「線を引ける」と言わない。
// 片方の答えしか無い(線を引けない)・試して分けられなかった・自動で引く途中・引けた、に分け、status に出し、
// 本人にもターンの終わりに 1 回だけ知らせる(状態が変わったらまた 1 回)。新しいオプションは足さない。

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

const (
	lineCollecting   = "collecting"
	lineOneAnswer    = "one-answer"
	lineNotSeparable = "not-separable"
	lineReady        = "ready"
	lineCalibrated   = "calibrated"
)

// lineState は、spec の今の文言(hash)とモデルの組の線の状態と、その組の正解の数・答えごとの数。
type lineState struct {
	Kind   string
	N      int
	Truths map[string]int
}

// lineStateOf は、(hash, model) の正解から線の状態を決める。数え方は自動の較正(hookMaybeCalibrate)と同じで、入力ごとに最新の 1 件、
// 人と結果の正解だけ(監査 0.8.3 の M3)。tried は、そのモデルで自動の較正を試して線が引けなかったか(triedWithoutLine)。
// 答えが片方だけで線を引けないのは noul だけ(choice は正解が 1 種類でも生の確率の確かめで線を引ける。監査 0.8.3 の L2)。
func lineStateOf(rs *spec.Spec, minN int, model, hash string, labs []store.Label, tried bool) lineState {
	s := lineState{Truths: map[string]int{}}
	var mine []store.Label
	for _, l := range labs {
		if l.Model == model && l.SpecHash == hash {
			mine = append(mine, l)
		}
	}
	for _, l := range store.LatestPerInput(mine) {
		if store.HumanSource(l.Source) {
			s.N++
			s.Truths[l.Truth]++
		}
	}
	switch {
	case rs.CalibrationFor(model) != nil:
		s.Kind = lineCalibrated
	case s.N < minN:
		s.Kind = lineCollecting
	case rs.Type == spec.Noul && len(s.Truths) < 2:
		s.Kind = lineOneAnswer
	case tried:
		s.Kind = lineNotSeparable
	default:
		s.Kind = lineReady
	}
	return s
}

// triedWithoutLine は、この文言とモデルで自動の較正を試し、線が引けなかったか(控え calibrating-<id> の時刻・モデル・文言で見る)。
// 別のモデルで試した控えでは true にしない(監査 0.8.3 の L1)。文脈の段も同じ時に試すので、最小の質問の文言で見る。
func triedWithoutLine(storeDir string, sp *spec.Spec, model string) bool {
	guard := filepath.Join(storeDir, "calibrating-"+sp.ID)
	info, err := os.Stat(guard)
	if err != nil || time.Since(info.ModTime()) < hookCalibrateGap {
		return false // 試していないか、まだ走っているかもしれない
	}
	_, ok := lastCalibrateAttempt(guard, sp.Hash(), model)
	return ok
}

// statusNote は status の行に付ける説明(英語、status の他の行と同じ)。
func (s lineState) statusNote(rs *spec.Spec, minN int) string {
	switch s.Kind {
	case lineCalibrated:
		return "  calibrated"
	case lineCollecting:
		return fmt.Sprintf("  collecting (%d/%d)", s.N, minN)
	case lineOneAnswer:
		return fmt.Sprintf("  only one answer so far (%s): a line needs both answers", fmtCounts(s.Truths))
	case lineNotSeparable:
		return "  tried: the answers could not be separated with this wording and model (no line yet; tried again as truths grow)"
	default:
		return fmt.Sprintf("  enough truths: the line is drawn automatically (>= min_n %d)", minN)
	}
}

// currentLineState は、spec の今の文言で正解の一番多いモデル(今使っているモデル)の線の状態。正解が無ければ ok=false。
func currentLineState(agent string, sp *spec.Spec) (model string, s lineState, ok bool) {
	_, labs, err := (store.Store{Dir: agent}).Counts(sp.ID)
	if err != nil || len(labs) == 0 {
		return "", lineState{}, false
	}
	byModel := map[string]int{}
	for _, l := range labs {
		if l.SpecHash == sp.Hash() {
			byModel[l.Model]++
		}
	}
	if len(byModel) == 0 {
		return "", lineState{}, false
	}
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool {
		if byModel[models[i]] != byModel[models[j]] {
			return byModel[models[i]] > byModel[models[j]]
		}
		return models[i] < models[j]
	})
	model = models[0]
	return model, lineStateOf(sp, sp.Eval.MinN, model, sp.Hash(), labs, triedWithoutLine(agent, sp, model)), true
}

// lineSummary は、本人に知らせる線の状態の変化(Stop の systemMessage)。spec ごとに、状態・文言・モデルが変わった時だけ 1 回。
func lineSummary(agent string) string {
	var lines []string
	for _, id := range append(append([]string{}, hookSpecs...), wikiSpecs...) {
		sp, err := spec.Load(filepath.Join(agent, "decisions", id+".yaml"))
		if err != nil {
			continue
		}
		model, s, ok := currentLineState(agent, sp)
		if !ok || s.Kind == lineCollecting || s.Kind == lineReady {
			continue
		}
		key := s.Kind + " " + sp.Hash() + " " + model
		mark := filepath.Join(agent, "line-shown-"+id)
		if b, err := os.ReadFile(mark); err == nil && strings.TrimSpace(string(b)) == key {
			continue
		}
		if err := os.WriteFile(mark, []byte(key+"\n"), 0o600); err != nil {
			continue
		}
		name := id
		if d, ok := sp.Display["ja"]; ok && d.Title != "" {
			name = d.Title + "(" + id + ")"
		}
		loc := hookLoc{Lang: hookLang()}
		machine := machineOnlySpecs[id]            // 人に聞かない問い(wiki の差し込み先・矛盾)。AI が答え、正解は結果から
		byResults := outcomeSpecs[id] && !machine  // hook の問いのうち、迷っても人に聞かず結果から正解が付く物(prompt-kind・changes-in-scope・done-as-asked)
		wordings := slices.Contains(hookSpecs, id) // 線が引けない時に、エージェントへ別の聞き方の候補を頼む問い(hook の 4 問)
		switch s.Kind {
		case lineCalibrated:
			en, ja := "From now on, decisions above the line go ahead without asking (act); the rest still ask, and 1 in 20 of the act decisions is shown to you as a spot check.", "これからは確信が線より上の判断は人に聞かずに進み(act)、それ以外は今までどおり聞きます。act の 20 件に 1 件は抜き取りで画面に出ます。"
			if machine {
				en, ja = "Its answers now carry a checked confidence (act) for the agent; nothing is asked of you, and lessons are never replaced silently.", "これからはその答えに、線で確かめた確信(act)が付いてエージェントへ渡ります。人に聞くことはなく、教訓を黙って置き換えることもありません。"
			}
			if byResults {
				en, ja = "From now on, decisions above the line go ahead (act); the rest wait for results to supply the truth instead of asking you, and 1 in 20 of the act decisions is shown to you as a spot check.", "これからは確信が線より上の判断は進み(act)、それ以外は迷っても人に聞かず、結果から正解が付くのを待ちます。act の 20 件に 1 件は抜き取りで画面に出ます。"
			}
			lines = append(lines, bi(
				fmt.Sprintf("shikii: a line was drawn for %s (%d truths). %s %s", id, s.N, en, projectPageURL(agent)),
				fmt.Sprintf("shikii: 判定「%s」に線が引けました(正解 %d 件)。%s %s", name, s.N, ja, pageRef(filepath.Dir(agent))), loc))
		case lineOneAnswer:
			en, ja := "It keeps asking until the other answer appears.", "もう片方の答えが付くまで、今までどおり聞きます。"
			if machine {
				en, ja = "The AI keeps answering it without a line; nothing is asked of you.", "線が無いまま AI が答え続けます。人に聞くことはありません。"
			}
			if byResults {
				en, ja = "It waits for results to supply the other answer; nothing is asked of you.", "もう片方の答えが結果から付くのを待ちます。人に聞くことはありません。"
			}
			lines = append(lines, bi(
				fmt.Sprintf("shikii: %s has %d truths but only one answer so far (%s), so no line can be drawn yet. %s", id, s.N, fmtCounts(s.Truths), en),
				fmt.Sprintf("shikii: 判定「%s」は正解が %d 件たまりましたが、答えが片方だけ(%s)なので線を引けません。%s", name, s.N, fmtCounts(s.Truths), ja), loc))
		case lineNotSeparable:
			en, ja := fmt.Sprintf("It is tried again when %d more truths arrive.", hookRecalibrateEvery), fmt.Sprintf("正解が %d 件増えたら、もう一度試します。", hookRecalibrateEvery)
			if wordings {
				en, ja = "The agent is asked for other wordings, which are scored on the same truths; it is also tried again as truths grow.", "エージェントに別の聞き方の候補を頼み、同じ正解で採点します。正解が増えた時にももう一度試します。"
			}
			lines = append(lines, bi(
				fmt.Sprintf("shikii: %s has %d truths, but the answers could not be separated with this wording and model, so no line was drawn. %s", id, s.N, en),
				fmt.Sprintf("shikii: 判定「%s」は正解が %d 件たまりましたが、今の聞き方とモデルでは答えを分けられず、線を引けませんでした。%s", name, s.N, ja), loc))
		}
	}
	return strings.Join(lines, "\n")
}
