package notebook

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DeliveryEntry keeps retrieval and the eventual delivery outcome separate.
// Discovery is the search text available then, not a later assessment.
type DeliveryEntry struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Retrieval string `json:"retrieval"`
	Reason    string `json:"reason"`
	Rank      int    `json:"rank,omitempty"`
	Trace     string `json:"trace,omitempty"`
	Discovery string `json:"discovery,omitempty"`
}

type DeliveryCase struct {
	ID       string          `json:"id"`
	Format   int             `json:"format"`
	At       time.Time       `json:"at"`
	TurnID   string          `json:"turn_id"`
	Request  string          `json:"request"`
	Mode     string          `json:"mode"`
	File     string          `json:"file,omitempty"`
	Phrases  []string        `json:"phrases,omitempty"`
	Entries  []DeliveryEntry `json:"entries"`
	Issues   []Issue         `json:"issues,omitempty"`
	Complete bool            `json:"complete"`
	Failure  string          `json:"failure,omitempty"`
}

func (c *DeliveryCase) Set(id, reason, trace string) {
	for i := range c.Entries {
		if c.Entries[i].ID == id {
			c.Entries[i].Reason = reason
			c.Entries[i].Trace = trace
			return
		}
	}
}

// DeliverySnapshot retains exclusions too. Tool callers can prefilter before
// checking sources, preserving the existing cost of a pre-tool lookup.
func (b Book) DeliverySnapshot(keep func(Page) bool) ([]Page, DeliveryCase, error) {
	pages, issues, err := b.pages()
	c := DeliveryCase{Format: 1, Complete: len(issues) == 0, Issues: issues, Entries: []DeliveryEntry{}}
	if err != nil {
		return nil, c, err
	}
	replaced := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			replaced[id] = true
		}
	}
	out := []Page{}
	for _, p := range pages {
		state := "eligible"
		switch {
		case p.Status == "deleted":
			state = "deleted"
		case replaced[p.ID]:
			state = "superseded"
		case keep != nil && !keep(p):
			state = "no_match"
			if p.Kind == "history" {
				state = "history"
			}
		case len(b.sourceIssues(p)) > 0:
			state = "invalid_sources"
		}
		c.Entries = append(c.Entries, DeliveryEntry{ID: p.ID, Revision: p.Revision, Retrieval: state, Reason: state})
		if state == "eligible" {
			p.Recheck = b.premiseChanges(p)
			out = append(out, p)
		}
	}
	return out, c, nil
}

// RecallRanking is shared by live delivery and read-only historical comparison.
// History participates in document frequency but cannot take candidate seats.
func RecallRanking(pages []Page, query string) ([]Page, map[string]DeliveryEntry) {
	states := map[string]DeliveryEntry{}
	for _, p := range pages {
		state := "no_match"
		if p.Kind == "history" {
			state = "history"
		}
		states[p.ID] = DeliveryEntry{ID: p.ID, Revision: p.Revision, Retrieval: state, Reason: state, Discovery: p.discovered}
	}
	ranked, _ := rankPages(pages, query, true)
	out := []Page{}
	rank := 0
	for _, p := range ranked {
		if p.Kind == "history" {
			continue
		}
		rank++
		e := states[p.ID]
		e.Rank = rank
		e.Retrieval = "candidate"
		if rank > RecallCandidates {
			e.Retrieval = "candidate_limit"
		} else {
			out = append(out, p)
		}
		e.Reason = e.Retrieval
		states[p.ID] = e
	}
	return out, states
}

func (b Book) RecallDetailed(query string) ([]Page, DeliveryCase, error) {
	pages, c, err := b.DeliverySnapshot(nil)
	if err != nil {
		return nil, c, err
	}
	contexts, err := b.discoveryContexts()
	if err != nil {
		return nil, c, err
	}
	for i := range pages {
		p := &pages[i]
		p.discovered = contexts[p.ID+":"+p.Revision]
	}
	out, states := RecallRanking(pages, query)
	for i, e := range c.Entries {
		if state, ok := states[e.ID]; ok {
			c.Entries[i] = state
		}
	}
	c.Mode, c.Request = "request", query
	return out, c, nil
}

// HistoricalPages uses only the recorded revisions and discovery contexts.
// A missing or changed revision/source makes comparison unavailable.
func (b Book) HistoricalPages(c DeliveryCase) ([]Page, error) {
	if c.Format != 1 || !c.Complete {
		return nil, errors.New("this case has no complete compatible inventory")
	}
	pages := []Page{}
	for _, e := range c.Entries {
		if e.Retrieval == "deleted" || e.Retrieval == "superseded" || e.Retrieval == "invalid_sources" {
			continue
		}
		p, err := b.readRevision("lesson:" + e.ID + "@" + e.Revision)
		if err != nil {
			return nil, err
		}
		p.discovered = e.Discovery
		pages = append(pages, p)
	}
	return pages, nil
}

const deliveryJournalLimit = 4 << 20

func readDeliveryJournal(dir string) ([]DeliveryCase, error) {
	out := []DeliveryCase{}
	f, err := os.OpenFile(filepath.Join(dir, ".deliveries.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("delivery diagnostics must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, deliveryJournalLimit+1))
	if err != nil || len(data) > deliveryJournalLimit || json.Unmarshal(data, &out) != nil || len(out) > 32 {
		return nil, errors.New("cannot read the bounded delivery diagnostics")
	}
	for _, c := range out {
		id := c.ID
		c.ID = ""
		raw, _ := json.Marshal(c)
		if digest(raw)[:16] != id || c.Format != 1 || len(c.Entries) > 512 || c.At.IsZero() {
			return nil, errors.New("invalid delivery diagnostic record")
		}
		for _, e := range c.Entries {
			if !idPattern.MatchString(e.ID) || !idPattern.MatchString(e.Revision) {
				return nil, errors.New("invalid diagnostic article reference")
			}
		}
	}
	return out, nil
}

func (b Book) DeliveryCases() ([]DeliveryCase, error) {
	dir, err := b.directory(false, "deliveries")
	if os.IsNotExist(err) {
		return []DeliveryCase{}, nil
	}
	if err != nil {
		return nil, err
	}
	return readDeliveryJournal(dir)
}

func (b Book) SaveDeliveryCase(c DeliveryCase) error {
	c.ID = ""
	c.Format = 1
	c.At = time.Now().UTC()
	if len(c.Entries) > 512 {
		c.Entries = c.Entries[:512]
		c.Complete = false
	}
	// Bound the encoded bytes as well as the item count. A partial inventory is
	// explicitly unavailable for replay; diagnostics still explain retained pages.
	for {
		raw, _ := json.Marshal(c)
		if len(raw) <= 512<<10 {
			break
		}
		if len(c.Entries) == 0 {
			return errors.New("delivery context exceeds its budget")
		}
		c.Entries = c.Entries[:len(c.Entries)-1]
		c.Complete = false
	}
	raw, _ := json.Marshal(c)
	c.ID = digest(raw)[:16]
	dir, err := b.directory(true, "deliveries")
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	cases, err := readDeliveryJournal(dir)
	if err != nil {
		return err
	}
	cases = append(cases, c)
	if len(cases) > 32 {
		cases = cases[len(cases)-32:]
	}
	for {
		raw, _ := json.Marshal(cases)
		if len(raw) <= deliveryJournalLimit {
			return writeFile(filepath.Join(dir, ".deliveries.json"), raw)
		}
		cases = cases[1:]
	}
}
