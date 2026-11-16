package main

// hook subagent-stop: a lesson delivered to a subagent is assessed by the subagent itself. Only the one a lesson was delivered to can
// tell whether it was used: the parent sees the child's report, not how the child worked, and when the parent's reflection was asked
// it answered "unknown" for more than half of these deliveries. The child ends its final report with one line per lesson
// (subagentAssessNotice, delivered at SubagentStart); this hook reads those lines from the child's last message and saves them. It
// outputs nothing and never keeps the child working, so the report the parent receives is the one the child wrote.

import (
	"regexp"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

// subagentAssessNotice tells a subagent how to report what it did with the lessons shikii delivers to it.
const subagentAssessNotice = "shikii: Only you can tell whether a lesson shikii delivers to you (here, or right before a command or a file) was used. End your final report with one line for each such lesson: `shikii: <its key> helpful - <what you did because of it>`, or `shikii: <its key> unused`. Leave out a lesson you cannot judge."

// subagentAssessLine is one such line: the exact key of a lesson revision, one of the three results a subagent can observe and, after
// " - ", what it did. Anything else in the report is not read.
var subagentAssessLine = regexp.MustCompile("(?m)^[ \\t]*(?:[-*][ \\t]+)?`?shikii: (lesson:[0-9a-f]{16}:[0-9a-f]{16}) (helpful|unused|harmful)(?:[ \\t]+-[ \\t]+([^`\\n]*))?`?[ \\t]*$")

// subagentAssessMax is how many lines of one report are read (a child receives two lessons at its start and one per tool call).
const subagentAssessMax = 16

// hookSubagentStop saves the assessments in the child's last message for the lessons delivered to that child in this turn. A key
// that was not delivered to this child is not saved (the store refuses it), and a lesson already assessed stays as it is.
func hookSubagentStop(agent string, in *hookInput, snapshot *hookSession) {
	if in.AgentID == "" || snapshot.Scope == "" || snapshot.TurnID == "" {
		return
	}
	lines := subagentAssessLine.FindAllStringSubmatch(in.LastAssistantMessage, subagentAssessMax)
	if len(lines) == 0 {
		return
	}
	st := store.Store{Dir: agent}
	deliveries, err := st.MemoryDeliveries(snapshot.Scope)
	if err != nil {
		hookLog("subagent-stop: " + err.Error())
		return
	}
	for _, line := range lines {
		key, result, did := line[1], line[2], strings.TrimSpace(line[3])
		if did == "" {
			if result != "unused" {
				continue // helpful and harmful say what the child did; without it there is nothing to keep
			}
			did = "The subagent reported that no decision of its task used this lesson."
		}
		for i := len(deliveries) - 1; i >= 0; i-- { // the newest delivery of that key to this child
			d := deliveries[i]
			if d.Agent != in.AgentID || d.Parent != snapshot.TurnID || !deliveryHasKey(d, key) {
				continue
			}
			a := store.MemoryAssessment{SourceID: d.ID, Key: key, Result: result, Decision: cut(did, 2000), Evidence: "The subagent's own final report.", Agent: in.AgentID}
			if err := st.SaveMemoryAssessment(snapshot.Scope, a); err != nil {
				hookLog("subagent-stop: " + err.Error())
			}
			break
		}
	}
}

func deliveryHasKey(d store.MemoryDelivery, key string) bool {
	for _, q := range d.Quotes {
		if q.Key == key {
			return true
		}
	}
	return false
}
