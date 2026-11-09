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
	f, err := r.store.openFile(r.store.feedbackPath(r.scope), os.O_RDONLY)
	if os.IsNotExist(err) {
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	start := max(int64(0), info.Size()-feedbackReadMax)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	h.Write([]byte("present\n"))
	if start > 0 {
		h.Write([]byte("skip-first\n"))
	}
	_, err = io.CopyBuffer(h, io.LimitReader(f, feedbackReadMax), scratch)
	return hex.EncodeToString(h.Sum(nil)), err
}
