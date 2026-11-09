// Package store keeps decision records and labels. One JSONL file per spec.
//
//	<dir>/records/<spec_id>.jsonl  … every decision (with raw probabilities)
//	<dir>/labels/<spec_id>.jsonl   … truth labels (picked up from the user's actions)
//
// The text (inputs) is always kept in the local record (so a person can add the truth later, and it can be re-asked when the wording or model changes). Whether it is sent out is decided by the spec's send_to and pii.
package store

import (
	"time"
)

// Record is one decision: decide.Decision plus provenance.
type Record struct {
	TraceID          string             `json:"trace_id"`
	SpecID           string             `json:"spec_id"`
	SpecHash         string             `json:"spec_hash"`
	Type             string             `json:"type"`
	Model            string             `json:"model,omitempty"`
	Endpoint         string             `json:"endpoint,omitempty"`
	Inputs           map[string]string  `json:"inputs,omitempty"` // the text (old records may not have it)
	InputsHash       string             `json:"inputs_hash"`
	Raw              map[string]float64 `json:"raw,omitempty"`
	Calibrated       map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated     bool               `json:"is_calibrated"`
	Top              string             `json:"top,omitempty"`
	P                float64            `json:"p"`
	Band             string             `json:"band"`
	Tainted          bool               `json:"tainted,omitempty"`        // a decision from a session that read production data. This text is never sent to hosted, even when re-asked
	Rung             string             `json:"rung,omitempty"`           // which step decided it: minimal (the minimal question; empty in old records) | context (context was added and asked again)
	Minimal          *Pass              `json:"minimal,omitempty"`        // when context was added, the minimal question's answer (that step's hash and raw; a person's truth is attached here too)
	Display          *RecordDisplay     `json:"display,omitempty"`        // side-by-side text in the configured language (written by the hook; absent for English): to trace later what was asked and what was done
	Project          string             `json:"project,omitempty"`        // which work the decision belongs to (written by the hook: the name of the repository root of cwd, or the cwd name outside git). Neither an input nor a hash key; a mark for splitting views on the page and in status
	RedoOf           string             `json:"redo_of,omitempty"`        // a past decision's (trace_id) text decided again with the current wording and line. The original record leaves the queue
	ReviewPending    bool               `json:"review_pending,omitempty"` // the AI provisionally scores it from the rest of the conversation. Not put in the queue for a person to score every item
	At               time.Time          `json:"at"`
	KnowledgeContext *KnowledgeContext  `json:"knowledge_context,omitempty"`
	// AgentPaired is set on a needs-plan decision organized under 0.11's rule (open filled only when something blocks): only such a
	// decision is compared with the agent's answer. One organized before filled open with reasons too, and comparing it asked the person
	AgentPaired bool `json:"agent_paired,omitempty"`
}

// Pass is the answer of one step. A record that added context and asked again uses it to keep the minimal question's answer.
// RecordDisplay is the translation in the configured language attached to a decision record (from the spec's display.<lang>). Never sent to the model and not part of the calibration key.
type RecordDisplay struct {
	Lang     string `json:"lang"`
	Question string `json:"question,omitempty"` // the question in people's words
	Top      string `json:"top,omitempty"`      // the model's answer (option) in people's words
	Message  string `json:"message,omitempty"`  // the text the hook showed to the person or agent (alongside English). Absent if nothing was shown
}

type Pass struct {
	SpecHash     string             `json:"spec_hash"`
	InputsHash   string             `json:"inputs_hash,omitempty"` // hash of only the inputs this step's template uses (the minimal question sees the same input even if the context inputs differ)
	Raw          map[string]float64 `json:"raw,omitempty"`
	Calibrated   map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated bool               `json:"is_calibrated"`
	Top          string             `json:"top,omitempty"`
	P            float64            `json:"p"`
	Band         string             `json:"band"`
}
