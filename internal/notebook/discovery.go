package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

// Lookups are bounded receipts for actual manual reads, not usefulness assessments.
type Lookup struct {
	ID    string                     `json:"id"`
	Query string                     `json:"query"`
	Pages []store.KnowledgeReference `json:"pages"`
}

type Discovery struct {
	LookupID string `json:"lookup_id"`
	PageID   string `json:"page_id"`
	Revision string `json:"revision"`
	Context  string `json:"context"`
	Decision string `json:"decision"`
	Evidence string `json:"evidence"`
	CaseID   string `json:"case_id,omitempty"`
	WorkID   string `json:"work_id,omitempty"`
}

// MissedDiscovery freezes a checked diagnostic reason beside the actual use.
// It is derived by Discover, never accepted as the caller's claim.
type MissedDiscovery struct {
	Discovery
	Reason string `json:"reason"`
}

type recordedDiscovery struct {
	Discovery
	MissedReason string `json:"missed_reason,omitempty"`
}

func (p Page) DiscoveryContext() string { return p.discovered }

type searchJournal struct {
	ReadingSaturated bool                `json:"reading_saturated,omitempty"`
	Lookups          []Lookup            `json:"lookups"`
	Discoveries      []recordedDiscovery `json:"discoveries"`
	Outcomes         []ProcedureOutcome  `json:"outcomes,omitempty"`
	Readings         []ReadingSample     `json:"readings,omitempty"`
}

