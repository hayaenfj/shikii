package notebook

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrNotWithdrawable is a withdrawal of an article that cites a source the caller did not name: the article is not only the caller's.
var ErrNotWithdrawable = errors.New("the article cites a source the withdrawal does not name; only an article whose every source the caller saved can be withdrawn")

// Withdrawal names an article and the sources the caller saved for it. A program that saves sources and an article from its own work
// (chiten, for a task that reached its goal) withdraws the article when the person deletes that work, so later work is not handed it.
// Naming the sources is what shows the article is the caller's: a search of its text never chooses one.
type Withdrawal struct {
	ID       string   `json:"id"`
	Revision string   `json:"revision"`
	Sources  []string `json:"sources"`
	Reason   string   `json:"reason"`
}

// Withdraw deletes the article as the Knowledge page's delete does (its history and sources stay, it is no longer delivered), after
// checking under the page lock that every source it cites is one the withdrawal names.
func (b Book) Withdraw(w Withdrawal) (Page, error) {
	reason := strings.TrimSpace(w.Reason)
	// The reason goes into the article's change_reason (1000 bytes) after a short prefix.
	if reason == "" || len(reason) > 900 {
		return Page{}, errors.New("a withdrawal says why, in up to 900 bytes")
	}
	named := map[string]bool{}
	for _, id := range w.Sources {
		if !idPattern.MatchString(id) {
			return Page{}, errors.New("withdrawal sources are 16 lowercase hexadecimal characters each")
		}
		named[id] = true
	}
	c := Change{ID: w.ID, Revision: w.Revision, Action: "delete", withdrawal: &Withdrawal{ID: w.ID, Revision: w.Revision, Sources: w.Sources, Reason: reason}}
	c.check = func(p Page) error {
		for _, src := range p.Sources {
			if !named[src.ID] {
				return ErrNotWithdrawable
			}
		}
		return nil
	}
	return b.Manage(c)
}

// withdrawalObservation is the source a withdrawal leaves on the article: the program that made the article says why it went.
func withdrawalObservation(w Withdrawal) Observation {
	data, _ := json.Marshal(w)
	return Observation{Kind: "agent", Title: "Withdrawn by the work that made it", Content: string(data), Reference: "lesson:" + w.ID + "@" + w.Revision, Version: w.Revision, ObservedAt: time.Now().UTC()}
}
