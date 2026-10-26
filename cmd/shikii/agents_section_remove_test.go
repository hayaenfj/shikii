package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.2: shikii no longer writes into a person's AGENTS.md; 0.9.7: it no longer edits it either. A section an older version added is only
// told (compat_cleanup_test.go).

func agentsOld(heading string) string {
	return agentsMarker + "\n" + heading + "\n\n- old rules\n" + agentsMarker + "\n"
}

// init does not add a section (a person's AGENTS.md stays byte for byte; none is created), and the update says to reopen the sessions.
func TestInitAndUpdateAddNoSection(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	plain, none := gitRepo(t), gitRepo(t)
	human := "# Office rules\n\n- Invoices go in invoices/.\n"
	os.WriteFile(filepath.Join(plain, "AGENTS.md"), []byte(human), 0o644)
	for _, d := range []string{plain, none} {
		t.Chdir(d)
		capture(t, func() error { return cmdInit(nil) })
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("init adds nothing to a person's AGENTS.md: %q", b)
	}
	if _, err := os.Stat(filepath.Join(none, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("init does not create AGENTS.md")
	}
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	// 0.9.1 and earlier read pages written by 0.9.2 (English fixed words) as changed and refuse every save (as 0.8 did with 0.9.0's kind and scene)
	if !strings.Contains(out, "reopen") || !strings.Contains(out, "0.9.2 or later") {
		t.Fatalf("the update says to reopen open sessions and move every machine using the same wiki to 0.9.2 or later: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("the update leaves AGENTS.md alone: %q", b)
	}
}

// Without the AGENTS.md section, Codex without hooks found shikii's habits only when the skill was in ~/.agents/skills (agents-md-check, 0.9.2),
// and one OpenCode run recorded its own judgments as truths because the MCP instructions lacked the safeguard the section had.
func TestRulesReachAgentsWithoutTheAgentsSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	capture(t, func() error { return cmdInit(nil) })
	for _, d := range []string{filepath.Join(home, ".claude", "skills", "shikii"), filepath.Join(home, ".agents", "skills", "shikii")} {
		if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err != nil {
			t.Errorf("init puts the skill at %s: %v", d, err)
		}
	}
	for _, want := range []string{"never call shikii_label with your own judgment", "rule, test or procedure"} {
		if !strings.Contains(mcpInstructions, want) {
			t.Errorf("the MCP instructions carry %q: %s", want, mcpInstructions)
		}
	}
	if i, j := strings.Index(mcpInstructions, "never call shikii_label"), strings.Index(mcpInstructions, "shikii_decide"); i < 0 || i > j {
		t.Errorf("the safeguard comes first, before the tools: %s", mcpInstructions)
	}
}
