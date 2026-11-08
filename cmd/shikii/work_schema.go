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
	fact := object(map[string]any{"unknown": map[string]any{"type": "boolean"}, "summary": text, "quotes": array(object(map[string]any{"ref": text, "text": text}), 3)})
	fact["description"] = "Hook planning: agreed states what the person agreed; open states a concrete choice blocking this task. Use {summary,quotes:[{ref,text}]} with exact prepared quotations, or {unknown:true} alone with no summary or quotes. Unverified execution results belong in observations or notes. Omit agreed/open for an agent-supplied task without hook context."
	note := object(map[string]any{"ref": text, "target": text, "title": text, "lesson": text, "body": text, "applies_to": text, "sources": refs, "related": refs, "kind": kindSchema, "scene": sceneSchema, "rationale": rationaleSchema, "watch": watchSchema, "passages": passagesSchema, "learning": learningSchema, "change_reason": text, "correction": correctionSchema, "edits": patchSchema["properties"].(map[string]any)["edits"]})
	note["description"] = "New notes use draft content. Existing article targets use sources, change_reason, exact edits, passages and/or learning. A placement pending ref accepts ref, target and edits only; target is required and target new forbids edits. A required-note error ref accepts the full corrected note with a current target, or restores the original draft and known sources with ref, target and edits. Repair an existing article using its original ID. Other fields are refused, not ignored."
	trialPlan := object(map[string]any{"case": text, "question": text, "expected": text, "method": text, "conditions": text, "decision": text, "effort": map[string]any{"type": "string", "enum": []string{"small", "medium", "large"}}, "evidence": array(text, 4)})
	trialResult := object(map[string]any{"assessment": map[string]any{"type": "string", "enum": []string{"supports", "challenges", "inconclusive"}}, "observed": text, "evidence": array(text, 4), "measurements": map[string]any{"type": "object", "maxProperties": 4, "propertyNames": map[string]any{"pattern": "^[a-z][a-z0-9_]{0,47}$"}, "additionalProperties": map[string]any{"type": "number", "minimum": 0, "maximum": 1e15}}})
	return object(map[string]any{
		"result":     object(map[string]any{"changed": text, "checked": text, "evidence": array(text, 4), "remaining": text, "sufficient": map[string]any{"type": "boolean", "description": "Optional actual quality check, using the same captured evidence; omission stays unknown. Drives bounded procedure validation without another report."}}),
		"reading":    object(map[string]any{"ref": text, "check": text, "sufficient": map[string]any{"type": "boolean"}, "evidence": array(text, 4)}),
		"trial":      object(map[string]any{"ref": text, "plan": trialPlan, "result": trialResult}),
		"comparison": object(map[string]any{"ref": text, "before": text, "after": text, "reason": text}),
		"reasoning":  object(map[string]any{"phase": map[string]any{"type": "string", "enum": []string{"start", "update", "finish"}}, "focus": text, "assessment": map[string]any{"type": "string", "enum": []string{"revise", "retain", "unresolved"}}, "reason": text, "evidence": array(text, 4), "next_action": text, "result": text, "depth": map[string]any{"type": "string", "enum": []string{"brief", "focused", "deep"}}, "next_trial": text}),
		"assignment": object(map[string]any{"id": text, "group": text, "role": text, "stage": text, "purpose": text, "question": text, "conditions": text, "commit": text, "branch": text, "code_state": text, "evidence_scope": text, "shareable": map[string]any{"type": "boolean"}}),
		"event":      object(map[string]any{"id": text, "state": map[string]any{"type": "string", "enum": []string{"observed", "completed", "failed", "cancelled", "integrated", "reverted"}}, "summary": text, "unknowns": array(text, 16), "contradictions": array(text, 16), "method": text, "commit": text, "code_state": text, "reference": text}),
		"max_bytes":  map[string]any{"type": "integer", "minimum": 8192, "maximum": 393216},
		"session_id": text, "turn_id": text, "query": text, "request": text, "receipt": text, "proposal": text, "conditions": text, "cursor": text,
		"agreed": fact, "open": fact, "continues_previous": map[string]any{"type": "boolean"},
		"observations": array(object(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"human", "agent", "code", "test", "release"}}, "title": text, "content": text, "reference": text, "version": text}), 4),
		"notes":        array(note, 2),
		"reviews":      array(object(map[string]any{"ref": text, "answer": map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}}, "reason": text}), 128),
		"memory":       array(object(map[string]any{"ref": text, "result": text, "decision": text, "evidence": text, "correction_result": text}), 256),
		"reviewed":     map[string]any{"type": "array", "maxItems": 64, "items": text, "description": "Inspected source/evidence refs without a lasting lesson; knowledgeN confirms inspected initial evidence, changeN acknowledges only the delivered change. Neither means usefulness or truth."}, "maintenance": array(object(map[string]any{"ref": text, "reason": text}), 8),
		"discoveries": array(object(map[string]any{"ref": text, "context": text, "decision": text, "evidence": text, "case_id": text}), 4),
		"outcomes":    array(object(map[string]any{"ref": text, "conditions": text, "action": text, "failure": text, "correction": text, "result": text, "sources": refs}), 2),
	})
}
