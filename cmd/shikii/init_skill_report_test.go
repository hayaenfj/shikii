package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shikii init and shikii hook install say what they did to the skill: "updated" for a skill they wrote, "up to date" for one they left
// as it was. They said "updated" every time, so a person who read that nothing changes (the fix of 0.43.2) saw it change on every run.
// The runs are under umask 027, which the skill left alone only under 022 before.
func TestInitAndHookInstallSayWhetherTheSkillWasWrittenOrLeft(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	repo := gitRepo(t)
	t.Chdir(repo)
	underUmask(t, 0o027)
	says := func(out, what string) {
		t.Helper()
		for _, rel := range skillDirs {
			if !strings.Contains(out, "skill: "+what+" "+filepath.Join("~", rel)+"\n") {
				t.Errorf("init should say %q for %s:\n%s", "skill: "+what, rel, out)
			}
		}
		other := map[string]string{"updated": "up to date", "up to date": "updated"}[what]
		if strings.Contains(out, "skill: "+other) {
			t.Errorf("init says %q although every skill is %s:\n%s", "skill: "+other, what, out)
		}
	}
	// the first run under 027 writes the skill the modes this umask gives; the second finds nothing to change
	capture(t, func() error { return cmdInit(nil) })
	says(capture(t, func() error { return cmdInit(nil) }), "up to date")
	// a skill that is not the embedded one is written, the other one is not
	if err := os.WriteFile(filepath.Join(home, skillDirs[0], "old.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "skill: updated "+filepath.Join("~", skillDirs[0])+"\n") || !strings.Contains(out, "skill: up to date "+filepath.Join("~", skillDirs[1])+"\n") {
		t.Errorf("init should say updated for the skill with a stale file and up to date for the other:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, skillDirs[0], "old.md")); !os.IsNotExist(err) {
		t.Errorf("the stale file stayed: %v", err)
	}
	// hook install puts the rules in place, or says they were already there
	install := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	for _, rel := range skillDirs {
		if !strings.Contains(install, filepath.Join("~", rel)+": the agent's rules were already in place, nothing changed\n") {
			t.Errorf("hook install should say the rules of %s were already in place:\n%s", rel, install)
		}
	}
	if err := os.WriteFile(filepath.Join(home, skillDirs[1], "old.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	install = capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if !strings.Contains(install, filepath.Join("~", skillDirs[1])+": the agent's rules put in place\n") || !strings.Contains(install, filepath.Join("~", skillDirs[0])+": the agent's rules were already in place") {
		t.Errorf("hook install should put the rules with a stale file in place and say the other was already there:\n%s", install)
	}
}
