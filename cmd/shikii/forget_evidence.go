package main

// The evidence forget goes by. Each id below is found in a record the session itself wrote and that names it by structure (its file, a
// field that holds its id or the hash of its id, or an id its records carry), never by searching the id as text. The evidence is read
// again at the start of each pass, so a run that applies it goes by what is there then.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

var forgetHex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)

// forgetReviewSpecs are the questions whose provisional scores are kept in records/reviews/.
var forgetReviewSpecs = []string{"changes-in-scope", "done-as-asked"}

type forgetRun struct {
	root, scope string
	st          store.Store
	book        notebook.Book
	ids         []string
	// asked is true when the removal was asked for over MCP: the server's own environment is the one of whoever started it, long before
	// the session that asks now, so it is never read, and the session of the call (callerIDs, the ones it names) is the one that stays.
	asked     bool
	callerIDs []string
	applying  bool
	f         *store.Forgotten
	plan      forgetPlan

	parents     map[string]bool      // the ids that are sessions of this project
	childOnly   map[string]string    // an id that is only a subagent's: the session its deliveries ran under
	hashes      map[string]bool      // store.CoverageSession of each session
	turns       map[string]bool      // the turns of the sessions
	made        map[string]string    // a source id the sessions made, and the record that says so
	read        map[string]bool      // a source id the sessions' work states name as read, not made
	sessTraces  map[string]bool      // the decisions the sessions' own records wait on
	quoteTraces map[string]bool      // the decisions the deliveries made
	deliveries  map[string]bool      // the deliveries removed
	children    map[string]bool      // the subagents of the sessions
	want        map[string]bool      // every decision trace that goes (unless a person labeled it)
	reasoning   []string             // the reasoning history files of the sessions
	newest      map[string]time.Time // the newest time any record of the id was written at (see touch)
	parts       map[string]string    // what each step added to the plan the last time it was only listed (see runStep)
	first, last time.Time

	procs    map[string]*agentProcess // the agent process each session's record names (see stillRunning)
	list     []processEntry           // the processes of this machine, read once for each pass (see processList)
	listErr  error
	listRead bool
	idle     map[string]bool // a step that the last listing found nothing to remove in: an application does not take its lock or run it
	ran      []string        // the steps that finished in the last application (see stepsLeft)
}

