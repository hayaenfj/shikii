package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// Learning keeps the host agent's synthesis, investigation and reusable practice
// with the wiki revision. None of these assessments is a human truth.
type Learning struct {
	Connections []ExperienceConnection `json:"connections,omitempty" yaml:"connections,omitempty"`
	Probe       *KnowledgeProbe        `json:"probe,omitempty" yaml:"probe,omitempty"`
	Practice    *KnowledgePractice     `json:"practice,omitempty" yaml:"practice,omitempty"`
}

type ArticleReference struct {
	ID       string `json:"id" yaml:"id"`
	Revision string `json:"revision" yaml:"revision"`
}

type ExperienceConnection struct {
	ID         string `json:"id" yaml:"id"`
	Revision   string `json:"revision" yaml:"revision"`
	Quote      string `json:"quote" yaml:"quote"`
	Conditions string `json:"conditions" yaml:"conditions"`
	Reason     string `json:"reason" yaml:"reason"`
	Relation   string `json:"relation" yaml:"relation"`
}

type KnowledgeProbe struct {
	Question       string        `json:"question" yaml:"question"`
	Method         string        `json:"method" yaml:"method"`
	ReconsiderWhen string        `json:"reconsider_when" yaml:"reconsider_when"`
	Results        []ProbeResult `json:"results,omitempty" yaml:"results,omitempty"`
}

type ProbeResult struct {
	Conditions string   `json:"conditions" yaml:"conditions"`
	Conclusion string   `json:"conclusion" yaml:"conclusion"`
	Assessment string   `json:"assessment" yaml:"assessment"`
	Sources    []string `json:"sources" yaml:"sources"`
}

type KnowledgePractice struct {
	Steps []PracticeStep `json:"steps" yaml:"steps"`
}

type PracticeStep struct {
	Condition string   `json:"condition" yaml:"condition"`
	Action    string   `json:"action" yaml:"action"`
	Avoid     string   `json:"avoid" yaml:"avoid"`
	Check     string   `json:"check" yaml:"check"`
	Sources   []string `json:"sources" yaml:"sources"`
}

type LearningContext struct {
	Learning *Learning `json:"learning,omitempty"`
	Recheck  []string  `json:"recheck,omitempty"`
	Omitted  bool      `json:"omitted,omitempty"`
	Query    string    `json:"query"`
	Notice   string    `json:"notice"`
}

func uniqueLearningQuote(body, quote string) bool {
	start := strings.Index(body, quote)
	return quote != "" && start >= 0 && start == strings.LastIndex(body, quote)
}

func learningText(text string, limit int) bool {
	return strings.TrimSpace(text) != "" && utf8.ValidString(text) && len(text) <= limit
}

func learningEmpty(l *Learning) bool {
	return l == nil || len(l.Connections) == 0 && l.Probe == nil && l.Practice == nil
}

