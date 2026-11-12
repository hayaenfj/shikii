package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// endedTurnForTest is the turn of the record when the session's turn on it ended, and "" when it goes on: the record's request is
// the session's last one (sessionLatest) and its turn did not end. A request that is no longer the last one is ended. It reads the
// state as it is now.
func (s *hookSession) endedTurnForTest() string {
	if s.TurnID == "" {
		return ""
	}
	common, err := agentHome()
	if err != nil {
		return s.TurnID
	}
	paths, _ := filepath.Glob(filepath.Join(common, "latest", "*.json"))
	for _, p := range paths {
		var latest sessionLatest
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &latest) == nil && latest.Turn == s.TurnID {
			if latest.Ended {
				return s.TurnID
			}
			return ""
		}
	}
	return s.TurnID
}
