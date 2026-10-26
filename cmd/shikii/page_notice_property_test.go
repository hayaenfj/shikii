package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Properties D1 and D2 of .shikii/handoff/audit-0.9.7/invariants.md (0.9.7 quality bar): pages of one project started at the same time,
// with other hooks peeking at the lock meanwhile, end with exactly one holding it.
func TestExactlyOnePageWinsEvenWhilePeeked(t *testing.T) {
	for round := 0; round < 40; round++ {
		common, scope := t.TempDir(), fmt.Sprintf("/work/p%d", round)
		var stop atomic.Bool
		var peeks sync.WaitGroup
		for i := 0; i < 3; i++ {
			peeks.Add(1)
			go func() {
				defer peeks.Done()
				for !stop.Load() {
					pageLockHeld(common, scope)
				}
			}()
		}
		var wins, losses atomic.Int32
		var starts sync.WaitGroup
		release := make(chan struct{})
		for i := 0; i < 8; i++ {
			starts.Add(1)
			go func() {
				defer starts.Done()
				if f, ok := holdPageLock(common, scope); ok {
					wins.Add(1)
					<-release // a page holds the lock for its whole life
					f.Close()
				} else {
					losses.Add(1)
				}
			}()
		}
		// the winner holds on until every other start has given up after its short retry
		for wins.Load()+losses.Load() < 8 && losses.Load() < 8 {
			if wins.Load() > 1 {
				break
			}
		}
		stop.Store(true)
		peeks.Wait()
		close(release)
		starts.Wait()
		if wins.Load() != 1 {
			t.Fatalf("round %d: %d pages took the lock of one project", round, wins.Load())
		}
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
