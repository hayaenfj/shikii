package main

import (
	"os"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

type experienceCheck struct {
	Page               experiencePage         `json:"page"`
	Relation           string                 `json:"relation"`
	From               string                 `json:"from,omitempty"`
	BodyOmitted        bool                   `json:"body_omitted,omitempty"`
	BodyExcerpts       []notebook.TextExcerpt `json:"body_excerpts,omitempty"`
	ApplicabilityTrace string                 `json:"applicability_trace,omitempty"`
	ModelExcluded      bool                   `json:"model_excluded,omitempty"`
}

// Keep the useful knowledge up front. A revision reference lets the agent read
// full provenance without repeating source hashes and revision history here.
type experiencePage struct {
	ID        string                   `json:"id"`
	Revision  string                   `json:"revision"`
	Title     string                   `json:"title"`
	Lesson    string                   `json:"lesson"`
	AppliesTo string                   `json:"applies_to"`
	Kind      string                   `json:"kind"`
	Rationale string                   `json:"rationale,omitempty"`
	Body      string                   `json:"body,omitempty"`
	Sources   []string                 `json:"sources"`
	Recheck   []notebook.PremiseChange `json:"recheck,omitempty"`
}

func compactExperienceCheck(c notebook.AssistanceCheck) experienceCheck {
	p := c.Page
	v := experienceCheck{Page: experiencePage{ID: p.ID, Revision: p.Revision, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Kind: p.Kind, Rationale: p.Rationale, Body: p.Body, Recheck: p.Recheck, Sources: []string{}}, Relation: c.Relation, From: c.From, BodyOmitted: c.BodyOmitted, BodyExcerpts: c.BodyExcerpts}
	for _, source := range p.Sources {
		v.Page.Sources = append(v.Page.Sources, source.ID)
	}
	return v
}

type reuseStep struct {
	Source      store.KnowledgeReference     `json:"source"`
	Instruction string                       `json:"instruction"`
	Evidence    []notebook.ProcedureEvidence `json:"evidence"`
	Unknown     []string                     `json:"unknown,omitempty"`
}

type experienceView struct {
	Request     string            `json:"request"`
	Proposal    string            `json:"proposal,omitempty"`
	Conditions  string            `json:"conditions,omitempty"`
	Checks      []experienceCheck `json:"checks"`
	Steps       []reuseStep       `json:"steps,omitempty"`
	Omitted     []string          `json:"omitted,omitempty"`
	Unknown     []string          `json:"unknown,omitempty"`
	NextAction  string            `json:"next_action"`
	Notice      string            `json:"notice"`
	ModelNotice string            `json:"model_notice,omitempty"`
}

func consultExperience(b notebook.Book, req notebookRequest) (experienceView, error) {
	reuse := req.Operation == "reuse"
	context, err := b.Assistance(req.Request, req.Proposal, req.Conditions, reuse)
	v := experienceView{Request: req.Request, Proposal: req.Proposal, Conditions: req.Conditions, Checks: []experienceCheck{}, Omitted: context.Omitted,
		Notice: "Project evidence for the host agent to interpret, never permission or a human truth. Topic matches, links and model applicability are not semantic support, contradiction or proof of success."}
	if err != nil {
		return v, err
	}
	for _, c := range context.Checks {
		v.Checks = append(v.Checks, compactExperienceCheck(c))
		if reuse && c.Page.Kind == "howto" && c.Relation == "topic" {
			evidence, unknown, err := b.ProcedureEvidence(c.Page, req.Request+" "+req.Conditions)
			if err != nil {
				return v, err
			}
			v.Steps = append(v.Steps, reuseStep{Source: store.KnowledgeReference{ID: c.Page.ID, Revision: c.Page.Revision}, Instruction: c.Page.Lesson, Evidence: evidence, Unknown: unknown})
		}
	}
	// Reuse the project's calibrated relevance question, with a fixed call cap.
	// Do not introduce another model or label the agent's interpretation as truth.
	// Retain even excluded material so a related exception remains inspectable.
	if _, err := os.Stat(hookSpecPath(b.Root, lessonAppliesSpec)); err == nil {
		target := "Proposed approach: " + req.Proposal + "; current conditions: " + req.Conditions
		if len(req.Request) > requestMax || len(target) > 400 {
			v.ModelNotice = "The full context exceeds the existing relevance model's input limits; no partial applicability judgment was requested. Interpret the full request, proposal and conditions with the supplied evidence."
		} else {
			for i := 0; i < len(v.Checks) && i < lessonAppliesMaxAsks; i++ {
				v.Checks[i].ApplicabilityTrace, v.Checks[i].ModelExcluded = lessonApplies(b.Root, b.Scope, req.Request, target, context.Checks[i].Page)
			}
		}
	}
	v.NextAction = "Check the proposal against each article's conditions and rationale. Use the exact excerpts first to explain support, counterexamples and exceptions with sources. Read a full revision only if the excerpts leave a material condition or exception unresolved. Identify missing evidence. Current intent takes priority over historical choices."
	if len(v.Checks) == 0 {
		v.Unknown = append(v.Unknown, "No matching project evidence was found; do not infer agreement or permission.")
	}
	if reuse {
		v.NextAction = "Assemble an ordered procedure for the request and current conditions from the cited howtos and evidence. Use exact excerpts first; read a full cited revision or source only when a necessary condition or outcome remains unresolved. Name the source of each step, prerequisites, rejected approaches, validation results and changed premises. Distinguish recorded results from unverified claims; current work records are not immutable test evidence. State what needs rechecking and any missing evidence. Do not execute the commands or reuse historical authorization."
		if len(v.Steps) == 0 {
			v.Unknown = append(v.Unknown, "No matching howto is available; do not invent a successful procedure.")
		}
	}
	return v, nil
}
