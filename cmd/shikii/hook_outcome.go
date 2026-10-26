package main

// Truths that come from outcomes (source outcome). No AI interpretation is used; only the working tree and the shape of the person's messages are looked at.
// prompt-kind: did the working tree change during the request (settlePending).
// changes-in-scope: were the files changed in that turn reverted before the person's next request (reverted = outside the scope; not reverted and still there by the request after next = inside the scope, partial weight).
// done-as-asked: is the person's next message a restatement of the same request (restatement = not finished; a clearly different request = finished, partial weight). Short replies and the in-between are not labeled.
// Observation is a proxy measure and does not get the same weight as the person's truths (only the clearly bad side gets weight 1).

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// Fixed rules (no options).
const (
	outcomeKeptWeight  = 0.5 // not reverted, or moved on to another request (a sign it was not bad, not proof it was good)
	outcomeRepeatAbove = 0.6 // at or above this similarity to the previous request (Dice on character pairs) it is a restatement
	outcomeMovedBelow  = 0.2 // at or below this it is a different request
	// a message shorter than this does not show the person moved on: an answer, a choice ("2") or a correction ("wrong", "no") is short and
	// dissimilar too. Grouping (the acknowledgement list, continues_previous) never decides a truth: the agent being judged must not erase a
	// restatement or turn its own grouping into a "done" truth (0.9.3 audit 3)
	outcomeMovedMinRunes = 40
	scopeWatchTurns      = 2   // number of the person's requests during which changed files are watched (whether they were reverted during the next request is decided at the one after)
	scopeWatchFiles      = 200 // upper limit of watched files (the rest are not looked at)
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

// requestGrouping is how this turn's message is grouped with the previous request (0.9.3). The hook decides provisionally with the
// acknowledgement list; when the agent organizes the turn, its continues_previous (hook plan submit) corrects it in either direction.
// Settling the previous request's outcome from this message waits until the grouping is final: the organizing, this turn's Stop,
// or the next request, whichever comes first. The organizing is the agent's hypothesis: it only decides the grouping, never a truth.
type requestGrouping struct {
	Joined       bool           `json:"joined"`            // attached to the previous request (now)
	Settled      bool           `json:"settled,omitempty"` // final: a later organizing no longer moves it
	Alone        string         `json:"alone"`             // the request when it stands alone
	AloneText    string         `json:"alone_text,omitempty"`
	Together     string         `json:"together"` // the request when attached to the previous one
	TogetherText string         `json:"together_text,omitempty"`
	Reply        string         `json:"reply"`              // this message (settles the previous outcome when it stands alone)
	Previous     *completedTurn `json:"previous,omitempty"` // the previous request's reply, kept until the grouping is final
}

// groupingOf is this turn's grouping, or nil.
func groupingOf(s *hookSession) *requestGrouping {
	if s.Planning == nil {
		return nil
	}
	return s.Planning.Grouping
}

// settleGrouping makes the grouping final and settles the previous request's outcome from this message when it stands alone (inside the lock).
func settleGrouping(agent string, s *hookSession) {
	if s.Planning == nil || s.Planning.Grouping == nil || s.Planning.Grouping.Settled {
		return
	}
	g := s.Planning.Grouping
	if err := settleDone(agent, g.Previous, g.Reply); err != nil {
		hookLog("outcome: " + err.Error())
	}
	g.Settled, g.Previous = true, nil
}

// deferralStale is how long a deferred outcome waits for its organizing or Stop. Past it (or once the record shows the session moved on),
// any later hook of the project or a reflect query settles it with the provisional grouping: a session closed or killed mid-turn never
// sends its Stop or next request. A fixed rule, not an option.
const deferralStale = 30 * time.Minute

// deferralMarkerPath is the empty marker <id>.deferred kept next to a session record while its grouping still waits with an outcome to settle.
// The markers are the small index of open deferrals: a sweep reads only the records that have one.
func deferralMarkerPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".deferred"
}

// openDeferral reports whether the record's grouping still waits with a previous outcome to settle.
func openDeferral(s *hookSession) bool {
	g := groupingOf(s)
	return g != nil && !g.Settled && g.Previous != nil
}

