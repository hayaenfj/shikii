package main

func schemaObject(properties map[string]any, required ...string) map[string]any {
	v := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		v["required"] = required
	}
	return v
}
func schemaArray(items any, max int) map[string]any {
	return map[string]any{"type": "array", "maxItems": max, "items": items}
}
