package main

// Work's schema describes content and local references; storage identifiers stay
// inside the receipt. Runtime validation enforces byte and operation limits.
var workSchema = makeWorkSchema()

func makeWorkSchema() map[string]any {
	text := map[string]any{"type": "string"}
	refs := map[string]any{"type": "array", "items": text}
	object := func(p map[string]any) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": p}
	}
	array := func(item any, n int) map[string]any {
		return map[string]any{"type": "array", "maxItems": n, "items": item}
	}
	fact := object(map[string]any{"unknown": map[string]any{"type": "boolean"}, "summary": text, "quotes": array(object(map[string]any{"ref": text, "text": text}), 16)})
	note := object(map[string]any{"ref": text, "target": text, "title": text, "lesson": text, "body": text, "applies_to": text, "sources": refs, "related": refs, "kind": kindSchema, "scene": sceneSchema, "rationale": rationaleSchema, "watch": watchSchema, "change_reason": text, "correction": correctionSchema, "edits": patchSchema["properties"].(map[string]any)["edits"]})
	return object(map[string]any{
		"session_id": text, "turn_id": text, "query": text, "request": text, "receipt": text,
		"agreed": fact, "open": fact, "continues_previous": map[string]any{"type": "boolean"},
		"observations": array(object(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"human", "agent", "code", "test", "release"}}, "title": text, "content": text, "reference": text, "version": text}), 4),
		"notes":        array(note, 2),
		"reviews":      array(object(map[string]any{"ref": text, "answer": map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}}, "reason": text}), 128),
		"memory":       array(object(map[string]any{"ref": text, "result": text, "decision": text, "evidence": text, "correction_result": text}), 256),
		"reviewed":     refs, "maintenance": array(object(map[string]any{"ref": text, "reason": text}), 8),
	})
}
