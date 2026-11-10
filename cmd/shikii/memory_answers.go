package main

import (
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// answerKindRejectUnused is how many "unused" assessments of quoted past answers, with not one "helpful", stop quoting past answers
// in a project. Measured in this repository's own project (2026-11-10): 81 assessed, none helpful, and each quote took one of the two
// seats from a lesson.
const answerKindRejectUnused = 10

// answerRejection is what the agent's reflection said about quoting the person's past answers in a project. An answer the reflection
// found unused twice and never helpful is rejected: it is never quoted again, it leaves the page's list of answered decisions, and
// the page lists it as judged not useful by the AI. The answer itself stays a truth for its decision: lines are still drawn from it.
type answerRejection struct{ effects store.AnswerEffects }

func readAnswerRejection(root, scope string) answerRejection {
	return answerRejection{effects: (store.Store{Dir: root}).AnswerEffects(scope)}
}

// rejected reports whether the answer to the decision trace is rejected (the rule a lesson is silenced by at a door).
func (r answerRejection) rejected(trace string) bool { return silenced(r.effects.ByTrace[trace]) }

// kindStopped reports whether quoting past answers is stopped in this project: enough of them were assessed and none ever helped.
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
	Unused  int              `json:"unused"`  // assessments of quoted answers: not used
	Helpful int              `json:"helpful"` // assessments of quoted answers: helped a decision
	Items   []rejectedAnswer `json:"items"`
}

// rejectedAnswers builds the page's list, newest assessment first. An answer whose decision is gone is still listed by its id.
func rejectedAnswers(root, scope string) (rejectedAnswersView, error) {
	r := readAnswerRejection(root, scope)
	view := rejectedAnswersView{Stopped: r.kindStopped(), Unused: r.effects.Total.Unused, Helpful: r.effects.Total.Helpful, Items: []rejectedAnswer{}}
	answers, err := (store.Store{Dir: root}).PersonAnswers(answerSpecs)
	if err != nil {
		return view, err
	}
	byTrace := map[string]store.PersonAnswer{}
	for _, a := range answers {
		byTrace[a.TraceID] = a
	}
	specs := map[string]*spec.Spec{}
	for trace, effect := range r.effects.ByTrace {
		if !silenced(effect) {
			continue
		}
		item := rejectedAnswer{TraceID: trace, Unused: effect.Unused, At: r.effects.LastAt[trace]}
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
