package main

import (
	"strconv"
	"strings"
)

const sharedReadingPrefix = "Read /reading_notices/"
const sharedReadingSuffix = " (shared reading notice)."

// Share only host-authored reading notices, never article text or quotations.
// Rebuild before each size check: budget reduction may replace whole entries.
// References and their verbatim notices always travel in the same response.
func shareWorkExplanations(reply map[string]any, size func(any) int) {
	entries, _ := reply["knowledge"].([]any)
	old, _ := reply["reading_notices"].(map[string]any)
	for _, raw := range entries {
		e, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		notice, _ := e["reading_notice"].(string)
		if strings.HasPrefix(notice, sharedReadingPrefix) && strings.HasSuffix(notice, sharedReadingSuffix) {
			id := strings.TrimSuffix(strings.TrimPrefix(notice, sharedReadingPrefix), sharedReadingSuffix)
			if original, ok := old[id].(string); ok {
				e["reading_notice"] = original
			}
		}
	}
	delete(reply, "reading_notices")
	counts := map[string]int{}
	for _, raw := range entries {
		if e, ok := raw.(map[string]any); ok {
			if notice, ok := e["reading_notice"].(string); ok && len(notice) >= 128 {
				counts[notice]++
			}
		}
	}
	pool, ids := map[string]any{}, map[string]string{}
	before := size(reply)
	for _, raw := range entries {
		e, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		notice, _ := e["reading_notice"].(string)
		if counts[notice] < 2 {
			continue
		}
		id := ids[notice]
		if id == "" {
			id = "notice" + strconv.Itoa(len(ids)+1)
			ids[notice], pool[id] = id, notice
		}
		e["reading_notice"] = sharedReadingPrefix + id + sharedReadingSuffix
	}
	if len(pool) == 0 {
		return
	}
	reply["reading_notices"] = pool
	if size(reply) < before {
		return
	}
	// Transport envelopes can outweigh small savings. Keep the original shape
	// whenever the actual response, including MCP text, would not shrink.
	for _, raw := range entries {
		if e, ok := raw.(map[string]any); ok {
			for notice, id := range ids {
				if e["reading_notice"] == sharedReadingPrefix+id+sharedReadingSuffix {
					e["reading_notice"] = notice
					break
				}
			}
		}
	}
	delete(reply, "reading_notices")
}
