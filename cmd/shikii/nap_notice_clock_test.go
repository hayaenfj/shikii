package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A clock set a little back (the last notice reads a few seconds ahead) leaves the notice before it as the place to count from.
func TestNapNoticeAfterASmallClockStepBackKeepsCountingFromTheNoticeBefore(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	before := now.Add(-48 * time.Hour)
	saveWorkResults(t, st, scope, 0, 7, now.Add(-72*time.Hour)) // before the notice before: not counted
	saveWorkResults(t, st, scope, 100, 5, now.Add(-time.Hour))  // after it
	text := formatNapMark(now.Add(30*time.Second), before, time.Time{})
	if err := os.WriteFile(napStatePath(st), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := noticeContext(t, startSession(t, scope, nil))
	since := before.UTC().Format(time.RFC3339)
	if !strings.Contains(ctx, "5 work results since "+since) {
		t.Fatalf("the re-notice counts from the notice before (%s):\n%s\nfile: %q", since, ctx, readNapStateFile(t, st))
	}
}
