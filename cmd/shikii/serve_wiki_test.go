package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Growing the wiki from the page: the list of drafts and claims (evidence and the confirmable mark), generalizing a draft into a claim, changing state, deleting.
// The line that only people change states holds in the API as well (the machine only says confirmable).
func TestServeWikiListsClaimsAndLetsAPersonPromoteAndDecide(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "false")
	pdir := filepath.Join(dir, ".shikii", "wiki", "patterns")
	if err := os.MkdirAll(filepath.Join(pdir, "drafts"), 0o755); err != nil {
		t.Fatal(err)
	}
	draft := filepath.Join(pdir, "drafts", "intent-strict.md")
	if err := os.WriteFile(draft, []byte("---\nclaim: intent では候補 strict が current より当たる\nstatus: draft\napplies_to:\n  type: choice\n  models: m\nevidence:\n  - date: \"2026-10-18\"\n    model: m\n    result: strict 1.000 vs current 0.500\n    verdict: supports\n    run: r1\n  - date: \"2026-10-18\"\n    model: other\n    result: strict 0.9 vs current 0.6\n    verdict: supports\n    run: r2\nretest:\n  spec: decisions/intent.yaml\n  better: strict\n  than: current\n---\n# 下書き\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "old-prose.md"), []byte("# 前書きの無い古いページ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ef endpointFlags
	ef.model, ef.local = "m", "http://127.0.0.1:1"
	s := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef}))
	t.Cleanup(s.Close)

	var list struct {
		Claims []map[string]any `json:"claims"`
	}
	if getJSON(t, s.URL+"/v1/wiki", &list) != 200 || len(list.Claims) != 2 {
		t.Fatalf("every page is listed, with or without front matter: %+v", list.Claims)
	}
	var d map[string]any
	for _, c := range list.Claims {
		if c["status"] == "draft" {
			d = c
		}
	}
	if d == nil || d["confirmable"] != true || d["supporting_models"] != 2.0 || len(d["evidence"].([]any)) != 2 {
		t.Fatalf("a draft with 2 supporting models is confirmable: %+v", d)
	}

	// HTTP evidence returns values with the lowercase keys the page reads.
	evidence := d["evidence"].([]any)[0].(map[string]any)
	for key, want := range map[string]string{
		"date": "2026-10-18", "model": "m", "result": "strict 1.000 vs current 0.500", "verdict": "supports", "run": "r1",
	} {
		if evidence[key] != want {
			t.Fatalf("evidence JSON field %s: got %v, want %s", key, evidence[key], want)
		}
	}

	// Generalize a draft into a claim: it moves to patterns/<slug>.md, status is tentative, and the body and evidence remain
	code, out := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": d["path"], "action": "promote", "slug": "strict-wording",
		"claim": "厳しく問う文言は迷いを減らす", "applies_to": map[string]string{"type": "choice", "models": "小さいモデル"}})
	if code != 200 || out["path"] != "patterns/strict-wording.md" {
		t.Fatalf("promote: %d %v", code, out)
	}
	if _, err := os.Stat(draft); !os.IsNotExist(err) {
		t.Fatalf("the draft must move, not copy")
	}
	pg, body, err := readClaimPage(filepath.Join(pdir, "strict-wording.md"))
	if err != nil || pg.Status != "tentative" || pg.Claim != "厳しく問う文言は迷いを減らす" || pg.AppliesTo["models"] != "小さいモデル" || len(pg.Evidence) != 2 || pg.Retest == nil || !strings.Contains(body, "下書き") {
		t.Fatalf("promoted page: %v %+v\n%s", err, pg, body)
	}
	// Change state (only confirmed / rejected). An unknown state is 400. A page without front matter is 409
	code, _ = postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/strict-wording.md", "action": "status", "status": "confirmed"})
	pg, _, _ = readClaimPage(filepath.Join(pdir, "strict-wording.md"))
	if code != 200 || pg.Status != "confirmed" {
		t.Fatalf("a person confirms: %d %+v", code, pg)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/strict-wording.md", "action": "status", "status": "maybe"}); code != 400 {
		t.Fatalf("unknown status must be 400, got %d", code)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/old-prose.md", "action": "status", "status": "confirmed"}); code != 409 {
		t.Fatalf("a page without front matter cannot change status, got %d", code)
	}
	// A path outside is 400. Only drafts are deleted (claims are rejected instead)
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "../etc/passwd", "action": "delete"}); code != 400 {
		t.Fatalf("paths outside wiki/patterns must be refused, got %d", code)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/strict-wording.md", "action": "delete"}); code != 409 {
		t.Fatalf("only drafts may be deleted; claims are rejected instead, got %d", code)
	}
	if err := os.WriteFile(filepath.Join(pdir, "drafts", "intent-worse.md"), []byte("---\nclaim: x\nstatus: draft\n---\n# d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/drafts/intent-worse.md", "action": "delete"}); code != 200 {
		t.Fatalf("a draft may be deleted, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(pdir, "drafts", "intent-worse.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted draft must be gone")
	}
	body2 := uiText(t, s.URL)
	for _, must := range []string{"v1/wiki", "主張にする", "promote"} {
		if !strings.Contains(body2, must) {
			t.Fatalf("the page must have the knowledge block; missing %q", must)
		}
	}
}
