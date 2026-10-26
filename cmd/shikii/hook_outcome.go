package main

// Truths that come from outcomes (source outcome). No AI interpretation is used; only the working tree and the shape of the person's messages are looked at.
// prompt-kind: did the working tree change during the request (settlePending).
// changes-in-scope: were the files changed in that turn reverted before the person's next request (reverted = outside the scope; not reverted and still there by the request after next = inside the scope, partial weight).
// done-as-asked: is the person's next message a restatement of the same request (restatement = not finished; a clearly different request = finished, partial weight). Short replies and the in-between are not labeled.
// Observation is a proxy measure and does not get the same weight as the person's truths (only the clearly bad side gets weight 1).

import (
	"strings"
	"unicode"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// Fixed rules (no options).
const (
	outcomeKeptWeight  = 0.5 // not reverted, or moved on to another request (a sign it was not bad, not proof it was good)
	outcomeRepeatAbove = 0.6 // at or above this similarity to the previous request (Dice on character pairs) it is a restatement
	outcomeMovedBelow  = 0.2 // at or below this it is a different request
	scopeWatchTurns    = 2   // number of the person's requests during which changed files are watched (whether they were reverted during the next request is decided at the one after)
	scopeWatchFiles    = 200 // upper limit of watched files (the rest are not looked at)
)

// outcomeSpecs are the decisions that get truths from outcomes. Even when unsure (ask) they do not wait for the person's answer (the outcome answers).
var outcomeSpecs = map[string]bool{"prompt-kind": true, "changes-in-scope": true, "done-as-asked": true, "wiki-place": true, "wiki-conflict": true}

// waitsForPersonFor reports whether this decision waits for the person's answer.
// Decisions without a line do nothing automatically, so there is nothing to spot-check. Decisions that get truths from outcomes wait for the outcome even when unsure.
func waitsForPersonFor(specID string, d *decide.Decision) bool {
	if d.Band == decide.Act {
		return d.IsCalibrated && store.AuditSample(d.TraceID)
	}
	return d.Band == decide.Ask && !outcomeSpecs[specID]
}

// scopeWatch is a changes-in-scope decision with the pre-turn state of the files changed in that turn ("mode sha"; "" for files that did not exist).
type scopeWatch struct {
	Trace  string            `json:"trace"`
	Before map[string]string `json:"before"`
	Seen   int               `json:"seen,omitempty"` // number of the person's requests that came after the decision
}

// newScopeWatch keeps the pre-turn state of the changed files.
func newScopeWatch(trace, before string, changed []string) scopeWatch {
	prior := parseListing(before)
	w := scopeWatch{Trace: trace, Before: map[string]string{}}
	for i, p := range changed {
		if i >= scopeWatchFiles {
			break
		}
		w.Before[p] = prior[p]
	}
	return w
}

// reverted reports whether any kept file returned to its pre-turn state (for a file that did not exist: whether it was removed).
func (w scopeWatch) reverted(now map[string]string) bool {
	for p, before := range w.Before {
		if now[p] == before {
			return true
		}
	}
	return false
}

// settleScope attaches outcome truths to the watched changes-in-scope decisions (inside the lock). If listed is false (outside git or too slow), nothing is compared.
// boundary is the boundary of the person's request: the watch count goes up by one, and once it reaches scopeWatchTurns without a revert, "yes" (partial weight).
// Reverts are only looked at after at least one request from the person came after the decision (what the agent reverted itself while continuing the same request does not count).
func settleScope(agent string, s *hookSession, listing string, listed, boundary bool) error {
	if len(s.Watch) == 0 {
		return nil
	}
	st := store.Store{Dir: agent}
	now := parseListing(listing)
	var firstErr error
	var rest []scopeWatch
	for _, w := range s.Watch {
		if listed && w.Seen >= 1 && w.reverted(now) {
			if err := labelTrace(st, "changes-in-scope", w.Trace, "no", 1, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if boundary {
			w.Seen++
		}
		if w.Seen >= scopeWatchTurns {
			if listed {
				if err := labelTrace(st, "changes-in-scope", w.Trace, "yes", outcomeKeptWeight, "outcome"); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			continue // a watch that could not be compared is dropped without a label
		}
		rest = append(rest, w)
	}
	s.Watch = rest
	return firstErr
}

// settleDone attaches an outcome truth to the previous request's done-as-asked from the shape of the person's next message (inside the lock, at the request boundary).
// Short replies ("ok", "go on") and the in-between (neither similar nor dissimilar) are not labeled.
func settleDone(agent string, c *completedTurn, reply string) error {
	if c == nil || isAckReply(reply) {
		return nil
	}
	trace := ""
	for _, t := range c.Targets {
		if t.Spec == "done-as-asked" {
			trace = t.TraceID
		}
	}
	if trace == "" {
		return nil
	}
	st := store.Store{Dir: agent}
	switch sim := textSimilarity(c.Request, reply); {
	case sim >= outcomeRepeatAbove:
		return labelTrace(st, "done-as-asked", trace, "no", 1, "outcome")
	case sim <= outcomeMovedBelow:
		return labelTrace(st, "done-as-asked", trace, "yes", outcomeKeptWeight, "outcome")
	}
	return nil
}

// textSimilarity is how similar two texts are (the Dice coefficient on character pairs, 0 to 1). Ignores spaces, symbols and case. Language-independent.
func textSimilarity(a, b string) float64 {
	grams := func(s string) map[string]int {
		var rs []rune
		for _, r := range strings.ToLower(s) {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				rs = append(rs, r)
			}
		}
		m := map[string]int{}
		for i := 0; i+1 < len(rs); i++ {
			m[string(rs[i:i+2])]++
		}
		return m
	}
	ga, gb := grams(a), grams(b)
	na, nb := 0, 0
	for _, n := range ga {
		na += n
	}
	for _, n := range gb {
		nb += n
	}
	if na+nb == 0 {
		return 0
	}
	common := 0
	for g, n := range ga {
		common += min(n, gb[g])
	}
	return 2 * float64(common) / float64(na+nb)
}

// parseListing turns a `git ls-files --stage` listing into path → "mode sha".
func parseListing(listing string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(listing, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(line[:tab])
		if len(meta) < 2 {
			continue
		}
		m[line[tab+1:]] = meta[0] + " " + meta[1] // mode and blob id (a chmod-only change also counts as "changed")
	}
	return m
}
