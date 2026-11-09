package notebook

import (
	"strings"
)

// These candidates can change only loop advice, never evidence delivery, guards,
// commands, evaluation criteria, or user permissions. All checks are host-side.
type ProcedureBasis struct {
	Topic    string             `json:"topic"`
	Stage    string             `json:"stage,omitempty"`
	Articles []ArticleReference `json:"articles"`
}
type ProcedureConfig struct {
	ID          string `json:"id"`
	Parent      string `json:"parent,omitempty"`
	RepeatAfter int    `json:"repeat_after"`
	Compact     int    `json:"compact"`
}
type ProcedureStatus struct {
	Ineligible    bool            `json:"ineligible,omitempty"`
	QualityNotice string          `json:"quality_notice,omitempty"`
	Baseline      ProcedureConfig `json:"-"`
	Source        string          `json:"source,omitempty"`
	Status        string          `json:"status"`
	Policy        string          `json:"policy"`
	Cases         int             `json:"cases"`
	Required      int             `json:"required"`
	SavedBytes    int             `json:"saved_bytes,omitempty"`
}
type ProcedureTrace struct {
	Baseline      ProcedureConfig `json:"baseline"`
	Basis         ProcedureBasis  `json:"basis"`
	Work          string          `json:"work"`
	Case          string          `json:"case"`
	Config        ProcedureConfig `json:"config"`
	Status        string          `json:"status"`
	BaselineBytes int             `json:"baseline_bytes"`
	ActualBytes   int             `json:"actual_bytes"`
	Stalls        int             `json:"stalls"`
	Mixed         bool            `json:"mixed,omitempty"`
	Safe          bool            `json:"safe"`
}
type ProcedureSample struct {
	Ineligible bool           `json:"ineligible,omitempty"`
	Trace      ProcedureTrace `json:"trace"`
	Sufficient *bool          `json:"sufficient,omitempty"`
	Evidence   []Source       `json:"evidence,omitempty"`
	Snapshot   Source         `json:"snapshot"`
}
type procedureSupport struct {
	Case       string   `json:"case"`
	Identities []string `json:"identities"`
	Source     Source   `json:"source"`
	Evidence   []Source `json:"evidence"`
}
type procedureProposal struct {
	Failure          *Source            `json:"failure,omitempty"`
	Config           ProcedureConfig    `json:"config"`
	Origin           string             `json:"origin"`
	Source           Source             `json:"source"`
	Required         int                `json:"required"`
	Exposures        map[string]string  `json:"exposures"`
	Support          []procedureSupport `json:"support,omitempty"`
	SavedBytes       int                `json:"saved_bytes"`
	Status           string             `json:"status"`
	OriginIdentities []string           `json:"origin_identities,omitempty"`
}
type procedureLane struct {
	Active    ProcedureConfig     `json:"active"`
	Candidate *procedureProposal  `json:"candidate,omitempty"`
	History   []procedureProposal `json:"history,omitempty"`
	Seen      map[string]string   `json:"seen"`
	Failures  int                 `json:"failures"`
	Limited   bool                `json:"limited,omitempty"`
}
type procedureJournal struct {
	Version int                       `json:"version"`
	Lanes   map[string]*procedureLane `json:"lanes"`
	Limited bool                      `json:"limited,omitempty"`
}

func validProcedureConfig(c ProcedureConfig) bool {
	if c.ID == "loop-v1" {
		return c == DefaultProcedure()
	}
	return len(c.ID) == 21 && strings.HasPrefix(c.ID, "loop-") && idPattern.MatchString(strings.TrimPrefix(c.ID, "loop-")) && c.Parent != "" && c.RepeatAfter >= 1 && c.RepeatAfter <= 2 && c.Compact >= 1 && c.Compact <= 2
}

func DefaultProcedure() ProcedureConfig { return ProcedureConfig{ID: "loop-v1", RepeatAfter: 2} }
