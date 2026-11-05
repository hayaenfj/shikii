package main

import (
	"encoding/json"
	"errors"
)

const hookSessionRecordLimit = 1 << 20

var errHookSessionCapacity = errors.New("the context exceeds the session record limit (1 MiB); prepare a smaller context")

// Every session writer applies the same encoded limit as the planning reader.
func encodeHookSession(s *hookSession) ([]byte, error) {
	copy := *s
	if s.Planning != nil && s.Planning.Work != nil && s.Reasoning != nil {
		planning := *s.Planning
		work := *planning.Work
		work.Reasoning = nil
		planning.Work, copy.Planning = &work, &planning
	}
	data, err := json.MarshalIndent(&copy, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) > hookSessionRecordLimit {
		return nil, errHookSessionCapacity
	}
	return data, nil
}

func restoreWorkReasoning(s *hookSession) {
	if s.Planning != nil && s.Planning.Work != nil && s.Reasoning != nil {
		s.Planning.Work.Reasoning = cloneReasoning(s.Reasoning)
	}
}
