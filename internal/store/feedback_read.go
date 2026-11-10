package store

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// FeedbackRead binds a bounded history to the bytes checked in one request.
// It carries no long-lived cache and never substitutes metadata for contents.
type FeedbackRead struct {
	store Store
	scope string
	hash  string
	items []Feedback
}

func (s Store) ReadFeedback(scope string) (FeedbackRead, error) {
	h := sha256.New()
	items, err := s.readRecentFeedback(scope, h)
	return FeedbackRead{store: s, scope: scope, hash: hex.EncodeToString(h.Sum(nil)), items: items}, err
}
func (r FeedbackRead) Hash() string { return r.hash }
func (r FeedbackRead) Items() []Feedback {
	if r.items == nil {
		return nil
	}
	out := append([]Feedback{}, r.items...)
	for i := range out {
		if out[i].Targets != nil {
			targets := make([]ReviewTarget, len(out[i].Targets))
			copy(targets, out[i].Targets)
			out[i].Targets = targets
		}
	}
	return out
}
func (r FeedbackRead) CurrentHash(scratch []byte) (string, error) {
	h := sha256.New()
	f, start, err := r.store.openRecentHistory(r.store.feedbackPath(r.scope))
	if os.IsNotExist(err) {
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()

	h.Write([]byte("present\n"))
	if start > 0 {
		h.Write([]byte("skip-first\n"))
	}
	_, err = io.CopyBuffer(h, io.LimitReader(f, feedbackReadMax), scratch)
	return hex.EncodeToString(h.Sum(nil)), err
}
