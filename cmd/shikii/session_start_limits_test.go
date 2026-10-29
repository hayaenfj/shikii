package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.12: the rules for every request arrive whole at SessionStart. 0.11 cut each rule at 300 bytes (a number without grounds), and the
// rule listing the words never to use lost its last words, among them the one the person had forbidden; the agent then used it. Now no
// rule is cut, up to sessionBriefRules of them, and what is cut to keep the block within sessionBriefMax is the work record's section,
// never a rule.
func TestSessionStartDeliversEveryAlwaysRuleWholeAndCutsOnlyTheRecord(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	long := "Words never to use: " + strings.Repeat("alpha beta gamma delta ", 25) + "and the last word, omega, matters most."
	if len(long) <= 500 {
		t.Fatalf("precondition: a rule longer than the old cut: %d", len(long))
	}
	ids := []string{}
	for i, text := range []string{long, "Visible text is Japanese.", "Tests show RED before GREEN.", "Commit per feature.", "Never push without the person's word."} {
		d := draft
		d.ID = "" // a new page each (the fixture's draft carries its page id, which would update that one page)
		d.Title, d.Lesson, d.Scene, d.AppliesTo = "Always rule "+string(rune('A'+i)), text, "\u3044\u3064\u3082", "every request"
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, "lesson:"+r.Page.ID+":"+r.Page.Revision)
	}
	record := "# Where the work stands\n\n## Current position\n" + strings.Repeat("The record has many lines about the work. ", 80)
	if _, err := (handoff.Book{Store: st.Dir}).Write("plan/scope.md", record, false); err != nil {
		t.Fatal(err)
	}
	brief, err := sessionBrief(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief, "and the last word, omega, matters most.") {
		t.Fatalf("a long rule arrives whole, to its last word:\n%s", brief)
	}
	for _, id := range ids {
		if !strings.Contains(brief, id) {
			t.Fatalf("every always rule is listed (%s missing):\n%s", id, brief)
		}
	}
	if !strings.Contains(brief, "plan/scope.md") || !strings.Contains(brief, "Current position") {
		t.Fatalf("the work record's last section still comes, cut to the room left:\n%s", brief)
	}
	if len(brief) > sessionBriefMax {
		t.Fatalf("the block stays within %d bytes by cutting the record, not the rules: %d", sessionBriefMax, len(brief))
	}
}
