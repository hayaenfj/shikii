package main

import (
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// answerKindRejectUnused is how many "unused" assessments of quoted past answers, with not one "helpful", stop quoting past answers
// in a project. A quoted answer takes one of the two seats of a delivery from a lesson, so quotes that never help cost lessons
// their deliveries.
const answerKindRejectUnused = 10

// answerRejection is what the agent's reflection said about quoting the person's past answers in a project. An answer the reflection
// found unused twice and never helpful is rejected: it is never quoted again, it leaves the page's list of answered decisions, and
// the page lists it as judged not useful by the AI. The answer itself stays a truth for its decision: lines are still drawn from it.
//
// The AI decides; the person can undo it from the page (/v1/answers/restore). An undone answer counts only the assessments after
// the undo, under every decision that carries it, so it is quoted again and is rejected again only by two later unused ones. An
// undone stop likewise counts only the assessments after it; the rejections of single answers stay.
//
// One answer can sit under several decisions: a decision asked again (/v1/redo) copies the person's answer to a new record. The
// assessments of all of them count as one, so a rejected answer does not come back under the new record, and the original of an
// answer rejected under its copy is not quoted in its place.
type answerRejection struct {
	effects store.AnswerEffects
	same    map[string][]string // decision trace -> the decisions that carry the same answer (itself included), newest first
}

func readAnswerRejection(root, scope string) answerRejection {
	return answerRejection{effects: (store.Store{Dir: root}).AnswerEffects(scope)}
}

// withAnswers tells the rejection which decisions carry the same answer: the same question, the same answer, and inputs that
// share a hash.
func (r answerRejection) withAnswers(answers []store.PersonAnswer) answerRejection {
	group := map[string]int{} // question, answer and inputs hash -> group
	of := map[string]int{}    // trace -> group
	members := [][]store.PersonAnswer{}
	for _, a := range answers {
		at := -1
		keys := []string{}
		for _, h := range a.Hashes {
			if h == "" { // a label from before inputs were hashed: nothing says it is about the same inputs as another one
				continue
			}
			key := a.SpecID + "\x00" + a.Truth + "\x00" + h
			keys = append(keys, key)
			if g, ok := group[key]; ok && at < 0 {
				at = g
			}
		}
		if at < 0 {
			at = len(members)
			members = append(members, nil)
		}
		for _, key := range keys {
			if g, ok := group[key]; ok && g != at { // this answer joins two groups: move the other one over
				for _, m := range members[g] {
					of[m.TraceID] = at
				}
				members[at], members[g] = append(members[at], members[g]...), nil
				for k, v := range group {
					if v == g {
						group[k] = at
					}
				}
			}
			group[key] = at
		}
		of[a.TraceID] = at
		members[at] = append(members[at], a)
	}
	r.same = map[string][]string{}
	for _, m := range members {
		for i, j := 0, len(m)-1; i < j; i, j = i+1, j-1 { // written later first, then by the time of the answer
			m[i], m[j] = m[j], m[i]
		}
		sort.SliceStable(m, func(i, j int) bool { return m[i].At.After(m[j].At) })
		traces := make([]string, len(m))
		for i, a := range m {
			traces[i] = a.TraceID
		}
		for _, trace := range traces {
			r.same[trace] = traces
		}
	}
	return r
}

// carriers are the decisions that carry the same answer as trace, newest first (trace alone when nothing else is known).
func (r answerRejection) carriers(trace string) []string {
	if same := r.same[trace]; len(same) > 0 {
		return same
	}
	return []string{trace}
}

// restoredAt is when the person last undid the rejection of the answer to the decision trace, under any decision that carries it
// (zero when never).
func (r answerRejection) restoredAt(trace string) time.Time {
	var undo time.Time
	for _, t := range r.carriers(trace) {
		if at := r.effects.Restored[t]; at.After(undo) {
			undo = at
		}
	}
	return undo
}

// effect is what the reflection said about the answer to the decision trace, under every decision that carries it, after the
// person's last undo of its rejection, with the time of the last of those assessments.
func (r answerRejection) effect(trace string) (store.PairEffect, time.Time) {
	var sum store.PairEffect
	var last time.Time
	undo := r.restoredAt(trace)
	for _, t := range r.carriers(trace) {
		for _, a := range r.effects.ByTrace[t] {
			if !store.CountsAfter(a.Delivered, undo) { // delivered after the undo; one delivered before it is a use from before
				continue
			}
			if a.Result == "helpful" {
				sum.Helpful++
			} else { // unused, and harmful counted as unused (AnswerEffects keeps only these)
				sum.Unused++
			}
			if a.At.After(last) {
				last = a.At
			}
		}
	}
	return sum, last
}

// rejected reports whether the answer to the decision trace is rejected (the rule a lesson is silenced by at a door).
func (r answerRejection) rejected(trace string) bool {
	e, _ := r.effect(trace)
	return silenced(e)
}

// kindStopped reports whether quoting past answers is stopped in this project: enough of them were assessed and none ever helped
// (counted after the person's last undo of the stop).
func (r answerRejection) kindStopped() bool {
	return r.effects.Total.Helpful == 0 && r.effects.Total.Unused >= answerKindRejectUnused
}

// rejectedAnswer is one rejected answer as the page lists it.
type rejectedAnswer struct {
	TraceID  string            `json:"trace_id"`
	Spec     string            `json:"spec,omitempty"`
	Request  string            `json:"request,omitempty"`
	Truth    string            `json:"truth,omitempty"`
	Question map[string]string `json:"question,omitempty"` // the question's title per display language
	Answer   map[string]string `json:"answer,omitempty"`   // the person's answer in the page's words per display language
	Unused   int               `json:"unused"`
	At       time.Time         `json:"at"` // the last assessment
}

// rejectedAnswersView is the page's list of what the AI judged not useful among the person's past answers.
type rejectedAnswersView struct {
	Stopped bool             `json:"stopped"` // quoting past answers is stopped in this project
	Unused  int              `json:"unused"`  // assessments of quoted answers since the last undo of the stop: not used
	Helpful int              `json:"helpful"` // assessments of quoted answers since the last undo of the stop: helped a decision
	Items   []rejectedAnswer `json:"items"`
}

// rejectedAnswers builds the page's list, newest assessment first: one line per answer, under its newest decision. An answer
// whose decision is gone is still listed by its id.
func rejectedAnswers(root, scope string) (rejectedAnswersView, error) {
	r := readAnswerRejection(root, scope)
	view := rejectedAnswersView{Stopped: r.kindStopped(), Unused: r.effects.Total.Unused, Helpful: r.effects.Total.Helpful, Items: []rejectedAnswer{}}
	answers, err := (store.Store{Dir: root}).PersonAnswers(answerSpecs)
	if err != nil {
		return view, err
	}
	r = r.withAnswers(answers)
	byTrace := map[string]store.PersonAnswer{}
	for _, a := range answers {
		byTrace[a.TraceID] = a
	}
	specs := map[string]*spec.Spec{}
	listed := map[string]bool{}
	for assessed := range r.effects.ByTrace {
		trace := r.carriers(assessed)[0]
		effect, last := r.effect(trace)
		if listed[trace] || !silenced(effect) {
			continue
		}
		listed[trace] = true
		item := rejectedAnswer{TraceID: trace, Unused: effect.Unused, At: last}
		if a, ok := byTrace[trace]; ok {
			item.Spec, item.Request, item.Truth = a.SpecID, cut(a.Request, requestMax), a.Truth
			sp, loaded := specs[a.SpecID]
			if !loaded {
				sp = loadOrNil(root, a.SpecID)
				specs[a.SpecID] = sp
			}
			if sp != nil {
				item.Question, item.Answer = map[string]string{}, map[string]string{}
				for _, lang := range []string{"en", "ja"} {
					item.Question[lang] = questionTitle(sp, lang)
					item.Answer[lang] = a.Truth
					if o := strings.TrimSpace(sp.Display[lang].Options[a.Truth]); o != "" {
						item.Answer[lang] = o
					}
				}
			}
		}
		view.Items = append(view.Items, item)
	}
	sort.Slice(view.Items, func(i, j int) bool {
		if !view.Items[i].At.Equal(view.Items[j].At) {
			return view.Items[i].At.After(view.Items[j].At)
		}
		return view.Items[i].TraceID < view.Items[j].TraceID
	})
	return view, nil
}