func readSearchJournal(dir string) (searchJournal, error) {
	var j searchJournal
	f, err := os.OpenFile(filepath.Join(dir, ".searches.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return j, errors.New("the search journal must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return j, errors.New("cannot read the bounded search journal")
	}
	if json.Unmarshal(data, &j) != nil || len(j.Lookups) > 64 || len(j.Discoveries) > 128 || len(j.Outcomes) > 128 || len(j.Readings) > 128 {
		return j, errors.New("cannot read the wiki search records")
	}
	for _, l := range j.Lookups {
		id := l.ID
		l.ID = ""
		data, _ := json.Marshal(l)
		if digest(data)[:16] != id || len(l.Pages) > 2 || len(l.Query) > 4000 {
			return j, errors.New("invalid wiki lookup receipt")
		}
		for _, p := range l.Pages {
			if !idPattern.MatchString(p.ID) || !idPattern.MatchString(p.Revision) {
				return j, errors.New("invalid wiki lookup revision")
			}
		}
	}
	for _, d := range j.Discoveries {
		if !idPattern.MatchString(d.LookupID) || !idPattern.MatchString(d.PageID) || !idPattern.MatchString(d.Revision) || d.WorkID != "" && !idPattern.MatchString(d.WorkID) {
			return j, errors.New("invalid saved discovery")
		}
		if d.CaseID != "" && !idPattern.MatchString(d.CaseID) || d.MissedReason != "" && (d.CaseID == "" || !missedDeliveryReason(d.MissedReason)) {
			return j, errors.New("invalid missed delivery evidence")
		}
		for _, s := range []string{d.Context, d.Decision, d.Evidence} {
			if strings.TrimSpace(s) == "" || len(s) > 1000 || !utf8.ValidString(s) {
				return j, errors.New("invalid saved discovery text")
			}
		}
	}
	for _, o := range j.Outcomes {
		if err := o.validate(); err != nil {
			return j, err
		}
	}
	for _, r := range j.Readings {
		if err := r.Validate(); err != nil || !idPattern.MatchString(r.Snapshot.ID) || !validOutcomeHash(r.Snapshot.Hash) {
			return j, errors.New("invalid saved reading execution")
		}
	}
	return j, nil
}

func saveSearchJournal(dir string, j searchJournal) error {
	for {
		data, err := json.Marshal(j)
		if err != nil {
			return err
		}
		if len(data) <= 1<<20 {
			return writeFile(filepath.Join(dir, ".searches.json"), data)
		}
		// JSON escaping can expand otherwise valid text. Trim older records from
		// the larger collection, keeping the latest lookup and discovery intact.
		lookups, _ := json.Marshal(j.Lookups)
		discoveries, _ := json.Marshal(j.Discoveries)
		outcomes, _ := json.Marshal(j.Outcomes)
		readings, _ := json.Marshal(j.Readings)
		if len(j.Readings) > 1 && (len(j.Outcomes) <= 1 || len(readings) >= len(outcomes)) && (len(j.Lookups) <= 1 || len(readings) >= len(lookups)) && (len(j.Discoveries) <= 1 || len(readings) >= len(discoveries)) {
			trimReadingHistory(&j)
		} else if len(j.Outcomes) > 1 && (len(j.Lookups) <= 1 || len(outcomes) >= len(lookups)) && (len(j.Discoveries) <= 1 || len(outcomes) >= len(discoveries)) {
			j.Outcomes = j.Outcomes[1:]
		} else if len(j.Lookups) > 1 && (len(j.Discoveries) <= 1 || len(lookups) >= len(discoveries)) {
			j.Lookups = j.Lookups[1:]
		} else if len(j.Discoveries) > 1 {
			j.Discoveries = j.Discoveries[1:]
		} else {
			return errors.New("the latest search records exceed the journal budget")
		}
	}
}

// RecordLookup is called at the public query boundary, after pagination. Internal searches do not create receipts.
func (b Book) RecordLookup(query string, pages []Page) (*Lookup, error) {
	if strings.TrimSpace(query) == "" || len(pages) == 0 {
		return nil, nil
	}
	if len(pages) > 2 {
		return nil, errors.New("a lookup records at most two returned article bodies")
	}
	if len(query) > 4000 || !utf8.ValidString(query) {
		return nil, errors.New("lookup query must be UTF-8, at most 4000 bytes")
	}
	l := Lookup{Query: query}
	for _, p := range pages {
		l.Pages = append(l.Pages, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
	}
	data, _ := json.Marshal(l)
	l.ID = digest(data)[:16]
	dir, err := b.directory(true, "searches")
	if err != nil {
		return nil, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	kept := []Lookup{}
	for _, old := range j.Lookups {
		if old.ID != l.ID {
			kept = append(kept, old)
		}
	}
	j.Lookups = append(kept, l)
	if len(j.Lookups) > 64 {
		j.Lookups = j.Lookups[len(j.Lookups)-64:]
	}
	if err = saveSearchJournal(dir, j); err != nil {
		return nil, err
	}
	return &l, nil
}

// Discover records an explicit, evidenced use of a returned revision. It never creates a human truth or an automatic evaluation.
func (d Discovery) Validate() error {
	if !idPattern.MatchString(d.LookupID) || !idPattern.MatchString(d.PageID) || !idPattern.MatchString(d.Revision) || d.CaseID != "" && !idPattern.MatchString(d.CaseID) || d.WorkID != "" && !idPattern.MatchString(d.WorkID) {
		return errors.New("discovery needs a lookup, article and revision")
	}
	for _, text := range []string{d.Context, d.Decision, d.Evidence} {
		if strings.TrimSpace(text) == "" || len(text) > 1000 || !utf8.ValidString(text) {
			return errors.New("discovery context, decision and evidence must each be 1 to 1000 UTF-8 bytes")
		}
	}
	return nil
}

// ErrDiscoveryRejected identifies a definite refusal, with no publication.
var ErrDiscoveryRejected = errors.New("discovery rejected")

func discoveryRejected(reason string) error {
	return fmt.Errorf("%w: %s", ErrDiscoveryRejected, reason)
}

func (b Book) Discover(d Discovery) error {
	_, err := b.discovery(d, true)
	return err
}

// CheckDiscovery shares publication validation without writing. An exact saved
// use is recoverable even after its article changes, disappears or is replaced.
func (b Book) CheckDiscovery(d Discovery) (bool, error) { return b.discovery(d, false) }

func (b Book) discovery(d Discovery, publish bool) (bool, error) {
	if err := d.Validate(); err != nil {
		return false, err
	}
	// Use the same page lock as editing, so a deletion or replacement cannot pass between validation and saving.
	pageDir, err := b.directory(false, "pages")
	if err != nil {
		return false, err
	}
	unlockPages, err := lock(pageDir)
	if err != nil {
		return false, err
	}
	defer unlockPages()
	dir, err := b.directory(false, "searches")
	if err != nil {
		return false, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := readSearchJournal(dir)
	if err != nil {
		return false, err
	}
	validateLive := func() error {
		live, err := b.Live()
		if err != nil {
			return err
		}
		for _, p := range live {
			if p.ID == d.PageID && p.Revision == d.Revision {
				return nil
			}
		}
		return discoveryRejected("the discovery's article revision is no longer live; read it again")
	}
	// Direct lookup submissions retain their current-revision requirement.
	// Only an internal work identity can recover a prior publication.
	if d.WorkID == "" {
		if err := validateLive(); err != nil {
			return false, err
		}
	}
	// Successful retries do not require an older lookup to remain in the rolling read window.
	for _, old := range j.Discoveries {
		same := old.LookupID == d.LookupID && old.PageID == d.PageID && old.CaseID == d.CaseID && old.WorkID == ""
		if d.CaseID != "" {
			same = old.CaseID == d.CaseID && old.PageID == d.PageID && old.Revision == d.Revision
		} else if d.WorkID != "" {
			same = old.WorkID == d.WorkID && old.PageID == d.PageID && old.Revision == d.Revision && old.CaseID == ""
		}
		if same {
			prior := old.Discovery
			// The same actual use can be found through another query. Keep the
			// original read receipt instead of counting that query as new evidence.
			prior.LookupID = d.LookupID
			if d.CaseID != "" {
				prior.WorkID = d.WorkID
			}
			if prior == d {
				return true, nil
			}
			return false, discoveryRejected("a saved discovery cannot be rewritten")
		}
	}
	if d.WorkID != "" {
		if err := validateLive(); err != nil {
			return false, err
		}
	}
	found := false
	for _, l := range j.Lookups {
		if l.ID == d.LookupID {
			for _, p := range l.Pages {
				found = found || p.ID == d.PageID && p.Revision == d.Revision
			}
		}
	}
	if !found {
		return false, discoveryRejected("the article revision was not returned by this lookup; query it again")
	}
	missedReason := ""
	if d.CaseID != "" {
		cases, err := b.DeliveryCases()
		if err != nil {
			return false, err
		}
		matched := false
		for _, c := range cases {
			if c.ID != d.CaseID {
				continue
			}
			if !c.Complete {
				return false, discoveryRejected("a missed delivery needs complete recorded diagnostics")
			}
			for _, e := range c.Entries {
				if e.ID == d.PageID && e.Revision == d.Revision {
					matched = true
					if missedDeliveryReason(e.Reason) {
						missedReason = e.Reason
					}
				}
			}
		}
		if !matched {
			return false, discoveryRejected("the diagnostic case does not contain this article revision in this project")
		}
	}
	if !publish {
		return false, nil
	}
	j.Discoveries = append(j.Discoveries, recordedDiscovery{Discovery: d, MissedReason: missedReason})
	if len(j.Discoveries) > 128 {
		j.Discoveries = j.Discoveries[len(j.Discoveries)-128:]
	}
	return false, saveSearchJournal(dir, j)
}

func missedDeliveryReason(reason string) bool {
	switch reason {
	case "no_match", "candidate_limit", "feedback_silenced", "model_rejected", "model_limit", "delivery_limit", "quote_budget":
		return true
	}
	return false
}

func (b Book) missedDiscoveries() ([]MissedDiscovery, error) {
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	out := []MissedDiscovery{}
	for _, d := range j.Discoveries {
		if d.MissedReason != "" {
			out = append(out, MissedDiscovery{Discovery: d.Discovery, Reason: d.MissedReason})
		}
	}
	return out, nil
}

// The extra search text comes only from an evidenced use of this exact revision. Normal ranking and the delivery gate still apply.
func (b Book) discoveryContexts() (map[string]string, error) {
	out := map[string]string{}
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	for i := len(j.Discoveries) - 1; i >= 0; i-- {
		d := j.Discoveries[i]
		key := d.PageID + ":" + d.Revision
		if len(out[key])+len(d.Context)+1 <= 4004 {
			out[key] += " " + d.Context
		}
	}
	return out, nil
}
