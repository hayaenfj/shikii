package notebook

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrNotWithdrawable is a withdrawal of an article that is not only the caller's work: it cites a source the caller did not name, or one that is
// not a record of a tool's work (a person's words, or a source that cannot be read as such).
var ErrNotWithdrawable = errors.New("the article cites a source the withdrawal does not name, or one that is not a record of a tool's own work; only an article whose every source the caller saved from its work (kind agent, code or test) can be withdrawn")

// withdrawableKinds are the kinds of source a withdrawal may take away an article for: a tool's record of its own work. A person's words (kind
// human, a conversation the hook recorded, a correction), a release record and anything unreadable are not on the list.
var withdrawableKinds = map[string]bool{"agent": true, "code": true, "test": true}

// Withdrawal names an article and the sources the caller saved for it. A program that saves sources and an article from its own work
// (chiten, for a task that reached its goal) withdraws the article when the person deletes that work, so later work is not handed it.
// Naming the sources does not show the article is the caller's (a query prints the sources of every article), so each source must also be a
// record of a tool's work: an article that cites a person's words is never withdrawn.
type Withdrawal struct {
	ID       string   `json:"id"`
	Revision string   `json:"revision"`
	Sources  []string `json:"sources"`
	Reason   string   `json:"reason"`
}

// Withdraw deletes the article as the Knowledge page's delete does (its history and sources stay, it is no longer delivered), after
// checking under the page lock that every source it cites is one the withdrawal names and a record of a tool's own work.
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
		if p.Correction != nil || len(p.Sources) == 0 {
			return ErrNotWithdrawable
		}
		dir, err := b.directory(false, "sources")
		if err != nil {
			return ErrNotWithdrawable
		}
		for _, src := range p.Sources {
			if !named[src.ID] {
				return ErrNotWithdrawable
			}
			// A conversation the hook recorded is not an observation, so it is not read here and the article stays.
			o, err := b.readObservation(dir, src.ID, src.Hash)
			if err != nil || !withdrawableKinds[o.Kind] {
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
