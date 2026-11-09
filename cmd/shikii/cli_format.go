package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

func fmtCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// parseSpecPath accepts both "<spec.yaml> flags..." and "flags... <spec.yaml>".
func parseSpecPath(fs *flag.FlagSet, args []string) (string, error) {
	pos, err := parseMixed(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", fmt.Errorf("spec path is required")
	}
	return pos[0], nil
}

// parseMixed lets flags come before or after positional arguments (`shikii status spec.yaml --store DIR` and
// `shikii status --store DIR spec.yaml` are the same). Go's flag stops at the first positional argument, so keep reading the rest.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			if err := resolveStoreFlag(fs); err != nil {
				return nil, err
			}
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func fmtDist(m map[string]float64) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// outcomeCheckLine is the status line on how often an outcome's truths agreed with a person who checked them, by the outcome's
// answer (0.11): each answer is measured, used or not, on its own.
func outcomeCheckLine(r store.OutcomeReport, act float64) string {
	truths := make([]string, 0, len(r.Sides))
	for truth := range r.Sides {
		truths = append(truths, truth)
	}
	sort.Strings(truths)
	parts := make([]string, 0, len(truths))
	for _, truth := range truths {
		c := r.Sides[truth]
		state := fmt.Sprintf("still measuring (%s shown)", map[bool]string{true: "all", false: fmt.Sprintf("1 in %d", store.OutcomeCheckEvery)}[c.Truths <= store.OutcomeCheckAll])
		switch {
		case c.Checked == 0:
			state = "none checked yet; " + state
		case c.OutcomeCheck.Untrusted(act):
			state = fmt.Sprintf("below act %.2f, not used to draw lines", act)
		case c.OutcomeCheck.Settled(act):
			state = fmt.Sprintf("at or above act %.2f, used; no more checks", act)
		}
		parts = append(parts, fmt.Sprintf("%q of %d: checked %d, agreed %d (95%% %.2f-%.2f), %s", truth, c.Truths, c.Checked, c.Agree, c.Low, c.High, state))
	}
	return "  outcome truths checked by a person, by the outcome's answer: " + strings.Join(parts, "; ")
}
