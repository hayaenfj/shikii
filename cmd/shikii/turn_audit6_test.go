package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The sixth audit of 0.41.0 (low 1). OpenCode's plugin tells shikii the start of every turn of a subagent, so that shikii keeps the subagent
// open while it thinks again. Only the first start has a child to give the rules to; a later start (continued) delivers nothing and records
// no delivery, which no report of the child would ever assess, and it still keeps the subagent open.
func TestALaterTurnStartOfASubagentDeliversNothingAndRecordsNoDelivery(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Kind, always.Scene, always.AppliesTo = "", "A rule for every request", "Report in the language of the person.", "rule", "always", "every request"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always); err != nil {
		t.Fatal(err)
	}
	deliveriesTo := func(child string) int {
		all, err := st.MemoryDeliveries(scope)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, d := range all {
			if d.Agent == child {
				n++
			}
		}
		return n
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "fix the colors of the page"})
	if out := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "child", "agent_type": "general", "prompt": "read the notes"})); out == "" || deliveriesTo("child") != 1 {
		t.Fatalf("precondition: the first start gives the child its rules and records the delivery: %q %d", out, deliveriesTo("child"))
	}
	runHook(t, "subagent-stop", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "child", "agent_type": "general"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "now fix the fonts too"}) // the parent's next request: another turn
	if out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "child", "agent_type": "general", "continued": true}); out != "" {
		t.Errorf("a later turn's start is told only so that the child is kept open, and gives nothing: %s", out)
	}
	if n := deliveriesTo("child"); n != 1 {
		t.Errorf("a later turn's start records no delivery the child never saw: %d deliveries", n)
	}
	if l := latestOfSession(t, "sub"); l.Subagents["child"].IsZero() {
		t.Errorf("the later start still keeps the subagent open: %+v", l.Subagents)
	}
}

// The sixth audit of 0.41.0 (low 2). A subagent left open by an earlier process of the agent (its end was never told; the session was
// resumed) that works again is counted by forget: any event of it that comes after the new process was noted puts its time to that event,
// whether that event is the one that noted the process or a later one.
func TestASubagentOfAnEarlierProcessThatWorksAgainIsCounted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first string // the event that notes the new process
	}{{"the event that notes the process is the subagent's", ""}, {"a later event of the subagent", "user-prompt-submit"}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := gitRepo(t)
			hookEnv(t, nil, repo)
			root, _, err := projectStoreAt(repo)
			if err != nil {
				t.Fatal(err)
			}
			useProcessTable(t, agentFamily("claude"))
			useParent(t, 500)
			noteSessionRequest("ses_resumed", root, "turn-1")
			anHourAgo := time.Now().Add(-time.Hour).UTC().Round(0)
			updateSessionLatest("ses_resumed", func(l *sessionLatest, known bool) bool {
				l.Subagents = map[string]time.Time{"agent-x": anHourAgo} // opened by the process before, never ended
				return known
			})
			if tc.first != "" {
				runHook(t, tc.first, map[string]any{"session_id": "ses_resumed", "cwd": repo, "prompt": "go on with the work"})
			}
			runHook(t, "subagent-start", map[string]any{"session_id": "ses_resumed", "cwd": repo, "agent_id": "agent-x", "agent_type": "Explore", "continued": true})
			p := hookProcessOf(t, root, "ses_resumed")
			if p == nil || p.Noted.IsZero() {
				t.Fatalf("the process was not noted: %+v", p)
			}
			l := latestOfSession(t, "ses_resumed")
			if id, _ := openSubagent(l, p.Noted); id != "agent-x" {
				t.Errorf("the subagent works again after the process was noted (%v) and is not counted: its time %v, open %q", p.Noted, l.Subagents, id)
			}
		})
	}
}
