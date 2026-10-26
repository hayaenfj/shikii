package main

// Line state (0.8.3): once truths reach eval.min_n, do not claim "a line can be drawn" from the count alone.
// Split it into: only one answer so far (no line possible), tried and could not separate, being drawn automatically, and drawn; show it in status
// and tell the person once at the end of a turn (once more when the state changes). No new options are added.

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

// lineState is the line state for the pair of the spec's current wording (hash) and model, with that pair's number of truths and the count per answer.
type lineState struct {
	Kind   string
	N      int
	Truths map[string]int
}

// lineStateOf decides the line state from the truths for (hash, model). It counts the same way as automatic calibration (hookMaybeCalibrate): the latest one per input,
// truths from people and results only (audit 0.8.3 M3). tried is whether automatic calibration was tried with that model and drew no line (triedWithoutLine).
// Only noul cannot draw a line with a single answer (choice can draw a line from the raw-probability check even with one kind of truth; audit 0.8.3 L2).
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

// triedWithoutLine reports whether automatic calibration was tried with this wording and model and drew no line (checked by the time, model and wording in the marker calibrating-<id>).
// A marker from a try with another model does not make it true (audit 0.8.3 L1). The context stages are tried at the same time, so the wording of the minimal question is used.
func triedWithoutLine(storeDir string, sp *spec.Spec, model string) bool {
	guard := filepath.Join(storeDir, "calibrating-"+sp.ID)
	info, err := os.Stat(guard)
	if err != nil || time.Since(info.ModTime()) < hookCalibrateGap {
		return false // not tried, or it may still be running
	}
	_, ok := lastCalibrateAttempt(guard, sp.Hash(), model)
	return ok
}

// statusNote is the note added to the status line (in English, like the other status lines).
func (s lineState) statusNote(rs *spec.Spec, minN int) string {
	switch s.Kind {
	case lineCalibrated:
		return "  calibrated"
	case lineCollecting:
		return fmt.Sprintf("  collecting (min_n %d)", minN) // the count itself is in the column before the note (one number per line)
	case lineOneAnswer:
		return fmt.Sprintf("  only one answer so far (%s): a line needs both answers", fmtCounts(s.Truths))
	case lineNotSeparable:
		return "  tried: the answers could not be separated with this wording and model (no line yet; tried again as truths grow)"
	default:
		return fmt.Sprintf("  enough truths: the line is drawn automatically (>= min_n %d)", minN)
	}
}

// currentLineState is the line state of the model with the most truths for the spec's current wording (the model in use). ok=false when there are no truths.
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

