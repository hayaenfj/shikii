package notebook

import (
	"os"
	"path/filepath"

	"github.com/hayaenfj/shikii/internal/store"
)

func (b Book) validationScratch() []byte {
	if b.snapshot.validation == nil {
		b.snapshot.validation = make([]byte, 32<<10)
	}
	return b.snapshot.validation
}

// RecentFeedback shares only this preparation's bounded history. Its complete
// tail is rehashed inside the same commit validation as articles and sources.
func (b Book) RecentFeedback() ([]store.Feedback, error) {
	st := store.Store{Dir: b.Root}
	if b.snapshot == nil {
		return st.RecentFeedback(b.Scope)
	}
	if b.snapshot.feedback != nil {
		return b.snapshot.feedback.Items(), nil
	}
	view, err := st.ReadFeedback(b.Scope)
	if err != nil {
		return nil, err
	}
	b.snapshot.feedback = &view
	b.snapshot.checks["feedback"] = readCheck{view.Hash(), nil, func() (string, error) { return view.CurrentHash(b.validationScratch()) }}
	return view.Items(), nil
}

// PendingSourcesFromFeedback keeps the original history order and review-file
// checks while allowing a caller to pass its already checked history.
func (b Book) PendingSourcesFromFeedback(recent []store.Feedback) ([]store.Feedback, error) {
	dir, err := b.directory(false, "sources")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	pending := []store.Feedback{}
	for i := len(recent) - 1; i >= 0; i-- {
		f := recent[i]
		if !idPattern.MatchString(f.ID) {
			continue
		}
		if dir != "" {
			if _, err := os.Lstat(filepath.Join(dir, f.ID+".reviewed")); err == nil {
				continue
			}
		}
		pending = append(pending, f)
	}
	return pending, nil
}
