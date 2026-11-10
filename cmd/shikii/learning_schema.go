package main

var learningSchema = makeLearningSchema()

func makeLearningSchema() map[string]any {
	text := map[string]any{"type": "string"}
	refs := map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": text}

	connection := schemaObject(map[string]any{"id": text, "revision": text, "quote": text, "conditions": text, "reason": text, "relation": map[string]any{"type": "string", "enum": []string{"supports", "counterexample"}}}, "id", "quote", "conditions", "reason", "relation")
	result := schemaObject(map[string]any{"conditions": text, "conclusion": text, "assessment": map[string]any{"type": "string", "enum": []string{"supports", "challenges", "inconclusive"}}, "sources": refs}, "conditions", "conclusion", "assessment", "sources")
	probe := schemaObject(map[string]any{"question": text, "method": text, "reconsider_when": text, "results": schemaArray(result, 4)}, "question", "method", "reconsider_when")
	step := schemaObject(map[string]any{"condition": text, "action": text, "avoid": text, "check": text, "sources": refs}, "condition", "action", "avoid", "check", "sources")
	practice := schemaObject(map[string]any{"steps": schemaArray(step, 8)}, "steps")
	schema := schemaObject(map[string]any{"connections": schemaArray(connection, 6), "probe": probe, "practice": practice})
	schema["description"] = "Versioned agent-authored learning, at most 12 KiB. Connections quote at least two distinct live article revisions with conditions and reasons. Probe results and howto practice steps cite observation sources, never truths or permission. Omit to preserve; {} clears. In work notes, connection id may be a prepared article ref and nested sources use prepared observation refs. Direct ingest/patch uses actual ids and revisions."
	return schema
}

var articleReferencesSchema = map[string]any{"type": "array", "maxItems": 8, "items": schemaObject(map[string]any{"id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"}}, []string{"id", "revision"}...)}

var organizationSchema = makeOrganizationSchema()

func makeOrganizationSchema() map[string]any {
	text := map[string]any{"type": "string"}

	array := func(p any, max int) map[string]any {
		v := schemaArray(p, max)
		v["minItems"] = 1
		return v
	}
	source := schemaObject(map[string]any{"id": text, "revision": text, "lesson": text}, "id", "revision", "lesson")
	target := schemaObject(map[string]any{"title": text, "lesson": text, "applies_to": text, "body": text, "kind": kindSchema, "scene": sceneSchema, "sources": map[string]any{"type": "array", "items": text}, "related": map[string]any{"type": "array", "items": text}, "rationale": rationaleSchema, "learning": learningSchema, "passages": passagesSchema}, "title", "lesson", "applies_to", "body", "kind", "scene")
	move := schemaObject(map[string]any{"from": text, "quote": text, "to": text}, "from", "quote", "to")
	return schemaObject(map[string]any{"reason": text, "sources": array(source, 4), "targets": array(target, 4), "moves": array(move, 8)}, "reason", "sources", "targets", "moves")
}
