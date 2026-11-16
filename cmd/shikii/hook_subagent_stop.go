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
// " - ", what it did. Anything else in the report is not read. A line may end with CRLF.
var subagentAssessLine = regexp.MustCompile("(?m)^[ \\t]*(?:[-*][ \\t]+)?`?shikii: (lesson:[0-9a-f]{16}:[0-9a-f]{16}) (helpful|unused|harmful)(?:[ \\t]+-[ \\t]+([^`\\r\\n]*))?`?[ \\t\\r]*$")

// subagentAssessMax is how many lessons one report assesses (a child receives two lessons at its start and one per tool call), and
// subagentReportMax how much of the end of a report is read for their lines.
const (
	subagentAssessMax = 16
	subagentReportMax = 1 << 18
)

// hookSubagentStop saves the assessments in the child's last message for the lessons delivered to that child. A line assesses
// every delivery of its lesson to the child (the same lesson can reach it at its start and again right before a command or a
// file). A delivery is tied to the child, not to the turn that is open when the child ends: a child that runs in the background
// can end after the person's next request. A key that was not delivered to this child is not saved (the store refuses it too) and
// does not count toward subagentAssessMax, and a lesson already assessed stays as it is. What the child says is also the answer
// to the delivery's own question (lesson-applies), as an assessment from the reflection is.
func hookSubagentStop(agent string, in *hookInput, snapshot *hookSession) {
	if in.AgentID == "" || snapshot.Scope == "" || snapshot.TurnID == "" {
		return
	}
	report := in.LastAssistantMessage
	if len(report) > subagentReportMax {
		report = report[len(report)-subagentReportMax:]
		report = report[strings.IndexByte(report, '\n')+1:] // the line the limit falls in is not read
	}
	lines := subagentAssessLine.FindAllStringSubmatch(report, -1)
	if len(lines) == 0 {
		return
	}
	st := store.Store{Dir: agent}
	deliveries, err := st.MemoryDeliveries(snapshot.Scope)
	if err != nil {
		hookLog("subagent-stop: " + err.Error())
		return
	}
	seen := map[string]bool{}
	for _, line := range lines {
		key, result, did := line[1], line[2], strings.TrimSpace(line[3])
		if len(seen) >= subagentAssessMax {
			break
		}
		if seen[key] {
			continue // the first line about a lesson is the one kept
		}
		if did == "" {
			if result != "unused" {
				continue // helpful and harmful say what the child did; without it there is nothing to keep
			}
			did = "The subagent reported that no decision of its task used this lesson."
		}
		for _, d := range deliveries {
			if d.Agent != in.AgentID || !deliveryHasKey(d, key) {
				continue
			}
			seen[key] = true
			a := store.MemoryAssessment{SourceID: d.ID, Key: key, Result: result, Decision: cut(did, 2000), Evidence: "The subagent's own final report.", Agent: in.AgentID}
			if err := st.SaveMemoryAssessment(snapshot.Scope, a); err != nil {
				hookLog("subagent-stop: " + err.Error())
				continue
			}
			if err := labelLessonApplies(st, snapshot.Scope, a); err != nil {
				hookLog("subagent-stop: lesson-applies truth: " + err.Error())
			}
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
