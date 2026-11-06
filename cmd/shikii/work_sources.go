package main

import (
	"encoding/json"
	"errors"
	"github.com/hayaenfj/shikii/internal/store"
	"strings"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Conversation originals and observations share a fixed encoded source budget.
const workSourceBudget = 128 << 10

type preparedWorkSource struct {
	Material map[string]any
	Query    map[string]any
	Complete bool
	Notice   string
}

// prepareWorkSource reads originals independently of whether scoring is pending.
// A source that cannot be read completely has a concrete, paged read path and no
// writable alias. The deeper operations can record unknown when facts are absent.
func prepareWorkSource(book notebook.Book, id string) preparedWorkSource {
	result := preparedWorkSource{Query: map[string]any{"operation": "query", "sources": []string{id}}}
	material, err := book.ReadSources([]string{id})
	if err != nil {
		result.Material = map[string]any{"source_id": id, "error": err.Error()}
		result.Notice = "The source could not be read. Inspect source_query with shikii_wiki or wiki query, resolve the missing evidence, and use individual reflect/wiki operations; do not judge this as a complete conversation."
		return result
	}
	result.Material = material
	missing := workUnretainedConversationFields(material)
	if len(missing) > 0 {
		material["text_status"] = missing
	}
	data, _ := json.Marshal(material)
	if len(data) > workSourceBudget {
		page, e := book.ReadSourcesPage([]string{id}, "")
		if e != nil {
			result.Material = map[string]any{"source_id": id, "error": e.Error()}
		} else {
			result.Material = page
			if len(missing) > 0 {
				result.Material["text_status"] = missing
			}
			if cursor, ok := page["next_cursor"].(string); ok && cursor != "" {
				result.Query["cursor"] = cursor
			}
		}
		result.Notice = "This source exceeds the 128 KiB encoded preparation budget. The supplied source is partial. Continue source_query with shikii_wiki, or wiki query with the source id and continue:<cursor>. For conversation originals, join original_part.json pieces in order and parse the combined JSON. For observations, order by observation_part.from and join observations[].content, including the last part. After reading all parts, use individual reflect/wiki operations. Do not judge the shortened material. If text_status names an unretained original, paging cannot restore that missing tail; read a verified host conversation or preserve unknown."
		return result
	}
	for _, originals := range material["originals"].(map[string][]notebook.Original) {
		for _, original := range originals {
			if len(original.Status) > 0 {
				result.Notice = "Saved original text is limited, unavailable or legacy. Read the source and text_status with source_query before using individual reflect/wiki operations; do not infer absent conditions or treat this as a complete original."
				return result
			}
		}
	}
	if len(missing) > 0 {
		result.Material["text_status"] = missing
		result.Notice = "This conversation has bounded excerpts without retained originals. Source_query inspects the stored excerpt; it cannot recover a missing tail. Read the original host conversation or a verified archive before judging; if it is unavailable, keep the missing conditions unknown and use individual reflect/wiki operations."
		return result
	}
	result.Complete = true
	return result
}

// Keep supporting evidence from reflect as a whole; only fields already mapped
// into the prepared source/observation/maintenance interface are omitted here.
// This preserves future reflection context instead of silently projecting it out.
func preparedReflectionContext(view reflectView, brief ...bool) (map[string]any, error) {
	view.Sources, view.Observations, view.Maintenance = nil, nil, nil
	view.HowToSubmit = ""
	data, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	var context map[string]any
	if err = json.Unmarshal(data, &context); err != nil {
		return nil, err
	}

	for _, key := range []string{"how_to_submit", "sources", "observations", "maintenance", "more_pending"} {
		delete(context, key)
	}
	if len(brief) > 0 && brief[0] {
		lessons, _ := context["lessons"].([]any)
		references := []any{}
		for _, raw := range lessons {
			page := raw.(map[string]any)
			id, _ := page["id"].(string)
			revision, _ := page["revision"].(string)
			references = append(references, map[string]any{"id": id, "revision": revision, "title": page["title"], "lesson": page["lesson"], "applies_to": page["applies_to"], "incomplete": true, "query": map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}})
		}
		if len(references) > 0 {
			context["lessons"], context["incomplete"], context["more_lessons"] = references, true, 0
			context["next_action"] = "Brief supporting knowledge retains exact revision queries instead of whole bodies. Read a referenced article when its conditions or exceptions matter. Human corrections and required source material are retained."
		}
	}
	if workContextSize(context) <= workSourceBudget {
		return context, nil
	}
	lessons, _ := context["lessons"].([]any)
	context["lessons"] = []any{}
	context["incomplete"] = true
	context["more_lessons"] = 0
	context["next_action"] = "Some related lessons are referenced instead of supplied whole. Read each incomplete article's exact query with shikii_wiki or wiki query before using it. If more_lessons remains, read the complete supporting context with shikii_reflect query. Human corrections are retained; references alone do not establish meaning."
	if workContextSize(context) > workSourceBudget-1024 {
		return nil, errors.New("the supporting human corrections exceed the preparation budget; read shikii reflect query and use the individual operations instead of an incomplete prepared context")
	}
	kept := []any{}
	for i, lesson := range lessons {
		candidate := append(append([]any{}, kept...), lesson)
		context["lessons"] = candidate
		if workContextSize(context) <= workSourceBudget-1024 {
			kept = candidate
			continue
		}
		page := lesson.(map[string]any)
		id, _ := page["id"].(string)
		revision, _ := page["revision"].(string)
		reference := map[string]any{"id": id, "revision": revision, "title": page["title"], "incomplete": true, "query": map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}}
		candidate = append(append([]any{}, kept...), reference)
		context["lessons"] = candidate
		if workContextSize(context) > workSourceBudget-1024 {
			context["lessons"] = kept
			context["more_lessons"] = len(lessons) - i
			break
		}
		kept = candidate
	}
	return context, nil
}

