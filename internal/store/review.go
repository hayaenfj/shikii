package store

// AI の仮採点は records/ に保存する。labels/ の正解と較正には混ぜない。
import (
	"crypto/sha256"
	"encoding/json"
	"errors"
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

// ReviewQuality は人が確認した採点だけを見る。無作為の枠を分け、
// 判断を保留したものや意図して選んだ難問で無作為の成績を置き換えない。
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
	// 最初と不一致が出た種類は 5 件に 1 件。直近の無作為の確認が
	// 20 件続けて一致した種類は 20 件に 1 件。実行許可の線は別に測る。
	if recent == 20 && errors == 0 && (lastError.IsZero() || lastError.Before(windowStart)) {
		stats.Every = 20
	}
	return stats
}

func (s Store) SaveReview(r Review) (Review, error) {
	if !ReviewSpec(r.Spec) || r.TraceID == "" || r.Scope == "" || r.Reason == "" || (r.Truth != "yes" && r.Truth != "no" && r.Truth != "unknown") {
		return r, errors.New("仮採点には対象・根拠・yes / no / unknown が必要です")
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
			return r, errors.New("仮採点の保存が混み合っています")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	all, err := s.Reviews(r.Spec)
	if err != nil {
		return r, err
	}
	if old, ok := all[r.TraceID]; ok {
		if old.Scope == r.Scope && old.Source.ID == r.Source.ID && old.Truth == r.Truth && old.Reason == r.Reason {
			return old, nil
		}
		return r, errors.New("この判定は仮採点済みです。人の確認で訂正します")
	}
	labels, err := s.LabeledTraces(r.Spec)
	if err != nil {
		return r, err
	}
	if l, ok := labels[r.TraceID]; ok && HumanSource(l.Source) {
		return r, errors.New("人が答えた判定を AI の採点で置き換えません")
	}
	r.Rubric, r.Every, r.At = ReviewRubric, ReviewQuality(all, labels).Every, time.Now().UTC()
	r.Sampled = ReviewSample(r.TraceID, r.Every)
	err = s.appendJSONL(filepath.Join(dir, r.Spec+".jsonl"), r)
	return r, err
}
