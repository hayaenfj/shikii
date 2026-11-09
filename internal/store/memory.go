package store

// Memory deliveries are kept apart from their contribution to decisions. An evaluation is the AI's observation, not a truth or a calibration.
import (
	"time"
)

type MemoryQuote struct {
	CacheKey      string      `json:"-"`
	Correction    *Correction `json:"correction,omitempty"`
	Key           string      `json:"key"`
	ID            string      `json:"id"`
	Revision      string      `json:"revision,omitempty"`
	Lesson        string      `json:"lesson,omitempty"`
	AppliesTo     string      `json:"applies_to,omitempty"`
	Rationale     string      `json:"rationale,omitempty"`
	Recheck       bool        `json:"recheck,omitempty"`
	DiscoveredFor string      `json:"discovered_for,omitempty"`
	Sources       []string    `json:"sources,omitempty"`
	Request       string      `json:"request,omitempty"`
	Summary       string      `json:"summary,omitempty"`
	Reply         string      `json:"reply,omitempty"`
	Trace         string      `json:"trace,omitempty"` // the lesson-applies decision made for this delivery (0.12): the reflection's assessment becomes its outcome truth
	Said          string      `json:"said,omitempty"`  // a person's past answer (key decision:<trace>): what the person answered, in the page's words; Request is that request
}

type MemoryDelivery struct {
	ID      string        `json:"id"`
	Scope   string        `json:"project"`
	Request string        `json:"request"`
	Quotes  []MemoryQuote `json:"quotes"`
	At      time.Time     `json:"at"`
	Parent  string        `json:"parent,omitempty"` // delivery to a subagent: the parent turn (the conversation source id). The review is evaluated alongside the parent's conversation (0.7.4)
	Agent   string        `json:"agent,omitempty"`  // the subagent's id
	Tool    string        `json:"tool,omitempty"`   // delivered right before a tool call (0.11): the tool and the file or command words it matched; Parent is the turn
	Phrase  string        `json:"phrase,omitempty"` // the door the quotes came through (0.12): the command phrase or file name the lesson matched at a tool call, "request" when chosen by the request's words. Empty for the always rules and older records
}

// toolMax is the longest Tool a delivery keeps (a file path or a command's leading words).
const toolMax = 512

type MemoryAssessment struct {
	CorrectionResult string    `json:"correction_result,omitempty"`
	SourceID         string    `json:"source_id"`
	Key              string    `json:"key"`
	Phrase           string    `json:"phrase,omitempty"` // the door the delivery came through, copied from it when saved (0.12), so the effects are counted from the assessments alone, not from the window of recent deliveries
	Result           string    `json:"result"`
	Decision         string    `json:"decision"`
	Evidence         string    `json:"evidence"`
	At               time.Time `json:"at"`
}

type MemoryStats struct {
	Delivered        int `json:"delivered"`
	Helpful          int `json:"helpful"`
	Unused           int `json:"unused"`
	Harmful          int `json:"harmful"`
	Unknown          int `json:"unknown"`
	Pending          int `json:"pending"`
	AwaitingFeedback int `json:"awaiting_feedback"`
	Answers          int `json:"answers"`         // of Delivered, the person's past answers (decision:<trace>, 0.11)
	AnswersHelpful   int `json:"answers_helpful"` // of Answers, those the reflection found helpful
	AtTool           int `json:"at_tool"`         // of Delivered, those delivered right before an edit or a command (0.11)
	AtToolHelpful    int `json:"at_tool_helpful"` // of AtTool, those the reflection found helpful
}

type MemorySummary struct {
	Corrections CorrectionStats `json:"corrections"`
	Window      int             `json:"window"` // upper limit on recent delivery turns. Do not pick only successful examples.
	Stats       MemoryStats     `json:"stats"`
}

type MemoryObservation struct {
	SourceID   string            `json:"source_id"`
	Parent     string            `json:"parent,omitempty"`
	Agent      string            `json:"agent,omitempty"`
	Tool       string            `json:"tool,omitempty"`
	Phrase     string            `json:"phrase,omitempty"`
	Request    string            `json:"request"`
	At         time.Time         `json:"at"`
	Quote      MemoryQuote       `json:"quote"`
	Assessment *MemoryAssessment `json:"assessment,omitempty"`
}
