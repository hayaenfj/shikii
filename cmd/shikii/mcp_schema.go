package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
)

// mcpProjectSchema is the project argument every tool takes.
var mcpProjectSchema = map[string]any{"type": "string", "description": "the absolute path inside a registered project on this machine; this call uses its .shikii/. For a worktree, pass the original project to keep its wiki and shared handoff; never initialize a separate wiki. Required when the server is shared (Codex)"}

// mcpTools is the list of tools. inputSchema is JSON Schema.
var correctionSchema = schemaObject(map[string]any{"source_id": map[string]any{"type": "string"}, "quote": map[string]any{"type": "string"}, "action": map[string]any{"type": "string"}}, []string{"source_id", "quote", "action"}...)

var maintenanceSchema = schemaObject(map[string]any{
	"page_id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
}, []string{"page_id", "revision", "key", "reason"}...)

var passagesSchema = map[string]any{"type": "array", "maxItems": 8, "items": schemaObject(map[string]any{
	"role":       map[string]any{"type": "string", "enum": []string{"current", "history", "recheck", "counterexample"}},
	"quote":      map[string]any{"type": "string", "description": "Exact unique body quotation, 1-600 UTF-8 bytes."},
	"conditions": map[string]any{"type": "string", "description": "Conditions for this interpretation, 1-400 UTF-8 bytes."},
	"reason":     map[string]any{"type": "string", "description": "Source-backed reason for this role, 1-400 UTF-8 bytes. Agent interpretation, never truth."},
}, []string{"role", "quote", "conditions", "reason"}...), "description": "Interpreted body passages. Read sources first. Omit to preserve, [] to clear. Unique nonoverlapping quotes are revision checked; update passages with changed body text."}

var patchSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "revision", "sources", "change_reason"}, "anyOf": []any{map[string]any{"required": []string{"edits"}, "properties": map[string]any{"edits": map[string]any{"minItems": 1}}}, map[string]any{"required": []string{"passages"}}, map[string]any{"required": []string{"learning"}}, map[string]any{"required": []string{"related"}}}, "properties": map[string]any{
	"id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"},
	"sources":       map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}},
	"change_reason": map[string]any{"type": "string"},
	"passages":      passagesSchema,
	"learning":      learningSchema, "requires": articleReferencesSchema, "related": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	"edits": map[string]any{"type": "array", "maxItems": 16, "items": schemaObject(map[string]any{
		"field":  map[string]any{"type": "string", "enum": []string{"title", "lesson", "applies_to", "body", "rationale", "kind", "scene"}},
		"before": map[string]any{"type": "string"}, "after": map[string]any{"type": "string"},
	}, []string{"field", "before", "after"}...)},
}, "description": "Exact text edits of a read revision; each before must occur once (empty only fills an empty field). Atomic, source-backed and conflict checked. Used by patch to save, or compare to preview without saving."}

var rationaleSchema = map[string]any{"type": "string", "description": "Source-backed conditions, choice, rejected alternatives and reasons, and observed outcome; unknown where unsupported. At most 1600 bytes. Omit to preserve; empty clears."}

var watchSchema = map[string]any{"type": "array", "maxItems": 4, "items": map[string]any{"type": "string"}, "description": "Project-relative regular files this knowledge depends on. Hashes are captured now; changed or unreadable files trigger rechecking. No links or hidden paths; each file at most 1 MiB. Omit to preserve; empty clears."}

var discoverySchema = schemaObject(map[string]any{
	"lookup_id": map[string]any{"type": "string"}, "page_id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"}, "context": map[string]any{"type": "string"}, "decision": map[string]any{"type": "string"}, "evidence": map[string]any{"type": "string"},
	"case_id": map[string]any{"type": "string", "description": "Optional diagnose case for this exact revision and work. A recorded non-delivery reopens maintenance with its reason; incomplete or foreign cases are rejected."},
}, []string{"lookup_id", "page_id", "revision", "context", "decision", "evidence"}...)

// kindSchema and sceneSchema are a page's kind and scene (0.9.0). The save endpoint requires them on pages with a lesson (omitting them on an edit keeps the old values).
var kindSchema = map[string]any{"type": "string", "enum": notebook.Kinds, "description": "the lesson's kind (required; omit on an edit to keep it): rule=a rule decided in this project, howto=how to do something, criteria=a basis for judging, fact=a premise or fact, history=how things came to be"}

var sceneSchema = map[string]any{"type": "string", "maxLength": notebook.SceneMax, "description": "the scene it applies to, short and in the work's own words (required; e.g. \"when committing\", \"when sending an invoice\"; one line, up to 48 characters). If place returns an existing scene close to it, use the same wording"}
