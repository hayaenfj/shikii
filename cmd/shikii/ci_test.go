package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Audit 0.8.1 item 2: CI runs with read-only permissions, cancels older runs on the same branch, and caches npm downloads (CI cost).
func TestCIWorkflowIsReadOnlyAndCached(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	// Audit 0.8.2-1 item 7: no write permissions mixed in, and concurrency has a group per ref
	if strings.Contains(ci, ": write") || strings.Contains(ci, "write-all") {
		t.Error("test.yml has a write permission")
	}
	for _, want := range []string{
		"permissions:\n  contents: read\nconcurrency:",
		"group: test-${{ github.ref }}",
		"cancel-in-progress: true",
		"cache: npm",
		"cache-dependency-path: web/package-lock.json",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("test.yml does not have %q", want)
		}
	}
}

// 0.8.2: CI checks for leaked secrets (gitleaks, version and hash pinned, history included) and known vulnerabilities in the Go packages and standard library in use
// (govulncheck, version pinned). The Go version is aligned through go.mod's toolchain to a version with the vulnerabilities fixed.
// Audit 0.8.2-2 B2: compare the security job's steps to a fixed sequence by exact match (an allowlist), not by whether strings are present.
// Removing the hash check, commenting out govulncheck, or swallowing gitleaks failures all fail.
func TestCIScansSecretsAndVulnerabilities(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
				Run  string            `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatal(err)
	}
	job, ok := wf.Jobs["security"]
	if !ok {
		t.Fatal("test.yml has no security job")
	}
	var got []string
	for _, st := range job.Steps {
		switch {
		case st.Uses != "":
			got = append(got, "uses "+st.Uses+" "+st.With["fetch-depth"]+st.With["go-version-file"])
		default:
			got = append(got, strings.TrimSpace(st.Run))
		}
	}
	want := []string{
		"uses actions/checkout@v4 0",
		"uses actions/setup-go@v5 go.mod",
		`curl -fsSL -o /tmp/gitleaks.tar.gz https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz
echo "551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb  /tmp/gitleaks.tar.gz" | sha256sum -c -
tar -xzf /tmp/gitleaks.tar.gz -C /tmp gitleaks
/tmp/gitleaks git --redact --no-banner .`,
		"go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the security job's steps differ from the fixed sequence:\n got %q\nwant %q", got, want)
	}
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "\ntoolchain go1.27.3\n") {
		t.Error("go.mod's toolchain is not go1.27.3 (GO-2026-6809 was fixed in go1.27.3)")
	}
}