// lineSummary is the change in line state told to the person (Stop's systemMessage). Once per spec, only when the state, wording or model changes.
func lineSummary(agent string) string {
	var lines []string
	for _, id := range append(append([]string{}, hookSpecs...), wikiSpecs...) {
		sp, err := spec.Load(hookSpecPath(agent, id))
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
		// the notices name the question by its title on the page, never by the spec id (0.9.3); the Japanese text uses the page's plain words (glossary)
		name, enName := questionTitle(sp, "ja"), questionTitle(sp, "en")
		loc := hookLoc{Lang: hookLang()}
		machine := machineOnlySpecs[id]            // questions never asked of people (wiki insertion point, contradiction). The AI answers; the truth comes from results
		byResults := outcomeSpecs[id] && !machine  // hook questions that, even when unsure, do not ask a person and get their truth from results (prompt-kind, changes-in-scope, done-as-asked)
		wordings := slices.Contains(hookSpecs, id) // questions for which the agent is asked for other wordings when no line can be drawn (the 4 hook questions)
		switch s.Kind {
		case lineCalibrated:
			en, ja := "From now on, when the AI is confident enough it goes ahead without asking; the rest still ask, and 1 in 20 of what it decided alone is shown to you to check.", "これからは AI の自信が境目より上なら人に聞かずに進み、それ以外は今までどおり聞きます。AI が自分で進めた物の 20 件に 1 件は、確かめのため画面に出ます。"
			if machine {
				en, ja = "Its answers now carry a checked confidence for the agent; nothing is asked of you, and lessons are never replaced silently.", "これからはその答えに、確かめた自信が付いてエージェントへ渡ります。人に聞くことはなく、教訓を黙って置き換えることもありません。"
			}
			if byResults {
				en, ja = "From now on, when the AI is confident enough it goes ahead; the rest wait for results to supply the truth instead of asking you, and 1 in 20 of what it decided alone is shown to you to check.", "これからは AI の自信が境目より上なら進み、それ以外は迷っても人に聞かず、結果から正解が付くのを待ちます。AI が自分で進めた物の 20 件に 1 件は、確かめのため画面に出ます。"
			}
			lines = append(lines, bi(
				fmt.Sprintf("shikii: a line was drawn for %q (%d truths). %s %s", enName, s.N, en, projectPageURL(agent)),
				fmt.Sprintf("shikii: 問い「%s」で、AI がひとりで決めてよい境目が決まりました(正解 %d 件)。%s %s", name, s.N, ja, pageRef(filepath.Dir(agent))), loc))
		case lineOneAnswer:
			en, ja := "It keeps asking until the other answer appears.", "もう片方の答えが付くまで、今までどおり聞きます。"
			if machine {
				en, ja = "The AI keeps answering it without a line; nothing is asked of you.", "境目が無いまま AI が答え続けます。人に聞くことはありません。"
			}
			if byResults {
				en, ja = "It waits for results to supply the other answer; nothing is asked of you.", "もう片方の答えが結果から付くのを待ちます。人に聞くことはありません。"
			}
			lines = append(lines, bi(
				fmt.Sprintf("shikii: %q has %d truths but only one answer so far (%s), so no line can be drawn yet. %s", enName, s.N, enCounts(sp, s.Truths), en),
				fmt.Sprintf("shikii: 問い「%s」は正解が %d 件たまりましたが、答えが片方だけ(%s)なので、AI がひとりで決めてよい境目をまだ決められません。%s", name, s.N, jaCounts(sp, s.Truths), ja), loc))
		case lineNotSeparable:
			en, ja := fmt.Sprintf("It is tried again when %d more truths arrive.", hookRecalibrateEvery), fmt.Sprintf("正解が %d 件増えたら、もう一度試します。", hookRecalibrateEvery)
			if wordings {
				en, ja = "The agent is asked for other wordings, which are scored on the same truths; it is also tried again as truths grow.", "エージェントに別の聞き方の候補を頼み、同じ正解で採点します。正解が増えた時にももう一度試します。"
			}
			lines = append(lines, bi(
				fmt.Sprintf("shikii: %q has %d truths, but the answers could not be separated with this wording and model, so no line was drawn. %s", enName, s.N, en),
				fmt.Sprintf("shikii: 問い「%s」は正解が %d 件たまりましたが、今の聞き方では答えを見分けられず、AI がひとりで決めてよい境目を決められませんでした。%s", name, s.N, ja), loc))
		}
	}
	return strings.Join(lines, "\n")
}

// jaCounts is the count per answer in the page's Japanese answer names (display.ja.options; plain Japanese yes / no for a noul without them), not raw ids like no=10.
// enCounts is jaCounts for English: answers by their names on the page (display.en options), not by their ids (0.9.3 audit 2, low 3).
func enCounts(sp *spec.Spec, truths map[string]int) string {
	keys := make([]string, 0, len(truths))
	for k := range truths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		name := sp.Display["en"].Options[k]
		if name == "" {
			name = k
		}
		parts = append(parts, fmt.Sprintf("%s: %d", name, truths[k]))
	}
	return strings.Join(parts, ", ")
}

func jaCounts(sp *spec.Spec, truths map[string]int) string {
	keys := make([]string, 0, len(truths))
	for k := range truths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		name := sp.Display["ja"].Options[k]
		if name == "" {
			name = map[string]string{"yes": "はい", "no": "いいえ"}[k]
		}
		if name == "" {
			name = k
		}
		parts = append(parts, fmt.Sprintf("%s %d 件", name, truths[k]))
	}
	return strings.Join(parts, "・")
}