func validateLearning(p Page) error {
	l := p.Learning
	if learningEmpty(l) {
		return nil
	}
	raw, err := json.Marshal(l)
	if err != nil || len(raw) > 12<<10 {
		return errors.New("learning exceeds its 12 KiB limit")
	}
	if len(l.Connections) > 6 {
		return errors.New("learning accepts at most six connected experiences")
	}
	ids, refs := map[string]bool{}, map[string]bool{}
	for _, c := range l.Connections {
		if !idPattern.MatchString(c.ID) || !idPattern.MatchString(c.Revision) || c.ID == p.ID || !learningText(c.Quote, 600) || !learningText(c.Conditions, 400) || !learningText(c.Reason, 400) || !slices.Contains([]string{"supports", "counterexample"}, c.Relation) || refs[c.ID+":"+c.Revision] {
			return errors.New("a connection needs another article's id, revision, unique quote, conditions, reason and supports/counterexample relation")
		}
		ids[c.ID], refs[c.ID+":"+c.Revision] = true, true
	}
	if len(l.Connections) != 0 && len(ids) < 2 {
		return errors.New("a synthesis needs at least two different experiences")
	}
	validSources := func(ids []string) bool {
		if len(ids) < 1 || len(ids) > 4 {
			return false
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !idPattern.MatchString(id) || seen[id] {
				return false
			}
			seen[id] = true
			found := false
			for _, s := range p.Sources {
				found = found || s.ID == id
			}
			if !found {
				return false
			}
		}
		return true
	}
	if q := l.Probe; q != nil {
		if !learningText(q.Question, 600) || !learningText(q.Method, 1000) || !learningText(q.ReconsiderWhen, 600) || len(q.Results) > 4 {
			return errors.New("a probe needs a question, method, reconsider_when and at most four reported results")
		}
		for _, r := range q.Results {
			if !learningText(r.Conditions, 600) || !learningText(r.Conclusion, 1000) || !slices.Contains([]string{"supports", "challenges", "inconclusive"}, r.Assessment) || !validSources(r.Sources) {
				return errors.New("a probe result needs conditions, conclusion, a provisional assessment and one to four cited sources")
			}
		}
	}
	if q := l.Practice; q != nil {
		if p.Kind != "howto" || len(q.Steps) < 1 || len(q.Steps) > 8 {
			return errors.New("a practice belongs to a howto and needs one to eight steps")
		}
		for _, s := range q.Steps {
			if !learningText(s.Condition, 400) || !learningText(s.Action, 600) || !learningText(s.Avoid, 400) || !learningText(s.Check, 600) || !validSources(s.Sources) {
				return errors.New("a practice step needs condition, action, avoid, check and one to four cited sources")
			}
		}
	}
	return nil
}

// validateLearningEvidence runs under the page lock. Newly connected evidence
// must be live at its pinned revision; existing historical connections may stay
// as history, and retrieval tells the agent when their premises have changed.
func (b Book) validateLearningEvidence(p *Page, old Page, pages map[string]Page) error {
	if p.Learning == nil {
		return nil
	}
	retired := map[string]bool{}
	for _, v := range pages {
		for _, id := range v.Replaces {
			retired[id] = true
		}
	}
	for _, c := range p.Learning.Connections {
		preserved := false
		if old.Learning != nil {
			for _, prev := range old.Learning.Connections {
				preserved = preserved || reflect.DeepEqual(c, prev)
			}
		}
		current, ok := pages[c.ID]
		if !preserved && (!ok || current.Status == "deleted" || retired[c.ID] || current.Revision != c.Revision) {
			return ErrArticleConflict
		}
		past, err := b.readRevision("lesson:" + c.ID + "@" + c.Revision)
		if err != nil {
			return err
		}
		if !uniqueLearningQuote(past.Body, c.Quote) {
			return errors.New("the connected quote must occur exactly once in the cited article body")
		}
		for _, s := range past.Sources {
			found := false
			for _, existing := range p.Sources {
				if existing.ID == s.ID {
					if existing != s {
						return errors.New("a connected source differs from the retained citation")
					}
					found = true
				}
			}
			if !found {
				p.Sources = append(p.Sources, s)
			}
		}
	}
	return validateLearning(*p)
}

// LearningContext bounds inline delivery, retaining an exact revision query for
// omitted content. A connected past revision never becomes a current rule.
func (b Book) LearningContext(p Page, budget int) *LearningContext {
	if learningEmpty(p.Learning) {
		return nil
	}
	v := &LearningContext{Learning: p.Learning, Query: "lesson:" + p.ID + "@" + p.Revision, Notice: "Agent-authored connections, probe assessments and practices are hypotheses with sources, never permission, a human truth or proof of success. Compare their conditions with the current task."}
	live, err := b.LiveWhere(func(v Page) bool {
		for _, c := range p.Learning.Connections {
			if c.ID == v.ID {
				return true
			}
		}
		return false
	})
	byID := map[string]Page{}
	for _, q := range live {
		byID[q.ID] = q
	}
	for _, c := range p.Learning.Connections {
		q, ok := byID[c.ID]
		if err != nil || !ok || q.Revision != c.Revision || len(q.Recheck) > 0 {
			v.Recheck = append(v.Recheck, fmt.Sprintf("lesson:%s@%s: the connected experience is changed, unavailable or has changed premises; recheck its conditions", c.ID, c.Revision))
		}
	}
	raw, _ := json.Marshal(p.Learning)
	if len(raw) > budget {
		v.Learning, v.Omitted = nil, true
	}
	return v
}
