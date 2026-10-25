package main

import (
	"path/filepath"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

type completedTurn struct {
	TurnID  string               `json:"turn_id"`
	Request string               `json:"request"`
	Summary string               `json:"summary"`
	At      time.Time            `json:"at"`
	Targets []store.ReviewTarget `json:"targets,omitempty"`
}

func feedbackScope(path string) string {
	path, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(path)
}

// observeFeedback, inside the session lock, links the person's next message to the reply just before it.
// Additions before the reply, other work and old Stops are not linked. No truth label is ever written.
func observeFeedback(agent string, s, snapshot *hookSession, scope, reply string) error {
	c := linkedCompletion(s, snapshot, scope)
	if c == nil {
		return nil
	}
	return (store.Store{Dir: agent}).AppendFeedback(scope, store.Feedback{
		ID: c.TurnID, Request: c.Request, Summary: c.Summary, Reply: reply, Targets: c.Targets,
	})
}

// linkedCompletion is the previous reply that may be linked to the person's next message (same work, same request, within the time limit). nil if none.
func linkedCompletion(s, snapshot *hookSession, scope string) *completedTurn {
	c := s.Completed
	if scope == "" || s.Scope != scope || s.TurnID == "" || s.TurnID != snapshot.TurnID ||
		s.Request != snapshot.Request || c == nil || c.TurnID != s.TurnID || c.Request != s.Request ||
		time.Since(c.At) > hookSessionTTL || c.At.After(time.Now()) {
		return nil
	}
	return c
}
