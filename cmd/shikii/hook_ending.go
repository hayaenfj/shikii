package main

import (
	"encoding/json"
	"strings"
)

// What the ending work record carries, as far as the hook knows it when it writes its notice.
//
// The notice used to say only "end with work record, with the receipt and result". Two things the hook already knew by then were
// asked for afterwards, each in a second record of the same request (counted in the records of this project: an edit-free request
// sent its record twice, most often as a result followed by continues_previous alone, or by memory alone):
//   - the Stop asks for continues_previous when the message is short and not an acknowledgement and nothing grouped it, and
//   - the reply of the record lists, in lessons_to_assess, the quotes delivered before the request, which only the agent that did
//     the work can assess.
//
// The notice now names both, in the form the record takes, so that the first record can carry them. Nothing else changes: the Stop
// still asks (groupingBlockReason), and the reply still lists what is left (lessonsLeft), for the agent that did not read the notice
// and for the lessons that arrive right before a command or a file, which are not known yet when the notice is written.
type endingHints struct {
	quotes   []string // keys of the quotes delivered to the agent before the request, as the work record takes them in memory
	grouping bool     // the Stop would ask for continues_previous
}

// text is the sentences the hints add to a notice, each led by a space (empty when there is nothing to add).
func (h endingHints) text() string {
	text := ""
	if len(h.quotes) > 0 {
		entries := make([]string, len(h.quotes))
		for i, key := range h.quotes {
			ref, _ := json.Marshal(key)
			entries[i] = `{"ref":` + string(ref) + `,"result":"..."}`
		}
		text += " With that record send memory:[" + strings.Join(entries, ",") + `] for the quotes delivered to you before this request, replacing each "..." with helpful, unused, harmful or unknown (helpful and harmful also take decision and evidence, as text).`
	}
	if h.grouping {
		text += " " + groupingShortWhy + ", so it is not known whether it continues the previous request: add " + groupingFields + " to that record " + groupingTrueMeans + "."
	}
	return text
}

// turnNotice is the notice the hook writes for a request: the planning notice, or the reasoning notice for a request that needs no
// planning judgment, with what the hints add. Without hints it is exactly planningNotice or reasoningNotice, which is what
// hookUserPrompt writes before the quotes of the request are delivered.
func turnNotice(sessionID, turnID string, reasoningOnly bool, h endingHints) string {
	if reasoningOnly {
		return reasoningNoticeWith(sessionID, turnID, h)
	}
	return planningNoticeWith(sessionID, turnID, h)
}

// addEndingHints replaces the notice hookUserPrompt wrote with the same notice carrying the hints, once the request's quotes are
// delivered (quoted, the keys the delivery saved) and the session record holds the request's grouping (s, read after the mutations
// of hookUserPrompt). The Stop's own test of whether it asks for continues_previous, groupingAsk, decides the grouping hint: the
// notice asks for it exactly when the Stop would. The hints are kept in in.ending for the preparation that replaces the notice again.
func addEndingHints(s *hookSession, quoted []string, in *hookInput, out *hookOut) {
	if s == nil || s.Planning == nil || s.TurnID != in.workTurn || s.Planning.TurnID != in.workTurn {
		return
	}
	h := endingHints{quotes: quoted, grouping: groupingAsk(s)}
	was := turnNotice(in.SessionID, in.workTurn, s.Planning.ReasoningOnly, endingHints{})
	now := turnNotice(in.SessionID, in.workTurn, s.Planning.ReasoningOnly, h)
	if now == was || !strings.Contains(out.Context, was) {
		return
	}
	out.Context = strings.Replace(out.Context, was, now, 1)
	in.ending = h
}
