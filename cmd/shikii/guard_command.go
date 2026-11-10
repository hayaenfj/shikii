package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

func cmdGuard(args []string) error {
	usage := errors.New("usage: shikii guard list | add (JSON on standard input) | enable <id> | disable <id> | try")
	if len(args) == 0 {
		return usage
	}
	root, _, err := currentProjectStore()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return guardList(root)
	case "add":
		if len(args) != 1 {
			return usage
		}
		return guardAdd(root, stdin)
	case "enable", "disable":
		if len(args) != 2 {
			return usage
		}
		return guardSetEnabled(root, args[1], args[0] == "enable")
	case "try":
		return guardTry(root)
	}
	return usage
}

func guardList(root string) error {
	guards, err := loadGuards(root)
	if err != nil {
		return err
	}
	if len(guards) == 0 {
		fmt.Fprintln(stdout, "No checks made from a person's correction yet. To make one: shikii guard add < draft.json (id, kind, title, pairs/words/except/allow/required)")
	}
	guards = append(builtinGuards(), guards...) // ritual checks (built in; cannot be removed)
	last := map[string]guardRecord{}
	for _, r := range readGuardRecords(root) {
		last[r.Guard] = r
	}
	for _, g := range guards {
		state := "disabled (draft)"
		if g.Enabled {
			state = "enabled"
		}
		if isBuiltinGuard(g.ID) {
			state = "built in (always enabled)"
		}
		line := fmt.Sprintf("%-24s %-16s %s  %s", g.ID, g.Kind, state, g.Title)
		if r, ok := last[g.ID]; ok {
			if r.Pass {
				line += fmt.Sprintf("  (last: passed %s)", r.At.Format("01-02 15:04"))
			} else {
				line += fmt.Sprintf("  (last: stopped %s: %s)", r.At.Format("01-02 15:04"), cut(strings.ReplaceAll(r.Reason, "\n", " / "), 80)) // a Japanese reason is on its own line (bi): keep the list one line per check
			}
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}

func guardAdd(root string, in io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(in, 64<<10))
	if err != nil {
		return err
	}
	if bytes.Contains(data, []byte(ritualPlan)) || bytes.Contains(data, []byte(ritualReflect)) {
		return errors.New("plan-before-work / reflect-before-end are ids of built-in ritual checks and cannot be used in a draft")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var g guard
	if err := dec.Decode(&g); err != nil {
		return errors.New("guard add reads one JSON object (id, kind, title, reason, source, pairs/words/except/allow/required)")
	}
	g.Enabled, g.Red = false, nil // drafts are disabled. enable turns them on (after seeing the negative example stop)
	if err := g.validate(); err != nil {
		return err
	}
	if fileExists(guardPath(root, g.ID)) {
		return fmt.Errorf("%s already exists (to change it, edit the file; if it is not needed, delete it)", guardPath(root, g.ID))
	}
	if err := saveGuard(root, g); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: wrote the draft (disabled). A person checks it, then enables it: shikii guard enable %s\n", guardPath(root, g.ID), g.ID)
	return nil
}

// guardSetEnabled, for enable, confirms that the negative example stops (RED) and the positive example passes (GREEN) before enabling. If either is wrong, it does not enable.
func guardSetEnabled(root, id string, enable bool) error {
	if !guardIDRe.MatchString(id) {
		return errors.New("a guard id is lowercase letters, digits and hyphens")
	}
	if isBuiltinGuard(id) {
		return errors.New(id + " is a built-in ritual check and is always enabled (there is no setting to turn it off)")
	}
	g, err := loadGuard(guardPath(root, id))
	if err != nil {
		return err
	}
	if !enable {
		g.Enabled = false
		if err := saveGuard(root, g); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: disabled (the file stays)\n", id)
		return nil
	}
	neg, what := guardNegative(g)
	if r := evalGuard(g, neg); r.Pass {
		return fmt.Errorf("%s: not enabled because the negative example (%s) does not stop", id, what)
	} else {
		fmt.Fprintf(stdout, "RED:   the negative example \"%s\" stopped: %s\n", what, r.Reason)
	}
	if r := evalGuard(g, guardPositive(g)); !r.Pass {
		return fmt.Errorf("%s: not enabled because the positive example stops: %s", id, r.Reason)
	}
	fmt.Fprintln(stdout, "GREEN: the positive example passed")
	g.Enabled = true
	g.Red = &guardRed{At: time.Now().UTC(), Negative: what}
	if err := saveGuard(root, g); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: enabled. If it fails at the end of a turn, it blocks Stop and returns the reason\n", id)
	return nil
}

// guardTry runs the enabled checks against the current working tree (its diff from HEAD) and shows the result (it does not stop anything). It does not look at commits.
func guardTry(root string) error {
	if _, err := loadGuards(root); err != nil {
		return err // the person is at the terminal: name the file that does not load instead of skipping it
	}
	guards, err := enabledGuards(root)
	if err != nil {
		return err
	}
	if len(guards) == 0 {
		fmt.Fprintln(stdout, "No enabled checks that stop the turn")
		return nil
	}
	in := guardWorkingTreeInput(filepath.Dir(root))
	for _, g := range guards {
		r := evalGuard(g, in)
		if r.Pass {
			fmt.Fprintf(stdout, "ok   %s\n", g.ID)
		} else {
			fmt.Fprintf(stdout, "STOP %s: %s\n", g.ID, r.Reason)
		}
	}
	return nil
}
