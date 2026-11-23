package main

// shikii forget <session id>... [--apply]: remove what finished agent sessions (trial runs of an agent, a session started by mistake)
// left in the project's .shikii/. Without --apply it only lists what would go, what stays and why. With it, what goes is moved into
// records/forgotten/<UTC time>-<short id>/ (each file under its path in the store, each line or element appended to the file of that
// path), so a person can read it back; plan.json there says what was removed, why it was sure to be this session's, and what was left
// and why.
//
// A record belongs to a session only when a structure says so: a field that holds the session id (or the hash of it), an id the session's
// own records name (the turn, a delivery, a decision), or a file named by the id. A time or a word in a text is not enough: those are only
// listed as candidates. What shikii forget removes is never chosen by searching the id as text.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
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

// forgetUnknownQuiet is how long the records of a session must have been left alone before forget takes it when no agent process says
// whether it still runs (forget_running.go). Where a process is noted, no time is used.
const forgetUnknownQuiet = 24 * time.Hour

// forgetSessionVariables are the environment variables an agent passes the id of its own session in: forget never removes that one.
var forgetSessionVariables = []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "OPENCODE_SESSION_ID"}

// forgetAfterStep runs after each step of an application, when the locks of the step are released (a test makes one fail to stop a run in
// the middle, and one looks at what another process can take between two steps).
var forgetAfterStep = func(step string) error { return nil }

var forgetIDShape = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

type forgetItem struct {
	Place string   `json:"place"`
	What  string   `json:"what"`
	Count int      `json:"count"`
	Why   string   `json:"why"`
	Names []string `json:"names,omitempty"`
}

type forgetNote struct {
	What string `json:"what"`
	Why  string `json:"why"`
}

// forgetPlan is what forget lists and what it writes to plan.json.
type forgetPlan struct {
	At         time.Time    `json:"at"`
	Sessions   []string     `json:"sessions"`
	Children   []string     `json:"children,omitempty"`
	Turns      []string     `json:"turns,omitempty"`
	Remove     []forgetItem `json:"remove"`
	Keep       []forgetNote `json:"keep,omitempty"`
	Candidates []forgetNote `json:"candidates,omitempty"`
	Refusals   []string     `json:"refusals,omitempty"`
	Done       bool         `json:"done"`
}

const forgetUsage = "usage: shikii forget <session id>... [--apply]\n  Lists what the finished sessions left in this project's .shikii/ (records, work states, sources) and what it would keep.\n  With --apply it moves them into .shikii/records/forgotten/ first. Run it in the project's folder, not from the session it removes.\n"

func cmdForget(args []string) error {
	fs := flag.NewFlagSet("forget", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, forgetUsage) }
	apply := fs.Bool("apply", false, "actually move the records away; without it, only list them")
	ids, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New(strings.TrimSpace(forgetUsage))
	}
	unique, err := forgetUniqueIDs(ids)
	if err != nil {
		return err
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	r := &forgetRun{root: root, scope: scope, st: store.Store{Dir: root}, book: notebook.Book{Root: root, Scope: scope}, ids: unique}
	return r.run(*apply)
}

