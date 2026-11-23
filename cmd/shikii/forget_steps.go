package main

// The steps of shikii forget, in the order they run: the evidence a step goes by is removed after it, so a run that stops in the middle
// can be run again and finds the rest (the decisions come first, as they are found through the deliveries; the session records last).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// forgetStep is one step of the removal. lock, when it is set, takes the lock the step needs (nil for a step whose files are locked
// one by one, as they are rewritten).
type forgetStep struct {
	name string
	run  func() error
	lock func() (func(), error)
}

func (r *forgetRun) steps() []forgetStep {
	return []forgetStep{
		{"decisions", r.stepDecisions, nil},
		{"assessments", r.stepAssessments, r.lockMemory},
		{"memories", func() error {
			return r.rewrite("records/memories", r.st.MemoriesFile(r.scope), r.dropMemory, "lessons delivered to the sessions and to their subagents")
		}, r.lockMemory},
		{"wiki", r.stepWiki, r.lockWiki},
		{"coverage", func() error {
			return r.rewrite("records/coverage", r.st.CoverageFile(r.scope), r.dropCoverage, "lines of the results the sessions made; a line that closed another work's row goes too, and that row is open again")
		}, nil},
		{"reviews", r.stepReviews, nil},
		{"feedback", func() error {
			return r.rewrite("records/feedback", r.st.FeedbackFile(r.scope), r.dropFeedback, "conversations of the sessions")
		}, nil},
		{"reasoning", r.stepReasoning, nil},
		{"guards", func() error {
			return r.rewrite("records/guards.jsonl", guardRecordsPath(r.root), r.dropGuard, "checks of the sessions' turns")
		}, nil},
		{"sessions", r.stepSessions, nil},
		{"agent", r.stepAgent, nil},
	}
}

// rewrite removes the lines of the file that drop chooses (or, in a plan, counts them).
func (r *forgetRun) rewrite(place, path string, drop func([]byte) bool, why string) error {
	n := 0
	if r.applying {
		var err error
		if n, err = r.st.RewriteKept(path, drop, r.f); err != nil {
			return err
		}
	} else {
		lines, err := r.st.JSONLLines(path)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if drop(line) {
				n++
			}
		}
	}
	r.remove(place, "lines", n, why)
	return nil
}

// stepDecisions removes the decisions the sessions made, with the labels no person gave. A decision a person labeled stays, with all
// its labels (they are the calibration). The original text of a decision goes when no decision line of any question holds its trace.
func (r *forgetRun) stepDecisions() error {
	if len(r.want) == 0 {
		return nil
	}
	traces, unnamed, err := r.st.RecordTraces()
	if err != nil {
		return err
	}
	bySpec := map[string]map[string]bool{}
	for t := range r.want {
		for _, spec := range traces[t] {
			if bySpec[spec] == nil {
				bySpec[spec] = map[string]bool{}
			}
			bySpec[spec][t] = true
		}
	}
	removed := map[string]map[string]bool{}
	for _, spec := range forgetSorted(forgetSetOf(bySpec)) {
		res, err := r.st.ForgetDecisions(spec, bySpec[spec], r.f, r.applying)
		if err != nil {
			return err
		}
		r.remove("records/"+spec+".jsonl", "decisions", len(res.Removed), "made for the sessions' deliveries and records")
		r.remove("labels/"+spec+".jsonl", "label lines", res.Labels, "no person gave them, and the decision goes")
		for _, t := range forgetSorted(forgetKeys(res.Kept)) {
			r.keep("decision "+t+" ("+spec+")", "a person labeled it (source "+res.Kept[t]+"), so it stays with all its labels")
		}
		for _, t := range res.Removed {
			if removed[t] == nil {
				removed[t] = map[string]bool{}
			}
			removed[t][spec] = true
		}
	}
	gone := map[string]bool{}
	for t, specs := range removed {
		held := false
		for _, spec := range traces[t] {
			held = held || !specs[spec]
		}
		gone[t] = !held
	}
	if unnamed {
		if slices.Contains(forgetValues(gone), true) {
			r.keep("original texts of the decisions", "a line in a decisions file names no decision, so no text can be said to be unused")
		}
		gone = map[string]bool{}
	}
	n, err := r.st.ForgetRecordTexts(forgetOnly(gone), r.f, r.applying)
	if err != nil {
		return err
	}
	r.remove("records/text", "files", n, "the original text of the decisions that went")
	return nil
}

