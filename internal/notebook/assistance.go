package notebook

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/store"
)

// AssistanceContext retrieves conditions and their reasons for the host agent to
// reason with. Lexical matches and links are never classified as semantic support.
type AssistanceContext struct {
	Checks  []AssistanceCheck `json:"checks"`
	Omitted []string          `json:"omitted,omitempty"`
}

type AssistanceCheck struct {
	Page         Page                   `json:"page"`
	Relation     string                 `json:"relation"`
	From         string                 `json:"from,omitempty"`
	BodyOmitted  bool                   `json:"body_omitted,omitempty"`
	BodyExcerpts []TextExcerpt          `json:"body_excerpts,omitempty"`
	Orientation  KnowledgeOrientation   `json:"orientation"`
	Outcomes     []ProcedureOutcomeView `json:"outcomes,omitempty"`
	Learning     *LearningContext       `json:"learning_context,omitempty"`
}

func ValidAssistanceText(text string, required bool) bool {
	return utf8.ValidString(text) && len(text) <= 16000 && (!required || strings.TrimSpace(text) != "")
}

// AssistanceContext searches the request and proposal separately so an added
// approach cannot drown out the person's intent. Direct project links bring in
// exceptions even when they use different words. History cannot supply rules.
func (b Book) Assistance(request, proposal, conditions string, reuse bool) (AssistanceContext, error) {
	out := AssistanceContext{Checks: []AssistanceCheck{}}
	if !ValidAssistanceText(request, true) || !ValidAssistanceText(proposal, !reuse) || !ValidAssistanceText(conditions, reuse) {
		return out, errors.New("provide request and proposal (consult), or request and current conditions (reuse); each field must be UTF-8 and at most 16000 bytes")
	}
	pages, err := b.Live()
	if err != nil {
		return out, err
	}
	contexts, err := b.discoveryContexts()
	if err != nil {
		return out, err
	}
	byID, seen := map[string]Page{}, map[string]bool{}
	for i := range pages {
		p := &pages[i]
		p.discovered = contexts[p.ID+":"+p.Revision]
		byID[p.ID] = *p
	}
	seeds := []Page{}
	// Preserve request candidates first, then add candidates specific to the plan.
	for _, query := range []string{request, proposal, conditions} {
		if strings.TrimSpace(query) == "" {
			continue
		}
		ranked, _ := rankPages(pages, query, false)
		count := 0
		for _, p := range ranked {
			if p.Kind == "history" || reuse && p.Kind != "howto" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			seeds = append(seeds, p)
			count++
			if count == 2 || len(seeds) == 3 {
				break
			}
		}
		if len(seeds) == 3 {
			break
		}
	}
	bodyBudget := 16000
	add := func(p Page, relation, from string) {
		check := AssistanceCheck{Page: p, Relation: relation, From: from, Learning: b.LearningContext(p, 2000)}
		check.Orientation = Orient(p, request+" "+proposal+" "+conditions, min(bodyBudget, 2400))
		if len(p.Body) > min(bodyBudget, 2400) {
			check.Page.Body, check.BodyOmitted = "", true
			check.BodyExcerpts = check.Orientation.Excerpts()
			for _, excerpt := range check.BodyExcerpts {
				bodyBudget -= len(excerpt.Text)
			}
			out.Omitted = append(out.Omitted, "lesson:"+p.ID+"@"+p.Revision+": partial body; exact excerpts supplied, read the revision if needed to resolve meaning")
		} else {
			bodyBudget -= len(p.Body)
		}
		out.Checks = append(out.Checks, check)
	}
	for _, p := range seeds {
		add(p, "topic", "")
	}
	if !reuse {
		counterexamples, err := b.counterexamples(pages, request, proposal)
		if err != nil {
			return out, err
		}
		for _, c := range counterexamples {
			// Preserve the separate role even when the page was a topic seed.
			found := false
			for i := range out.Checks {
				if out.Checks[i].Page.ID == c.Page.ID {
					out.Checks[i].Relation = c.Relation
					out.Checks[i].Outcomes = c.Outcomes
					found = true
					break
				}
			}
			if !found {
				add(c.Page, c.Relation, "")
				out.Checks[len(out.Checks)-1].Outcomes = c.Outcomes
				seen[c.Page.ID] = true
			}
		}
	}
	// A bounded two-hop walk uses the project's own relationships, not a global
	// inference that a page is an exception or contradicts the current proposal.
	for i := 0; i < len(out.Checks) && len(out.Checks) < 6; i++ {
		from := out.Checks[i]
		if from.Relation == "related" && from.From != "" {
			parentIsSeed := false
			for _, seed := range seeds {
				parentIsSeed = parentIsSeed || seed.ID == from.From
			}
			if !parentIsSeed {
				continue
			}
		}
		for _, id := range from.Page.Related {
			p, ok := byID[id]
			if !ok || seen[id] || p.Kind == "history" {
				continue
			}
			seen[id] = true
			add(p, "related", from.Page.ID)
			if len(out.Checks) == 6 {
				break
			}
		}
	}
	return out, nil
}

