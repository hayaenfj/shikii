package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReleaseBundledMarkdownLinks(t *testing.T) {
	command := exec.Command("python3", filepath.Join("..", "..", "scripts", "test-build-release.py"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bundled Markdown must resolve local and versioned targets without changing license bytes: %v\n%s", err, output)
	}
}
