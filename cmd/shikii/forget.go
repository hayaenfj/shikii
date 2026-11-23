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

// forgetQuiet is how long a session's record must have been left alone before forget takes it: a session may still be running.
const forgetQuiet = 10 * time.Minute

// forgetSessionVariables are the environment variables an agent passes the id of its own session in: forget never removes that one.
var forgetSessionVariables = []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "OPENCODE_SESSION_ID"}

// forgetAfterStep runs after each step of an application (a test makes one fail to stop a run in the middle).
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

// execute makes the plan and, with apply, moves what it lists away. It prints nothing (over MCP stdout is the protocol's channel).
// expect, when it is not empty, is the digest of the plan a person agreed to: the plan read again under the locks must be that
// plan, or nothing is changed.
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
	unlock, err := r.lockAll()
	if err != nil {
		return err
	}
	defer unlock()
	// what the locks now protect is read again: the plan below is of that
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
		return fmt.Errorf("%w; what was already removed is in %s, and running the same command again removes the rest", err, r.f.Dir)
	}
	r.plan.Done = true
	return r.savePlan()
}

// digest is what a person agrees to when they agree to a plan: what goes, what stays and what only might belong, in an order that
// does not depend on how the records were read. The time, the refusals (a plan with one is never applied) and Done are not in it.
func (r *forgetRun) digest() string {
	p := r.plan
	canon := func(v any) string {
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
	sum := sha256.Sum256([]byte(strings.Join([]string{strings.Join(p.Sessions, ","), strings.Join(p.Children, ","), strings.Join(p.Turns, ","), canon(p.Remove), canon(p.Keep), canon(p.Candidates)}, "\n--\n")))
	return hex.EncodeToString(sum[:])
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
// (from the evidence read again, under the locks).
func (r *forgetRun) pass(apply bool) error {
	r.applying = apply
	if err := r.gather(); err != nil {
		return err
	}
	for _, s := range r.steps() {
		if err := s.run(); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if apply {
			if err := forgetAfterStep(s.name); err != nil {
				return fmt.Errorf("%s: %w", s.name, err)
			}
		}
	}
	return nil
}

// lockAll takes every lock the removal needs, outside in: each session's own locks (its work state, its planning, its record), the
// wiki's folders, the memory deliveries. A lock that cannot be taken means the session may still be running: nothing is changed.
func (r *forgetRun) lockAll() (func(), error) {
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
	for _, take := range []func() (func(), error){r.book.LockForForget, func() (func(), error) { return r.st.LockMemory(r.scope) }} {
		unlock, err := take()
		if err != nil {
			release()
			return nil, fmt.Errorf("refused, nothing was changed: %w", err)
		}
		held.Add(unlock)
	}
	return release, nil
}

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
