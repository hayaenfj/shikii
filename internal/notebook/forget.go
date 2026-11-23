package notebook

// shikii forget removes what a finished agent session left in the wiki's own folders: the sources it wrote, the delivery cases of its
// requests and the lines of the search journal that are its own. Until forget, a source was never removed: an article cites it by its
// id and hash, a source that goes breaks the article's check (lint: missing-source) and the revisions it cites. So a source is removed
// only when nothing cites it, and a source something does cite stays with the reason. What is removed is kept in the run's folder
// (store.Forgotten) first.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/rootfile"
	"github.com/hayaenfj/shikii/internal/store"
)

// ForgetRequest names what the forgotten sessions leave in the wiki's folders.
type ForgetRequest struct {
	Owned   []string          // the source ids the sessions made (or the conversation copies of their requests)
	HeldBy  map[string]string // a source id and the reason a record outside the notebook still names it
	Turns   []string          // the turn ids whose delivery cases go
	Misses  []string          // store.CoverageSession of each session: the misses that carry it go
	WorkIDs []string          // the work ids of the sessions' turns: the discoveries and readings that carry one go
}

// ForgetResult is what forget did, or with apply false would do, in the wiki's folders.
type ForgetResult struct {
	Removed     []string          // the sources moved out
	Kept        map[string]string // a source that stays, and what names it
	Cases       int               // delivery cases
	Misses      int
	Discoveries int
	Readings    int
}

// LockForForget takes the locks forget needs, from the outside in: the articles', the sources', the search journal's and the delivery
// journal's (each only when its folder exists). The same order the wiki's own writers take them in.
func (b Book) LockForForget() (func(), error) {
	var held store.Unlocks
	release := func() { held.Release() }
	for _, area := range []string{"pages", "sources", "searches"} {
		dir, err := b.directory(false, area)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			release()
			return nil, err
		}
		unlock, err := lock(dir)
		if err != nil {
			release()
			return nil, err
		}
		held.Add(unlock)
	}
	if root, err := b.openDeliveryDirectory(false); err == nil {
		f, err := rootfile.OpenNoFollow(root, ".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
		root.Close()
		if err != nil {
			release()
			return nil, err
		}
		unlock, err := lockOpened(f)
		if err != nil {
			release()
			return nil, err
		}
		held.Add(unlock)
	} else if !os.IsNotExist(err) {
		release()
		return nil, err
	}
	return release, nil
}

// sourceFiles are the files that belong to one source: the record, the mark that it was reviewed as a conversation, and the long text.
var sourceFiles = []string{".reviewed", ".text.json", ".json"}

// Forget removes what the request names, keeping it in f first, or with apply false only reports it. The caller holds LockForForget.
func (b Book) Forget(req ForgetRequest, f *store.Forgotten, apply bool) (ForgetResult, error) {
	res := ForgetResult{Kept: map[string]string{}}
	journal, err := b.forgetJournal(req)
	if err != nil {
		return res, err
	}
	cases, err := b.forgetCases(req.Turns)
	if err != nil {
		return res, err
	}
	res.Cases, res.Misses, res.Discoveries, res.Readings = len(cases.drop), journal.misses, journal.discoveries, journal.readings
	if err := b.forgetSources(req, journal, &res, f, apply); err != nil {
		return res, err
	}
	if apply {
		if err := b.saveForgetCases(cases, f); err != nil {
			return res, err
		}
		if err := b.saveForgetJournal(journal, f); err != nil {
			return res, err
		}
	}
	return res, nil
}

