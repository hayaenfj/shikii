package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

func (s Store) memoryAssessmentPath(scope, id, key string) string {
	h := sha256.Sum256([]byte(id + ":" + key))
	return filepath.Join(s.recordPath("memory-reviews", "", scope), hex.EncodeToString(h[:])+".json")
}

// errAssessmentBroken is an assessment file whose content cannot be read as one (the report counts it as not yet assessed); a file that
// cannot be opened, or is a link, is a different error that is never hidden.
var errAssessmentBroken = errors.New("cannot confirm the provisional memory evaluation")

func (s Store) memoryAssessment(scope, id, key string) (*MemoryAssessment, error) {
	f, err := s.openFile(s.memoryAssessmentPath(scope, id, key), os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (32<<10)+1))
	var a MemoryAssessment
	if err != nil || len(data) > 32<<10 || json.Unmarshal(data, &a) != nil || a.SourceID != id || a.Key != key || a.At.IsZero() || !validMemoryAssessment(a) {
		return nil, errAssessmentBroken
	}
	return &a, nil
}

func (s Store) SaveMemoryAssessment(scope string, a MemoryAssessment) error {
	if scope == "" || !validMemoryAssessment(a) {
		return errors.New("a provisional memory evaluation needs grounds for the target, the judgment and the result. Use unknown when unclear")
	}
	unlock, err := s.memoryLock(scope)
	if err != nil {
		return err
	}
	defer unlock()
	deliveries, err := s.MemoryDeliveries(scope)
	if err != nil {
		return err
	}
	delivered, correction := false, false
	conversation := a.SourceID // the conversation id. For a delivery to a subagent, the parent turn
	for _, d := range deliveries {
		for _, q := range d.Quotes {
			if d.ID == a.SourceID && q.Key == a.Key {
				delivered = true
				correction = q.Correction != nil
				a.Phrase = d.Phrase // the door, kept with the assessment (0.12)
				if d.Parent != "" {
					conversation = d.Parent
				}
			}
		}
	}
	if !delivered {
		return errors.New("name a memory actually delivered to this request")
	}
	if !correction && a.CorrectionResult != "" {
		return errors.New("correction_result is only for a delivered correction; leave it out for this lesson (behaviour observations can only be attached to corrections delivered to this request)")
	}
	if correction && a.CorrectionResult == "" {
		a.CorrectionResult = "unknown"
	}
	feedback, err := s.RecentFeedback(scope)
	if err != nil {
		return err
	}
	observed := false
	for _, f := range feedback {
		observed = observed || f.ID == conversation
	}
	if !observed {
		return errors.New("review only after checking the reply to the request and the person's next message")
	}
	old, err := s.memoryAssessment(scope, a.SourceID, a.Key)
	replace := errors.Is(err, errAssessmentBroken) // a file whose content cannot be read is replaced by this assessment (the query lists the delivery as not yet assessed; confirmation audit 2 of 0.12)
	if replace {
		old, err = nil, nil
	}
	if err != nil {
		return err
	}
	if old != nil {
		if old.Result == a.Result && old.Decision == a.Decision && old.Evidence == a.Evidence && old.CorrectionResult == a.CorrectionResult {
			return nil
		}
		return errors.New("the original provisional memory evaluation is not overwritten")
	}
	a.At = time.Now().UTC()
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// do not leave an unfinished evaluation under the final name. Publish it once, inside the lock.
	root, name, err := s.parentRoot(s.memoryAssessmentPath(scope, a.SourceID, a.Key), true)
	if err != nil {
		return err
	}
	defer root.Close()
	tmp := "." + rand.Text() + ".tmp"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if replace {
		_ = root.Remove(name) // the broken file gives way; the link below publishes the new one
	}
	return root.Link(tmp, name)
}
