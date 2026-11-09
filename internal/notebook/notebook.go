// Package notebook is a wiki grown from source records. The agent in use writes the text;
// shikii manages sources, the index and the change history. A lesson is never a truth label or permission to act.
package notebook

import (
	"errors"

	"github.com/hayaenfj/shikii/internal/store"
)

type Book struct {
	Root, Scope string
	name        string
	read        *readView
	snapshot    *readSnapshot
}

type Source struct {
	ID       string `json:"id" yaml:"id"`
	Hash     string `json:"hash" yaml:"hash"`
	TextHash string `json:"text_hash,omitempty" yaml:"text_hash,omitempty"`
}

type Page struct {
	ID           string             `json:"id" yaml:"id"`
	Title        string             `json:"title" yaml:"title"`
	Lesson       string             `json:"lesson" yaml:"lesson"`
	AppliesTo    string             `json:"applies_to" yaml:"applies_to"`
	Status       string             `json:"status" yaml:"status"`
	Sources      []Source           `json:"sources" yaml:"sources"`
	Replaces     []string           `json:"replaces,omitempty" yaml:"replaces,omitempty"`
	Revision     string             `json:"revision" yaml:"revision"`
	Previous     string             `json:"previous,omitempty" yaml:"previous,omitempty"`
	Correction   *store.Correction  `json:"correction,omitempty" yaml:"correction,omitempty"`
	Body         string             `json:"body,omitempty" yaml:"body,omitempty"`
	Rationale    string             `json:"rationale,omitempty" yaml:"rationale,omitempty"`
	Premises     []Premise          `json:"premises,omitempty" yaml:"premises,omitempty"`
	Recheck      []PremiseChange    `json:"recheck,omitempty" yaml:"-"`
	Passages     []KnowledgePassage `json:"passages,omitempty" yaml:"passages,omitempty"`
	Learning     *Learning          `json:"learning,omitempty" yaml:"learning,omitempty"`
	discovered   string
	Related      []string `json:"related,omitempty" yaml:"related,omitempty"`
	ChangeReason string   `json:"change_reason,omitempty" yaml:"change_reason,omitempty"`
	Kind         string   `json:"kind,omitempty" yaml:"kind,omitempty"`   // kind (one of Kinds; 0.9.0)
	Scene        string   `json:"scene,omitempty" yaml:"scene,omitempty"` // scene (a "when ..." phrase in the project's own words, up to sceneMax characters; 0.9.0)
}

// Kinds are the kinds of page. Fixed to shapes every kind of work has (do not add more): a rule decided in this project, a how-to, a criterion for judging, a premise or fact, and history.
var Kinds = []string{"rule", "howto", "criteria", "fact", "history"}

// SceneMax is the maximum number of characters in a scene (48 from 0.9.2: 24 was sized for Japanese, and natural English scenes
// such as "when checking invoice totals" were refused).
const SceneMax = 48

// errKindScene is the reply when kind or scene is missing or invalid (it says how to pick the values, so the agent can fix it as is).
var errKindScene = errors.New("a lesson needs kind (one of rule|howto|criteria|fact|history: rule = a rule decided in this project, howto = how to do something, criteria = criteria for judging, fact = premises and facts, history = how things came to be) and scene (the situation in the project's own words, as a \"when ...\" phrase such as \"when committing\", one line, up to 48 characters); when updating an existing page they are inherited from that page if omitted")
