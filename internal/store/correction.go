package store

import (
	"strings"
	"unicode/utf8"
)

// Correction is a quote of what a person said and the agent's hypothesis of what it will do differently next.
// That the quote exists and that its interpretation is right are separate things. It is never a permission to act.
type Correction struct {
	SourceID string `json:"source_id" yaml:"source_id"`
	Quote    string `json:"quote" yaml:"quote"`
	Action   string `json:"action" yaml:"action"`
}

func (c Correction) Valid() bool {
	return textTrace.MatchString(c.SourceID) && strings.TrimSpace(c.Quote) != "" && strings.TrimSpace(c.Action) != "" &&
		len(c.Quote) <= 400 && len(c.Action) <= 400 && utf8.ValidString(c.Quote) && utf8.ValidString(c.Action)
}

type CorrectionStats struct {
	Delivered     int `json:"delivered"`
	Followed      int `json:"followed"`
	Repeated      int `json:"repeated"`
	NotApplicable int `json:"not_applicable"`
	Unknown       int `json:"unknown"`
	Pending       int `json:"pending"`
}

func validCorrectionResult(s string) bool {
	return s == "" || s == "followed" || s == "repeated" || s == "not_applicable" || s == "unknown"
}
