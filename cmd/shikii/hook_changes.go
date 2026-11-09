package main

import (
	"fmt"
	"sort"
	"strings"
)

// changedFiles compares two listings (`git ls-files --stage`: "mode sha stage\tpath") and returns, sorted by name, the paths of files whose content or mode changed, or that were added or removed.
func changedFiles(before, after string) []string {
	a, b := parseListing(before), parseListing(after)
	seen := map[string]bool{}
	var out []string
	for p, sha := range a {
		if b[p] != sha {
			out = append(out, p)
			seen[p] = true
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok && !seen[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// filesInput turns the changed file list into the question input (at most filesMax, the rest as a count). "(unknown)" if unknown, "(none)" if none.
func filesInput(changed []string, known bool) string {
	switch {
	case !known:
		return "(unknown: no git listing for this turn)"
	case len(changed) == 0:
		return "(none)"
	}
	if len(changed) <= filesMax {
		return strings.Join(changed, "\n")
	}
	return strings.Join(changed[:filesMax], "\n") + fmt.Sprintf("\n... and %d more", len(changed)-filesMax)
}
