package store

// The AI's provisional scores are saved in records/. They are never mixed into the truths and calibration in labels/.
import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

const ReviewRubric = "conversation-v1"

type Review struct {
	TraceID    string    `json:"trace_id"`
	Spec       string    `json:"spec"`
	Scope      string    `json:"project"`
	InputsHash string    `json:"inputs_hash"`
	Truth      string    `json:"truth"`
	Reason     string    `json:"reason"`
	Source     Feedback  `json:"source_material"`
	Rubric     string    `json:"rubric"`
	Sampled    bool      `json:"sampled"`
	Every      int       `json:"audit_every"`
	At         time.Time `json:"at"`
}

type ReviewStats struct {
	Scored           int `json:"scored"`
	Uncertain        int `json:"uncertain"`
	UncertainChecked int `json:"uncertain_checked"`
	Checked          int `json:"checked"`
	Correct          int `json:"correct"`
	RandomChecked    int `json:"random_checked"`
	RandomCorrect    int `json:"random_correct"`
	Every            int `json:"audit_every"`
}

func ReviewSpec(id string) bool { return id == "changes-in-scope" || id == "done-as-asked" }

func ReviewSample(trace string, every int) bool {
	if trace == "" || every < 1 {
		return false
	}
	h := sha256.Sum256([]byte("review:" + trace))
	return (int(h[0])*256+int(h[1]))%every == 0
}

func (s Store) Reviews(specID string) (map[string]Review, error) {
	out := map[string]Review{}
	if !ReviewSpec(specID) {
		return out, nil
	}
	err := s.scanJSONL(filepath.Join(s.Dir, "records", "reviews", specID+".jsonl"), func(line []byte) error {
		var r Review
		if json.Unmarshal(line, &r) == nil && r.Spec == specID && r.TraceID != "" && r.Rubric == ReviewRubric {
			out[r.TraceID] = r
		}
		return nil
	})
	if os.IsNotExist(err) {
		err = nil
	}
	return out, err
}

// ReviewQuality looks only at scores a person confirmed. It keeps the random slot separate,
// and does not replace the random results with held judgments or deliberately chosen hard cases.
func ReviewQuality(reviews map[string]Review, labels map[string]Label) ReviewStats {
	stats := ReviewStats{Scored: len(reviews), Every: 5}
	ordered := []Review{}
	for _, r := range reviews {
		ordered = append(ordered, r)
		if r.Truth == "unknown" {
			stats.Uncertain++
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return labels[ordered[i].TraceID].At.After(labels[ordered[j].TraceID].At) })
	seen := map[string]bool{}
	recent, errors := 0, 0
	var lastError, windowStart time.Time
	for _, r := range ordered {
		l, ok := labels[r.TraceID]
		if !ok || !HumanSource(l.Source) || l.At.Before(r.At) || Partial(l.Truth) || seen[r.InputsHash] {
			continue
		}
		seen[r.InputsHash] = true
		if r.Truth == "unknown" {
			stats.UncertainChecked++
			continue
		}
		stats.Checked++
		correct := Correct(r.Truth, l.Truth)
		if correct {
			stats.Correct++
		} else if l.At.After(lastError) {
			lastError = l.At
		}
		if !r.Sampled {
			continue
		}
		stats.RandomChecked++
		if correct {
			stats.RandomCorrect++
		}
		if recent < 20 {
			recent++
			windowStart = l.At
			if !correct {
				errors++
			}
		}
	}
	// 1 in 5 at first and for kinds that had a mismatch. 1 in 20 for kinds whose latest random checks
	// matched 20 times in a row. The line that allows acting is measured separately.
	if recent == 20 && errors == 0 && (lastError.IsZero() || lastError.Before(windowStart)) {
		stats.Every = 20
	}
	return stats
}

// reviewLockBusy, when set (tests), is called each time SaveReview finds the history's lock held and is about to wait: the moment another
// save is in progress.
var reviewLockBusy func()

func (s Store) SaveReview(r Review) (Review, error) {
	if !ReviewSpec(r.Spec) || r.TraceID == "" || r.Scope == "" || r.Reason == "" || (r.Truth != "yes" && r.Truth != "no" && r.Truth != "unknown") {
		return r, errors.New("a provisional score needs a target, grounds and yes / no / unknown")
	}
	dir := filepath.Join(s.Dir, "records", "reviews")
	f, err := s.openFile(filepath.Join(dir, r.Spec+".lock"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return r, err
	}
	defer f.Close()
	until := time.Now().Add(2 * time.Second)
	for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if time.Now().After(until) {
			return r, errors.New("saving provisional scores is busy")
		}
		if reviewLockBusy != nil {
			reviewLockBusy()
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	all, err := s.Reviews(r.Spec)
	if err != nil {
		return r, err
	}
	if old, ok := all[r.TraceID]; ok { // the path is not compared: the same score resent after the folder was moved is the same (0.9.7)
		if old.Source.ID == r.Source.ID && old.Truth == r.Truth && old.Reason == r.Reason {
			return old, nil
		}
		return r, errors.New("this decision already has a provisional score. Correct it with a person's confirmation")
	}
	labels, err := s.LabeledTraces(r.Spec)
	if err != nil {
		return r, err
	}
	if l, ok := labels[r.TraceID]; ok && HumanSource(l.Source) {
		return r, fmt.Errorf("this decision already has a truth (source %s) and is not replaced by an AI score; leave it out of reviews", l.Source)
	}
	r.Rubric, r.Every, r.At = ReviewRubric, ReviewQuality(all, labels).Every, time.Now().UTC()
	r.Sampled = ReviewSample(r.TraceID, r.Every)
	err = s.appendJSONL(filepath.Join(dir, r.Spec+".jsonl"), r)
	return r, err
}
