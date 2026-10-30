package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

type MaintenanceTask struct {
	PageID        string                   `json:"page_id"`
	Revision      string                   `json:"revision"`
	Key           string                   `json:"key"`
	Title         string                   `json:"title"`
	Reasons       []string                 `json:"reasons"`
	Dependencies  map[string]string        `json:"dependencies"`
	Feedback      []store.MemoryAssessment `json:"feedback,omitempty"`
	FeedbackTotal int                      `json:"feedback_total,omitempty"`
	ContentKey    string                   `json:"-"`
	FeedbackKeys  []string                 `json:"-"`
}
type MaintenanceReview struct {
	PageID   string `json:"page_id"`
	Revision string `json:"revision"`
	Key      string `json:"key"`
	Reason   string `json:"reason"`
}
type maintenanceCheckpoint struct {
	MaintenanceReview
	At           time.Time `json:"at"`
	ContentKey   string    `json:"content_key,omitempty"`
	FeedbackKeys []string  `json:"feedback_keys,omitempty"`
}

func maintenanceTasks(pages []Page) []MaintenanceTask {
	known, deps := map[string]Page{}, map[string]map[string]string{}
	for _, p := range pages {
		known[p.ID] = p
		deps[p.ID] = map[string]string{}
	}
	for _, p := range pages {
		for _, id := range append(append([]string{}, p.Related...), p.Replaces...) {
			if other, ok := known[id]; ok {
				deps[p.ID][id] = other.Revision
				deps[id][p.ID] = p.Revision
			}
		}
	}
	out := []MaintenanceTask{}
	for _, p := range pages {
		task := MaintenanceTask{PageID: p.ID, Revision: p.Revision, Title: p.Title, Dependencies: deps[p.ID]}
		data, _ := json.Marshal(struct {
			Revision     string
			Dependencies map[string]string
		}{p.Revision, task.Dependencies})
		task.Key = digest(data)[:16]
		out = append(out, task)
	}
	return out
}

func readCheckpoints(dir string) (map[string]maintenanceCheckpoint, error) {
	data, err := readRegistry(filepath.Join(dir, ".maintenance.json"))
	if os.IsNotExist(err) {
		return map[string]maintenanceCheckpoint{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]maintenanceCheckpoint
	if json.Unmarshal(data, &out) != nil {
		return nil, errors.New("cannot read the wiki review log")
	}
	if out == nil {
		out = map[string]maintenanceCheckpoint{}
	}
	return out, nil
}

// MaintenanceKeys returns the current maintenance task (revision and key) of every live page whose sources check out, by page id,
// whether or not a review is pending (0.12: a submit compares the keys before and after its own saves).
func (b Book) MaintenanceKeys() (map[string]MaintenanceTask, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	valid := []Page{}
	for _, p := range pages {
		if len(b.sourceIssues(p)) == 0 {
			valid = append(valid, p)
		}
	}
	out := map[string]MaintenanceTask{}
	tasks, err := b.maintenanceTasks(valid)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		out[task.PageID] = task
	}
	return out, nil
}

// A helpful assessment can remove unused evidence from a review. A subset of already reviewed evidence is not new work.
func hasNewFeedback(current, reviewed []string) bool {
	for _, key := range current {
		if _, found := slices.BinarySearch(reviewed, key); !found {
			return true
		}
	}
	return false
}

