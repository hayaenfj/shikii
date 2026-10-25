package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit 0.9.2 (behavior), low 4: what the English notice about a changed wording shows.

// Low 4: with the English setting, the notice that the wording was changed shows the current and the new question (shortened), not only the candidate's name.
func TestWordingAdoptedNoticeInEnglishShowsBothQuestions(t *testing.T) {
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
	if got, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"}); err != nil || got["adopted"] != true {
		t.Fatalf("adopt: %v %v", got, err)
	}
	s := wordingSummary(agent)
	for _, want := range []string{"current: " + cut(res.Current.Question, 30), "new: " + cut(res.WinnerWording.Question, 30)} {
		if !strings.Contains(s, want) {
			t.Fatalf("the English notice shows %q: %q", want, s)
		}
	}
	if long := trim(res.Current.Question, wordingNoticeRunes); len([]rune(res.Current.Question)) > wordingNoticeRunes && !strings.Contains(s, long) {
		t.Fatalf("a long question is shortened: %q", s)
	}
}