// forgetUniqueIDs checks the shape of each session id and drops repeats, keeping the order.
func forgetUniqueIDs(ids []string) ([]string, error) {
	seen := map[string]bool{}
	var unique []string
	for _, id := range ids {
		if !forgetIDShape.MatchString(id) {
			return nil, fmt.Errorf("%q is not a session id (letters, digits, - and _ only)", id)
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique, nil
}

func (r *forgetRun) run(apply bool) error {
	if err := r.execute(apply, ""); err != nil {
		return err
	}
	r.print(apply)
	return nil
}

// errForgetPlanChanged is why an application stops when the records are no longer what the person was shown.
var errForgetPlanChanged = errors.New("what would be removed changed after the plan was made, so nothing was changed; list it again")

// errForgetStepChanged is why an application stops before a step whose records changed after the plan was made (the plan was of them
// all, and the step read its own again under its own lock): the steps before it had already gone by the plan.
var errForgetStepChanged = errors.New("what this step would remove changed after the plan was made, so it was not run")

// execute makes the plan and, with apply, moves what it lists away. It prints nothing (over MCP stdout is the protocol's channel).
// expect, when it is not empty, is the digest of the plan a person agreed to: the plan read again under the sessions' locks must be
// that plan, or nothing is changed. The wiki's locks and the memory's are not held across the run, because other sessions' hooks wait
// only two seconds for them: each is taken by the step that needs it, which reads its records again under it and runs only when they
// are what the plan said (see runStep).
func (r *forgetRun) execute(apply bool, expect string) error {
	// Nothing is locked or written until the plan is known and nothing in it is refused: a refusal changes nothing, not even a lock file.
	if err := r.pass(false); err != nil {
		return err
	}
	if !apply {
		return nil
	}
	if err := r.refused(); err != nil {
		return err
	}
	unlock, err := r.lockSessions()
	if err != nil {
		return err
	}
	defer unlock()
	// what the sessions' locks now protect is read again: the plan below is of that
	if err := r.pass(false); err != nil {
		return err
	}
	if err := r.refused(); err != nil {
		return err
	}
	if expect != "" && r.digest() != expect {
		return errForgetPlanChanged
	}
	r.f, err = r.st.NewForgotten(time.Now(), r.short())
	if err != nil {
		return err
	}
	if err := r.savePlan(); err != nil {
		return err
	}
	if err := r.pass(true); err != nil {
		return &forgetStopped{err: err, dir: r.f.Dir, left: r.stepsLeft()}
	}
	r.plan.Done = true
	if err := r.savePlan(); err != nil {
		return &forgetStopped{err: err, dir: r.f.Dir}
	}
	return nil
}

// forgetStopped is the error of an application that stopped after it had begun: the folder that keeps what it removes was made, and the
// steps before the one that stopped have run, so something has changed. Running it again removes the rest. left names the steps that
// did not finish (the one that stopped and those after it).
type forgetStopped struct {
	err  error
	dir  string
	left []string
}

func (e *forgetStopped) Error() string {
	return fmt.Sprintf("%v; what was already removed is in %s, and running the same command again removes the rest", e.err, e.dir)
}

func (e *forgetStopped) Unwrap() error { return e.err }

// stepsLeft names the steps that did not finish in the last application: those after the ones that ran.
func (r *forgetRun) stepsLeft() []string {
	var left []string
	for i, s := range r.steps() {
		if i >= len(r.ran) {
			left = append(left, s.name)
		}
	}
	return left
}

// digest is what a person agrees to when they agree to a plan: what goes, what stays and what only might belong, in an order that
// does not depend on how the records were read. The time, the refusals (a plan with one is never applied) and Done are not in it.
func (r *forgetRun) digest() string {
	p := r.plan
	sum := sha256.Sum256([]byte(strings.Join([]string{strings.Join(p.Sessions, ","), strings.Join(p.Children, ","), strings.Join(p.Turns, ","), forgetCanon(p.Remove), forgetCanon(p.Keep), forgetCanon(p.Candidates)}, "\n--\n")))
	return hex.EncodeToString(sum[:])
}

// forgetCanon is a list of the plan (of forgetItem or of forgetNote) as one text that does not depend on the order it was found in.
func forgetCanon(v any) string {
	out := []string{}
	switch items := v.(type) {
	case []forgetItem:
		for _, item := range items {
			if item.Place == "sessions" && item.What == "lock files" {
				continue // the locks taken to apply the plan make empty lock files of their own, which is no change of what goes
			}
			b, _ := json.Marshal(item)
			out = append(out, string(b))
		}
	case []forgetNote:
		for _, item := range items {
			b, _ := json.Marshal(item)
			out = append(out, string(b))
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func (r *forgetRun) refused() error {
	if len(r.plan.Refusals) == 0 {
		return nil
	}
	return fmt.Errorf("refused, nothing was changed:\n  %s", strings.Join(r.plan.Refusals, "\n  "))
}

func (r *forgetRun) short() string {
	short := r.ids[0]
	if len(short) > 12 {
		short = short[:12]
	}
	return short
}

func (r *forgetRun) savePlan() error {
	data, err := json.MarshalIndent(r.plan, "", "  ")
	if err != nil {
		return err
	}
	return r.f.WriteFile("plan.json", append(data, '\n'))
}

// pass gathers the evidence and runs each step: with apply false it only builds the plan, with apply true it does what the plan lists
// (from the evidence read again, under the sessions' locks).
func (r *forgetRun) pass(apply bool) error {
	r.applying = apply
	if r.parts == nil {
		r.parts, r.idle = map[string]string{}, map[string]bool{}
	}
	r.ran = nil
	if err := r.gather(); err != nil {
		return err
	}
	for _, s := range r.steps() {
		if err := r.runStep(s, apply); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if apply {
			r.ran = append(r.ran, s.name)
			if err := forgetAfterStep(s.name); err != nil {
				return fmt.Errorf("%s: %w", s.name, err)
			}
		}
	}
	return nil
}

// planMark is where the lists of the plan stood before a step ran: what the step added is what stands after it.
type planMark struct{ remove, keep, candidates int }

func (r *forgetRun) mark() planMark {
	return planMark{len(r.plan.Remove), len(r.plan.Keep), len(r.plan.Candidates)}
}

// part is what the step added to the plan since the mark, in the form the digest compares, and rewind takes it out again.
func (r *forgetRun) part(m planMark) string {
	p := r.plan
	return strings.Join([]string{forgetCanon(p.Remove[m.remove:]), forgetCanon(p.Keep[m.keep:]), forgetCanon(p.Candidates[m.candidates:])}, "\n--\n")
}

func (r *forgetRun) rewind(m planMark) {
	r.plan.Remove, r.plan.Keep, r.plan.Candidates = r.plan.Remove[:m.remove], r.plan.Keep[:m.keep], r.plan.Candidates[:m.candidates]
}

// runStep runs one step. A listing runs it as it is and notes what it adds to the plan. An application takes the lock the step needs
// (the wiki's or the memory's), holds it for this step alone, and first lists again what the step would remove: that must be what
// the plan said (the part noted by the listing that came before), or the step is not run. The records the lock protects could change
// between that listing and the lock, and a plan that is not the one the person saw is not applied.
func (r *forgetRun) runStep(s forgetStep, apply bool) error {
	if !apply {
		m := r.mark()
		if err := s.run(); err != nil {
			return err
		}
		r.parts[s.name] = r.part(m)
		r.idle[s.name] = len(r.plan.Remove) == m.remove
		return nil
	}
	if r.idle[s.name] {
		// The plan removes nothing here, so there is nothing to protect with a lock, and a record that appeared since the plan was made is
		// not what the person agreed to: the step only says what it keeps, and a busy lock cannot stop a step that changes nothing.
		m := r.mark()
		r.applying = false
		err := s.run()
		r.applying = true
		r.plan.Remove = r.plan.Remove[:m.remove]
		return err
	}
	if s.lock != nil {
		unlock, err := s.lock()
		if err != nil {
			return err
		}
		defer unlock()
		m := r.mark()
		r.applying = false
		err = s.run()
		got := r.part(m)
		r.rewind(m)
		r.applying = true
		if err != nil {
			return err
		}
		if got != r.parts[s.name] {
			return errForgetStepChanged
		}
	}
	return s.run()
}

// lockSessions takes the locks of each session asked for, outside in: its work state, its planning, its record. A lock that cannot be
// taken means the session may still be running: nothing is changed.
func (r *forgetRun) lockSessions() (func(), error) {
	var held store.Unlocks
	release := func() { held.Release() }
	for _, id := range r.ids {
		base := sessionPath(r.root, id)
		if _, err := os.Lstat(base); err != nil {
			continue
		}
		for _, lock := range []struct {
			path string
			wait time.Duration
		}{{base + ".work.lock", 0}, {base + ".planning.lock", 0}, {base + ".lock", hookLockWait}} {
			unlock, ok := lockPathWithin(lock.path, lock.wait, nil)
			if !ok {
				release()
				return nil, fmt.Errorf("refused, nothing was changed: %s is locked (the session may still be running); try again when it has ended", filepath.Base(lock.path))
			}
			held.Add(unlock)
		}
	}
	return release, nil
}

// lockWiki and lockMemory are the locks of the steps that read and write the wiki's folders and the memory deliveries: the other
// writers of those files take them, in the order the wiki's own writers do.
func (r *forgetRun) lockWiki() (func(), error)   { return r.book.LockForForget() }
func (r *forgetRun) lockMemory() (func(), error) { return r.st.LockMemory(r.scope) }

func (r *forgetRun) remove(place, what string, count int, why string, names ...string) {
	if count > 0 {
		sort.Strings(names)
		r.plan.Remove = append(r.plan.Remove, forgetItem{Place: place, What: what, Count: count, Why: why, Names: names})
	}
}

func (r *forgetRun) keep(what, why string) { r.plan.Keep = append(r.plan.Keep, forgetNote{what, why}) }
func (r *forgetRun) candidate(what, why string) {
	r.plan.Candidates = append(r.plan.Candidates, forgetNote{what, why})
}

const forgetListed = 8

func (r *forgetRun) print(applied bool) {
	p := r.plan
	fmt.Fprintf(stdout, "Sessions: %s\n", strings.Join(p.Sessions, ", "))
	if len(p.Children) > 0 {
		fmt.Fprintf(stdout, "Subagents that ran under them: %s\n", strings.Join(p.Children, ", "))
	}
	verb := "Would remove"
	if applied {
		verb = "Removed"
	}
	if len(p.Remove) == 0 {
		fmt.Fprintln(stdout, verb+": nothing")
	} else {
		fmt.Fprintln(stdout, verb+":")
	}
	for _, item := range p.Remove {
		fmt.Fprintf(stdout, "  %s: %d %s (%s)\n", item.Place, item.Count, item.What, item.Why)
		for i, name := range item.Names {
			if i == forgetListed {
				fmt.Fprintf(stdout, "    ... and %d more\n", len(item.Names)-forgetListed)
				break
			}
			fmt.Fprintf(stdout, "    %s\n", name)
		}
	}
	if len(p.Keep) > 0 {
		fmt.Fprintln(stdout, "Kept:")
		for _, k := range p.Keep {
			fmt.Fprintf(stdout, "  %s: %s\n", k.What, k.Why)
		}
	}
	if len(p.Candidates) > 0 {
		fmt.Fprintln(stdout, "Candidates (only a word or a time says they may belong to the sessions; not removed):")
		for i, c := range p.Candidates {
			if i == forgetListed {
				fmt.Fprintf(stdout, "  ... and %d more\n", len(p.Candidates)-forgetListed)
				break
			}
			fmt.Fprintf(stdout, "  %s: %s\n", c.What, c.Why)
		}
	}
	for _, refusal := range p.Refusals {
		fmt.Fprintf(stdout, "Would be refused with --apply: %s\n", refusal)
	}
	switch {
	case applied:
		fmt.Fprintf(stdout, "Kept in %s (plan.json says what and why).\n", r.f.Dir)
	case len(p.Refusals) == 0:
		fmt.Fprintln(stdout, "Dry run: nothing was changed. Re-run with --apply to remove them (they are kept in .shikii/records/forgotten/ first).")
	default:
		fmt.Fprintln(stdout, "Dry run: nothing was changed.")
	}
}
