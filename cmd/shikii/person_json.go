package main

import (
	"bytes"
	"encoding/json"
)

func writePersonJSON(path string, cfg map[string]any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return writePersonFile(path, buf.Bytes(), 0o600)
}
