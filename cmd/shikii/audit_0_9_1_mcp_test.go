package main

import "testing"

// 0.9.1's changelog and shikii_reflect's description say "submit has no limit on conversations or items". The input schema (what MCP clients and models follow) must not limit them either
func TestAudit091ReflectSchemaHasNoItemLimits(t *testing.T) {
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_reflect" {
			continue
		}
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		for _, k := range []string{"reviews", "wiki", "memory", "maintenance"} {
			if m, ok := props[k].(map[string]any)["maxItems"]; ok {
				t.Errorf("shikii_reflect's %s still has maxItems=%v (the description says there is no limit)", k, m)
			}
		}
	}
}
