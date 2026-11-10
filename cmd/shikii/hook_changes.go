package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// gitPath is a path as git prints it without -z, read back as it is spelled on disk: git wraps a name with non-ASCII bytes,
// quotes, backslashes or control characters (and, in `status --porcelain`, spaces) in double quotes with C escapes ("\346\211\213").
// A name that is not quoted, or whose quoting cannot be read, is returned unchanged.
func gitPath(p string) string {
	if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' {
		return p
	}
	if u, err := strconv.Unquote(p); err == nil {
		return u
	}
	return p
}

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
