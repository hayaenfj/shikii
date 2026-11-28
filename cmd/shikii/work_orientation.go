package main

import (
	"encoding/json"
	"maps"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// workOrientation is the orientation of a prepared candidate as it is written out. notebook.Orient always repeats the article's own
// summary, conditions, id and revision, so a candidate carried each of those texts twice: once in its own fields (or in the article
// page it holds) and once in the orientation (8.9% of a prepare reply, measured on 2026-11-28).
//
// The orientation leaves one of those four texts out only when, at the moment it is encoded, the candidate it belongs to holds the
// very same non-empty string: in its own field (summary, applies_to, id, revision) or in the article page it carries (lesson,
// applies_to, id, revision). The comparison is made on every encoding against the candidate as it is then. The reductions of
// limitWorkPreparation take fields off a candidate after its orientation was made, and a text that no longer stands in the
// candidate must stay in the orientation. A text that differs, an empty text, and a field of another type are written as before,
// and every other part of the orientation (current, history, recheck, counterexamples, unclassified and the three flags) is always
// written, empty lists included. The JSON of notebook.KnowledgeOrientation itself, as other entrances save and send it, is unchanged.
//
// A candidate and its orientation point at each other, so bind through bindOrientation, and bind again (rebindOrientation) when the
// candidate's fields move to another map.
type workOrientation struct {
	notebook.KnowledgeOrientation
	holder map[string]any
}

// orientationView writes a KnowledgeOrientation with the four texts that may stay out. The fields declared here hide the embedded
// fields of the same JSON name whatever their value, so a nil pointer leaves the name out and every other field of the embedded
// struct is written exactly as it is (a field added to KnowledgeOrientation is written too). The order of the members is the same.
type orientationView struct {
	ID         *string `json:"id,omitempty"`
	Revision   *string `json:"revision,omitempty"`
	Summary    *string `json:"summary,omitempty"`
	Conditions *string `json:"conditions,omitempty"`
	notebook.KnowledgeOrientation
}

// orientationPlaces are the places a candidate can hold each of the four texts, in the order they are looked at: its own field, then
// the field of its article page. Only these names are written to or read from a saved delivery record (workMetadataRef.Held).
var orientationPlaces = map[string][2]string{
	"id":         {"id", "article.id"},
	"revision":   {"revision", "article.revision"},
	"summary":    {"summary", "article.lesson"},
	"conditions": {"applies_to", "article.applies_to"},
}

// held says where the candidate holds each text of the orientation right now, by the names of orientationPlaces. A text the
// candidate does not hold (and so the orientation writes) is not in the result.
func (o *workOrientation) held() map[string]string {
	page, _ := o.holder["article"].(notebook.Page)
	texts := map[string][2]string{
		"id":         {o.ID, page.ID},
		"revision":   {o.Revision, page.Revision},
		"summary":    {o.Summary, page.Lesson},
		"conditions": {o.Conditions, page.AppliesTo},
	}
	at := map[string]string{}
	for key, text := range texts {
		if text[0] == "" {
			continue
		}
		if own, _ := o.holder[orientationPlaces[key][0]].(string); own == text[0] {
			at[key] = orientationPlaces[key][0]
		} else if text[1] == text[0] {
			at[key] = orientationPlaces[key][1]
		}
	}
	return at
}

func (o *workOrientation) MarshalJSON() ([]byte, error) {
	at := o.held()
	// keep returns the text to write, or nil when the candidate holds the same text under its own field or in its article page.
	keep := func(key, text string) *string {
		if at[key] != "" {
			return nil
		}
		return &text
	}
	return json.Marshal(orientationView{
		ID:                   keep("id", o.ID),
		Revision:             keep("revision", o.Revision),
		Summary:              keep("summary", o.Summary),
		Conditions:           keep("conditions", o.Conditions),
		KnowledgeOrientation: o.KnowledgeOrientation,
	})
}

// bindOrientation sets the orientation of a candidate (or a memory upgrade page) so that it is written against that candidate.
func bindOrientation(candidate map[string]any, o notebook.KnowledgeOrientation) {
	candidate["orientation"] = &workOrientation{KnowledgeOrientation: o, holder: candidate}
}

// rebindOrientation points the orientation of a candidate at the candidate itself, after its fields were copied from another map
// (a clone is measured as it will be written, and a clone that replaces its original is written against the original's map).
func rebindOrientation(candidate map[string]any) {
	if o, ok := candidate["orientation"].(*workOrientation); ok {
		bindOrientation(candidate, o.KnowledgeOrientation)
	}
}

// orientationOf returns the whole orientation held in a candidate, in either form: bound to its candidate, or as Orient made it.
func orientationOf(v any) (notebook.KnowledgeOrientation, bool) {
	switch o := v.(type) {
	case notebook.KnowledgeOrientation:
		return o, true
	case *workOrientation:
		return o.KnowledgeOrientation, true
	}
	return notebook.KnowledgeOrientation{}, false
}

// wholeOrientationEntry is the candidate with its orientation as Orient made it. A hash of the candidate takes this form, so its value
// does not depend on which texts happen to be written once (and does not change from before the orientation left them out).
func wholeOrientationEntry(e map[string]any) map[string]any {
	o, ok := e["orientation"].(*workOrientation)
	if !ok {
		return e
	}
	whole := maps.Clone(e)
	whole["orientation"] = o.KnowledgeOrientation
	return whole
}
