package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func auditETree(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out[rel] = info.Size()
		}
		return nil
	})
	return out
}

func auditENew(before, after map[string]int64) []string {
	var out []string
	for name := range after {
		if _, ok := before[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// audit E (INVARIANTS 4): requests whose JSON form is much longer than their text (a pasted page of markup: every <, > and & is
// six bytes in the reply). The CLI widens its default up to the largest bound a caller can name.
func TestAuditECLIPrepareWithARequestThatGrowsAsJSON(t *testing.T) {
	for _, c := range []struct {
		name string
		unit string
		size int
	}{
		{"markup, 20 KiB", "<td>&nbsp;</td>\n", 20 << 10},
		{"markup, 48 KiB", "<td>&nbsp;</td>\n", 48 << 10},
		{"markup, 64 KiB", "<td>&nbsp;</td>\n", 64 << 10},
		{"angle brackets only, 64 KiB", "<<<<>>>>\n", 64 << 10},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			t.Chdir(repo)
			prompt := "Fix the table of the export page. The page follows.\n" + strings.Repeat(c.unit, c.size/len(c.unit))
			notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
			ids := auditIDs.FindString(notice)
			if ids == "" {
				t.Fatalf("the notice carries the ids: %.300s", notice)
			}
			var out string
			var err error
			withStdin(t, strings.TrimSuffix(ids, "}")+`,"query":"export page table"}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
			encoded, _ := json.Marshal(strings.TrimSpace(prompt))
			if err != nil {
				t.Errorf("INVARIANT 4: the request is %d bytes of text (%d bytes as JSON), within the 64 KiB the hook keeps; the call the notice asks for is refused and the agent holds no receipt: %v", len(prompt), len(encoded), err)
				return
			}
			var reply map[string]any
			if e := json.Unmarshal([]byte(out), &reply); e != nil {
				t.Fatalf("%v: %.200s", e, out)
			}
			task := fmt.Sprint(reply["task"])
			t.Logf("request %d bytes (%d as JSON): reply %d bytes, task %d bytes, receipt=%v", len(prompt), len(encoded), len(out), len(task), reply["receipt"] != nil)
			if !strings.HasPrefix(strings.TrimSpace(prompt), task) || len(task) < min(len(strings.TrimSpace(prompt)), 64<<10)-4 {
				t.Errorf("the task is cut: %d of %d bytes", len(task), len(prompt))
			}
		})
	}
}

// audit E: what a preparation that the CLI runs up to three times leaves in the store, beside what one preparation with the bound
// named leaves. A receipt without hook ids, with a task that grows as JSON.
func TestAuditEWhatTheWidenedStandalonePreparationLeaves(t *testing.T) {
	task := "Check the export page: " + strings.Repeat("<td>&</td>", 1500) // 15,000 bytes of text, about 60,000 as JSON
	run := func(input string) (string, []string, error) {
		repo, root, _ := planningFixture(t)
		t.Chdir(repo)
		before := auditETree(t, root)
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
		return out, auditENew(before, auditETree(t, root)), err
	}
	request, _ := json.Marshal(task)
	named, namedFiles, err := run(`{"query":"export page table","request":` + string(request) + `,"max_bytes":393216}`)
	if err != nil {
		t.Fatalf("precondition: the preparation with the bound named: %v", err)
	}
	widened, widenedFiles, err := run(`{"query":"export page table","request":` + string(request) + `}`)
	if err != nil {
		t.Fatalf("the preparation without a bound: %v", err)
	}
	t.Logf("with max_bytes named: reply %d bytes, %d new files: %v", len(named), len(namedFiles), namedFiles)
	t.Logf("without a bound:      reply %d bytes, %d new files: %v", len(widened), len(widenedFiles), widenedFiles)
	count := func(files []string, prefix, suffix string) int {
		n := 0
		for _, f := range files {
			if strings.HasPrefix(f, prefix) && strings.HasSuffix(f, suffix) {
				n++
			}
		}
		return n
	}
	if a, b := count(namedFiles, "sessions/work-", ".json"), count(widenedFiles, "sessions/work-", ".json"); b > a {
		t.Errorf("one preparation left %d session records of its own (sessions/work-*.json); the same preparation with the bound named leaves %d: the attempts the CLI ran before it widened its bound each left one, and no receipt names them", b, a)
	}
}
