package main

import (
	"os"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// guardTurn is the stretch of a session a slash command opened (/commit and the like). A slash command is not a request, so the
// stretch has no questions, no delivered lessons and no routine check; it exists only so the person's guards can tell what
// changed and what was committed since the slash command, and stop that turn once.
type guardTurn struct {
	ID     string    `json:"id"`                // the stretch's own id: the turn_id of its lines in records/guards.jsonl
	Scope  string    `json:"scope,omitempty"`   // the repository the listing was taken in
	Head   string    `json:"head,omitempty"`    // HEAD when the slash command came
	HeadAt time.Time `json:"head_at,omitempty"` // when it came. Commits before it are not looked at
}

// sessionGuardFilesPath is the working tree listing kept when a slash command opened a stretch. The request's own listing
// (sessionFilesPath) is left alone: the request's Stop and its watch still compare with it.
func sessionGuardFilesPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".guard.files"
}

// guardBaseline is what the person's guards compare the present with: the kept request's listing, HEAD and turn. While a stretch
// a slash command opened is current, the turn is the stretch's own (its guards may stop it once each, and its records are its
// own). guardBaselineOf gives the start a command about to run is checked against (a refused command, a publication): the
// request's, as before slash commands had a stretch, so a /push is still refused while the request's work fails a guard. Only
// a session without a kept request (the slash command came first, or in another repository) is compared with the listing and
// HEAD of the slash command itself. The start of the stretch's own Stop is chosen in guardTurnStop.
type guardBaseline struct {
	Turn   string    // the turn_id written to records/guards.jsonl
	Scope  string    // the repository the listing was taken in
	Head   string    // HEAD at the start ("" when it is not known)
	HeadAt time.Time // the start. Commits before it are not looked at
	Files  string    // the kept listing
	Header string    // the first line a listing of this start carries
}

func guardBaselineOf(root, session string, s *hookSession) guardBaseline {
	if g := s.GuardTurn; g != nil {
		if s.TurnID != "" && s.Scope == g.Scope {
			if _, err := os.Stat(sessionFilesPath(root, session)); err == nil {
				base := guardBaseline{Turn: g.ID, Scope: s.Scope, Head: s.Head, HeadAt: s.HeadAt, Files: sessionFilesPath(root, session), Header: listingHeader(s.Request)}
				if base.Head == "" { // the request came before the repository's first commit: commits are read from the slash command's HEAD
					base.Head, base.HeadAt = g.Head, g.HeadAt
				}
				return base
			}
		}
		return stretchStart(root, session, g)
	}
	return guardBaseline{Turn: s.TurnID, Scope: s.Scope, Head: s.Head, HeadAt: s.HeadAt, Files: sessionFilesPath(root, session), Header: listingHeader(s.Request)}
}

// stretchStart is the start of the stretch itself: the listing and HEAD kept at its slash command.
func stretchStart(root, session string, g *guardTurn) guardBaseline {
	return guardBaseline{Turn: g.ID, Scope: g.Scope, Head: g.Head, HeadAt: g.HeadAt, Files: sessionGuardFilesPath(root, session), Header: listingHeader(g.ID)}
}

// openGuardTurn is all the request hook does for a slash command: it keeps the working tree listing and HEAD of this moment as
// a new stretch, for the guards alone. The kept request, whether it was judged and its watch are not touched, and nothing is
// output. Where no guard is enabled there is nothing to compare for, so the session is left exactly as it is (a stretch opened
// while a guard was still enabled is dropped).
func openGuardTurn(agent string, in *hookInput, snapshot *hookSession) []func(*hookSession) {
	if guards, _ := enabledGuards(agent); len(guards) == 0 {
		if snapshot.GuardTurn == nil {
			return nil
		}
		return []func(*hookSession){func(s *hookSession) { closeGuardTurn(agent, in.SessionID, s) }}
	}
	_, listing, listed, _, scope := treeStateWithScope(in.CWD)
	turn := &guardTurn{ID: store.NewTraceID(), Scope: scope, Head: gitHead(in.CWD), HeadAt: time.Now()}
	return []func(*hookSession){func(s *hookSession) {
		s.GuardTurn = turn
		p := sessionGuardFilesPath(agent, in.SessionID)
		if !listed {
			_ = os.Remove(p) // outside git (or too slow, or too large): the changed files stay unknown, and unknown passes
			return
		}
		if err := writeAtomic(p, []byte(listingHeader(turn.ID)+listing)); err != nil {
			hookLog("guard: the listing for the slash command's turn was not kept: " + err.Error())
			_ = os.Remove(p)
		}
	}}
}

// closeGuardTurn ends the stretch a slash command opened (inside the session lock): the person's next request starts its own turn.
func closeGuardTurn(agent, session string, s *hookSession) {
	if s.GuardTurn == nil {
		return
	}
	s.GuardTurn = nil
	_ = os.Remove(sessionGuardFilesPath(agent, session))
}

// guardTurnStop is the Stop of a stretch a slash command opened. Only the person's guards run; each guard stops the stretch at
// most once, as in a request's turn. The stretch is compared with its own start (the listing and HEAD at its slash command):
// what the person did between the request and the slash command (a pull, a checkout, an edit) is not the stretch's. One case
// keeps the request's start: the request's turn never reached its end (it was not judged: the person interrupted it, or a
// Stop was stopped and never came back) and the stretch changed or committed something. That stretch is finishing the
// request's work, so a line the request's turn added is still an added line when /commit commits it. The kept request is not
// settled, checked by the routine or judged here, and the session record is not changed.
func guardTurnStop(agent string, in *hookInput, snapshot *hookSession) (hookOut, error) {
	base := guardBaselineOf(agent, in.SessionID, snapshot)
	_, listing, listed, _, _ := treeStateWithScope(in.CWD)
	if g := snapshot.GuardTurn; g != nil {
		own := stretchStart(agent, in.SessionID, g)
		idle := false
		if b, err := os.ReadFile(own.Files); listed && err == nil && strings.HasPrefix(string(b), own.Header) {
			idle = len(changedFiles(string(b), listing)) == 0 && gitHead(in.CWD) == g.Head
		}
		if snapshot.Judged || idle {
			base = own
		}
	}
	var changed []string
	var before []byte
	known := false
	if listed {
		if b, err := os.ReadFile(base.Files); err == nil && strings.HasPrefix(string(b), base.Header) {
			before, changed, known = b, changedFiles(string(b), listing), true
		}
	}
	return guardStop(agent, in, base, string(before), changed, known, false)
}
