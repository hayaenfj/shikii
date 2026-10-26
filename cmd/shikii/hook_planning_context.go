package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Past conversations and the wiki are material for judgment. They do not replace the person's current message or the current agreement.
// Only the original text of the latest 4 turns is shown; no re-summarizing of summaries or joining of conversations. Older requests of the
// session move to Earlier, which only restates looks up.
type planningTurn struct {
	ID      string    `json:"id"`
	Request string    `json:"request"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at"`
	// Done is that turn's done-as-asked decision: restates in hook plan submit names the turn, and its outcome is settled from the
	// comparison with Request (the person's own text). Kept in this session's record only (0.9.3)
	Done string `json:"done,omitempty"`
}

func addPlanningContext(scope string, previous *hookSession, f *planningFrame) {
	if previous.Scope == scope && previous.Planning != nil && time.Since(previous.Planning.At) >= 0 && time.Since(previous.Planning.At) < hookSessionTTL {
		f.Recent = append([]planningTurn{}, previous.Planning.Recent...)
		f.Earlier = append([]planningTurn{}, previous.Planning.Earlier...) // the same session and project only, like Recent
		// only refer to original text that also exists in the pre-update format. Nothing is built from the old joined Request.
		if len(f.Recent) == 0 && previous.Planning.Sources["previous_user"] != "" {
			f.Recent = append(f.Recent, planningTurn{ID: previous.TurnID + "-prior", Request: cut(previous.Planning.Sources["previous_user"], 1200), At: previous.Planning.At})
		}
		if previous.Planning.Sources["current_user"] != "" {
			turn := planningTurn{ID: previous.TurnID, Request: cut(previous.Planning.Sources["current_user"], 1200), At: previous.Planning.At}
			if previous.Completed != nil && previous.Completed.TurnID == previous.TurnID {
				turn.Summary = cut(previous.Completed.Summary, 1200)
				for _, t := range previous.Completed.Targets {
					if t.Spec == "done-as-asked" {
						turn.Done = t.TraceID
					}
				}
			}
			// a message the agent attached to the previous request (an acknowledgement, a go-ahead) is not a request of its own: it does not
			// take a place among the 4 turns, so an earlier request stays nameable in restates; its judgment of the joined request is that request's
			if g := previous.Planning.Grouping; g != nil && g.Joined && len(f.Recent) > 0 {
				if turn.Done != "" {
					f.Recent[len(f.Recent)-1].Done = turn.Done
				}
			} else {
				f.Recent = append(f.Recent, turn)
			}
		}
	}
	if len(f.Recent) > 4 {
		for _, turn := range f.Recent[:len(f.Recent)-4] {
			turn.Summary = "" // restates reads only the person's text: the agent's reply is not kept for the session's lifetime
			f.Earlier = append(f.Earlier, turn)
		}
		f.Recent = f.Recent[len(f.Recent)-4:]
	}
	kept := f.Earlier[:0]
	for _, turn := range f.Earlier {
		if time.Since(turn.At) >= 0 && time.Since(turn.At) <= hookSessionTTL {
			kept = append(kept, turn)
		}
	}
	f.Earlier = kept
	for _, turn := range f.Recent {
		if time.Since(turn.At) < 0 || time.Since(turn.At) > hookSessionTTL {
			continue
		}
		f.Sources["history:"+turn.ID+":user"] = cut(turn.Request, 1200)
		if turn.Summary != "" {
			f.Sources["history:"+turn.ID+":assistant"] = cut(turn.Summary, 1200)
		}
	}
}

// The agent that read the skill picks the search words. The program does not guess the topic or the agreement.
func planningWikiSources(root, scope, query string, sources map[string]string) ([]notebook.Page, string) {
	if strings.TrimSpace(query) == "" {
		return nil, ""
	}
	view, err := (notebook.Book{Root: root, Scope: scope}).Query(query)
	if err != nil {
		return nil, "Cannot read the wiki references. Do not fill in unknown conditions; put the current request first."
	}
	for _, page := range view.Lessons {
		key := "lesson:" + page.ID + ":" + page.Revision
		ids := []string{}
		for _, source := range page.Sources {
			ids = append(ids, source.ID)
		}
		sources[key] = fmt.Sprintf("Lesson (hypothesis): %s\nApplies when: %s\nSources: %s", cut(page.Lesson, 800), cut(page.AppliesTo, 300), strings.Join(ids, ", "))
	}
	if len(view.Issues) > 0 {
		return view.Lessons, "Wiki pages whose sources or versions could not be checked were left out of the references."
	}
	return view.Lessons, ""
}
