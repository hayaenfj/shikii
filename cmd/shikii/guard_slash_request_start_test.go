package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// A slash command's stretch is compared with the request's start once it changes or commits something: a line the request's
// turn added before the slash command (the person interrupted the turn, so it had no Stop yet) is still an added line when
// /commit commits it. (Compared with the slash command's own listing, no Stop ever saw it as added.)
func TestGuardsStopALineTheRequestAddedWhenASlashCommandCommitsIt(t *testing.T) {
	const request = "add a paragraph about the storage layout to the README"
	stopped := func(root string) bool {
		for _, r := range guardRecordsFor(root, "no-word") {
			if !r.Pass && !r.Recheck && !r.Unverified {
				return true
			}
		}
		return false
	}
	t.Run("control: without the slash command the request's Stop stops the line", func(t *testing.T) {
		repo, root, readme := slashGuardRepo(t)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": request})
		submitOrganizing(t, "c", loadSession(root, "c").TurnID)
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "added"})
		if !strings.Contains(out, "- no-word:") || !stopped(root) {
			t.Fatalf("control: the request's Stop stops the forbidden line: %q", out)
		}
	})
	t.Run("the person interrupts and sends a slash command", func(t *testing.T) {
		repo, root, readme := slashGuardRepo(t)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": request})
		submitOrganizing(t, "c", loadSession(root, "c").TurnID)
		// the agent adds the forbidden line; the person interrupts (no Stop) and sends a slash command that commits the work
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "/commit"})
		gitCommitAll(t, repo, "docs: the storage layout\n\nProblem: the layout was not described.\nVerification: read the README.")
		out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "committed"})
		if !strings.Contains(out, "- no-word:") {
			t.Errorf("the Stop of the slash command's turn stops the line it committed: %q", out)
		}
		// the next request and its Stop: the line is in that request's listing by now
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "now add a section about the license to the README as well"})
		submitOrganizing(t, "c", loadSession(root, "c").TurnID)
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n\n## License\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out2 := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "added the license"})
		if !stopped(root) {
			t.Fatalf("the line the request's turn added (now committed) was never stopped by no-word: after the slash command %q, after the next request %q", out, out2)
		}
	})
}

// The check before a publication keeps the request's start too: a push in a slash command's turn is refused while the request's
// own line and commit fail the guards, as without the slash command.
func TestPushInASlashCommandTurnChecksTheRequestsWork(t *testing.T) {
	work := func(t *testing.T, slash bool) string {
		repo, root, readme := slashGuardRepo(t)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "add a paragraph about the storage layout to the README"})
		submitOrganizing(t, "c", loadSession(root, "c").TurnID)
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommitAll(t, repo, "docs: subject only")
		// the request's own Stops: stopped once, not fixed, and the turn ends (once per turn)
		if out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "added"}); !strings.Contains(out, "- no-word:") || !strings.Contains(out, "- commit-body:") {
			t.Fatalf("both guards stop the request's turn once: %q", out)
		}
		if out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "done", "stop_hook_active": true}); strings.Contains(out, `"decision":"block"`) {
			t.Fatalf("the turn ends at the Stop after the stop: %q", out)
		}
		if slash {
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "/push"})
		}
		return runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "c", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "git push origin HEAD"}})
	}
	t.Run("control: no slash command", func(t *testing.T) {
		if out := work(t, false); !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "no-word") || !strings.Contains(out, "commit-body") {
			t.Fatalf("control: the push is refused while the request's work fails the guards: %q", out)
		}
	})
	t.Run("the push comes in a slash command's turn", func(t *testing.T) {
		if out := work(t, true); !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Fatalf("the push of the request's unfixed line and commit is not refused after a slash command: %q", out)
		}
	})
}

// The Stop of a slash command's stretch fails the guards while the person's next request (or a second slash command) closes or
// replaces the stretch: nothing is said and nothing is kept.
func TestStopOfASlashCommandStretchOvertakenStaysSilent(t *testing.T) {
	for name, next := range map[string]string{"the next request": "now add a section about the license to the README as well", "a second slash command": "/commit again"} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := slashGuardRepo(t)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": "/commit"})
			stretch := loadSession(root, "s").GuardTurn
			if stretch == nil {
				t.Fatal("the slash command opened a stretch")
			}
			if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, "docs: subject only")
			ran := atTheStopsLock(t, root, func() {
				runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": next})
			})
			out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "committed"})
			if !ran() {
				t.Fatal("the Stop found the session lock held")
			}
			requireSilentStop(t, out)
			for _, r := range readGuardRecords(root) {
				if r.Session == "s" && r.Turn == stretch.ID {
					t.Errorf("the overtaken Stop kept a record of the stretch that is over: %+v", r)
				}
			}
			if shikiiStoppedSession(root, "s") {
				t.Errorf("the session counts as stopped by shikii")
			}
		})
	}
}

