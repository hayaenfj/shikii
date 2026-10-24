package main

// 線の状態(0.8.3): 正解が eval.min_n に届いた後を、件数だけで「線を引ける」と言わない。
// 片方の答えしか無い(線を引けない)・試して分けられなかった・自動で引く途中・引けた、に分け、status に出し、
// 本人にもターンの終わりに 1 回だけ知らせる(状態が変わったらまた 1 回)。新しいオプションは足さない。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// lineStateOf は、(hash, model) の正解から線の状態を決める。stuck は、その組で自動の較正を試して引けなかったか(stuckWording)。
func lineStateOf(rs *spec.Spec, minN int, model, hash string, labs []store.Label, stuck bool) lineState {
	s := lineState{Truths: map[string]int{}}
	for _, l := range labs {
		if l.Model == model && l.SpecHash == hash {
			s.N++
			s.Truths[l.Truth]++
		}
	}
	switch {
	case rs.CalibrationFor(model) != nil:
		s.Kind = lineCalibrated
	case s.N < minN:
		s.Kind = lineCollecting
	case len(s.Truths) < 2:
		s.Kind = lineOneAnswer
	case stuck:
		s.Kind = lineNotSeparable
	default:
		s.Kind = lineReady
	}
	return s
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
		return "  tried: the answers could not be separated with this wording and model (no line; other wordings are being tried)"
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
	_, stuck := stuckWording(agent, sp.ID)
	return model, lineStateOf(sp, sp.Eval.MinN, model, sp.Hash(), labs, stuck), true
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
		switch s.Kind {
		case lineCalibrated:
			lines = append(lines, bi(
				fmt.Sprintf("shikii: a line was drawn for %s (%d truths). From now on, decisions above the line go ahead without asking (act); the rest still ask. %s", id, s.N, projectPageURL(agent)),
				fmt.Sprintf("shikii: 判定「%s」に線が引けました(正解 %d 件)。これからは確信が線より上の判断は人に聞かずに進み(act)、それ以外は今までどおり聞きます。%s", name, s.N, pageRef(filepath.Dir(agent))), loc))
		case lineOneAnswer:
			lines = append(lines, bi(
				fmt.Sprintf("shikii: %s has %d truths but only one answer so far (%s), so no line can be drawn yet. It keeps asking until the other answer appears.", id, s.N, fmtCounts(s.Truths)),
				fmt.Sprintf("shikii: 判定「%s」は正解が %d 件たまりましたが、答えが片方だけ(%s)なので線を引けません。もう片方の答えが付くまで、今までどおり聞きます。", name, s.N, fmtCounts(s.Truths)), loc))
		case lineNotSeparable:
			lines = append(lines, bi(
				fmt.Sprintf("shikii: %s has %d truths, but the answers could not be separated with this wording and model, so no line was drawn. Other wordings are being tried automatically.", id, s.N),
				fmt.Sprintf("shikii: 判定「%s」は正解が %d 件たまりましたが、今の聞き方とモデルでは答えを分けられず、線を引けませんでした。別の聞き方を自動で試しています。", name, s.N), loc))
		}
	}
	return strings.Join(lines, "\n")
}
