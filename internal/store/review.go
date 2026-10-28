package store

// The AI's provisional scores are saved in records/. They are never mixed into the truths and calibration in labels/.
import (
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
	At         time.Time `json:"at"`
	// ModelShown is set when the agent gave this score after seeing the decision model's answer (0.11): only such a score is compared
	// with the model. One given before was not made against it, and comparing it put old disagreements in front of the person
	ModelShown bool `json:"model_shown,omitempty"`
}

type ReviewStats struct {
	Scored           int `json:"scored"`
	Uncertain        int `json:"uncertain"`
	UncertainChecked int `json:"uncertain_checked"`
	Checked          int `json:"checked"`
	Correct          int `json:"correct"`
}

func ReviewSpec(id string) bool { return id == "changes-in-scope" || id == "done-as-asked" }

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

// ReviewQuality looks only at scores a person confirmed. Since 0.11 a person sees a score only when it disagrees with the decision model or
// is undecided (no random sampling), so the share that matched is the agent's share among those disagreements.
func ReviewQuality(reviews map[string]Review, labels map[string]Label) ReviewStats {
	stats := ReviewStats{Scored: len(reviews)}
	ordered := []Review{}
	for _, r := range reviews {
		ordered = append(ordered, r)
		if r.Truth == "unknown" {
			stats.Uncertain++
		}
	}
	// the newest check first: of several scores of the same text, the one checked last counts
	sort.Slice(ordered, func(i, j int) bool { return labels[ordered[i].TraceID].At.After(labels[ordered[j].TraceID].At) })
	seen := map[string]bool{}
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
		if Correct(r.Truth, l.Truth) {
			stats.Correct++
		}
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
	r.Rubric, r.At = ReviewRubric, time.Now().UTC()
	err = s.appendJSONL(filepath.Join(dir, r.Spec+".jsonl"), r)
	return r, err
}
