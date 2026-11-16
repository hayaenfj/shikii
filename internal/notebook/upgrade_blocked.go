package notebook

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// memoryUpgradeBlock is a memory-upgrade page the agent declared blocked: its original evidence cannot be read or does not
// support it. It is kept at the revision the agent was shown, so an edit returns the page to review.
type memoryUpgradeBlock struct {
	Revision string `json:"revision"`
	Reason   string `json:"reason"`
}

// BlockMemoryUpgrade declares a pending memory-upgrade page blocked. Without this the batch returns the page first on every
// preparation and never reaches the pages after it. The declaration is refused unless the page, revision and key match the
// current pending task, so an agent cannot block a page it was not shown or one it already reviewed.
func (b Book) BlockMemoryUpgrade(pageID, revision, key, reason string) error {
	if !idPattern.MatchString(pageID) || !idPattern.MatchString(revision) || !idPattern.MatchString(key) || strings.TrimSpace(reason) == "" || len(reason) > 1000 || !utf8.ValidString(reason) {
		return errors.New("a blocked page needs its page, revision, key and a reason (up to 1000 bytes)")
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := readMemoryUpgrade(dir)
	if err != nil {
		return errors.New("no memory upgrade is running in this project")
	}
	if c.Complete || c.Targets[pageID] == "" {
		return errors.New("the page is not a target of the running memory upgrade")
	}
	if c.Reviews[pageID] == key {
		return errors.New("the page is already reviewed at this key; it cannot be declared blocked")
	}
	pages, issues, err := b.pages()
	if err != nil || len(issues) > 0 {
		return errors.New("cannot check the state of the page")
	}
	valid := []Page{}
	for _, p := range pages {
		if len(b.sourceIssues(p)) == 0 {
			valid = append(valid, p)
		}
	}
	tasks, err := b.maintenanceTasks(valid)
	if err != nil {
		return err
	}
	pending := false
	for _, task := range tasks {
		pending = pending || task.PageID == pageID && task.Key == key && task.Revision == revision
	}
	if !pending {
		return errors.New("the page, its revision or its key changed; read it again before declaring it blocked")
	}
	if c.Blocked == nil {
		c.Blocked = map[string]memoryUpgradeBlock{}
	}
	c.Blocked[pageID] = memoryUpgradeBlock{Revision: revision, Reason: reason}
	if err := saveMemoryUpgrade(dir, c); err != nil {
		return err
	}
	return logEvent(dir, "block "+pageID+"@"+revision)
}

// MemoryUpgradeBlocked reports whether a page is declared blocked at this revision in the running memory upgrade.
// Procedure reviews use it so a blocked page is not left as a pending review of the record that declared it.
func (b Book) MemoryUpgradeBlocked(pageID, revision string) bool {
	dir, err := b.directory(false, "pages")
	if err != nil {
		return false
	}
	c, err := readMemoryUpgrade(dir)
	if err != nil {
		return false
	}
	mark, ok := c.Blocked[pageID]
	return ok && mark.Revision == revision
}
