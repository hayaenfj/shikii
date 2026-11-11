package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sort"
)

// A reply an agent reads is written in reading order. encoding/json writes a map's keys alphabetically, so a prepare reply began
// with agent_supplied_task and attention, and receipt (which every later call needs) came after the lessons, the candidates and
// the pending conversations; inside a lesson, title and summary came after the long orientation. An agent's tool shows a bounded
// part of a command's output and cuts the middle of a longer one, so what the agent needs to act must come first.
//
// replyHead is the start of the top object, for the members that are present: the handle and the outcome (receipt; complete,
// errors, next_action), what was left out (output), what to do (task, how_to_record, planning_required, reasoning), then the
// lessons and what is pending. Every other member follows from small to large, and so do the members of every object inside.
// One rule for all replies, with no list per reply to keep in step.
var replyHead = []string{"receipt", "complete", "errors", "next_action", "output", "task", "how_to_record", "planning_required", "reasoning", "knowledge", "pending"}

// orderedReply encodes a reply compactly with its members in reading order. The values and the number of bytes are exactly those
// of json.Marshal (the response budget counts them); only the order of the members of objects differs.
func orderedReply(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // numbers keep their digits
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return orderReplyValue(tree, true)
}

func orderReplyValue(value any, top bool) ([]byte, error) {
	switch value := value.(type) {
	case map[string]any:
		type member struct {
			key  string
			raw  []byte
			rank int
		}
		members := make([]member, 0, len(value))
		for key, child := range value {
			raw, err := orderReplyValue(child, false)
			if err != nil {
				return nil, err
			}
			rank := len(replyHead)
			if top {
				if i := slices.Index(replyHead, key); i >= 0 {
					rank = i
				}
			}
			members = append(members, member{key: key, raw: raw, rank: rank})
		}
		sort.Slice(members, func(i, j int) bool {
			a, b := members[i], members[j]
			if a.rank != b.rank {
				return a.rank < b.rank
			}
			if len(a.raw) != len(b.raw) {
				return len(a.raw) < len(b.raw)
			}
			return a.key < b.key
		})
		var out bytes.Buffer
		out.WriteByte('{')
		for i, m := range members {
			if i > 0 {
				out.WriteByte(',')
			}
			key, err := json.Marshal(m.key)
			if err != nil {
				return nil, err
			}
			out.Write(key)
			out.WriteByte(':')
			out.Write(m.raw)
		}
		out.WriteByte('}')
		return out.Bytes(), nil
	case []any:
		var out bytes.Buffer
		out.WriteByte('[')
		for i, element := range value {
			if i > 0 {
				out.WriteByte(',')
			}
			raw, err := orderReplyValue(element, false)
			if err != nil {
				return nil, err
			}
			out.Write(raw)
		}
		out.WriteByte(']')
		return out.Bytes(), nil
	}
	return json.Marshal(value)
}

// writeReply writes a reply an agent reads as one compact line, in reading order.
func writeReply(w io.Writer, v any) error {
	data, err := orderedReply(v)
	if err != nil {
		return json.NewEncoder(w).Encode(v)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// writeReplyJSON is writeReply for the HTTP endpoints that answer indented.
func writeReplyJSON(w http.ResponseWriter, code int, v any) {
	data, err := orderedReply(v)
	var out bytes.Buffer
	if err != nil || json.Indent(&out, data, "", "  ") != nil {
		writeJSON(w, code, v)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	out.WriteByte('\n')
	_, _ = w.Write(out.Bytes())
}
