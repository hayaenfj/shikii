package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type MaintenanceTask struct {
	PageID       string            `json:"page_id"`
	Revision     string            `json:"revision"`
	Key          string            `json:"key"`
	Title        string            `json:"title"`
	Reasons      []string          `json:"reasons"`
	Dependencies map[string]string `json:"dependencies"`
}
type MaintenanceReview struct {
	PageID   string `json:"page_id"`
	Revision string `json:"revision"`
	Key      string `json:"key"`
	Reason   string `json:"reason"`
}
type maintenanceCheckpoint struct {
	MaintenanceReview
	At time.Time `json:"at"`
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
	data, err := readOriginals(filepath.Join(dir, ".maintenance.json"))
	if os.IsNotExist(err) {
		return map[string]maintenanceCheckpoint{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]maintenanceCheckpoint
	if json.Unmarshal(data, &out) != nil || len(out) > pageLimit {
		return nil, errors.New("wikiの見直し記録を読み取れません")
	}
	if out == nil {
		out = map[string]maintenanceCheckpoint{}
	}
	return out, nil
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
	for _, task := range maintenanceTasks(valid) {
		previous, seen := checkpoints[task.PageID]
		switch {
		case !seen:
			task.Reasons = []string{"unreviewed"}
		case previous.Key != task.Key:
			task.Reasons = []string{"page-or-related-changed"}
		case time.Since(previous.At) >= 30*24*time.Hour:
			task.Reasons = []string{"periodic"}
		default:
			continue
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
		return review, errors.New("見直した記事・版・keyと根拠（1000バイト以内）が必要です")
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
		return review, errors.New("記事の状態を確認できません")
	}
	var selected *MaintenanceTask
	for _, task := range maintenanceTasks(pages) {
		if task.PageID == review.PageID {
			t := task
			selected = &t
		}
	}
	if selected == nil || selected.Key != review.Key || selected.Revision != review.Revision {
		return review, errors.New("記事か関連する版が変わりました。読み直してから見直しを記録します")
	}
	for _, p := range pages {
		if p.ID == review.PageID || selected.Dependencies[p.ID] != "" {
			if len(b.sourceIssues(p)) > 0 {
				return review, errors.New("出典を確認できない記事は見直し済みにしません")
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
	checkpoints[review.PageID] = maintenanceCheckpoint{review, time.Now().UTC()}
	data, _ := json.Marshal(checkpoints)
	if err := writeFile(filepath.Join(dir, ".maintenance.json"), data); err != nil {
		return review, err
	}
	if err := logEvent(dir, "maintain "+review.PageID+"@"+review.Key); err != nil {
		return review, err
	}
	return review, nil
}
