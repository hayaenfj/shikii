package notebook

import (
	"encoding/json"
	"strings"
)

// IsNamedRead says whether query reads one article by name (lesson:<id>, or lesson:<id>@<revision> for an earlier revision) rather
// than searching words or listing the articles: the rule searchPages and query apply.
func IsNamedRead(query string) bool { return strings.HasPrefix(query, "lesson:") }

// WithSourceIDsOnly returns v marked as the reply of a search or a list: its JSON names each source of each lesson by its id alone.
// The article's id with its revision fixes the hash and the text_hash of every source (the revision is made from them), and an
// article read by name carries them, so a list that repeated them (64 digits each) only made the reply longer. Only the JSON of
// the returned view changes: the lessons in memory, the saved articles and every other reply keep their sources as they are.
func (v View) WithSourceIDsOnly() View {
	v.sourceIDsOnly = true
	return v
}

// MarshalJSON writes a view as its fields say, except that a view marked by WithSourceIDsOnly names each source by its id alone.
func (v View) MarshalJSON() ([]byte, error) {
	type fields View // View without this method
	if !v.sourceIDsOnly {
		return json.Marshal(fields(v))
	}
	lessons := []listedPage(nil) // an absent list stays absent, an empty one stays empty
	if v.Lessons != nil {
		lessons = make([]listedPage, len(v.Lessons))
		for i, p := range v.Lessons {
			lessons[i] = listedPage{Page: p}
			if p.Sources != nil {
				lessons[i].Sources = make([]listedSource, len(p.Sources))
				for j, s := range p.Sources {
					lessons[i].Sources[j] = listedSource{ID: s.ID}
				}
			}
		}
	}
	// Lessons of the outer struct hides the one View has; the other fields, and the members of Remaining, are written as before.
	return json.Marshal(struct {
		fields
		Lessons []listedPage `json:"lessons"`
	}{fields: fields(v), Lessons: lessons})
}

// listedPage is a Page with the sources of a list: Sources hides the one Page has, and every other field is Page's own.
type listedPage struct {
	Page
	Sources []listedSource `json:"sources"`
}

type listedSource struct {
	ID string `json:"id"`
}