// A turn the routine check stopped once and the agent fixed; at the Stop after the fix records/guards.jsonl cannot be written.
// The Stop ends the turn incomplete before it judges anything: no completion decision is kept. (With the records written only
// at the commit of the session record, the decisions were already kept by then.)
func TestStopWhoseRecordsCannotBeWrittenKeepsNoCompletionDecision(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	done := func() int {
		recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
		if err != nil {
			t.Fatal(err)
		}
		return len(recs)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": repo, "prompt": "Please update the documentation with the complete installation steps."})
	turn := loadSession(root, "w").TurnID
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := recoveryOutput(t, runHook(t, "stop", map[string]any{"session_id": "w", "cwd": repo, "last_assistant_message": "Done."})); got["decision"] != "block" {
		t.Fatalf("the routine check stops the first Stop: %v", got)
	}
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "w", "turn_id": turn, "agreed": map[string]bool{"unknown": true}, "open": map[string]bool{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	// a directory where the records belong makes the write fail (the kept lines are moved aside and put back afterwards)
	p := guardRecordsPath(root)
	if err := os.Rename(p, p+".aside"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	requireRecoveryHalt(t, runHook(t, "stop", map[string]any{"session_id": "w", "cwd": repo, "last_assistant_message": "Done.", "stop_hook_active": true}))
	if n := done(); n != 0 {
		t.Errorf("the Stop that ended the turn incomplete (records not written) kept %d completion decision(s)", n)
	}
	if s := loadSession(root, "w"); s.Judged {
		t.Errorf("the turn that ended incomplete is not judged: %+v", s.RitualRecovery)
	}
}

// Answers whose labels carry no inputs hash (records from before inputs were hashed) are each their own answer: the rejection of
// one does not reach another answer to another request, and both are quoted before any assessment.
func TestRejectionDoesNotJoinAnswersWithoutAnInputsHash(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	sp, err := spec.Load(hookSpecPath(st.Dir, "needs-plan"))
	if err != nil {
		t.Fatal(err)
	}
	seed := func(request string, hash func(map[string]string) string) string {
		trace := store.NewTraceID()
		inputs := map[string]string{"request": request}
		at := time.Now().UTC()
		rec := store.Record{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", Inputs: inputs, InputsHash: hash(inputs), Top: "yes", P: .6, Band: "ask", At: at}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabel(store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", InputsHash: rec.InputsHash, Inputs: inputs, Truth: "yes", Weight: 1, Source: "ui", At: at}); err != nil {
			t.Fatal(err)
		}
		return trace
	}
	for name, hash := range map[string]func(map[string]string) string{
		"control: labels with inputs_hash": store.HashInputs,
		"labels without inputs_hash":       func(map[string]string) string { return "" },
	} {
		t.Run(name, func(t *testing.T) {
			st, scope, _ = reviewFixture(t)
			rejected := seed(pastSetupRequest, hash)
			other := seed("translate the README setup steps into Japanese and keep the badges", hash)
			// before any assessment the other answer is quoted (so a record without inputs_hash is not left out for another reason)
			ctx, err := deliverMemory(st.Dir, scope, similarSetupRequest, nil, true, &hookSession{TurnID: "dddddddddddddd00"}, "", "")
			if err != nil || !strings.Contains(ctx, "decision:"+other) {
				t.Fatalf("the other answer is quoted before any assessment: %v %q", err, cut(ctx, 300))
			}
			assessAnswer(t, st, scope, rejected, "unused", 1)
			assessAnswer(t, st, scope, rejected, "unused", 2)
			ctx, err = deliverMemory(st.Dir, scope, similarSetupRequest, nil, true, &hookSession{TurnID: "dddddddddddddd01"}, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(ctx, "decision:"+other) {
				t.Fatalf("another answer to another request is still quoted after %s was rejected: %q", rejected, cut(ctx, 300))
			}
		})
	}
}

// A command a guard refused before it ran is not a stopped Stop: a session whose only failed records are such refusals does
// not count as stopped by shikii, so another hook's continuation is not ended when the session record is gone.
func TestARefusedCommandDoesNotCountAsAStopByShikii(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	if err := appendGuardRecords(root, []guardRecord{{At: now, Session: "s", Turn: "t", Guard: "no-pipe-commit", Pass: false, Reason: "refused before it ran", Tool: true}}); err != nil {
		t.Fatal(err)
	}
	if shikiiStoppedSession(root, "s") {
		t.Errorf("a refused command alone must not count as a stop of the session")
	}
	if err := appendGuardRecords(root, []guardRecord{{At: now, Session: "s", Turn: "t", Guard: "no-word", Pass: false, Reason: "added lines contain a word"}}); err != nil {
		t.Fatal(err)
	}
	if !shikiiStoppedSession(root, "s") {
		t.Errorf("a failed check at a Stop counts as a stop of the session")
	}
}