// deferralDue reports whether an open deferral no longer waits for its own session: older than deferralStale, or the record moved on
// (the turn was judged, or the record is on another turn).
func deferralDue(s *hookSession, now time.Time) bool {
	return now.Sub(s.Planning.At) >= deferralStale || s.Judged || s.TurnID != s.Planning.TurnID
}

// syncDeferral keeps the marker in step with the record (inside the session's lock, before the record is saved).
func syncDeferral(agent, id string, s *hookSession) {
	p := deferralMarkerPath(agent, id)
	if !openDeferral(s) {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			hookLog("deferral: " + err.Error())
		}
		return
	}
	if _, err := os.Lstat(p); err == nil {
		return
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		hookLog("deferral: " + err.Error())
	}
}

// sweepPeeked runs between a sweep's unlocked read and its lock (tests make the session settle itself there). Only tests replace it.
var sweepPeeked = func(id string) {}

// sweepDeferrals settles other sessions' deferrals that no longer wait (deferralDue) with the provisional grouping, once each.
// It runs outside any session lock and takes one session's lock at a time without waiting (a busy session is left for the next sweep),
// so it never holds two locks. The record is read again inside the lock: a deferral its own session settled meanwhile is left alone,
// and its own session later finds the grouping settled and adds nothing. except is the calling session (its own path settles it).
func sweepDeferrals(agent, except string) {
	entries, err := os.ReadDir(filepath.Join(agent, "sessions"))
	if err != nil {
		return
	}
	skip := ""
	if except != "" {
		skip = filepath.Base(deferralMarkerPath(agent, except))
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".deferred")
		if !ok || id == "" || e.Name() == skip || !e.Type().IsRegular() || filepath.Base(deferralMarkerPath(agent, id)) != e.Name() {
			continue
		}
		peek := loadSession(agent, id)
		if openDeferral(peek) && !deferralDue(peek, time.Now()) {
			continue // still waiting within the time: not even locked
		}
		sweepPeeked(id)
		unlock, ok := lockSessionWithin(agent, id, 0)
		if !ok {
			continue
		}
		s := loadSession(agent, id) // read again inside the lock: another sweep or the session itself may have settled it meanwhile
		if openDeferral(s) && deferralDue(s, time.Now()) {
			settleGrouping(agent, s)
			if err := saveSession(agent, id, s); err != nil {
				hookLog("deferral: " + err.Error())
				unlock()
				continue
			}
		}
		syncDeferral(agent, id, s)
		unlock()
	}
}

// regroup applies the agent's continues_previous to this turn (inside the lock, for the current turn only) and makes the grouping final.
// It returns the request before the change ("" when unchanged), so the caller can move the listing header with it.
// Ignored once the grouping is final (the Stop already judged this turn with it) or when there is no previous request.
func regroup(agent string, s *hookSession, continues bool) string {
	if s.Planning == nil || s.Planning.Grouping == nil || s.Planning.Grouping.Settled || s.Judged {
		return ""
	}
	g := s.Planning.Grouping
	old := ""
	if g.Joined != continues && s.Request == map[bool]string{true: g.Together, false: g.Alone}[g.Joined] {
		old = s.Request
		g.Joined = continues
		if continues {
			s.Request, s.RequestText = g.Together, g.TogetherText
		} else {
			s.Request, s.RequestText = g.Alone, g.AloneText
		}
	}
	settleGrouping(agent, s)
	if old == s.Request {
		return ""
	}
	return old
}

// settleDone attaches an outcome truth to the previous request's done-as-asked from the shape of the person's next message (inside the lock, at the request boundary).
// Only the person's message decides it, by fixed rules: a bare acknowledgement ("ok", "go on"), a short message and the in-between
// (neither similar nor dissimilar) are not labeled. How the message is grouped (continues_previous) does not change it.
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
	case sim <= outcomeMovedBelow && utf8.RuneCountInString(strings.TrimSpace(reply)) >= outcomeMovedMinRunes:
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
