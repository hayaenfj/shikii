package main

import (
	"testing"
	"unicode/utf8"
)

// 0.9.3 audit 4: the 40 for a "done" truth counts characters, not bytes. A Japanese message under 40 characters is over 40 bytes
// (3 bytes a character), so counting bytes would turn most short Japanese corrections and answers back into "done" truths.
func TestAudit4MinimumCountsCharactersNotBytes(t *testing.T) {
	const reply = "違います、README の英語版の手順の方を直して" // 25 characters, 55 bytes
	if n := utf8.RuneCountInString(reply); n >= outcomeMovedMinRunes || len(reply) < outcomeMovedMinRunes {
		t.Fatalf("the fixture must be under the minimum in characters and over it in bytes: %d characters, %d bytes", n, len(reply))
	}
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, reply)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Fixed the English steps."})
	if l, ok := doneLabel(t, root, done); ok && l.Truth == "yes" {
		t.Fatalf("a Japanese correction under 40 characters must not label the previous request as done: %+v", l)
	}
}

// a session that moved to its next request between the sweep's first read and its lock keeps that request: the sweep must work on the
// record read inside the lock, never write back the copy it read before (test hole: the re-read was not caught, the dedup of outcome
// truths hid the second label and nothing checked the record)
func TestAudit4SweepDoesNotWriteBackItsFirstRead(t *testing.T) {
	repo, root, _ := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	const next = "Now add the upgrade notes for people on 0.8."
	old := sweepPeeked
	t.Cleanup(func() { sweepPeeked = old })
	sweepPeeked = func(id string) {
		sweepPeeked = func(string) {}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": id, "cwd": repo, "prompt": next})
	}
	sweepDeferrals(root, "")
	if s := loadSession(root, "planning"); s.Request != next {
		t.Fatalf("the sweep must not put back the record it read before the session moved on: request=%q", s.Request)
	}
}

// doneTraceAfter runs one request and its Stop in a session and returns that request's done-as-asked trace.
func doneTraceAfter(t *testing.T, repo, root, session, prompt string) string {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
	for _, r := range loadSession(root, session).Reviews {
		if r.Spec == "done-as-asked" {
			return r.Trace
		}
	}
	t.Fatalf("the request must have a done-as-asked decision: %+v", loadSession(root, session))
	return ""
}

// a short restatement (under 40 characters) still says the previous request was not done: the minimum is only for "done"
// (test hole: making the restatement need 40 characters too was not caught)
func TestAudit4ShortRestatementIsNotDone(t *testing.T) {
	const ask = "READMEのインストール手順を直して"
	repo, root, _ := planningFixture(t)
	done := doneTraceAfter(t, repo, root, "planning", ask)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": ask})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" || l.Weight != 1 {
		t.Fatalf("a short restatement must label the previous request as not done: %+v %v", l, ok)
	}
}

// a repeated acknowledgement is not a restatement: the same Japanese acknowledgement twice gets no "not done" (test hole: dropping the
// acknowledgement check in settleDone was not caught)
func TestAudit4RepeatedAckIsNotARestatement(t *testing.T) {
	const ack = "よろしくお願いします"
	repo, root, _ := planningFixture(t)
	done := doneTraceAfter(t, repo, root, "planning", ack)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": ack})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("an acknowledgement must not label the previous request: %+v", l)
	}
}

// another session's Stop sweeps too, as the documents say (test hole: removing the sweep from Stop was not caught)
func TestAudit4OtherSessionStopSweeps(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": repo, "last_assistant_message": "Explained."})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("another session's Stop must settle a stale deferral: %+v %v", l, ok)
	}
}

// a session that came back between the sweep's first read and its lock and opened a new deferral keeps it open: the sweep checks
// again inside the lock that the deferral is due (test hole: dropping that second check was not caught)
func TestAudit4SweepLeavesTheNewFreshDeferral(t *testing.T) {
	repo, root, _ := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	old := sweepPeeked
	t.Cleanup(func() { sweepPeeked = old })
	sweepPeeked = func(id string) {
		sweepPeeked = func(string) {}
		runHook(t, "stop", map[string]any{"session_id": id, "cwd": repo, "last_assistant_message": "Wrote them."})
		runHook(t, "user-prompt-submit", map[string]any{"session_id": id, "cwd": repo, "prompt": "Explain how the hook decides which wiki lessons to deliver with each request."})
		if !openDeferral(loadSession(root, id)) {
			t.Error("the new turn's outcome must wait for its organizing")
		}
	}
	sweepDeferrals(root, "")
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("a fresh deferral opened while the sweep waited for the lock must stay open")
	}
}
