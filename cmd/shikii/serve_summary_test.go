package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 0.7.4: `shikii status` の中身(人の手間・届いた教訓の効果・止める検査)を画面にも出す。/v1/status に summary を足す。
func TestStatusEndpointCarriesTheSummaryForThePage(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	get := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("status JSON: %v %s", err, rec.Body.String())
		}
		s, _ := out["summary"].(map[string]any)
		if s == nil {
			t.Fatalf("status には summary が要る: %s", rec.Body.String())
		}
		return s
	}
	s := get()
	mem, _ := s["memory"].(map[string]any)
	gu, _ := s["guards"].(map[string]any)
	if mem["delivered"] != 0.0 || gu["total"] != 0.0 || gu["enabled"] != 0.0 {
		t.Fatalf("最初は何も無い: %v", s)
	}
	writeGuard(t, st.Dir, guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}})
	memoryEffectPrompt(t, scope, "s1")
	s = get()
	mem, _ = s["memory"].(map[string]any)
	gu, _ = s["guards"].(map[string]any)
	items, _ := gu["items"].([]any)
	if mem["delivered"] != 1.0 || mem["pending"] != 1.0 || gu["total"] != 1.0 || gu["enabled"] != 1.0 || len(items) != 1 || items[0].(map[string]any)["id"] != "changelog-pair" {
		t.Fatalf("届けた教訓と止める検査が summary に出る: %v", s)
	}
	person, _ := s["person"].(map[string]any)
	if person == nil || person["records"] == nil {
		t.Fatalf("人の手間も出る: %v", s)
	}
}
