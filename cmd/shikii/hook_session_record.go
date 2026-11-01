package main

import (
	"encoding/json"
	"errors"
)

const hookSessionRecordLimit = 1 << 20

// Every session writer applies the same encoded limit as the planning reader.
func encodeHookSession(s *hookSession) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) > hookSessionRecordLimit {
		return nil, errors.New("the context exceeds the session record limit (1 MiB); prepare a smaller context")
	}
	return data, nil
}
