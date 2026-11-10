package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func guardListLine(t *testing.T, root, id string) string {
	t.Helper()
	for _, line := range strings.Split(capture(t, func() error { return guardList(root) }), "\n") {
		if strings.HasPrefix(line, id+" ") {
			return line
		}
	}
	t.Fatalf("guard list has no line for %s", id)
	return ""
}

// When the turn rewrites the commit that was HEAD at the request (--amend, rebase), the request's HEAD is no longer an ancestor
// of the current HEAD and commit-body cannot tell which commits are the turn's. It does not stop the turn, and it does not
// claim the check passed either: the record says the check could not be verified, status counts neither a stop nor a fix, and
// guard list shows it as not checked. A commit stacked on top of the request's HEAD is still read and stops the turn.
func TestCommitBodyRecordsARewrittenRequestHeadAsNotVerified(t *testing.T) {
	for name, rewrite := range map[string]func(t *testing.T, repo string){
		"amend": func(t *testing.T, repo string) {
			gitAs(t, repo, "commit", "-q", "--amend", "-m", "docs: reworded, still without a body")
		},
		"rebase": func(t *testing.T, repo string) {
			gitAs(t, repo, "rebase", "-q", "--force-rebase", "HEAD~1")
		},
	} {
		t.Run(name, func(t *testing.T) {
			model := fakeAgentModel(t, func(string) float64 { return .1 })
			hookEnv(t, model)
			repo := gitRepo(t)
			t.Chdir(repo)
			capture(t, func() error { return cmdInit(nil) })
			root := filepath.Join(repo, ".shikii")
			readme := filepath.Join(repo, "README.md")
			if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, "init")
			if err := os.WriteFile(readme, []byte("# project\n\nmore\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			// dated in the past, so a commit that replaces it never gets the same id by being made within the same second
			gitAs(t, repo, "add", "-A")
			old := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "docs: subject only, made before the request")
			old.Env = append(os.Environ(), "GIT_COMMITTER_DATE=2026-10-21T00:00:00Z", "GIT_AUTHOR_DATE=2026-10-21T00:00:00Z")
			if out, err := old.CombinedOutput(); err != nil {
				t.Fatalf("git commit: %v %s", err, out)
			}
			writeGuard(t, root, guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "the body names the problem and the verification", Required: []string{"Problem", "Verification"}})

			runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "reword the last commit"})
			before := gitHead(repo)
			rewrite(t, repo)
			if after := gitHead(repo); after == before {
				t.Fatal("premise: the turn made a new commit in place of the request's HEAD")
			}
			out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "reworded"})
			recs := guardRecordsFor(root, "commit-body")
			if strings.Contains(out, "commit-body") {
				t.Fatalf("a check that could not be verified does not stop the turn: %q", out)
			}
			if len(recs) != 1 || recs[0].Pass || !recs[0].Unverified || recs[0].Recheck || !strings.Contains(recs[0].Reason, "could not be read") {
				t.Fatalf("the record says the commits could not be verified, not that the check passed: %+v", recs)
			}
			if line := guardsStatusLine(t, root); !strings.Contains(line, "1 of 1 enabled; stopped 0 turns, fixed before the turn ended 0") {
				t.Fatalf("status counts neither a stop nor a fix: %s", line)
			}
			if line := guardListLine(t, root, "commit-body"); !strings.Contains(line, "(last: not checked ") || !strings.Contains(line, "could not be read") || strings.Contains(line, "last: passed") || strings.Contains(line, "last: stopped") {
				t.Fatalf("guard list shows the last result as not checked: %s", line)
			}

			// the next turn stacks a commit without a body on top: that one is read and stops the turn
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "add a line to the README and commit it"})
			if err := os.WriteFile(readme, []byte("# project\n\nmore\n\nand more\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, "docs: another subject only")
			out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "committed"})
			recs = guardRecordsFor(root, "commit-body")
			if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "commit-body") {
				t.Fatalf("a commit stacked on the request's HEAD is read and stops the turn: %q", out)
			}
			if last := recs[len(recs)-1]; last.Pass || last.Unverified || last.Recheck {
				t.Fatalf("a commit that was read and lacks the words is a stop, not an unverified check: %+v", last)
			}
			if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 0") {
				t.Fatalf("status counts the stop of the commit that was read: %s", line)
			}
			if line := guardListLine(t, root, "commit-body"); !strings.Contains(line, "(last: stopped ") {
				t.Fatalf("guard list shows the stop: %s", line)
			}
		})
	}
}

