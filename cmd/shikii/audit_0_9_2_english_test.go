package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

// Audit 0.9.2 (English): behaviour for a person whose language setting is English.
var auditJapanese = regexp.MustCompile("[\u3040-\u30ff\u4e00-\u9fff]")

// The agent's organizing becomes the needs-plan decision's input (sent to the decision model and kept in records). For an English user it starts with a Japanese label.
func TestAuditEnglishPlanningInputHasNoJapanese(t *testing.T) {
	out, err := renderPlanningFact(&planningFact{Summary: "Write scripts/check-totals.sh and run it", Evidence: []planningQuote{{Source: "current_user", Quote: "Write a script"}}},
		map[string]string{"current_user": "Write a script at scripts/check-totals.sh"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if auditJapanese.MatchString(out) {
		t.Fatalf("the decision input for an English user contains Japanese: %q", out)
	}
}

// With the English setting, "unsure" tells the person only the variant's name; the Japanese notice also carries the agent's view of the difference and both wordings.
func TestAuditEnglishUnsureNoticeCarriesTheDifference(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	delete(cfg, "language")
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	diff := "the candidate no longer allows supporting files such as tests"
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "unsure", "difference": diff}); err != nil {
		t.Fatal(err)
	}
	s := wordingSummary(agent)
	if !strings.Contains(s, "undecided") && !strings.Contains(s, "could not decide") {
		t.Fatalf("no unsure notice: %q", s)
	}
	if !strings.Contains(s, diff) {
		t.Fatalf("the English notice does not tell the person what the difference is: %q", s)
	}
}

// Two `shikii --init` at once (two projects, one HOME) rewrite ~/.claude/skills/shikii at the same time. Before the lock, a copy failed with
// "directory not empty" or left the skill without SKILL.md. Checked in one run instead of waiting for copies to race: right before the old
// skill is deleted, the lock next to it cannot be taken from outside, so another copy waits until this one has put every file back.
func TestAuditSkillCopyHoldsTheLock(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if err := copySkill(dst); err != nil { // a skill already there, as on every --init after the first
		t.Fatal(err)
	}
	tried := errors.New("whileCopyingSkill did not run")
	old := whileCopyingSkill
	t.Cleanup(func() { whileCopyingSkill = old })
	whileCopyingSkill = func() { tried = tryLockFromOutside(filepath.Join(filepath.Dir(dst), ".shikii-skill.lock")) }
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	if tried != syscall.EWOULDBLOCK {
		t.Fatalf("the skill is rewritten while its lock is held: taking it from outside gave %v", tried)
	}
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatal("the skill was left without SKILL.md")
	}
}

// Audit 0.9.2 (English), low 8: Claude first passed agreed as an array in 7 runs out of 7. The notice before the request and the Stop block
// show that agreed and open are objects, with the shape.
func TestPlanningTextsSayAgreedAndOpenAreObjects(t *testing.T) {
	for name, text := range map[string]string{"notice": planningNotice("s", "t"), "block": planningBlockReason("s", "t", 1)} {
		if !strings.Contains(text, "not an array") || !strings.Contains(text, `"agreed":{"summary"`) {
			t.Errorf("the %s shows that agreed and open are objects: %s", name, text)
		}
	}
	if b := planningBlockReason("s", "t", 1); !strings.Contains(b, "1 file was changed") {
		t.Errorf("one file is \"1 file was changed\": %s", b)
	}
	if b := planningBlockReason("s", "t", 3); !strings.Contains(b, "3 files were changed") {
		t.Errorf("several files: %s", b)
	}
}
