package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// audit 6 (0.9.3): a turn whose first Stop came without a reply, judged late at the next request, is not judged with the previous
// turn's reply (Previous still holds it)
func TestAudit6LateJudgmentDoesNotUseThePreviousTurnsReply(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OKです")
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": ""})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the Stop of a short message asks for continues_previous: %s", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("premise: the asked turn is judged late: %d records", len(recs))
	}
	for _, r := range recs {
		if r.Inputs["request"] == "OKです" && strings.Contains(r.Inputs["summary"], "Kept the filters") {
			t.Fatalf("the late judgment used the previous turn's reply as this turn's reply: %q", r.Inputs)
		}
	}
}

// audit 6 (0.9.3): a turn the person's guard stopped, whose Stop after the block never came, does not count as "fixed before the turn
// ended" because the tree at the person's next request passes (the person may have fixed it; the late judgment is not a Stop)
func TestAudit6LateJudgmentDoesNotCountAGuardAsFixed(t *testing.T) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	notes := filepath.Join(repo, "NOTES.md")
	if err := os.WriteFile(notes, []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote the notes."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the guard stops the turn: %s", out)
	}
	// Esc: no Stop after the block. The person fixes the file and asks the next thing
	if err := os.WriteFile(notes, []byte("a fine word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	guards, err := loadGuards(root)
	if err != nil {
		t.Fatal(err)
	}
	if s := countGuardStats(guards, readGuardRecords(root)); s.Stopped != 1 || s.Fixed != 0 {
		t.Fatalf("a guard that was not rechecked at a Stop must not count as fixed: %+v", s)
	}
}

// audit 6 (0.9.3): a long request the person's guard stopped is not recorded at the Stop after the block as a short message whose
// grouping was asked for (the page shows the record's reason as the last result of the plan ritual)
func TestAudit6GuardStoppedLongTurnRecordsNoGroupingRitual(t *testing.T) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	long := "please also add a section about the archive format to the README and link it from the install steps"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": long})
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Committed."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the guard stops the turn: %s", out)
	}
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Committed.", "stop_hook_active": true})
	for _, r := range readGuardRecords(root) {
		if r.Guard == ritualPlan && strings.Contains(r.Reason, "short message") {
			t.Fatalf("a long request got a grouping ritual record it was never asked for: %+v", r)
		}
	}
}

// audit 6 (0.9.3): requests that leave the recent turns keep only what restates uses (id, the person's text, the done trace, the time):
// the agent's reply is not kept for the session's lifetime
func TestAudit6EarlierKeepsNoReply(t *testing.T) {
	repo, root, _ := planningFixture(t)
	restateFarTurns(t, repo, root, "planning")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateOther})
	p := loadSession(root, "planning").Planning
	if p == nil || len(p.Earlier) == 0 {
		t.Fatal("premise: older requests moved to Earlier")
	}
	for _, e := range p.Earlier {
		if e.Summary != "" {
			t.Fatalf("Earlier keeps the agent's reply, which restates never reads: %+v", e)
		}
	}
}

// audit 6 (0.9.3): once the Stop after the block has judged the asked turn, the turn is like any other: a completion notice whose
// Stop never comes does not make the next request judge the same request again (a plain turn is not judged again either)
func TestAudit6JudgedAskedTurnIsNotJudgedAgainAfterANotice(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OKです")
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
	if s := loadSession(root, "planning"); !s.Judged {
		t.Fatal("premise: the Stop after the block judges the turn")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>background job finished</task-notification>"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range recs {
		if r.Inputs["request"] == "OKです" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the asked turn was judged %d times (once by the Stop after the block)", n)
	}
}

// audit 6 (0.9.3, test holes J2-J4): a completion notice, a slash command or a child's prompt during the continuation after the block
// is not the person's next request: the asked turn is not judged late by it
func TestAudit6LateJudgmentWaitsForThePersonsRequest(t *testing.T) {
	for name, in := range map[string]map[string]any{
		"notice": {"prompt": "<task-notification>background job finished</task-notification>"},
		"slash":  {"prompt": "/compact"},
		"child":  {"prompt": "search the repository for the archive format", "agent_id": "child-1"},
	} {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, "OKです")
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
			in["session_id"], in["cwd"] = "planning", repo
			runHook(t, "user-prompt-submit", in)
			recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range recs {
				if r.Inputs["request"] == "OKです" {
					t.Fatalf("%s judged the asked turn as if the person had asked the next thing", name)
				}
			}
		})
	}
}
