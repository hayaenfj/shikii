package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// Property D1 of .shikii/handoff/audit-0.9.7/invariants.md (0.9.7 quality bar): while a page holds the project's lock, another start cannot
// take it, even when another hook peeks at the lock at the very moment of that start, and the peek sees the page as running. Checked in one
// run, without waiting on a race: the peek is made right at the second start's try (beforePageLockTry).
func TestSecondPageCannotTakeTheLockEvenWhilePeeked(t *testing.T) {
	common, scope := t.TempDir(), "/work/p"
	oldRetry, oldTry := pageLockRetry, beforePageLockTry
	t.Cleanup(func() { pageLockRetry, beforePageLockTry = oldRetry, oldTry })
	pageLockRetry = 0 // a start that cannot take the lock gives up after one try
	first, ok := holdPageLock(common, scope)
	if !ok {
		t.Fatal("the first page takes the free lock")
	}
	defer first.Close()
	var peeks []bool
	beforePageLockTry = func(int) { peeks = append(peeks, pageLockHeld(common, scope)) }
	if second, ok := holdPageLock(common, scope); ok {
		second.Close()
		t.Fatal("a second page of the same project took the lock the first page holds")
	}
	beforePageLockTry = oldTry
	if len(peeks) != 1 || !peeks[0] {
		t.Fatalf("the peek at the second start sees the first page running: %v", peeks)
	}
	if !pageLockHeld(common, scope) {
		t.Fatal("after the peek and the failed start, the first page still holds the lock")
	}
}

// Properties F1 to F3 (F2 as decided on 2026-10-27: no miss is tracked, and the short form alone is enough) over random sequences of requests where the agent sometimes submits its organizing and sometimes does not.
func TestPlanningNoticeFormOverRandomSessions(t *testing.T) {
	repo, root, _ := planningFixture(t)
	r := rand.New(rand.NewSource(97))
	const full, short = "putting topic words in query", "as earlier in this session"
	missedBefore, shortsInARow := false, 0
	var history []string
	for i := 0; i < 40; i++ {
		prompt := fmt.Sprintf("Request %d: use the agreed CSV columns and keep the existing filters.", i)
		notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
		isFull, isShort := strings.Contains(notice, full), strings.Contains(notice, short)
		if isFull == isShort {
			t.Fatalf("request %d: one form of the organizing notice: %q", i, notice)
		}
		switch {
		case i == 0 && !isFull:
			t.Fatalf("F1: the session's first notice is the full text\nhistory: %v", history)
		case missedBefore && !strings.Contains(notice, "hook plan submit"):
			t.Fatalf("F2: after a missed organizing the notice alone is enough to submit\nhistory: %v", history)
		}
		if isShort {
			shortsInARow++
		} else {
			shortsInARow = 0
		}
		if shortsInARow >= noticeFullEvery {
			t.Fatalf("F3: %d short notices in a row\nhistory: %v", shortsInARow, history)
		}
		submitted := r.Intn(4) != 0
		if submitted {
			if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
				t.Fatal(err)
			}
		}
		missedBefore = !submitted
		form := "short"
		if isFull {
			form = "full"
		}
		history = append(history, fmt.Sprintf("%s/submitted=%v", form, submitted))
	}
}
