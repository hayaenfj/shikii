package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 監査 0.8.1 の 2: CI は読むだけの権限で走り、同じ枝の古い実行を止め、npm の取得をキャッシュする(CI の費用)。
func TestCIWorkflowIsReadOnlyAndCached(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	// 監査 0.8.2-1 の 7: 書き込みの権限が混ざらない、concurrency は ref ごとの group を持つ
	if strings.Contains(ci, ": write") || strings.Contains(ci, "write-all") {
		t.Error("test.yml に書き込みの権限がある")
	}
	for _, want := range []string{
		"permissions:\n  contents: read\nconcurrency:",
		"group: test-${{ github.ref }}",
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
// 監査 0.8.2-2 の B2: 文字列の有無ではなく、security の job の手順を決まった並びと完全一致で比べる(許可リスト)。
// ハッシュの照合を外す・govulncheck をコメントにする・gitleaks の失敗を握りつぶす、のどれも落ちる。
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
		t.Fatal("test.yml に security の job が無い")
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
		t.Fatalf("security の job の手順が決まった並びと違う:\n got %q\nwant %q", got, want)
	}
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "\ntoolchain go1.27.3\n") {
		t.Error("go.mod の toolchain が go1.27.3 でない(GO-2026-6809 は go1.27.3 で直った)")
	}
}
