package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Second 0.9.3 audit: Codex's shared registration (args = ["mcp"]) dropped --local, which every other registration and the hook pass.
// Without a key, shikii_decide on Codex then had no endpoint at all ("no permitted endpoint") where 0.9.2 reached the local model.
func TestAudit2CodexRegistrationKeepsTheLocalModel(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.toml")
	if did, err := addCodexServer(fresh, codexServerArgs); err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	b, _ := os.ReadFile(fresh)
	if !strings.Contains(string(b), `"--local", "`+hookLocalURL+`"`) || strings.Contains(string(b), "--store") {
		t.Fatalf("Codex's registration must keep the local model and name no store:\n%s", b)
	}
	// older forms (the 0.9.2 one, args = ["mcp"] of 0.9.3 builds) are only told since 0.9.7: TestOlderCodexFormIsOnlyTold
}

// Second 0.9.3 audit: a tune run through the MCP server for a named project (Codex: the server's folder is not the project) did not find the
// claim draft a tune from the project folder wrote (its retest spec is relative to the project folder), so it wrote the draft again over it:
// the earlier evidence and the person's notes in the draft were lost.
func TestAudit2TuneFromAnotherFolderKeepsTheClaimDraft(t *testing.T) {
	proj := t.TempDir()
	decisions := filepath.Join(proj, ".shikii", "decisions")
	os.MkdirAll(decisions, 0o755)
	abs := writeSpec(t, decisions, "false")
	scores := func(sp0 string) ([]candidate, []candScore) {
		sp := mustLoad(t, sp0)
		cands := []candidate{{"current", sp}, {"c1", sp}}
		f, tr := make([]bool, 8), []bool{true, true, true, true, true, true, true, true}
		w := []float64{1, 1, 1, 1, 1, 1, 1, 1}
		return cands, []candScore{{name: "current", model: "m", correct: f, weights: w, acc: 0}, {name: "c1", model: "m", correct: tr, weights: w, acc: 1}}
	}
	// 1. from the project folder (the CLI, or Claude Code's server started there)
	t.Chdir(proj)
	cands, sc := scores(filepath.Join(".shikii", "decisions", "intent.yaml"))
	if drafted, _, err := updateClaims(filepath.Join(".shikii", "wiki"), cands[0].sp, cands, sc, "run1", 8); err != nil || drafted != 1 {
		t.Fatalf("drafted=%d err=%v", drafted, err)
	}
	draft := filepath.Join(proj, ".shikii", "wiki", "patterns", "drafts", "intent-c1.md")
	b, _ := os.ReadFile(draft)
	os.WriteFile(draft, append(b, []byte("\nNOTE BY THE PERSON\n")...), 0o644)
	// 2. from another folder, with the absolute paths the MCP server uses for a named project
	t.Chdir(t.TempDir())
	cands, sc = scores(abs)
	drafted, updated, err := updateClaims(filepath.Join(proj, ".shikii", "wiki"), cands[0].sp, cands, sc, "run2", 8)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(draft)
	if drafted != 0 || updated != 1 || !strings.Contains(string(b), "NOTE BY THE PERSON") || strings.Count(string(b), "run: run") != 2 {
		t.Fatalf("the second run must add its evidence to the same draft: drafted=%d updated=%d\n%s", drafted, updated, b)
	}
}

// Second 0.9.3 audit (test holes): a working folder below a registered project outside git is found by walking up; project is taken out
// whatever its case, only once, and only as a string.
func TestAudit2MCPProjectWalkUpAndArgumentKeys(t *testing.T) {
	plain := t.TempDir() // not a git repository
	hookEnv(t, nil, plain)
	if top := projectScopeAt(plain); top != feedbackScope(plain) {
		t.Skipf("the temporary folder is inside git (%s)", top)
	}
	sub := filepath.Join(plain, "src", "deep")
	os.MkdirAll(sub, 0o755)
	t.Chdir(t.TempDir())
	write := func(id int, args map[string]any) map[string]any {
		args["operation"], args["path"] = "handoff_write", "w.md"
		return mcpCall(id, "shikii_wiki", args)
	}
	answers, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:1"},
		mcpInit(1),
		write(2, map[string]any{"project": sub, "content": "walked up"}),
		write(3, map[string]any{"PROJECT": plain, "content": "any case"}),
		write(4, map[string]any{"project": plain, "Project": plain, "content": "twice"}),
		write(5, map[string]any{"project": 1, "content": "number"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 3} {
		if text, isErr := toolText(t, answers, id); isErr {
			t.Fatalf("id %d: %s", id, text)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(plain, ".shikii", "handoff", "w.md")); string(got) != "any case" {
		t.Fatalf("both calls write to the registered project above: %q", got)
	}
	for id, must := range map[int]string{4: "only once", 5: "must be a string"} {
		if text, isErr := toolText(t, answers, id); !isErr || !strings.Contains(text, must) {
			t.Fatalf("id %d: %v %s", id, isErr, text)
		}
	}
}