func workContextSize(value any) int { data, _ := json.Marshal(value); return len(data) }

// Preserve the ordinary query's classifications, candidates, issues and future
// metadata; full article bodies already supplied through knowledge are not copied.
func preparedKnowledgeContext(view notebook.View, query string) (map[string]any, error) {
	view.Lessons = nil
	data, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	var context map[string]any
	if err = json.Unmarshal(data, &context); err != nil {
		return nil, err
	}
	delete(context, "lessons")
	if search, ok := context["search"].(map[string]any); ok {
		if candidates, ok := search["candidates"].([]any); ok {
			for _, candidate := range candidates {
				page := candidate.(map[string]any)
				id, _ := page["id"].(string)
				revision, _ := page["revision"].(string)
				page["query"] = map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}
			}
		}
	}
	context["next_action"] = "Read the remaining classification guidance and search total, not only the bodies in knowledge. Candidate query reads that exact article revision. Use knowledge_query for another search page when next_cursor is present; a completed candidate list does not mean every body was supplied. Search matches and read receipts are not usefulness or permission. Pending source records here are queue heads; use pending[].source and its completeness instructions before judging."
	if workContextSize(context) > workSourceBudget {
		return nil, errors.New("the supporting knowledge query context exceeds the preparation budget; use shikii wiki query with the same topic words and continue its pages before using the individual recording operations")
	}
	return context, nil
}

// A missing scoring record is not evidence that a bounded conversation was complete.
func workUnretainedConversationFields(material map[string]any) map[string]map[string]string {
	missing := map[string]map[string]string{}
	conversations, _ := material["source_material"].([]store.Feedback)
	originals, _ := material["originals"].(map[string][]notebook.Original)
	for _, source := range conversations {
		fields := map[string]string{}
		for field, excerpt := range map[string]string{"request": source.Request, "summary": source.Summary, "reply": source.Reply} {
			if len(excerpt) < store.FeedbackTextLimit-utf8.UTFMax+1 {
				continue
			}
			verified := false
			for _, original := range originals[source.ID] {
				text, ok := original.Inputs[field]
				if ok && original.Status[field] == "" && strings.HasPrefix(text, excerpt) {
					verified = true
				}
			}
			if !verified {
				fields[field] = "bounded_excerpt_without_retained_original"
			}
		}
		if len(fields) > 0 {
			missing[source.ID] = fields
		}
	}
	return missing
}
