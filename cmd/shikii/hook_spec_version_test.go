package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 0.9.2: adopting a wording adds <id>-v<N>.yaml. The hook read <id>.yaml directly, so adopting did not change the wording of the hook's 4 questions.
// hookSpecPath returns the file with the highest version.
func TestHookSpecPathUsesAdoptedVersion(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	base := filepath.Join(agent, "decisions", "changes-in-scope.yaml")
	if got := hookSpecPath(agent, "changes-in-scope"); got != base {
		t.Fatalf("before adoption, the original file: %s", got)
	}
	b, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, base)
	next := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(b), fmt.Sprintf("version: %d", sp.Version+1))
	if next == string(b) {
		t.Fatal("the version line was not found")
	}
	path := filepath.Join(agent, "decisions", fmt.Sprintf("changes-in-scope-v%d.yaml", sp.Version+1))
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hookSpecPath(agent, "changes-in-scope"); got != path {
		t.Fatalf("uses the adopted version: %s", got)
	}
	if got := hookSpecPath(agent, "done-as-asked"); filepath.Base(got) != "done-as-asked.yaml" {
		t.Fatalf("other questions stay as they were: %s", got)
	}
	// an undone version (not ending in .yaml) is not used
	if err := os.Rename(path, path+".reverted"); err != nil {
		t.Fatal(err)
	}
	if got := hookSpecPath(agent, "changes-in-scope"); got != base {
		t.Fatalf("an undone version is not used: %s", got)
	}
}

// Only hookSpecPath builds the path of a question file (decisions/<id>.yaml) (allow list). Building it elsewhere misses the adopted version.
func TestOnlyHookSpecPathBuildsSpecFilePaths(t *testing.T) {
	pattern := regexp.MustCompile(`"decisions",\s*[^)]*\+\s*"\.yaml"|"decisions",\s*"[a-z-]+\.yaml"`)
	if !pattern.MatchString(`filepath.Join(agent, "decisions", id+".yaml")`) || !pattern.MatchString(`filepath.Join(root, "decisions", "needs-plan.yaml")`) {
		t.Fatal("the check's pattern does not catch building with a fixed name")
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
			t.Errorf("%s:%d builds a question file path itself (use hookSpecPath)", f, line)
		}
	}
}
