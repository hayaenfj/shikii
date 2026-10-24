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
