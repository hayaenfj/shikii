package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// forget over MCP: the same plan as the list of shikii forget, shown to the person by the client (elicitation/create), and applied
// only when the person's answer says yes. What the agent passes names the sessions and, for its own protection, its own session; it
// never carries the answer.

// forgetNeedsPerson is what an agent is told when the person cannot be asked here.
const forgetNeedsPerson = `this client does not offer to ask the person, so nothing was changed, and an agent does not remove records on its own: a person runs shikii forget <session id>... in the project's folder (without --apply it only lists what would go; with --apply it moves the records into .shikii/records/forgotten/ first). Show the person the session ids to give it`

// forgetAskLines is how many lines of each kind the question to the person lists.
const forgetAskLines = 20

// forgetArguments are the arguments of shikii_wiki forget. Anything else is refused, so that a confirmation passed as an argument is
// never read as one.
type forgetArguments struct {
	Operation string          `json:"operation"`
	Sessions  []string        `json:"sessions"`
	Work      json.RawMessage `json:"work"`
}

// forget runs the operation for the call with the id call on the project server target (the routes of its store). It reports false when the client cancelled
// the call, which then takes no answer.
func (c *mcpConn) forget(call json.RawMessage, target http.Handler, args json.RawMessage) (map[string]any, bool) {
	srv, ok := target.(*storeMux)
	if !ok || srv.root == "" || srv.scope == "" {
		return toolError(errors.New("forget needs a project's store: pass project, the absolute path of a registered project")), true
	}
	var fa forgetArguments
	if err, extra := decodeStrictObject(args, &fa); err != nil || extra {
		if err == nil {
			err = errors.New("one JSON object is expected")
		}
		return toolError(fmt.Errorf("forget takes sessions (the ids to remove) and work.session_id (your own session, which stays); the person's answer is asked by shikii and is never an argument: %w", err)), true
	}
	var own struct {
		SessionID string `json:"session_id"`
	}
	if len(fa.Work) > 0 && json.Unmarshal(fa.Work, &own) != nil {
		return toolError(errors.New("work must be an object: forget reads only work.session_id")), true
	}
	if len(fa.Sessions) == 0 {
		return toolError(errors.New("forget needs sessions: the ids of the finished sessions to remove")), true
	}
	ids, err := forgetUniqueIDs(fa.Sessions)
	if err != nil {
		return toolError(err), true
	}
	root, scope := srv.root, srv.scope
	r := &forgetRun{root: root, scope: scope, st: store.Store{Dir: root}, book: notebook.Book{Root: root, Scope: scope}, ids: ids,
		asked: true, callerIDs: []string{own.SessionID}}
	if err := r.execute(false, ""); err != nil {
		return forgetReply("failed", err.Error(), nil, true), true
	}
	plan := r.plan
	switch {
	case len(plan.Refusals) > 0:
		return forgetReply("refused", "nothing was changed: "+strings.Join(plan.Refusals, "; "), &plan, true), true
	case len(plan.Remove) == 0:
		return forgetReply("nothing_to_remove", "nothing was changed: no record of these sessions is left to remove", &plan, false), true
	case !c.canAsk():
		return forgetReply("needs_person", forgetNeedsPerson, &plan, true), true
	}
	shown := r.digest()
	answer := c.ask(call, forgetQuestion(plan), forgetFormSchema())
	switch answer.kind {
	case answerCallCancelled:
		return nil, false
	case answerNo:
		return forgetReply("declined", "the person said no: nothing was changed. Do not ask again unless the person asks", &plan, false), true
	case answerDismissed:
		return forgetReply("dismissed", "the person dismissed the question: nothing was changed. Do not ask again unless the person asks", &plan, false), true
	case answerMalformed:
		return forgetReply("not_confirmed", "the answer was not a yes to the removal ("+answer.detail+"): nothing was changed", &plan, true), true
	case answerFailed:
		return forgetReply("needs_person", answer.detail+": the client could not ask the person, so nothing was changed. "+forgetNeedsPerson, &plan, true), true
	case answerNone:
		return forgetReply("unanswered", answer.detail+": nothing was changed. Ask again only if the person wants it", &plan, true), true
	}
	// a yes: the records are read again under the locks, and what is removed is what the person was shown or nothing
	if err := r.execute(true, shown); err != nil {
		status := "failed"
		if errors.Is(err, errForgetPlanChanged) {
			status = "changed"
		}
		return forgetReply(status, err.Error(), nil, true), true
	}
	done := r.plan
	reply := forgetReply("removed", "the person agreed, and what was removed is kept in "+r.f.Dir+" (plan.json there says what and why)", &done, false)
	return reply, true
}