func (b Book) Maintenance() ([]MaintenanceTask, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return []MaintenanceTask{}, nil
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		return nil, err
	}
	checkpoints, err := readCheckpoints(dir)
	if err != nil {
		return nil, err
	}
	valid := []Page{}
	for _, p := range pages {
		if len(b.sourceIssues(p)) == 0 {
			valid = append(valid, p)
		}
	}
	pending := []MaintenanceTask{}
	tasks, err := b.maintenanceTasks(valid)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		previous, seen := checkpoints[task.PageID]
		previousContent := previous.ContentKey
		if previousContent == "" {
			previousContent = previous.Key
		}
		switch {
		case !seen:
			task.Reasons = []string{"unreviewed"}
		case previous.Key != task.Key && (previousContent != task.ContentKey || hasNewFeedback(task.FeedbackKeys, previous.FeedbackKeys)):
			if previousContent != task.ContentKey {
				task.Reasons = []string{"page-or-related-changed"}
			}
		case time.Since(previous.At) >= 30*24*time.Hour:
			task.Reasons = []string{"periodic"}
		default:
			continue
		}
		if task.FeedbackTotal > 0 {
			task.Reasons = append([]string{"delivery-feedback"}, task.Reasons...)
		}
		if len(task.Dependencies) == 0 {
			task.Reasons = append(task.Reasons, "unlinked")
		}
		for _, p := range valid {
			if p.ID == task.PageID && p.Body == "" {
				task.Reasons = append(task.Reasons, "summary-only")
			}
		}
		pending = append(pending, task)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		a, b := pending[i], pending[j]
		if (a.FeedbackTotal > 0) != (b.FeedbackTotal > 0) {
			return a.FeedbackTotal > 0
		}
		ca, cb := a.Reasons[0] == "page-or-related-changed", b.Reasons[0] == "page-or-related-changed"
		if ca != cb {
			return ca
		}
		return checkpoints[a.PageID].At.Before(checkpoints[b.PageID].At)
	})
	if len(pending) > 2 {
		pending = pending[:2]
	}
	return pending, nil
}

func (b Book) Maintain(review MaintenanceReview) (MaintenanceReview, error) {
	if !idPattern.MatchString(review.PageID) || !idPattern.MatchString(review.Revision) || !idPattern.MatchString(review.Key) || strings.TrimSpace(review.Reason) == "" || len(review.Reason) > 1000 || !utf8.ValidString(review.Reason) {
		return review, errors.New("a review needs the reviewed page, revision, key and a reason (up to 1000 bytes)")
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		return review, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return review, err
	}
	defer unlock()
	pages, issues, err := b.pages()
	if err != nil || len(issues) > 0 {
		return review, errors.New("cannot check the state of the page")
	}
	// the key is computed over the same pages the query computes it over (those whose sources check out), or a page whose related page
	// lost its source could never be reviewed: the query's key and this key would come from different sets (audit B of 0.12)
	valid := []Page{}
	for _, p := range pages {
		if len(b.sourceIssues(p)) == 0 {
			valid = append(valid, p)
		} else if p.ID == review.PageID {
			return review, errors.New("a page whose sources cannot be checked is not marked as reviewed") // the real reason, not "changed" (confirmation audit 1)
		}
	}
	var selected *MaintenanceTask
	tasks, err := b.maintenanceTasks(valid)
	if err != nil {
		return review, err
	}
	for _, task := range tasks {
		if task.PageID == review.PageID {
			t := task
			selected = &t
		}
	}
	if selected == nil || selected.Key != review.Key || selected.Revision != review.Revision {
		return review, errors.New("the page, a related revision or its delivery feedback changed; read it again before recording the review")
	}
	for _, p := range pages {
		if p.ID == review.PageID || selected.Dependencies[p.ID] != "" {
			if len(b.sourceIssues(p)) > 0 {
				return review, errors.New("a page whose sources cannot be checked is not marked as reviewed")
			}
		}
	}
	checkpoints, err := readCheckpoints(dir)
	if err != nil {
		return review, err
	}
	if old, ok := checkpoints[review.PageID]; ok && old.Key == review.Key && old.Reason == review.Reason && time.Since(old.At) < 30*24*time.Hour {
		return review, nil
	}
	checkpoints[review.PageID] = maintenanceCheckpoint{MaintenanceReview: review, At: time.Now().UTC(), ContentKey: selected.ContentKey, FeedbackKeys: selected.FeedbackKeys}
	data, _ := json.Marshal(checkpoints)
	if err := writeFile(filepath.Join(dir, ".maintenance.json"), data); err != nil {
		return review, err
	}
	if err := logEvent(dir, "maintain "+review.PageID+"@"+review.Key); err != nil {
		return review, err
	}
	return review, nil
}