func (r *forgetRun) stepAssessments() error {
	deliveries := map[string]bool{}
	for id := range r.deliveries {
		deliveries[id] = true
	}
	for id := range r.turns {
		deliveries[id] = true
	}
	n, err := r.st.ForgetAssessments(r.scope, deliveries, r.f, r.applying)
	if err != nil {
		return err
	}
	r.remove("records/memory-reviews", "files", n, "assessments of the lessons delivered to them")
	return nil
}

// stepWiki is the part of the wiki's own folders: the sources the sessions made that nothing cites, the delivery diagnostics of their
// requests, and their lines of the search journal.
func (r *forgetRun) stepWiki() error {
	held, err := r.heldByOthers()
	if err != nil {
		return err
	}
	req := notebook.ForgetRequest{HeldBy: held}
	for id := range r.made {
		req.Owned = append(req.Owned, id)
	}
	for t := range r.turns {
		req.Owned = append(req.Owned, t) // the copy of the session's conversation kept as a source, named by the request's id
		req.Turns = append(req.Turns, t)
		req.WorkIDs = append(req.WorkIDs, forgetWorkIDs(r.ids, t)...)
	}
	for _, id := range r.ids {
		req.Misses = append(req.Misses, store.CoverageSession(id))
	}
	res, err := r.book.Forget(req, r.f, r.applying)
	if err != nil {
		return err
	}
	r.remove("records/wiki-sources", "sources", len(res.Removed), "made by the sessions' work (or the copy of their conversation); no article, assignment or other record names them; their .reviewed marks and long texts go with them", res.Removed...)
	r.remove("records/wiki-deliveries", "elements", res.Cases, "delivery diagnostics of the sessions' requests")
	r.remove("records/wiki-searches", "elements", res.Misses+res.Discoveries+res.Readings, "misses, discoveries and readings of the sessions' own work")
	for _, id := range forgetSorted(forgetKeys(res.Kept)) {
		r.keep("source "+id, res.Kept[id])
	}
	for _, id := range forgetSorted(r.read) {
		if r.made[id] == "" {
			r.keep("source "+id, "the sessions' work read it (another conversation's source); it is not theirs")
		}
	}
	skip := map[string]bool{}
	for id := range r.made {
		skip[id] = true
	}
	candidates, err := r.book.ForgetCandidates(r.ids, r.first, r.last, skip)
	if err != nil {
		return err
	}
	for _, c := range candidates {
		r.candidate(c.What, c.Why)
	}
	return nil
}

// forgetWorkIDs are the work ids of the sessions' turn: the hash of the session and the turn, 16 digits of it.
func forgetWorkIDs(sessions []string, turn string) []string {
	var out []string
	for _, id := range sessions {
		out = append(out, workHash([]string{id, turn})[:16])
	}
	return out
}

func (r *forgetRun) stepReviews() error {
	for _, spec := range forgetReviewSpecs {
		if err := r.rewrite("records/reviews/"+spec+".jsonl", r.st.ReviewsFile(spec), r.dropReview, "provisional scores of the sessions' conversations"); err != nil {
			return err
		}
	}
	return nil
}

func (r *forgetRun) stepReasoning() error {
	for _, name := range r.reasoning {
		if r.applying {
			if err := r.st.MoveOut(filepath.Join(r.root, "records", name), r.f); err != nil {
				return err
			}
		}
	}
	r.remove("records/work-reasoning", "files", len(r.reasoning), "the reasoning history of the sessions' work", r.reasoning...)
	return nil
}

// forgetSessionSuffixes are the files kept next to a session's record: the record and its caches (kept in the folder), and the locks
// (nothing in them).
var (
	forgetSessionData  = []string{".json", ".files", ".lines", ".guard.files", ".guard.lines", ".deferred"}
	forgetSessionLocks = []string{".json.lock", ".json.work.lock", ".json.planning.lock"}
)

