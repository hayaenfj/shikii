package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// sessionLatestForTest is the state of the session whose last request is turn (known false when no state names it now).
func sessionLatestForTest(turn string) (sessionLatest, bool) {
	common, err := agentHome()
	if err != nil || turn == "" {
		return sessionLatest{}, false
	}
	paths, _ := filepath.Glob(filepath.Join(common, "latest", "*.json"))
	for _, p := range paths {
		var latest sessionLatest
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &latest) == nil && latest.Turn == turn {
			return latest, true
		}
	}
	return sessionLatest{}, false
}

// endedTurnForTest is the turn of the record when a Stop ended the session's turn on it, and "" otherwise: when the agent works on
// it, and when it is no longer the session's last request (another request replaced it, whether a Stop ended it or not). It reads
// the state as it is now.
func (s *hookSession) endedTurnForTest() string {
	if latest, ok := sessionLatestForTest(s.TurnID); ok && latest.Ended {
		return s.TurnID
	}
	return ""
}

// goesOnForTest reports whether the agent's turn goes on with the record's request: it is the session's last request and no Stop
// ended it (what a message the person sends next in its project takes over, and what a notice reopened). A request another one
// replaced never goes on. It reads the state as it is now.
func (s *hookSession) goesOnForTest() bool {
	latest, ok := sessionLatestForTest(s.TurnID)
	return ok && !latest.Ended
}
