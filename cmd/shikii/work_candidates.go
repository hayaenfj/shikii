package main

import (
	"encoding/json"
	"maps"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// workCandidateQuery is the call that reads any search candidate at the revision the search found. The reply says it once, in
// knowledge_context.search.candidate_query, and a candidate no longer carries a query of its own (each carried the same words
// around its id and revision: 80 bytes a candidate, 26% of a prepare reply measured on 2026-11-28). The reader puts the candidate's
// id and revision in place of <id> and <revision>; without the revision the call reads the latest article, not the one found.
func workCandidateQuery() map[string]any {
	return map[string]any{"operation": "query", "query": "lesson:<id>@<revision>"}
}

// workCandidate is a search candidate of a prepared reply, written against the knowledge entries the reply holds at the moment it is
// written. A candidate that was delivered as an entry of knowledge repeated that entry's title and applies_to. The candidate leaves out
// one of the two only when, at the moment it is encoded, the entry of the same id and revision writes the very same non-empty text,
// in its own field (a shortened entry) or in the article page it carries, and names the entry by knowledge_ref instead. The comparison
// is made on every encoding against the reply as it is then: the reductions of limitWorkPreparation take the title, the article
// or the whole entry out of knowledge after the candidates were made, and a text that no longer stands in knowledge must stay in
// the candidate. Every other member (id, revision, fields, terms) is written as the search made it, and so is a text that differs, an
// empty text and a candidate no entry holds.
//
// A candidate points at its reply, so bind through bindCandidates. Plain map candidates (as a caller may build them) are bound there.
type workCandidate struct {
	fields map[string]any
	reply  map[string]any
}

// bindCandidates makes the candidates of the reply's search be written against the reply's knowledge. Every prepare reply goes
// through limitWorkPreparation, which calls it before the first measure.
func bindCandidates(reply map[string]any) {
	context, _ := reply["knowledge_context"].(map[string]any)
	search, _ := context["search"].(map[string]any)
	list, _ := search["candidates"].([]any)
	for i, raw := range list {
		switch c := raw.(type) {
		case map[string]any:
			list[i] = &workCandidate{fields: c, reply: reply}
		case *workCandidate:
			c.reply = reply
		}
	}
}

// candidateFields returns the members a candidate holds (a bound one holds them in fields).
func candidateFields(raw any) map[string]any {
	if c, ok := raw.(*workCandidate); ok {
		return c.fields
	}
	return raw.(map[string]any)
}

// entry returns the entry of knowledge, as the reply holds it now, that is the same article revision as the candidate, with its ref.
func (c *workCandidate) entry() (map[string]any, string) {
	id, _ := c.fields["id"].(string)
	revision, _ := c.fields["revision"].(string)
	if id == "" || revision == "" {
		return nil, ""
	}
	entries, _ := c.reply["knowledge"].([]any)
	for _, raw := range entries {
		if e, ok := raw.(map[string]any); ok && e["id"] == id && e["revision"] == revision {
			ref, _ := e["ref"].(string)
			return e, ref
		}
	}
	return nil, ""
}

// entryWrites says whether the entry writes the text under key (title or applies_to): in its own field, or in its article page.
func entryWrites(e map[string]any, key, text string) bool {
	if own, _ := e[key].(string); own == text {
		return true
	}
	page, _ := e["article"].(notebook.Page)
	switch key {
	case "title":
		return page.Title == text
	case "applies_to":
		return page.AppliesTo == text
	}
	return false
}

// written returns the members to write: the candidate's own, less the texts the entry of the reply writes, with knowledge_ref when
// one was left out.
func (c *workCandidate) written() map[string]any {
	e, ref := c.entry()
	if e == nil || ref == "" {
		return c.fields
	}
	var held []string
	for _, key := range []string{"title", "applies_to"} {
		if text, _ := c.fields[key].(string); text != "" && entryWrites(e, key, text) {
			held = append(held, key)
		}
	}
	if len(held) == 0 {
		return c.fields
	}
	out := maps.Clone(c.fields)
	for _, key := range held {
		delete(out, key)
	}
	out["knowledge_ref"] = ref
	return out
}

func (c *workCandidate) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.written())
}
