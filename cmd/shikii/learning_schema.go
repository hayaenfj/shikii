package main

var learningSchema = makeLearningSchema()

func makeLearningSchema() map[string]any {
	text := map[string]any{"type": "string"}
	refs := map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": text}
	object := func(p map[string]any, required ...string) map[string]any {
		v := map[string]any{"type": "object", "additionalProperties": false, "properties": p}
		if len(required) > 0 {
			v["required"] = required
		}
		return v
	}
	array := func(p any, max int) map[string]any {
		return map[string]any{"type": "array", "maxItems": max, "items": p}
	}
	connection := object(map[string]any{"id": text, "revision": text, "quote": text, "conditions": text, "reason": text, "relation": map[string]any{"type": "string", "enum": []string{"supports", "counterexample"}}}, "id", "quote", "conditions", "reason", "relation")
	result := object(map[string]any{"conditions": text, "conclusion": text, "assessment": map[string]any{"type": "string", "enum": []string{"supports", "challenges", "inconclusive"}}, "sources": refs}, "conditions", "conclusion", "assessment", "sources")
	probe := object(map[string]any{"question": text, "method": text, "reconsider_when": text, "results": array(result, 4)}, "question", "method", "reconsider_when")
	step := object(map[string]any{"condition": text, "action": text, "avoid": text, "check": text, "sources": refs}, "condition", "action", "avoid", "check", "sources")
	practice := object(map[string]any{"steps": array(step, 8)}, "steps")
	schema := object(map[string]any{"connections": array(connection, 6), "probe": probe, "practice": practice})
	schema["description"] = "Versioned agent-authored learning, at most 12 KiB. Connections quote at least two distinct live article revisions with conditions and reasons. Probe results and howto practice steps cite observation sources, never truths or permission. Omit to preserve; {} clears. In work notes, connection id may be a prepared article ref and nested sources use prepared observation refs. Direct ingest/patch uses actual ids and revisions."
	return schema
}

var articleReferencesSchema = map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "revision"}, "properties": map[string]any{"id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"}}}}

var organizationSchema = makeOrganizationSchema()

func makeOrganizationSchema() map[string]any {
	text := map[string]any{"type": "string"}
	object := func(p map[string]any, required ...string) map[string]any {
		v := map[string]any{"type": "object", "additionalProperties": false, "properties": p}
		if len(required) > 0 {
			v["required"] = required
		}
		return v
	}
	array := func(p any, max int) map[string]any {
		return map[string]any{"type": "array", "maxItems": max, "minItems": 1, "items": p}
	}
	source := object(map[string]any{"id": text, "revision": text, "lesson": text}, "id", "revision", "lesson")
	target := object(map[string]any{"title": text, "lesson": text, "applies_to": text, "body": text, "kind": kindSchema, "scene": sceneSchema, "sources": map[string]any{"type": "array", "items": text}, "related": map[string]any{"type": "array", "items": text}, "rationale": rationaleSchema, "learning": learningSchema, "passages": passagesSchema}, "title", "lesson", "applies_to", "body", "kind", "scene")
	move := object(map[string]any{"from": text, "quote": text, "to": text}, "from", "quote", "to")
	return object(map[string]any{"reason": text, "sources": array(source, 4), "targets": array(target, 4), "moves": array(move, 8)}, "reason", "sources", "targets", "moves")
}
