package main

import "testing"

// 0.9.1 の変更履歴と shikii_reflect の説明は「submit は会話と項目の数を限らない」と言う。入力の形(MCP のクライアントとモデルが従う物)も限らないこと
func TestAudit091ReflectSchemaHasNoItemLimits(t *testing.T) {
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_reflect" {
			continue
		}
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		for _, k := range []string{"reviews", "wiki", "memory", "maintenance"} {
			if m, ok := props[k].(map[string]any)["maxItems"]; ok {
				t.Errorf("shikii_reflect の %s に maxItems=%v が残っている(説明では数を限らない)", k, m)
			}
		}
	}
}
