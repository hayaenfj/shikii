package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
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
	ChangedFiles  []PremiseChange          `json:"changed_files,omitempty"`
	Missed        []MissedDiscovery        `json:"missed,omitempty"`
	MissedTotal   int                      `json:"missed_total,omitempty"`
	Outcomes      []OutcomeReview          `json:"outcomes,omitempty"`
	OutcomeTotal  int                      `json:"outcome_total,omitempty"`
	NextAction    string                   `json:"next_action,omitempty"`
	ContentKey    string                   `json:"-"`
	BasisKey      string                   `json:"-"`
	FeedbackKeys  []string                 `json:"-"`
	outcomeRefs   []ProcedureOutcome
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
	BasisKey     string    `json:"basis_key,omitempty"`
	FeedbackKeys []string  `json:"feedback_keys,omitempty"`
}

func maintenanceTasks(pages []Page) []MaintenanceTask {
	known, deps := map[string]Page{}, map[string]map[string]string{}
	contents := map[string]string{}
	for _, p := range pages {
		known[p.ID] = p
		deps[p.ID] = map[string]string{}
		// Version lineage alone is not new evidence about a related page.
		// Keep every stored content field, including sources and edit reasons.
		p.Revision, p.Previous, p.Recheck = "", "", nil
		raw, _ := json.Marshal(p)
		contents[p.ID] = digest(raw)
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
		if p.Status == "deleted" {
			continue
		}
		task := MaintenanceTask{PageID: p.ID, Revision: p.Revision, Title: p.Title, Dependencies: deps[p.ID]}
		data, _ := json.Marshal(struct {
			Revision     string
			Dependencies map[string]string
		}{p.Revision, task.Dependencies})
		task.Key = digest(data)[:16]
		content := map[string]string{}
		for id := range task.Dependencies {
			content[id] = contents[id]
		}
		data, _ = json.Marshal(struct {
			Revision     string
			Dependencies map[string]string
		}{p.Revision, content})
		task.BasisKey = digest(data)[:16]
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
	b = b.scopedRead()
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

func sameMaintenanceContent(previous maintenanceCheckpoint, task MaintenanceTask) bool {
	if previous.BasisKey != "" && task.BasisKey != "" {
		return previous.Revision == task.Revision && previous.BasisKey == task.BasisKey
	}
	// Older checkpoints lack the content comparison evidence. Preserve their
	// existing exact-version behavior until a real review records that evidence.
	key := previous.ContentKey
	if key == "" {
		key = previous.Key
	}
	return key == task.ContentKey
}

func (b Book) Maintenance() ([]MaintenanceTask, error) {
	return b.maintenance(nil, false)
}

// MaintenanceForPages applies the page selection before the two-review limit.
func (b Book) MaintenanceForPages(ids []string) ([]MaintenanceTask, error) {
	if len(ids) == 0 {
		return []MaintenanceTask{}, nil
	}
	return b.maintenance(ids, false)
}

// OutcomeMaintenance returns pending reviews only for the procedures just used.
// It shares checkpoints with ordinary maintenance, so retries do not reopen it.
func (b Book) OutcomeMaintenance(ids []string) ([]MaintenanceTask, error) {
	if len(ids) == 0 {
		return []MaintenanceTask{}, nil
	}
	return b.maintenance(ids, true)
}

func (b Book) maintenance(ids []string, outcomesOnly bool) ([]MaintenanceTask, error) {
	b = b.scopedRead()
	pages, issues, err := b.pages()
	if err != nil {
		return nil, err
	}
	if ids != nil && !outcomesOnly {
		// Maintain also rejects unreadable pages: their relationships are unknown.
		if len(issues) > 0 {
			return nil, fmt.Errorf("wiki page %s is unreadable or changed; restore it or inspect wiki lint before reviewing", issues[0].Page)
		}
		known := map[string]Page{}
		for _, p := range pages {
			known[p.ID] = p
		}
		for _, id := range ids {
			p, exists := known[id]
			if !exists {
				return nil, fmt.Errorf("selected page %s is missing; restore its current article before reviewing", id)
			}
			if p.Status == "deleted" {
				continue
			}
			required := append([]string{id}, p.Related...)
			required = append(required, p.Replaces...)
			for _, other := range pages {
				if slices.Contains(other.Related, id) || slices.Contains(other.Replaces, id) {
					required = append(required, other.ID)
				}
			}
			for _, relatedID := range required {
				related, exists := known[relatedID]
				if !exists {
					return nil, fmt.Errorf("related page %s is missing; restore it before reviewing page %s", relatedID, id)
				}
				if len(b.sourceIssues(related)) > 0 {
					return nil, fmt.Errorf("page %s has unreadable or changed sources; restore them before reviewing page %s", relatedID, id)
				}
			}
		}
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
		if ids != nil && !slices.Contains(ids, task.PageID) || outcomesOnly && task.OutcomeTotal == 0 {
			continue
		}
		previous, seen := checkpoints[task.PageID]
		sameContent := sameMaintenanceContent(previous, task)
		switch {
		case !seen:
			task.Reasons = []string{"unreviewed"}
		case previous.Key != task.Key && (!sameContent || hasNewFeedback(task.FeedbackKeys, previous.FeedbackKeys)):
			if !sameContent {
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
		if task.OutcomeTotal > 0 {
			task.Reasons = append([]string{"outcome-feedback"}, task.Reasons...)
		}
		if task.MissedTotal > 0 {
			task.Reasons = append([]string{"missed-delivery"}, task.Reasons...)
		}
		if len(task.ChangedFiles) > 0 {
			task.Reasons = append([]string{"premise-changed"}, task.Reasons...)
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
		if (a.OutcomeTotal > 0) != (b.OutcomeTotal > 0) {
			return a.OutcomeTotal > 0
		}
		if (a.MissedTotal > 0) != (b.MissedTotal > 0) {
			return a.MissedTotal > 0
		}
		if (a.FeedbackTotal > 0) != (b.FeedbackTotal > 0) {
			return a.FeedbackTotal > 0
		}
		ca, cb := a.Reasons[0] == "page-or-related-changed", b.Reasons[0] == "page-or-related-changed"
		if ca != cb {
			return ca
		}
		return checkpoints[a.PageID].At.Before(checkpoints[b.PageID].At)
	})
	// The list carries two tasks. One saved source that can no longer be read must not hide every other task: a task
	// whose report cannot be checked stays listed, with that report named as unreadable. Such a task cannot be reviewed
	// until its sources are recovered, so while other work waits it takes at most one of the two places.
	listed, held := []MaintenanceTask{}, []MaintenanceTask{}
	for i := range pending {
		if len(listed) == 2 {
			break
		}
		readable := true
		for _, p := range valid {
			if p.ID == pending[i].PageID {
				readable = b.expandOutcomeReview(&pending[i], p) == nil
				break
			}
		}
		if !readable && slices.ContainsFunc(listed, unreadableTask) {
			held = append(held, pending[i])
			continue
		}
		listed = append(listed, pending[i])
	}
	for _, task := range held {
		if len(listed) < 2 {
			listed = append(listed, task)
		}
	}
	return listed, nil
}

func unreadableTask(task MaintenanceTask) bool {
	return slices.ContainsFunc(task.Outcomes, func(o OutcomeReview) bool { return o.Outcome.unreadable })
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
		return review, errors.New("the page, related knowledge, delivery feedback or execution evidence changed; read it again before recording the review")
	}
	for _, p := range pages {
		if p.ID == review.PageID || selected.Dependencies[p.ID] != "" {
			if len(b.sourceIssues(p)) > 0 {
				return review, errors.New("a page whose sources cannot be checked is not marked as reviewed")
			}
			if p.ID == review.PageID {
				if err := b.expandOutcomeReview(selected, p); err != nil {
					return review, err
				}
			}
		}
	}
	checkpoints, err := readCheckpoints(dir)
	if err != nil {
		return review, err
	}
	if old, ok := checkpoints[review.PageID]; ok && old.Key == review.Key && old.Reason == review.Reason && time.Since(old.At) < 30*24*time.Hour {
		return review, recordMemoryUpgradeReview(dir, review)
	}
	checkpoints[review.PageID] = maintenanceCheckpoint{MaintenanceReview: review, At: time.Now().UTC(), ContentKey: selected.ContentKey, BasisKey: selected.BasisKey, FeedbackKeys: selected.FeedbackKeys}
	data, _ := json.Marshal(checkpoints)
	if err := writeFile(filepath.Join(dir, ".maintenance.json"), data); err != nil {
		return review, err
	}
	if err := logEvent(dir, "maintain "+review.PageID+"@"+review.Key); err != nil {
		return review, err
	}
	return review, recordMemoryUpgradeReview(dir, review)
}