func (r *forgetRun) stepSessions() error {
	var data, locks []string
	for _, id := range r.ids {
		if !r.parents[id] {
			continue
		}
		base := strings.TrimSuffix(sessionPath(r.root, id), ".json")
		for _, suffix := range forgetSessionData {
			if info, err := os.Lstat(base + suffix); err == nil && info.Mode().IsRegular() {
				data = append(data, base+suffix)
			}
		}
		for _, suffix := range forgetSessionLocks {
			if info, err := os.Lstat(base + suffix); err == nil && info.Mode().IsRegular() {
				locks = append(locks, base+suffix)
			}
		}
	}
	names := func(paths []string) []string {
		var out []string
		for _, p := range paths {
			out = append(out, filepath.Join("sessions", filepath.Base(p)))
		}
		return out
	}
	if r.applying {
		for _, p := range data {
			if err := r.st.MoveOut(p, r.f); err != nil {
				return err
			}
		}
		for _, p := range locks {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	r.remove("sessions", "files", len(data), "the record of each session and the caches beside it", names(data)...)
	r.remove("sessions", "lock files", len(locks), "empty, left by the session", names(locks)...)
	return nil
}

// stepAgent removes the state the agents' common folder (~/.shikii/agent) keeps of the sessions: where the session's last turn was, and
// the notes that it was told about a folder.
func (r *forgetRun) stepAgent() error {
	common, err := agentHome()
	if err != nil {
		return err
	}
	if info, err := os.Lstat(filepath.Join(common, "latest")); err == nil && info.IsDir() {
		unlock, ok := lockPathWithin(filepath.Join(common, "latest", ".lock"), hookLockWait, nil)
		if !ok {
			return fmt.Errorf("the lock of %s is busy", filepath.Join(common, "latest"))
		}
		defer unlock()
	}
	var names []string
	for _, id := range r.ids {
		path, ok := r.latestOf(id)
		if !ok {
			continue
		}
		names = append(names, filepath.Base(path))
		if r.applying {
			if err := r.moveUserFile(path, filepath.Join("agent", "latest", filepath.Base(path))); err != nil {
				return err
			}
		}
	}
	r.remove("~/.shikii/agent/latest", "files", len(names), "where each session's last turn was, when it was in this project", names...)
	return r.stepTold(common)
}

// moveUserFile keeps a file of the person's home in the run's folder, under home/.shikii/<rel>, and removes it.
func (r *forgetRun) moveUserFile(path, rel string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := r.f.SaveFile(filepath.Join("home", ".shikii", rel), data); err != nil {
		return err
	}
	return os.Remove(path)
}

func (r *forgetRun) stepTold(common string) error {
	path := filepath.Join(common, "told.json")
	if _, err := os.Lstat(path); err != nil {
		return nil
	}
	unlock, ok := lockPathWithin(path+".lock", hookLockWait, nil)
	if !ok {
		return fmt.Errorf("the lock of %s is busy", path)
	}
	defer unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var told []string
	if json.Unmarshal(data, &told) != nil {
		return nil // not a list of ours: left as it is
	}
	keys := map[string]bool{}
	for _, id := range r.ids {
		sum := sha256.Sum256([]byte(id + "\n" + r.scope))
		keys[hex.EncodeToString(sum[:8])] = true
	}
	var kept, taken []string
	for _, k := range told {
		if keys[k] {
			taken = append(taken, k)
		} else {
			kept = append(kept, k)
		}
	}
	if r.applying && len(taken) > 0 {
		out, _ := json.Marshal(taken)
		if err := r.f.SaveFile(filepath.Join("home", ".shikii", "agent", "told.json"), out); err != nil {
			return err
		}
		if kept == nil {
			kept = []string{}
		}
		out, _ = json.Marshal(kept)
		if err := writeAtomic(path, out); err != nil {
			return err
		}
	}
	r.remove("~/.shikii/agent/told.json", "elements", len(taken), "notes that a session was told about a folder")
	return nil
}

func forgetKeys[V any](m map[string]V) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func forgetSetOf(m map[string]map[string]bool) map[string]bool { return forgetKeys(m) }

func forgetValues(m map[string]bool) []bool {
	var out []bool
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// forgetOnly is the keys of m that are true.
func forgetOnly(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		if v {
			out[k] = true
		}
	}
	return out
}