type ProcedureEvidence struct {
	SourceID        string        `json:"source_id"`
	Kind            string        `json:"kind"`
	Content         string        `json:"content"`
	ContentExcerpts []TextExcerpt `json:"content_excerpts,omitempty"`
	Reference       string        `json:"reference,omitempty"`
	Version         string        `json:"version,omitempty"`
	ObservedAt      string        `json:"observed_at,omitempty"`
	Cut             bool          `json:"content_cut,omitempty"`
	Record          string        `json:"record,omitempty"`
	RecordState     string        `json:"record_state,omitempty"`
	RecordCut       bool          `json:"record_cut,omitempty"`
}

func evidenceExcerpt(text string, max int) (string, bool) {
	if len(text) <= max {
		return text, false
	}
	for !utf8.ValidString(text[:max]) {
		max--
	}
	return text[:max], true
}

// ProcedureEvidence reads only cited immutable snapshots and explicitly cited
// local work records. It never scans arbitrary files or follows references as URLs.
func (b Book) ProcedureEvidence(page Page, context string) ([]ProcedureEvidence, []string, error) {
	dir, err := b.directory(false, "sources")
	if err != nil {
		return nil, nil, err
	}
	out, unknown := []ProcedureEvidence{}, []string{}
	hasValidationSnapshot := false
	type candidate struct {
		source      Source
		data        []byte
		observation Observation
		validation  bool
	}
	candidates := []candidate{}
	readBytes := 0
	for i, source := range page.Sources {
		if i == 32 {
			unknown = append(unknown, "Source selection reached its 32-source inspection limit.")
			break
		}
		data, err := readSourceRecord(filepath.Join(dir, source.ID+".json"))
		if err != nil || digest(data) != source.Hash || !validSource(data, source.ID) {
			return nil, nil, errors.New("a procedure source changed during retrieval; read the article again")
		}
		readBytes += len(data)
		if readBytes > 2<<20 {
			unknown = append(unknown, "Source selection reached its 2 MiB inspection budget.")
			break
		}
		var o Observation
		_ = json.Unmarshal(data, &o)
		candidates = append(candidates, candidate{source, data, o, o.Kind == "test" || o.Kind == "release"})
	}
	// Prefer current validation snapshots to arbitrary source-id order. They
	// remain reports to inspect, never automatic labels that a procedure passed.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.validation != b.validation {
			return a.validation
		}
		return a.observation.ObservedAt.After(b.observation.ObservedAt)
	})
	if len(candidates) > 3 {
		unknown = append(unknown, "Additional cited sources are not included; read them if the provided evidence is insufficient or conflicting.")
	}
	for _, candidate := range candidates[:min(3, len(candidates))] {
		source, data, o := candidate.source, candidate.data, candidate.observation
		e := ProcedureEvidence{SourceID: source.ID}
		if o.Kind != "" {
			e.Kind, e.Reference, e.Version = o.Kind, o.Reference, o.Version
			e.ObservedAt = o.ObservedAt.Format(time.RFC3339Nano)
			if len(o.Content) > 1200 {
				e.Cut = true
				e.ContentExcerpts = FocusExcerpts(o.Content, context, 1200)
			} else {
				e.Content = o.Content
			}
			hasValidationSnapshot = hasValidationSnapshot || o.Kind == "test" || o.Kind == "release"
		} else {
			var f store.Feedback
			_ = json.Unmarshal(data, &f)
			e.Kind = "conversation"
			text := f.Request + "\n" + f.Summary + "\n" + f.Reply
			if len(text) > 1200 {
				e.Cut = true
				e.ContentExcerpts = FocusExcerpts(text, context, 1200)
			} else {
				e.Content = text
			}
		}
		if strings.HasPrefix(e.Reference, ".shikii/handoff/") {
			// Read uses the existing link, path, type and 4 MiB limit checks. The
			// changing file is context, never the historical validation snapshot.
			f, err := (handoff.Book{Store: b.Root}).Read(e.Reference)
			e.RecordState = "unavailable"
			if err == nil && f.Kind == "text" && f.Content != "" {
				e.Record, e.RecordCut = evidenceExcerpt(f.Content, 600)
				e.RecordState = "current_unverified"
			}
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		unknown = append(unknown, "No procedure evidence is available.")
	}
	if !hasValidationSnapshot {
		unknown = append(unknown, "No test or release snapshot is included; reported instructions do not establish success. Read the original sources and verify the outcome.")
	}
	return out, unknown, nil
}
