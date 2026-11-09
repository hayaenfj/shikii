// Package spec reads and writes decision specs (Decision Spec).
// One decision = one YAML file. It keeps the model-independent part (question, state, thresholds)
// apart from the model-dependent part (calibration); calibration is keyed by (spec hash, model).
package spec

import (
	"regexp"
)

// Type is the question type. The three System One forms.
type Type string

const (
	Noul   Type = "noul"
	Choice Type = "choice"
	Score  Type = "score"
)

// Option is one choice in a choice question. A description is required (a bare label causes misrouting).
type Option struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

// State is how the text passed to the decision is built. Input missing a Required name is not decided.
type State struct {
	Template string   `yaml:"template"`
	Required []string `yaml:"required"`
	// Context is an optional layout that "adds context and asks again only when unsure". It uses every required input and adds one or more inputs not in template.
	// It is used only when the added inputs are present and the minimal question's answer is in the ask band. The context question has its own hash (ContextHash) and its own calibration.
	Context string `yaml:"context,omitempty"`
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)