// forgetReply is the tool result of forget: what became of the request, whether anything was changed, and the plan in a short form.
func forgetReply(status, message string, plan *forgetPlan, failed bool) map[string]any {
	body := map[string]any{"operation": "forget", "status": status, "changed": status == "removed", "message": message}
	if plan != nil {
		body["plan"] = plan.view()
	}
	b, _ := json.Marshal(body)
	code := http.StatusOK
	if failed {
		code = http.StatusBadRequest
	}
	return toolResult(code, b)
}

// view is the plan for an agent: what goes, what stays and what only might belong, with the names of each cut at forgetListed.
func (p forgetPlan) view() map[string]any {
	remove := make([]map[string]any, 0, len(p.Remove))
	for _, item := range p.Remove {
		m := map[string]any{"place": item.Place, "what": item.What, "count": item.Count, "why": item.Why}
		names := item.Names
		if len(names) > forgetListed {
			m["more_names"] = len(names) - forgetListed
			names = names[:forgetListed]
		}
		if len(names) > 0 {
			m["names"] = names
		}
		remove = append(remove, m)
	}
	out := map[string]any{"sessions": p.Sessions, "remove": remove}
	if len(p.Children) > 0 {
		out["subagents"] = p.Children
	}
	if len(p.Keep) > 0 {
		out["kept"] = p.Keep
	}
	if len(p.Candidates) > 0 {
		candidates := p.Candidates
		if len(candidates) > forgetListed {
			out["more_candidates"] = len(candidates) - forgetListed
			candidates = candidates[:forgetListed]
		}
		out["candidates"] = candidates
	}
	if len(p.Refusals) > 0 {
		out["refusals"] = p.Refusals
	}
	return out
}

// forgetQuestion is the text the person reads in the client: what goes, what stays, what only might belong, and where it is kept.
func forgetQuestion(p forgetPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "An agent asks to remove the records that finished sessions left in this project's .shikii/. Sessions: %s.\n", strings.Join(p.Sessions, ", "))
	if len(p.Children) > 0 {
		fmt.Fprintf(&b, "Subagents that ran under them: %s.\n", strings.Join(p.Children, ", "))
	}
	b.WriteString("Nothing is deleted at once: what goes is moved into .shikii/records/forgotten/ first, where it can be read back.\n\nWould remove:\n")
	for i, item := range p.Remove {
		if i == forgetAskLines {
			fmt.Fprintf(&b, "- ... and %d more places\n", len(p.Remove)-forgetAskLines)
			break
		}
		fmt.Fprintf(&b, "- %s: %d %s\n", item.Place, item.Count, item.What)
	}
	if len(p.Keep) > 0 {
		fmt.Fprintf(&b, "\nKept (%d):\n", len(p.Keep))
		for i, k := range p.Keep {
			if i == forgetAskLines {
				fmt.Fprintf(&b, "- ... and %d more\n", len(p.Keep)-forgetAskLines)
				break
			}
			fmt.Fprintf(&b, "- %s: %s\n", k.What, k.Why)
		}
	}
	if len(p.Candidates) > 0 {
		fmt.Fprintf(&b, "\nNot removed: %d records that only a word or a time links to these sessions.\n", len(p.Candidates))
	}
	b.WriteString("\nShould shikii remove these records now?")
	return b.String()
}

// forgetFormSchema is the form of the question: one yes or no that starts as no.
func forgetFormSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"remove": map[string]any{"type": "boolean", "title": "Remove these records", "description": "They are moved into .shikii/records/forgotten/, not deleted.", "default": false},
		},
		"required": []string{"remove"},
	}
}
