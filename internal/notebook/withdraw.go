package notebook

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// ErrNotWithdrawable is a withdrawal of an article that is not only the caller's work: it cites a source the caller did not name, or it
// holds a person's correction.
var ErrNotWithdrawable = errors.New("the article cites a source the withdrawal does not name, or holds a person's correction; only an article whose every source the caller names, and that holds no correction, can be withdrawn")

// withdrawalTitle is the title of the source a withdrawal leaves on the article.
const withdrawalTitle = "Withdrawn by the work that made it"

// Withdrawal names an article and the sources the caller saved for it. A program that saves sources and an article from its own work
// (chiten, for a task that reached its goal) withdraws the article when the person deletes that work, so later work is not handed it.
// The article is not deleted: it stays withdrawn, with its text, history and sources, and the person restores it or deletes it on the
// Knowledge page. Naming the sources does not show the article is the caller's (a query prints the sources of every article, and the
// kind of a source is whatever its saver wrote), which is why a withdrawal can always be undone by a person and never by an agent.
type Withdrawal struct {
	ID       string   `json:"id"`
	Revision string   `json:"revision"`
	Sources  []string `json:"sources"`
	Reason   string   `json:"reason"`
}

// Withdraw takes the article out of delivery and search as a withdrawn article (its history and sources stay, and a person decides
// whether it comes back), after checking under the page lock that every source it cites is one the withdrawal names and that it holds no
// person's correction.
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
	c := Change{ID: w.ID, Revision: w.Revision, Action: "withdraw", withdrawal: &Withdrawal{ID: w.ID, Revision: w.Revision, Sources: w.Sources, Reason: reason}}
	c.check = func(p Page) error {
		if p.Correction != nil || len(p.Sources) == 0 {
			return ErrNotWithdrawable
		}
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
	return Observation{Kind: "agent", Title: withdrawalTitle, Content: string(data), Reference: "lesson:" + w.ID + "@" + w.Revision, Version: w.Revision, ObservedAt: time.Now().UTC()}
}

// WithdrawnArticle is a withdrawn article with what the person needs to decide on it.
type WithdrawnArticle struct {
	Page
	Withdrawal WithdrawalNote `json:"withdrawal"`
}

// WithdrawalNote says why an article was withdrawn and which work it came from: the titles of the records the withdrawal named.
type WithdrawalNote struct {
	Reason string   `json:"reason"`
	Work   []string `json:"work"`
}

// withdrawnWorkMax is how many titles of the work's records a withdrawn article shows.
const withdrawnWorkMax = 5

// Withdrawn lists the articles a work withdrew and nobody has restored or deleted, in id order, for the Knowledge page. They are not
// part of any search or delivery. A replaced article is left out (it cannot be restored or deleted), and one whose sources cannot be
// read is listed without the titles of its work.
func (b Book) Withdrawn() ([]WithdrawnArticle, error) {
	b = b.scopedRead()
	waiting, err := b.waitingForAPerson()
	if err != nil {
		return nil, err
	}
	out := []WithdrawnArticle{}
	for _, p := range waiting {
		out = append(out, WithdrawnArticle{Page: p, Withdrawal: b.withdrawalNote(p)})
	}
	return out, nil
}

// WithdrawnCount is how many articles Withdrawn lists, without reading their sources (shikii status prints it).
func (b Book) WithdrawnCount() (int, error) {
	waiting, err := b.scopedRead().waitingForAPerson()
	return len(waiting), err
}

// replacedIDs are the ids of the articles another article replaces.
func replacedIDs(pages []Page) map[string]bool {
	replaced := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			replaced[id] = true
		}
	}
	return replaced
}

// waitingForAPerson is the withdrawn articles nobody has restored or deleted and nothing replaced, in id order.
func (b Book) waitingForAPerson() ([]Page, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	superseded := replacedIDs(pages)
	out := []Page{}
	for _, p := range pages {
		if p.Status == StatusWithdrawn && !superseded[p.ID] {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// withdrawalNote reads the source the withdrawal left on the article, and the titles of the records it named. What cannot be read is left
// out: the article's own change reason then says why.
func (b Book) withdrawalNote(p Page) WithdrawalNote {
	note := WithdrawalNote{Reason: p.ChangeReason, Work: []string{}}
	dir, err := b.directory(false, "sources")
	if err != nil {
		return note
	}
	for _, src := range p.Sources {
		o, err := b.readObservation(dir, src.ID, src.Hash)
		var w Withdrawal
		if err != nil || o.Kind != "agent" || o.Title != withdrawalTitle || o.Reference != "lesson:"+p.ID+"@"+p.Previous || json.Unmarshal([]byte(o.Content), &w) != nil {
			continue
		}
		note.Reason = w.Reason
		for _, id := range w.Sources {
			ref := ""
			for _, s := range p.Sources {
				if s.ID == id {
					ref = s.Hash
				}
			}
			if named, err := b.readObservation(dir, id, ref); err == nil && len(note.Work) < withdrawnWorkMax {
				note.Work = append(note.Work, named.Title)
			}
		}
		break
	}
	return note
}
