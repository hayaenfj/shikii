package main

import (
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

// Aliases simplify recording without discarding who said each historical text.
func preparedWorkConversation(s *hookSession, w *workState, turn string) (map[string]string, map[string]any) {
	texts, context := map[string]string{}, map[string]any{}
	previous := ""
	if g := s.Planning.Grouping; g != nil && g.Previous != nil {
		previous = g.Previous.TurnID
	}
	add := func(ref, key, role, id, relative string, limit int) map[string]any {
		texts[ref], w.Keys[ref] = s.Planning.Sources[key], key
		_, idErr := hex.DecodeString(id)
		knownTurn := len(id) == 16 && idErr == nil
		item := map[string]any{"role": role, "source_key": key, "relative": relative, "text_status": "planning_excerpt", "limit_bytes": limit, "turn_known": knownTurn}
		if knownTurn {
			item["turn_id"] = id
		} else if id != "" {
			item["legacy_turn_reference"] = id
		}
		context[ref] = item
		return item
	}
	for _, p := range [][5]string{{"current", "current_user", "user", turn, "current"}, {"previous", "previous_user", "user", previous, "previous"}, {"previous_answer", "previous_assistant", "assistant", previous, "previous"}} {
		if s.Planning.Sources[p[1]] != "" {
			add(p[0], p[1], p[2], p[3], p[4], 4000)
		}
	}
	keys, seen, order := []string{}, map[string]bool{}, map[string]int{}
	for _, old := range s.Planning.Recent {
		for _, role := range []string{"user", "assistant"} {
			key := "history:" + old.ID + ":" + role
			if s.Planning.Sources[key] != "" && !seen[key] {
				order[key], seen[key] = len(keys), true
				keys = append(keys, key)
			}
		}
	}
	unknown := []string{}
	for key := range s.Planning.Sources {
		if strings.HasPrefix(key, "history:") && !seen[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	keys = append(keys, unknown...)
	for i, key := range keys {
		parts := strings.Split(key, ":")
		if len(parts) != 3 {
			continue
		}
		ref := "history" + strconv.Itoa(i+1)
		item := add(ref, key, parts[2], parts[1], "history", 1200)
		position, known := order[key]
		item["order_known"] = known
		if known {
			item["order"] = position
		}
	}
	return texts, context
}

// RequestText retains the human request separately from bounded planning quotes.
func preparedWorkTask(s *hookSession, w *workState, req workRequest) (string, map[string]any, error) {
	task := w.Current
	context := map[string]any{"role": "user", "scope": "stored_request_including_joined_messages", "text_status": "stored_request", "quote_ref": "current"}
	if w.Standalone {
		task = req.Request
		if task == "" {
			task = req.Query
		}
		context = map[string]any{"role": "agent", "scope": "agent_supplied_task", "text_status": "agent_supplied"}
	} else if s.RequestText != "" {
		task = cut(s.RequestText, store.RecordTextLimit)
		if len(s.RequestText) > store.RecordTextLimit {
			context["text_status"] = "stored_request_truncated"
			context["next_action"] = "Read the original human message in the host conversation before interpreting missing conditions; stored text cannot reconstruct the omitted tail."
		}
	} else {
		context["text_status"] = "legacy_excerpt_only"
		context["next_action"] = "The full request was not retained here. Read the original human message in the host conversation before interpreting missing conditions."
	}
	context["provided_bytes"] = len(task)
	context["quote_notice"] = "Conversation references quote bounded planning excerpts. Task text supplies context, including joined human messages; it is not a new quotation reference or permission."
	if workContextSize(map[string]any{"task": task, "task_context": context}) > workSourceBudget {
		return "", nil, errors.New("the stored request exceeds the preparation budget; read the original human message in the host conversation and use the individual planning and recording operations")
	}
	return task, context, nil
}