// A record that could not be verified is neither a stop nor a fix anywhere it is counted: not in status, not as "this guard
// already stopped the turn", and not as a sign that shikii stopped the session. A stop followed by an unverified recheck stays
// stopped and not fixed. Records written before the field existed read as they always did.
func TestUnverifiedGuardRecordIsNeitherAStopNorAFix(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	if err := saveGuard(root, guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "the body names the problem", Required: []string{"Problem"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	guards, err := loadGuards(root)
	if err != nil {
		t.Fatal(err)
	}
	write := func(lines ...string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(guardRecordsPath(root)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(guardRecordsPath(root), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const unverified = `{"at":"2026-11-10T01:00:00Z","session_id":"s","turn_id":"t1","guard":"commit-body","pass":false,"reason":"the commits could not be read","unverified":true}`

	write(unverified)
	if s := countGuardStats(guards, readGuardRecords(root)); s.Stopped != 0 || s.Fixed != 0 {
		t.Fatalf("an unverified record alone is not a stop: %+v", s)
	}
	if shikiiStoppedSession(root, "s") {
		t.Fatal("an unverified record does not mean shikii stopped a turn of the session")
	}
	if stop, _ := turnStoppedGuards(root, "s", "t1"); stop["commit-body"] {
		t.Fatal("an unverified record does not use up the guard's one stop of the turn")
	}

	// a stop, then a recheck that could not be verified: still stopped, and not fixed
	write(
		`{"at":"2026-11-10T01:00:00Z","session_id":"s","turn_id":"t2","guard":"commit-body","pass":false,"reason":"the body lacks \"Problem\""}`,
		`{"at":"2026-11-10T01:01:00Z","session_id":"s","turn_id":"t2","guard":"commit-body","pass":false,"reason":"the commits could not be read","recheck":true,"unverified":true}`,
	)
	if s := countGuardStats(guards, readGuardRecords(root)); s.Stopped != 1 || s.Fixed != 0 {
		t.Fatalf("an unverified recheck does not fix the stop: %+v", s)
	}
	// the page's summary keeps the last result that was verified, and names the unverified check apart
	var summary struct {
		Stopped, Fixed int
		Items          []struct {
			ID   string
			Last *struct {
				Pass   bool
				Reason string
			}
			Unverified *struct{ Reason string }
		}
	}
	b, _ := json.Marshal(guardSummaryJSON(root))
	if err := json.Unmarshal(b, &summary); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range summary.Items {
		if it.ID != "commit-body" {
			continue
		}
		found = true
		if it.Last == nil || it.Last.Pass || !strings.Contains(it.Last.Reason, "lacks") || it.Unverified == nil || !strings.Contains(it.Unverified.Reason, "could not be read") {
			t.Fatalf("the summary keeps the verified result and names the unverified one apart: %s", b)
		}
	}
	if !found || summary.Stopped != 1 || summary.Fixed != 0 {
		t.Fatalf("the summary counts one stop and no fix: %s", b)
	}

	// records from before the field existed: a pass is a pass, a failure is a stop, a passing recheck is the fix
	write(
		`{"at":"2026-11-09T01:00:00Z","session_id":"s","turn_id":"t3","guard":"commit-body","pass":true}`,
		`{"at":"2026-11-09T02:00:00Z","session_id":"s","turn_id":"t4","guard":"commit-body","pass":false,"reason":"the body lacks \"Problem\""}`,
		`{"at":"2026-11-09T02:01:00Z","session_id":"s","turn_id":"t4","guard":"commit-body","pass":true,"recheck":true}`,
	)
	recs := readGuardRecords(root)
	if len(recs) != 3 || !recs[0].Pass || recs[0].Unverified || recs[1].Pass || recs[1].Unverified || !recs[2].Recheck {
		t.Fatalf("older records read as before: %+v", recs)
	}
	if s := countGuardStats(guards, recs); s.Stopped != 1 || s.Fixed != 1 {
		t.Fatalf("older records count as before: %+v", s)
	}
	if !shikiiStoppedSession(root, "s") {
		t.Fatal("an older failure still shows that shikii stopped a turn of the session")
	}
	if line := guardListLine(t, root, "commit-body"); !strings.Contains(line, "(last: passed 11-09 02:01)") {
		t.Fatalf("guard list reads an older record as before: %s", line)
	}
}
