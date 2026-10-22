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

// observeFeedback はセッションの錠の中で、人の次の発言を直前の返答と結ぶ。
// 返答前の追記・別の仕事・古い Stop は結ばない。正解ラベルは一切書かない。
func observeFeedback(agent string, s, snapshot *hookSession, scope, reply string) error {
	c := linkedCompletion(s, snapshot, scope)
	if c == nil {
		return nil
	}
	return (store.Store{Dir: agent}).AppendFeedback(scope, store.Feedback{
		ID: c.TurnID, Request: c.Request, Summary: c.Summary, Reply: reply, Targets: c.Targets,
	})
}

// linkedCompletion は、人の次の発言と結んでよい直前の返答(同じ仕事・同じ依頼・期限内)。無ければ nil。
func linkedCompletion(s, snapshot *hookSession, scope string) *completedTurn {
	c := s.Completed
	if scope == "" || s.Scope != scope || s.TurnID == "" || s.TurnID != snapshot.TurnID ||
		s.Request != snapshot.Request || c == nil || c.TurnID != s.TurnID || c.Request != s.Request ||
		time.Since(c.At) > hookSessionTTL || c.At.After(time.Now()) {
		return nil
	}
	return c
}
