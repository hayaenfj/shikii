package main

import (
	"strings"
)

// buildMessages builds the conversation for one question. Options get A, B, … in display order.
// With stateFirst=false (the default) the state goes last: the leading system message, question and options form a prefix
// shared by every question of the same spec, which llama-server's cache_prompt reuses.
func buildMessages(system, state string, q question, layout string) []message {
	if layout == "plain" {
		return buildPlain(state, q)
	}
	stateFirst := layout == "state-first"
	var b strings.Builder
	stateBlock := "<state>\n" + state + "\n</state>\n"
	if stateFirst {
		b.WriteString(stateBlock)
		b.WriteByte('\n')
	}
	switch q.Type {
	case "choice":
		b.WriteString("Question: ")
		b.WriteString(q.Instructions)
		b.WriteString("\nOptions:\n")
	case "score":
		b.WriteString("Question: ")
		b.WriteString(q.Instructions)
		b.WriteString("\nScale (lowest first):\n")
	case "noul":
		b.WriteString("Is the following statement true of the state?\nStatement: ")
		b.WriteString(q.Instructions)
		b.WriteString("\nOptions:\n")
	}
	for i, o := range q.Options {
		b.WriteString(labels[i])
		b.WriteString(". ")
		if q.Type == "choice" {
			b.WriteString(o.ID)
			if o.Description != "" && o.Description != o.ID {
				b.WriteString(": ")
				b.WriteString(o.Description)
			}
		} else {
			b.WriteString(o.Description)
		}
		b.WriteByte('\n')
	}
	if !stateFirst {
		b.WriteByte('\n')
		b.WriteString(stateBlock)
	}
	b.WriteString("\nReply with one letter.")
	return []message{{Role: "system", Content: system}, {Role: "user", Content: b.String()}}
}

// buildPlain is the plain layout: no system message, the question, options as "- id: description", the input after Input:, and finally a request to answer with an id.
// It reads the first token of the option id itself, not a letter (A, B, …). For a plain chat model this can fit better than the layout meant for decision models
// (13 points on Qwen3-4B, docs/bench.md).
func buildPlain(state string, q question) []message {
	var b strings.Builder
	switch q.Type {
	case "noul":
		b.WriteString("Is the following statement true of the input?\nStatement: ")
		b.WriteString(q.Instructions)
		b.WriteString("\n\nOptions:\n")
	case "score":
		b.WriteString(q.Instructions)
		b.WriteString("\n\nScale (lowest first):\n")
	default:
		b.WriteString(q.Instructions)
		b.WriteString("\n\nOptions:\n")
	}
	for _, o := range q.Options {
		b.WriteString("- ")
		b.WriteString(o.ID)
		if o.Description != "" && o.Description != o.ID {
			b.WriteString(": ")
			b.WriteString(o.Description)
		}
		b.WriteByte('\n')
	}
	b.WriteString(plainMarker)
	b.WriteString(state)
	b.WriteString("\n\nAnswer with the option id only.")
	return []message{{Role: "user", Content: b.String()}}
}

type tokenProb struct {
	Token string  `json:"token"`
	Prob  float64 `json:"prob"`
}
