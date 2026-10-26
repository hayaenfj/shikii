package main

import (
	"os"
	"strings"
	"testing"
)

// 0.9.3: the person asked that the skill says briefly how to remove shikii, so an agent asked to "uninstall shikii" runs the dry run,
// confirms with the person, keeps the data, and passes on the rm command instead of deleting files by hand.
func TestSkillSaysHowToUninstall(t *testing.T) {
	b, err := os.ReadFile("../../skills/shikii/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shikii uninstall", "--yes", "data", "rm"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("SKILL.md must say how to uninstall (missing %q)", want)
		}
	}
}
