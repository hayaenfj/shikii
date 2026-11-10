package notebook

import (
	"fmt"
	"regexp"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

// validKindScene reports whether kind is one of Kinds and scene is UTF-8, one line, has no control characters and is at most SceneMax characters.
// kindSceneError is errKindScene, or a reply naming the encoding or the length when only the scene is wrong that way.
func kindSceneError(kind, scene string) error {
	if slices.Contains(Kinds, kind) && !utf8.ValidString(scene) {
		return fmt.Errorf("the scene must be UTF-8 text (it holds bytes that are not; a command-line argument cut in the middle of a character does this)")
	}
	if n := utf8.RuneCountInString(scene); slices.Contains(Kinds, kind) && n > SceneMax {
		return fmt.Errorf("the scene is too long (%d characters; keep it to %d, a short \"when ...\" phrase)", n, SceneMax)
	}
	return errKindScene
}

func validKindScene(kind, scene string) bool {
	// A scene that is not UTF-8 (raw bytes from the command line) would be written into the page as a binary value.
	if !slices.Contains(Kinds, kind) || scene == "" || !utf8.ValidString(scene) || utf8.RuneCountInString(scene) > SceneMax {
		return false
	}
	for _, r := range scene {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

type Draft struct {
	CreateOnly bool `json:"create_only,omitempty"` // a prepared new note must not overwrite a concurrent article

	ID           string              `json:"id,omitempty"`
	Title        string              `json:"title,omitempty"`
	Lesson       string              `json:"lesson,omitempty"`
	AppliesTo    string              `json:"applies_to,omitempty"`
	Sources      []string            `json:"sources"`
	Replaces     []string            `json:"replaces,omitempty"`
	Correction   *store.Correction   `json:"correction,omitempty"`
	Body         *string             `json:"body,omitempty"`
	Rationale    *string             `json:"rationale,omitempty"`
	Watch        *[]string           `json:"watch,omitempty"`
	Passages     *[]KnowledgePassage `json:"passages,omitempty"`
	Learning     *Learning           `json:"learning,omitempty"`
	Related      []string            `json:"related,omitempty"`
	ChangeReason string              `json:"change_reason,omitempty"`
	Kind         string              `json:"kind,omitempty"`  // kind (one of Kinds). Required for a draft with a lesson; inherited if omitted when updating
	Scene        string              `json:"scene,omitempty"` // scene (a "when ..." phrase). Same as above
	// baseRevision is the page revision classify started from. Inside the lock, refuse if it differs from the current revision (do not undo a concurrent edit with an old lesson; audit 0.9.0 B1)
	change         *Change
	baseRevision   string
	preview        bool
	preparedCreate *Page
}

type Issue struct {
	Kind   string `json:"kind"`
	Page   string `json:"page,omitempty"`
	Source string `json:"source,omitempty"`
	Store  string `json:"store,omitempty"` // the store for other-path-store (relative path from Root)
}

type View struct {
	// Pages without kind and scene (made before the upgrade; 0.9.0). Only the first reply carries their count, up to unclassifiedBatch of them and how to fill them in.
	// Placed at the start of the reply (at the end, agents without a hook skimmed past it)
	Remaining
	Lessons      []Page            `json:"lessons"`
	Pending      []store.Feedback  `json:"pending_sources"`
	Issues       []Issue           `json:"issues"`
	Matches      []Match           `json:"matches,omitempty"`
	Observations []ObservationHead `json:"observations,omitempty"`
	Maintenance  []MaintenanceTask `json:"maintenance,omitempty"`
	Search       *SearchResult     `json:"search,omitempty"`
	NextCursor   string            `json:"next_cursor,omitempty"`
	Lookup       *Lookup           `json:"lookup,omitempty"`
}

type Result struct {
	committed bool
	Warnings  []string `json:"warnings,omitempty"`
	Remaining
	Page     *Page    `json:"page"`
	Reviewed []string `json:"reviewed"`
}

// Remaining is how to fill in the remaining pages without kind and scene, their count and the next unclassifiedBatch of them (0.9.0). It goes into
// query and save replies, and shows nothing when there are none.
type Remaining struct {
	UnclassifiedHow   string             `json:"unclassified_how,omitempty"`
	UnclassifiedCount int                `json:"unclassified_count,omitempty"`
	Unclassified      []UnclassifiedPage `json:"unclassified,omitempty"`
}

// RemainingUnclassified is what remains now. When it cannot be read, nothing is added (it does not block the body of the reply).
func (b Book) RemainingUnclassified() Remaining {
	un, err := b.Unclassified()
	if err != nil || len(un) == 0 {
		return Remaining{}
	}
	r := Remaining{UnclassifiedHow: unclassifiedHow, UnclassifiedCount: len(un)}
	for i, p := range un {
		if i == unclassifiedBatch {
			break
		}
		r.Unclassified = append(r.Unclassified, UnclassifiedPage{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo})
	}
	return r
}

type Report struct {
	Issues   []Issue  `json:"issues"`
	Repaired []string `json:"repaired"`
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
