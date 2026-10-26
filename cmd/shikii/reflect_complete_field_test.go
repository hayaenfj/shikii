package main

import (
	"strings"
	"testing"
)

// 0.9.5 check: the reflection notice said "fix any error with complete:false and resend", but submit has no complete field and refuses it
// ("json: unknown field \"complete\""), so an agent that followed the notice was refused. The notice says to fix the error and resend.
func TestReflectNoticeDoesNotAskForACompleteField(t *testing.T) {
	if n := reflectAsk("shikii"); strings.Contains(n, "complete:false") || !strings.Contains(n, "fix it and resend") {
		t.Fatalf("the notice must not ask to send complete:false (submit refuses that field): %s", n)
	}
}