func forgetSet(ids []string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func forgetSorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Small readers for a JSON record kept as it is.
func jget(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func jstr(v any, keys ...string) string { s, _ := jget(v, keys...).(string); return s }

func jlist(v any, keys ...string) []any { l, _ := jget(v, keys...).([]any); return l }

func jmap(v any, keys ...string) map[string]any { m, _ := jget(v, keys...).(map[string]any); return m }

func (r *forgetRun) turn(id string) {
	if forgetHex16.MatchString(id) {
		r.turns[id] = true
	}
}

// touch notes that a record of the id was written at the time. The newest of these says whether the session may still be running when
// no agent process does (stillRunning): the record of the session is not the only thing a live session writes, and a session whose own
// record is gone leaves the others.
func (r *forgetRun) touch(id string, at time.Time) {
	if id != "" && at.After(r.newest[id]) {
		r.newest[id] = at
	}
}

func (r *forgetRun) see(at time.Time) {
	if at.IsZero() {
		return
	}
	if r.first.IsZero() || at.Before(r.first) {
		r.first = at
	}
	if at.After(r.last) {
		r.last = at
	}
}

// collectSession takes what the session's own record names: its turn, the decisions it waits on, the sources its work made.
func (r *forgetRun) collectSession(doc map[string]any) {
	for _, path := range [][]string{{"turn_id"}, {"planning", "turn_id"}, {"reasoning", "turn_id"}, {"completed", "turn_id"}, {"reflect_asked_turn"}, {"planning", "work", "reasoning", "turn_id"}} {
		r.turn(jstr(doc, path...))
	}
	for _, list := range []string{"recent", "earlier"} {
		for _, t := range jlist(doc, "planning", list) {
			r.turn(jstr(t, "id"))
			r.trace(jstr(t, "done"))
		}
	}
	for _, key := range []string{"pending", "watch", "asked", "reviews"} {
		for _, item := range jlist(doc, key) {
			r.trace(jstr(item, "trace"))
		}
	}
	for _, t := range jlist(doc, "reported_traces") {
		s, _ := t.(string)
		r.trace(s)
	}
	for _, t := range jlist(doc, "completed", "targets") {
		r.trace(jstr(t, "trace_id"))
	}
	const work = "the work state of the session"
	// what the work read (the sources of another conversation it looked at, the evidence it cited) is named there, but it is not its own
	for _, v := range jmap(doc, "planning", "work", "sources") {
		if s, ok := v.(string); ok && forgetHex16.MatchString(s) {
			r.read[s] = true
		}
	}
	for _, v := range jmap(doc, "planning", "work", "reviews") {
		if s := jstr(v, "source"); forgetHex16.MatchString(s) {
			r.read[s] = true
		}
	}
	for _, key := range []string{"captures", "saved"} {
		for _, c := range jmap(doc, "planning", "work", key) {
			r.own(jstr(c, "id"), work)
		}
	}
	for id := range jmap(doc, "planning", "work", "reports") {
		r.own(id, work)
	}
	r.own(jstr(doc, "planning", "work", "result", "source"), work)
	for _, key := range []string{"coverage_handled", "coverage_left"} {
		for _, id := range jlist(doc, "planning", "work", key) {
			s, _ := id.(string)
			r.own(s, work)
		}
	}
	for _, base := range [][]string{{"reasoning"}, {"planning", "work", "reasoning"}} {
		for _, key := range []string{"result", "start", "latest", "finish"} {
			r.own(jstr(doc, append(append([]string{}, base...), key, "source")...), work)
		}
	}
}

func (r *forgetRun) trace(t string) {
	if forgetHex16.MatchString(t) {
		r.sessTraces[t] = true
	}
}

func (r *forgetRun) own(id, why string) {
	if forgetHex16.MatchString(id) && r.made[id] == "" {
		r.made[id] = why
	}
}

// Rows of the files forget goes through. Each decodes only the fields it goes by.
type forgetGuardRow struct {
	At      time.Time `json:"at"`
	Session string    `json:"session_id"`
	Turn    string    `json:"turn_id"`
}

type forgetDelivery struct {
	ID      string    `json:"id"`
	Parent  string    `json:"parent"`
	Agent   string    `json:"agent"`
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Quotes  []struct {
		Trace string `json:"trace"`
	} `json:"quotes"`
}

type forgetFeedbackRow struct {
	ID      string    `json:"id"`
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Targets []struct {
		TraceID string `json:"trace_id"`
	} `json:"targets"`
}

type forgetReviewRow struct {
	TraceID string `json:"trace_id"`
	Source  struct {
		ID      string `json:"id"`
		Session string `json:"session"`
	} `json:"source_material"`
}

type forgetCoverageRow struct {
	Session string `json:"session"`
	Source  string `json:"source"`
}

func decodeRow[T any](line []byte) (T, bool) {
	var row T
	return row, json.Unmarshal(line, &row) == nil
}

// forgetRows reads the lines of a JSONL file as rows of type T; a line that does not decode is left out.
func forgetRows[T any](st store.Store, path string) ([]T, error) {
	lines, err := st.JSONLLines(path)
	if err != nil {
		return nil, err
	}
	var rows []T
	for _, line := range lines {
		if row, ok := decodeRow[T](line); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// The predicates that choose the lines of each file. All of them read the evidence as gathered, so the plan and the removal choose the
// same lines.
func (r *forgetRun) ownDelivery(d forgetDelivery) bool {
	if r.parents[d.Session] || r.turns[d.ID] || r.turns[d.Parent] {
		return true
	}
	session, child := r.childOnly[d.Agent]
	return child && d.Agent != "" && d.Session == session
}

func (r *forgetRun) dropMemory(line []byte) bool {
	d, ok := decodeRow[forgetDelivery](line)
	return ok && r.ownDelivery(d)
}

func (r *forgetRun) dropFeedback(line []byte) bool {
	f, ok := decodeRow[forgetFeedbackRow](line)
	return ok && (r.parents[f.Session] || r.turns[f.ID])
}

func (r *forgetRun) dropReview(line []byte) bool {
	v, ok := decodeRow[forgetReviewRow](line)
	return ok && (r.parents[v.Source.Session] || r.turns[v.Source.ID] || r.sessTraces[v.TraceID] || r.quoteTraces[v.TraceID])
}

func (r *forgetRun) dropGuard(line []byte) bool {
	g, ok := decodeRow[forgetGuardRow](line)
	return ok && r.parents[g.Session]
}

func (r *forgetRun) dropCoverage(line []byte) bool {
	c, ok := decodeRow[forgetCoverageRow](line)
	return ok && (r.hashes[c.Session] || r.made[c.Source] != "")
}

// gather reads the evidence and starts a plan. It changes nothing.
func (r *forgetRun) gather() error {
	r.plan = forgetPlan{At: time.Now().UTC(), Sessions: append([]string{}, r.ids...)}
	r.parents, r.childOnly, r.hashes = map[string]bool{}, map[string]string{}, map[string]bool{}
	r.turns, r.made, r.sessTraces, r.quoteTraces, r.read = map[string]bool{}, map[string]string{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	r.deliveries, r.children, r.want, r.newest = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]time.Time{}
	r.reasoning, r.first, r.last = nil, time.Time{}, time.Time{}
	r.procs = map[string]*agentProcess{}
	r.list, r.listErr, r.listRead = nil, nil, false
	ids := forgetSet(r.ids)
	for _, id := range r.ids {
		r.hashes[store.CoverageSession(id)] = true
	}
	if err := r.gatherSessions(ids); err != nil {
		return err
	}
	if err := r.gatherLogs(ids); err != nil {
		return err
	}
	if err := r.classify(); err != nil {
		return err
	}
	return r.gatherLinked()
}

func (r *forgetRun) gatherSessions(ids map[string]bool) error {
	for _, id := range r.ids {
		path := sessionPath(r.root, id)
		data, err := r.st.ReadFileWhole(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var doc map[string]any
		if json.Unmarshal(data, &doc) != nil {
			return fmt.Errorf("the record of session %s cannot be read, so what it names cannot be found; nothing was changed", id)
		}
		if owner := jstr(doc, "reasoning", "session_id"); owner != "" && owner != id {
			r.plan.Refusals = append(r.plan.Refusals, fmt.Sprintf("%s holds the record of the session %s (two ids share one file name)", filepath.Base(path), owner))
		}
		r.parents[id] = true
		r.procs[id] = agentProcessOf(doc)
		r.collectSession(doc)
		if info, err := os.Lstat(path); err == nil {
			r.touch(id, info.ModTime())
			r.see(info.ModTime())
		}
		var saved struct {
			At time.Time `json:"at"`
		}
		if json.Unmarshal(data, &saved) == nil {
			r.see(saved.At)
		}
	}
	return nil
}

// gatherLogs reads the files that carry the session id as a field, and the turns they name.
func (r *forgetRun) gatherLogs(ids map[string]bool) error {
	lines, err := r.st.JSONLLines(guardRecordsPath(r.root))
	if err != nil {
		return err
	}
	for _, line := range lines {
		if g, ok := decodeRow[forgetGuardRow](line); ok && ids[g.Session] {
			r.parents[g.Session] = true
			r.turn(g.Turn)
			r.see(g.At)
			r.touch(g.Session, g.At)
		}
	}
	files, err := filepath.Glob(filepath.Join(r.root, "records", "work-reasoning-*.json"))
	if err != nil {
		return err
	}
	for _, path := range files {
		data, err := r.st.ReadFileWhole(path)
		if err != nil {
			return err
		}
		var h struct {
			Session string `json:"session"`
			Turn    string `json:"turn"`
			Entry   struct {
				Source string    `json:"source"`
				At     time.Time `json:"at"`
			} `json:"entry"`
		}
		if json.Unmarshal(data, &h) != nil || !ids[h.Session] {
			continue
		}
		r.parents[h.Session] = true
		r.reasoning = append(r.reasoning, filepath.Base(path))
		r.turn(h.Turn)
		r.own(h.Entry.Source, "the reasoning history of the session")
		r.see(h.Entry.At)
		r.touch(h.Session, h.Entry.At)
	}
	feedback, err := forgetRows[forgetFeedbackRow](r.st, r.st.FeedbackFile(r.scope))
	if err != nil {
		return err
	}
	for _, f := range feedback {
		if ids[f.Session] {
			r.parents[f.Session] = true
			r.turn(f.ID)
			r.touch(f.Session, f.At)
		}
	}
	for _, spec := range forgetReviewSpecs {
		lines, err := r.st.JSONLLines(r.st.ReviewsFile(spec))
		if err != nil {
			return err
		}
		for _, line := range lines {
			if v, ok := decodeRow[forgetReviewRow](line); ok && ids[v.Source.Session] {
				r.parents[v.Source.Session] = true
				r.turn(v.Source.ID)
			}
		}
	}
	deliveries, err := forgetRows[forgetDelivery](r.st, r.st.MemoriesFile(r.scope))
	if err != nil {
		return err
	}
	for _, d := range deliveries {
		if ids[d.Session] {
			r.parents[d.Session] = true
			r.turn(d.Parent)
			r.see(d.At)
			r.touch(d.Session, d.At)
		}
		if ids[d.Agent] {
			r.touch(d.Agent, d.At) // an id that is only a subagent's is given: its own deliveries say whether it still works
		}
	}
	lines, err = r.st.JSONLLines(r.st.CoverageFile(r.scope))
	if err != nil {
		return err
	}
	for _, line := range lines {
		if c, ok := decodeRow[forgetCoverageRow](line); ok && r.hashes[c.Session] {
			r.own(c.Source, "a line of the coverage ledger that carries the session")
		}
	}
	for _, id := range r.ids {
		if path, ok := r.latestOf(id); ok {
			r.parents[id] = true
			if info, err := os.Stat(path); err == nil {
				r.touch(id, info.ModTime())
			}
		}
	}
	return nil
}

// classify finds the ids that are only a subagent's, and refuses the ids that name nothing here or that are too loose to take.
func (r *forgetRun) classify() error {
	lines, err := r.st.JSONLLines(r.st.MemoriesFile(r.scope))
	if err != nil {
		return err
	}
	var unknown []string
	for _, id := range r.ids {
		if r.parents[id] {
			continue
		}
		sessions := map[string]bool{}
		for _, line := range lines {
			if d, ok := decodeRow[forgetDelivery](line); ok && d.Agent == id {
				sessions[d.Session] = true
			}
		}
		covered := false // the session it ran under is given too: that session's rows take its deliveries, and only those
		for session := range sessions {
			covered = covered || r.parents[session]
		}
		switch {
		case covered:
		case len(sessions) == 0:
			unknown = append(unknown, id)
		case len(sessions) == 1:
			r.childOnly[id] = forgetSorted(sessions)[0]
		default:
			r.plan.Refusals = append(r.plan.Refusals, fmt.Sprintf("the subagent id %s ran under several sessions (%s); give the session id too", id, strings.Join(forgetSorted(sessions), ", ")))
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("nothing in this project's .shikii/ names %s (check the id; run this in the project's folder)", strings.Join(unknown, ", "))
	}
	for _, id := range r.ids {
		for _, why := range r.ownSession(id) {
			r.plan.Refusals = append(r.plan.Refusals, fmt.Sprintf("%s is %s", id, why))
		}
		if why := r.stillRunning(id); why != "" {
			r.plan.Refusals = append(r.plan.Refusals, why)
		}
	}
	return nil
}

// ownSession says why the session id is the one that must stay: the session this command runs in (the command line, by the variables an
// agent passes its session in) or the session that asked for the removal over MCP (the one the call names).
func (r *forgetRun) ownSession(id string) []string {
	var why []string
	if r.asked {
		for _, own := range r.callerIDs {
			if own == id {
				why = append(why, "the session that asked for this removal")
			}
		}
		return why
	}
	for _, name := range forgetSessionVariables {
		if os.Getenv(name) == id {
			why = append(why, "the session this command runs in ("+name+")")
		}
	}
	return why
}

// gatherLinked reads what the deliveries and the records of the sessions lead to: the decisions they made and the deliveries
// they received.
func (r *forgetRun) gatherLinked() error {
	lines, err := r.st.JSONLLines(r.st.MemoriesFile(r.scope))
	if err != nil {
		return err
	}
	for _, line := range lines {
		d, ok := decodeRow[forgetDelivery](line)
		if !ok || !r.ownDelivery(d) {
			continue
		}
		r.deliveries[d.ID] = true
		for _, q := range d.Quotes {
			if forgetHex16.MatchString(q.Trace) {
				r.quoteTraces[q.Trace] = true
			}
		}
		if r.parents[d.Session] && d.Agent != "" {
			r.children[d.Agent] = true
		}
		r.see(d.At)
	}
	feedback, err := forgetRows[forgetFeedbackRow](r.st, r.st.FeedbackFile(r.scope))
	if err != nil {
		return err
	}
	for _, f := range feedback {
		if r.parents[f.Session] || r.turns[f.ID] {
			for _, t := range f.Targets {
				r.want[t.TraceID] = true
			}
		}
	}
	for t := range r.sessTraces {
		r.want[t] = true
	}
	for t := range r.quoteTraces {
		r.want[t] = true
	}
	for _, spec := range forgetReviewSpecs {
		lines, err := r.st.JSONLLines(r.st.ReviewsFile(spec))
		if err != nil {
			return err
		}
		for _, line := range lines {
			if r.dropReview(line) {
				if v, ok := decodeRow[forgetReviewRow](line); ok && v.TraceID != "" {
					r.want[v.TraceID] = true
				}
			}
		}
	}
	r.plan.Children = forgetSorted(r.children)
	for child, session := range r.childOnly {
		r.plan.Children = append(r.plan.Children, child+" (under "+session+")")
	}
	r.plan.Turns = forgetSorted(r.turns)
	return nil
}

// latestOf is the record the agent's folder keeps of the session's last turn, when it says the session worked in this project.
func (r *forgetRun) latestOf(id string) (string, bool) {
	common, err := agentHome()
	if err != nil {
		return "", false
	}
	path := sessionLatestPath(common, id)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var l sessionLatest
	if json.Unmarshal(data, &l) != nil || l.Root == "" || !sameFolder(l.Root, r.root) {
		return "", false
	}
	return path, true
}

// heldByOthers finds, for the sources the sessions made, the working states of other sessions that name them: those sessions read or
// wrote the same source, and it is theirs too.
func (r *forgetRun) heldByOthers() (map[string]string, error) {
	held := map[string]string{}
	check := func(label string, data []byte) {
		for id := range r.made {
			if held[id] == "" && bytes.Contains(data, []byte(id)) {
				held[id] = "named by the work state of another session (" + label + ")"
			}
		}
	}
	own := map[string]bool{}
	for _, id := range r.ids {
		own[filepath.Base(sessionPath(r.root, id))] = true
	}
	names, err := store.RegularFiles(filepath.Join(r.root, "sessions"), ".json")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if own[name] {
			continue
		}
		data, err := r.st.ReadFileWhole(filepath.Join(r.root, "sessions", name))
		if err != nil {
			return nil, err
		}
		check(strings.TrimSuffix(name, ".json"), data)
	}
	files, _ := filepath.Glob(filepath.Join(r.root, "records", "work-reasoning-*.json"))
	ours := forgetSet(r.reasoning)
	for _, path := range files {
		if ours[filepath.Base(path)] {
			continue
		}
		data, err := r.st.ReadFileWhole(path)
		if err != nil {
			return nil, err
		}
		check("reasoning history "+filepath.Base(path), data)
	}
	return held, nil
}
