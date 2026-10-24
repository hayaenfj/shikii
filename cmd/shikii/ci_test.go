package main

import (
	"os"
	"strings"
	"testing"
)

// 監査 0.8.1 の 2: CI は読むだけの権限で走り、同じ枝の古い実行を止め、npm の取得をキャッシュする(CI の費用)。
func TestCIWorkflowIsReadOnlyAndCached(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	for _, want := range []string{
		"permissions:\n  contents: read\n",
		"cancel-in-progress: true",
		"cache: npm",
		"cache-dependency-path: web/package-lock.json",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("test.yml に %q が無い", want)
		}
	}
}

// 0.8.2: CI で秘密の混入(gitleaks、版とハッシュを固定し履歴も見る)と、使っている Go の部品・標準ライブラリの
// 既知の脆弱性(govulncheck、版を固定)を見る。Go の版は go.mod の toolchain で脆弱性の直った版に揃える。
func TestCIScansSecretsAndVulnerabilities(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	for _, want := range []string{
		"fetch-depth: 0",
		"gitleaks_8.30.1_linux_x64.tar.gz",
		"551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb",
		"gitleaks git --redact",
		"golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("test.yml に %q が無い", want)
		}
	}
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "\ntoolchain go1.27.3\n") {
		t.Error("go.mod の toolchain が go1.27.3 でない(GO-2026-6809 は go1.27.3 で直った)")
	}
}
