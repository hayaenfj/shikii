package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

func readHookInput() (string, hookInput, error) {
	var in hookInput
	agent, err := agentHome()
	if err != nil {
		return "", in, err
	}
	if !fileExists(filepath.Join(agent, "decisions")) {
		return "", in, fmt.Errorf("%s: not installed (shikii hook install --yes)", agent)
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 4<<20))
	if err != nil {
		return "", in, err
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", in, fmt.Errorf("stdin is not the agent's hook JSON: %w", err)
	}
	return agent, in, nil
}
