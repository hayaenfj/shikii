package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// parseRequest reads and validates the body. The criteria keep their JSON order (they are not put in a Go map).
func parseRequest(body []byte) (state string, qs []question, layout string, err error) {
	var req rawRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, "", fmt.Errorf("bad json: %w", err)
	}
	if len(req.Questions) == 0 {
		return "", nil, "", errors.New("questions is required")
	}
	state = stateText(req.State)
	keys := make([]string, 0, len(req.Questions))
	for k := range req.Questions {
		keys = append(keys, k)
	}
	sort.Strings(keys) // question order does not affect the response (each is asked separately). Sorted for stability
	for _, k := range keys {
		rq := req.Questions[k]
		q, err := parseQuestion(k, rq)
		if err != nil {
			return "", nil, "", err
		}
		qs = append(qs, q)
	}
	return state, qs, req.Layout, nil
}

func parseQuestion(key string, rq rawQuestion) (question, error) {
	q := question{Key: key, Type: rq.Type, Instructions: strings.TrimSpace(rq.Instructions)}
	if q.Instructions == "" {
		return q, fmt.Errorf("questions.%s.instructions is required", key)
	}
	switch rq.Type {
	case "choice":
		var oc systemone.OrderedCriteria
		if len(rq.Criteria) == 0 || oc.UnmarshalJSON(rq.Criteria) != nil {
			return q, fmt.Errorf("questions.%s.criteria must be an object of id -> description", key)
		}
		for _, c := range oc {
			q.Options = append(q.Options, option{ID: c.ID, Description: c.Description})
		}
	case "score":
		var levels []string
		if len(rq.Criteria) == 0 || json.Unmarshal(rq.Criteria, &levels) != nil {
			return q, fmt.Errorf("questions.%s.criteria must be an array of levels (lowest first)", key)
		}
		for i, l := range levels {
			q.Options = append(q.Options, option{ID: fmt.Sprintf("%d", i), Description: l})
		}
	case "noul", "boolean":
		q.Type = "noul"
		yes, no := "yes", "no"
		if len(rq.Criteria) > 0 && string(rq.Criteria) != "null" {
			var m map[string]string
			if json.Unmarshal(rq.Criteria, &m) != nil {
				return q, fmt.Errorf("questions.%s.criteria must be a true/false (or yes/no) map", key)
			}
			for k, v := range m {
				switch strings.ToLower(k) {
				case "true", "yes":
					yes = v
				case "false", "no":
					no = v
				default:
					return q, fmt.Errorf("questions.%s.criteria: unknown key %q (true/false or yes/no)", key, k)
				}
			}
		}
		q.Options = []option{{ID: "yes", Description: yes}, {ID: "no", Description: no}}
	default:
		return q, fmt.Errorf("questions.%s.type %q is not choice, score or noul", key, rq.Type)
	}
	if n := len(q.Options); n < 2 || n > len(labels) {
		return q, fmt.Errorf("questions.%s: %d options (need 2..%d)", key, n, len(labels))
	}
	return q, nil
}

func stateText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		return buf.String()
	}
	return string(raw)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
