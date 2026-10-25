package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 0.9.2: 聞き方を採用すると <id>-v<N>.yaml が増える。hook は <id>.yaml を直接読んでいたので、hook の 4 問は採用しても聞き方が変わらなかった。
// hookSpecPath は版の一番大きいファイルを返す。
func TestHookSpecPathUsesAdoptedVersion(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	base := filepath.Join(agent, "decisions", "changes-in-scope.yaml")
	if got := hookSpecPath(agent, "changes-in-scope"); got != base {
		t.Fatalf("採用の前は元のファイル: %s", got)
	}
	b, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, base)
	next := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(b), fmt.Sprintf("version: %d", sp.Version+1))
	if next == string(b) {
		t.Fatal("版の行が見つからない")
	}
	path := filepath.Join(agent, "decisions", fmt.Sprintf("changes-in-scope-v%d.yaml", sp.Version+1))
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hookSpecPath(agent, "changes-in-scope"); got != path {
		t.Fatalf("採用した版を使う: %s", got)
	}
	if got := hookSpecPath(agent, "done-as-asked"); filepath.Base(got) != "done-as-asked.yaml" {
		t.Fatalf("他の問いは元のまま: %s", got)
	}
	// 元に戻した版(.yaml で終わらない)は使わない
	if err := os.Rename(path, path+".reverted"); err != nil {
		t.Fatal(err)
	}
	if got := hookSpecPath(agent, "changes-in-scope"); got != base {
		t.Fatalf("元に戻した版は使わない: %s", got)
	}
}

// 問いのファイルの場所(decisions/<id>.yaml)を組むのは hookSpecPath だけ(許可リスト)。他の所で組むと、採用した版を読み落とす。
func TestOnlyHookSpecPathBuildsSpecFilePaths(t *testing.T) {
	pattern := regexp.MustCompile(`"decisions",\s*[^)]*\+\s*"\.yaml"|"decisions",\s*"[a-z-]+\.yaml"`)
	if !pattern.MatchString(`filepath.Join(agent, "decisions", id+".yaml")`) || !pattern.MatchString(`filepath.Join(root, "decisions", "needs-plan.yaml")`) {
		t.Fatal("検査の形が、固定の名で組む書き方を捕まえない")
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range pattern.FindAllStringIndex(string(data), -1) {
			if f == "hook_variants.go" && strings.Contains(string(data[max(0, m[0]-400):m[0]]), "func hookSpecPath(") {
				continue
			}
			line := strings.Count(string(data[:m[0]]), "\n") + 1
			t.Errorf("%s:%d が問いのファイルの場所を自分で組んでいる(hookSpecPath を使う)", f, line)
		}
	}
}