// forgetSources decides which of the owned sources nothing names, and moves those out.
func (b Book) forgetSources(req ForgetRequest, journal forgetJournal, res *ForgetResult, f *store.Forgotten, apply bool) error {
	dir, err := b.directory(false, "sources")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	name := b.storeName()
	present := func(id string) bool {
		for _, suffix := range sourceFiles {
			if _, err := os.Lstat(filepath.Join(dir, id+suffix)); err == nil {
				return true
			}
		}
		return false
	}
	var owned []string
	for _, id := range req.Owned {
		if idPattern.MatchString(id) && present(id) {
			owned = append(owned, id)
		}
	}
	sort.Strings(owned)
	if len(owned) == 0 {
		return nil
	}
	named, err := b.namedSources(owned, journal)
	if err != nil {
		return err
	}
	for _, id := range owned {
		switch why := named[id]; {
		case why != "":
			res.Kept[id] = why
		case req.HeldBy[id] != "":
			res.Kept[id] = req.HeldBy[id]
		default:
			res.Removed = append(res.Removed, id)
		}
	}
	if !apply || len(res.Removed) == 0 {
		return nil
	}
	// The queue of sources not yet reviewed goes first: an id left in it with no record would fail the listing of every pending source.
	if err := b.forgetFromQueue(dir, name, res.Removed, f); err != nil {
		return err
	}
	st := store.Store{Dir: b.Root}
	for _, id := range res.Removed {
		for _, suffix := range sourceFiles {
			path := filepath.Join(b.Root, "records", "wiki-sources", name, id+suffix)
			if _, err := os.Lstat(path); err != nil {
				continue
			}
			if err := st.MoveOut(path, f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b Book) forgetFromQueue(dir, name string, removed []string, f *store.Forgotten) error {
	ids, err := capturedIDs(dir)
	if err != nil {
		return err
	}
	gone := map[string]bool{}
	for _, id := range removed {
		gone[id] = true
	}
	var kept, taken []string
	for _, id := range ids {
		if gone[id] {
			taken = append(taken, id)
		} else {
			kept = append(kept, id)
		}
	}
	if len(taken) == 0 {
		return nil
	}
	data, _ := json.Marshal(taken)
	if err := f.SaveFile(filepath.Join("records", "wiki-sources", name, ".captured.json"), data); err != nil {
		return err
	}
	if kept == nil {
		kept = []string{}
	}
	data, _ = json.Marshal(kept)
	return writeFile(filepath.Join(dir, ".captured.json"), data)
}

// namedSources says, for each source, what still names it: an article or a revision of one (cites it), an assignment, or an element of
// the search journal that stays. A source nothing names has no entry.
func (b Book) namedSources(owned []string, journal forgetJournal) (map[string]string, error) {
	named := map[string]string{}
	check := func(what string, text []byte) {
		for _, id := range owned {
			if named[id] == "" && bytes.Contains(text, []byte(id)) {
				named[id] = what
			}
		}
	}
	if err := b.forEachPage(func(what, name string, text []byte) {
		check("cited by the "+what+" "+name, text)
	}); err != nil {
		return nil, err
	}
	assignments := filepath.Join(b.Root, "records", "assignments", b.storeName())
	names, err := store.RegularFiles(assignments, ".json")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		text, err := os.ReadFile(filepath.Join(assignments, name))
		if err != nil {
			return nil, err
		}
		check("named by the assignment "+name, text)
	}
	check("named by the wiki's search records", journal.remaining)
	return named, nil
}

// forgetJournal is the search journal read for forget: the elements that go, and the text of those that stay.
type forgetJournal struct {
	dir                           string
	file                          map[string]json.RawMessage
	drop                          map[string][]json.RawMessage
	keep                          map[string][]json.RawMessage
	remaining                     []byte
	misses, discoveries, readings int
	missSessions, workIDs         map[string]bool
}

func toSet(ids []string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// journalArrays are the arrays of the search journal forget removes elements from, and the field of an element that names its owner.
var journalArrays = []struct{ key, field string }{{"misses", "session"}, {"discoveries", "work_id"}, {"readings", "work"}}

func (b Book) forgetJournal(req ForgetRequest) (forgetJournal, error) {
	j := forgetJournal{missSessions: toSet(req.Misses), workIDs: toSet(req.WorkIDs), drop: map[string][]json.RawMessage{}, keep: map[string][]json.RawMessage{}}
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	j.dir = dir
	data, err := readRegistry(filepath.Join(dir, ".searches.json"))
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	if err := json.Unmarshal(data, &j.file); err != nil {
		return j, errors.New("cannot read the wiki search records")
	}
	for key, raw := range j.file {
		owner := ""
		for _, a := range journalArrays {
			if a.key == key {
				owner = a.field
			}
		}
		var elements []json.RawMessage
		if json.Unmarshal(raw, &elements) != nil {
			j.remaining = append(j.remaining, raw...)
			continue
		}
		for _, element := range elements {
			if owner != "" && j.owns(key, owner, element) {
				j.drop[key] = append(j.drop[key], element)
			} else {
				j.keep[key] = append(j.keep[key], element)
				j.remaining = append(j.remaining, element...)
			}
		}
	}
	j.misses, j.discoveries, j.readings = len(j.drop["misses"]), len(j.drop["discoveries"]), len(j.drop["readings"])
	return j, nil
}

func (j forgetJournal) owns(key, field string, element json.RawMessage) bool {
	var one map[string]json.RawMessage
	if json.Unmarshal(element, &one) != nil {
		return false
	}
	var value string
	if json.Unmarshal(one[field], &value) != nil || value == "" {
		return false
	}
	if key == "misses" {
		return j.missSessions[value]
	}
	return j.workIDs[value]
}

func (b Book) saveForgetJournal(j forgetJournal, f *store.Forgotten) error {
	if j.misses+j.discoveries+j.readings == 0 {
		return nil
	}
	dropped := map[string][]json.RawMessage{}
	for key, elements := range j.drop {
		dropped[key] = elements
	}
	data, _ := json.Marshal(dropped)
	if err := f.SaveFile(filepath.Join("records", "wiki-searches", b.storeName(), ".searches.json"), data); err != nil {
		return err
	}
	out := map[string]json.RawMessage{}
	for key, raw := range j.file {
		out[key] = raw
	}
	for _, a := range journalArrays {
		if len(j.drop[a.key]) == 0 {
			continue
		}
		kept := j.keep[a.key]
		if kept == nil {
			kept = []json.RawMessage{}
		}
		out[a.key], _ = json.Marshal(kept)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(j.dir, ".searches.json"), data)
}

// forgetCasesPlan is the delivery journal read for forget.
type forgetCasesPlan struct {
	keep, drop []json.RawMessage
}

// forgetCases splits the delivery cases of the requests named from the rest.
func (b Book) forgetCases(turns []string) (forgetCasesPlan, error) {
	var plan forgetCasesPlan
	root, err := b.openDeliveryDirectory(false)
	if os.IsNotExist(err) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	defer root.Close()
	f, err := rootfile.OpenNoFollow(root, ".deliveries.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	data, err := readAllLimited(f, deliveryJournalLimit)
	f.Close()
	if err != nil {
		return plan, err
	}
	var all []json.RawMessage
	if json.Unmarshal(data, &all) != nil {
		return plan, errors.New("cannot read the delivery diagnostics")
	}
	gone := toSet(turns)
	for _, element := range all {
		var c struct {
			TurnID string `json:"turn_id"`
		}
		if json.Unmarshal(element, &c) == nil && gone[c.TurnID] {
			plan.drop = append(plan.drop, element)
		} else {
			plan.keep = append(plan.keep, element)
		}
	}
	return plan, nil
}

func (b Book) saveForgetCases(p forgetCasesPlan, f *store.Forgotten) error {
	if len(p.drop) == 0 {
		return nil
	}
	data, _ := json.Marshal(p.drop)
	if err := f.SaveFile(filepath.Join("records", "wiki-deliveries", b.storeName(), ".deliveries.json"), data); err != nil {
		return err
	}
	keep := p.keep
	if keep == nil {
		keep = []json.RawMessage{}
	}
	data, _ = json.Marshal(keep)
	root, err := b.openDeliveryDirectory(false)
	if err != nil {
		return err
	}
	defer root.Close()
	return writeDeliveryJournal(root, data)
}

func readAllLimited(f *os.File, limit int64) ([]byte, error) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("a wiki record must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = errors.New("a wiki record is too large to read")
	}
	return data, err
}

// ForgetCandidate is a source or an article that may belong to the sessions, though only a word or a time in it says so. Forget lists
// it and removes nothing: the same words are written by whoever worked on the sessions, and sources made in the same minutes are
// often another session's.
type ForgetCandidate struct{ What, Why string }

// ForgetCandidates finds the sources whose text names one of the words (the session ids) or that were observed between from and to
// (when both are set), and the articles and revisions that name one of the words. A source in skip is not listed.
func (b Book) ForgetCandidates(words []string, from, to time.Time, skip map[string]bool) ([]ForgetCandidate, error) {
	var out []ForgetCandidate
	if dir, err := b.directory(false, "sources"); err == nil {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok || !idPattern.MatchString(id) || !e.Type().IsRegular() || skip[id] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			if word := mentioned(data, words); word != "" {
				out = append(out, ForgetCandidate{"source " + id, "its text names " + word})
				continue
			}
			var o struct {
				ObservedAt time.Time `json:"observed_at"`
			}
			if !from.IsZero() && !to.IsZero() && json.Unmarshal(data, &o) == nil && !o.ObservedAt.Before(from) && !o.ObservedAt.After(to) {
				out = append(out, ForgetCandidate{"source " + id, "observed at " + o.ObservedAt.UTC().Format(time.RFC3339) + ", while the session ran"})
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	err := b.forEachPage(func(what, name string, text []byte) {
		if word := mentioned(text, words); word != "" {
			out = append(out, ForgetCandidate{what + " " + name, "its text names " + word})
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// forEachPage reads each article and each revision of an article: what is "article" or "revision", name the file's name (the lists the
// wiki writes itself, log.md and index.md, are not articles).
func (b Book) forEachPage(fn func(what, name string, text []byte)) error {
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, folder := range []string{"", "revisions"} {
		names, err := store.RegularFiles(filepath.Join(dir, folder), ".md")
		if err != nil {
			return err
		}
		for _, name := range names {
			if folder == "" && (name == "log.md" || name == "index.md") {
				continue
			}
			text, err := os.ReadFile(filepath.Join(dir, folder, name))
			if err != nil {
				return err
			}
			what := "article"
			if folder != "" {
				what = "revision"
			}
			fn(what, name, text)
		}
	}
	return nil
}

func mentioned(data []byte, words []string) string {
	for _, w := range words {
		if bytes.Contains(data, []byte(w)) {
			return w
		}
	}
	return ""
}
