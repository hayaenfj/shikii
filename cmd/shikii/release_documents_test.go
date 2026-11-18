package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBundledMarkdownLinks(t *testing.T) {
	command := exec.Command("python3", filepath.Join("..", "..", "scripts", "test-build-release.py"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bundled Markdown must resolve local and versioned targets without changing license bytes: %v\n%s", err, output)
	}
}

// An option is not an output folder: build-release.py prints how to call it and stops before building, and makes no folder named
// after the option (0.37.2: `--help` built the release into ./--help).
func TestReleaseBuildRefusesAnOption(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "build-release.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"--help", "-h", "--out"} {
		dir := t.TempDir()
		command := exec.Command("python3", "-I", script, option)
		command.Dir = dir
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "usage: python3 -I scripts/build-release.py <output folder>") {
			t.Errorf("%s is refused with the usage: %v\n%s", option, err, output)
		}
		if _, statErr := os.Stat(filepath.Join(dir, option)); !os.IsNotExist(statErr) {
			t.Errorf("%s makes no folder: %v", option, statErr)
		}
	}
}
