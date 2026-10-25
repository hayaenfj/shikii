package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.2: shikii no longer writes into a person's AGENTS.md. init and the update (hook install) remove only the section an earlier version
// wrote between the marker-only lines, and keep the person's text and the file (the owner: never delete the file; remove only what was added).
// The edge cases are those of the earlier "replace the section" tests (audit 0.9.0 A1-A3, new-1, the marker counting).

func agentsOld(heading string) string {
	return agentsMarker + "\n" + heading + "\n\n- old rules\n" + agentsMarker + "\n"
}

func TestRemoveAgentsSectionKeepsThePersonsTextAndTheFile(t *testing.T) {
	ja, en := agentsOld("## shikii(判定の作法)"), agentsOld("## shikii (decision rules)")
	cases := []struct{ name, in, want string }{
		{"Japanese section between the person's text", "# Rules\n\n- a person's line\n\n" + ja + "\n## After\n\n- stays\n", "# Rules\n\n- a person's line\n\n## After\n\n- stays\n"},
		{"English section at the end", "# Rules\n\n" + en, "# Rules\n"},
		{"ends with the closing marker, no newline", "# Rules\n\n" + strings.TrimSuffix(ja, "\n"), "# Rules\n"},
		{"only the section (the file stays, empty)", ja, ""},
		{"CRLF stays CRLF", strings.ReplaceAll("# Rules\n\n"+ja+"\n## After\n", "\n", "\r\n"), "# Rules\r\n\r\n## After\r\n"},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "AGENTS.md")
		if err := os.WriteFile(path, []byte(c.in), 0o644); err != nil {
			t.Fatal(err)
		}
		if did, err := removeAgentsSection(path); !did || err != nil {
			t.Fatalf("%s: removes the section: %v %v", c.name, did, err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: the file must stay: %v", c.name, err)
		}
		if string(b) != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, b, c.want)
		}
		if did, err := removeAgentsSection(path); did || err != nil {
			t.Fatalf("%s: the second run does nothing: %v %v", c.name, did, err)
		}
		if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
			t.Fatalf("%s: a temporary file was left: %v", c.name, entries)
		}
	}
}

func TestRemoveAgentsSectionLeavesWhatItCannotBeSureOf(t *testing.T) {
	var skipped agentsSkipped
	// a link inside the repo: the target loses the section, the link and the target's permissions stay
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Rules\n\n"+agentsOld("## shikii(判定の作法)")), 0o600)
	os.Symlink("CLAUDE.md", filepath.Join(dir, "AGENTS.md"))
	if did, err := removeAgentsSection(filepath.Join(dir, "AGENTS.md")); !did || err != nil {
		t.Fatalf("a link inside the repo: the target is edited: %v %v", did, err)
	}
	info, _ := os.Lstat(filepath.Join(dir, "AGENTS.md"))
	real, _ := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	if b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); info.Mode()&os.ModeSymlink == 0 || real.Mode().Perm() != 0o600 || string(b) != "# Rules\n" {
		t.Fatalf("link=%v perm=%v %q", info.Mode()&os.ModeSymlink != 0, real.Mode().Perm(), b)
	}
	// a link pointing outside the repo: left alone with a reason
	outside := filepath.Join(t.TempDir(), "shared.md")
	os.WriteFile(outside, []byte("# Shared\n\n"+agentsOld("## shikii(判定の作法)")), 0o644)
	repo := t.TempDir()
	os.Symlink(outside, filepath.Join(repo, "AGENTS.md"))
	if did, err := removeAgentsSection(filepath.Join(repo, "AGENTS.md")); did || !errors.As(err, &skipped) {
		t.Fatalf("a link pointing outside is left alone with a reason: %v %v", did, err)
	}
	// one marker, three marker lines, a marker only inside the person's text, no AGENTS.md: untouched
	for name, in := range map[string]string{
		"one marker":               "# Rules\n\n" + agentsMarker + "\n## shikii(判定の作法)\n\n- no closing marker\n",
		"three marker lines":       "# Rules\n\n" + agentsMarker + "\n## shikii(判定の作法)\n\n- old\n" + agentsMarker + "\n- a person's rule inside\n" + agentsMarker + "\n",
		"marker inside the person": "# Rules\n\n- shikii's section is wrapped in `" + agentsMarker + "`, but this repo has none\n",
	} {
		path := filepath.Join(t.TempDir(), "AGENTS.md")
		os.WriteFile(path, []byte(in), 0o644)
		did, err := removeAgentsSection(path)
		if did || (err != nil && !errors.As(err, &skipped)) {
			t.Fatalf("%s: untouched: %v %v", name, did, err)
		}
		if b, _ := os.ReadFile(path); string(b) != in {
			t.Fatalf("%s: the file changed: %q", name, b)
		}
	}
	missing := filepath.Join(t.TempDir(), "AGENTS.md")
	if did, err := removeAgentsSection(missing); did || err != nil {
		t.Fatalf("no AGENTS.md: nothing to do: %v %v", did, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("must not create AGENTS.md")
	}
}

// init does not add a section (a person's AGENTS.md stays byte for byte; none is created), and removes one an earlier version wrote.
// The update (hook install) removes the section in registered projects.
func TestInitAndUpdateRemoveTheSectionAndAddNone(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	plain, withOld, none := gitRepo(t), gitRepo(t), gitRepo(t)
	human := "# Office rules\n\n- Invoices go in invoices/.\n"
	os.WriteFile(filepath.Join(plain, "AGENTS.md"), []byte(human), 0o644)
	os.WriteFile(filepath.Join(withOld, "AGENTS.md"), []byte(human+"\n"+agentsOld("## shikii(判定の作法)")), 0o644)
	for _, d := range []string{plain, withOld, none} {
		t.Chdir(d)
		capture(t, func() error { return cmdInit(nil) })
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("init adds nothing to a person's AGENTS.md: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(withOld, "AGENTS.md")); string(b) != human {
		t.Fatalf("init removes the section of an earlier version and keeps the rest: %q", b)
	}
	if _, err := os.Stat(filepath.Join(none, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("init does not create AGENTS.md")
	}
	// the update: a registered project whose AGENTS.md got a section again (e.g. an older binary on another machine)
	os.WriteFile(filepath.Join(withOld, "AGENTS.md"), []byte(human+"\n"+agentsOld("## shikii (decision rules)")), 0o644)
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if b, _ := os.ReadFile(filepath.Join(withOld, "AGENTS.md")); string(b) != human {
		t.Fatalf("the update removes the section and keeps the rest: %q", b)
	}
	if !strings.Contains(out, "AGENTS.md") {
		t.Fatalf("the update says it removed the section: %s", out)
	}
	// 0.9.1 and earlier read pages written by 0.9.2 (English fixed words) as changed and refuse every save (as 0.8 did with 0.9.0's kind and scene)
	if !strings.Contains(out, "reopen") || !strings.Contains(out, "0.9.2 or later") {
		t.Fatalf("the update says to reopen open sessions and move every machine using the same wiki to 0.9.2 or later: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("the update leaves an AGENTS.md without a section alone: %q", b)
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
