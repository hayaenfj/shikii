package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

type memoryDelivery struct {
	Key string    `json:"key"`
	At  time.Time `json:"at"`
}

type memoryQuote = store.MemoryQuote

// memoryPrefix opens the quotes every request gets. Short on purpose: the room goes to the quotes (0.11)
const memoryPrefix = "shikii: quotes from this project's memory, close to this request. Each is a hypothesis with sources, never permission or a person's truth: check its condition against this request first.\n"

// memoryContext is called inside the session lock. It prefers lessons whose conditions match,
// and does not repeat the same revision within an hour. An updated revision, or another session, gets it right away.
func memoryContext(root, scope, request string, session *hookSession) (string, error) {
	return memoryContextFor(root, scope, request, session, "", "")
}

// memoryContextFor carries where the delivery comes from. An empty parent means a person's request (delivery id = turn); otherwise a delivery from parent turn parent to subagent agent
// (the id is derived from the parent, the agent and the quoted revisions: the same quotes give the same id, a new revision gives a separate delivery).
// Automatic delivery covers pages other than history (0.9.1). History is "why it came to be", read when the agent searches for it (wiki query).
// Measured (haya1, 70 evaluations): history pages were delivered 7 times and used 0 of those 7. Pages without a kind (from before the version bump) are still delivered as before
func memoryContextFor(root, scope, request string, session *hookSession, parent, agent string) (string, error) {
	if scope == "" {
		return "", nil
	}
	pages, err := (notebook.Book{Root: root, Scope: scope}).Recall(request) // Recall drops history before ranking
	if err != nil {
		return "", err
	}
	return deliverMemory(root, scope, request, pages, true, session, parent, agent)
}

// alwaysContextFor delivers only the rules that apply to every request (rules whose scene is "いつも", always) (0.9.1). For subagents, whose task text is unknown:
// choosing by the parent's request pulled in lessons unrelated to the child's task, drawn by words in the parent's conversation (measured: 4 of 37 deliveries to children helped)
func alwaysContextFor(root, scope, request string, session *hookSession, parent, agent string) (string, error) {
	if scope == "" {
		return "", nil
	}
	pages, err := (notebook.Book{Root: root, Scope: scope}).AlwaysRules()
	if err != nil {
		return "", err
	}
	return deliverMemory(root, scope, request, pages, false, session, parent, agent)
}

// deliverMemory turns the chosen pages into short quotes, at most 2, leaving out revisions already delivered, records the delivery and returns the text.
// feedback: when there is no page to deliver, whether to return the raw text of one conversation with a similar request (only when choosing by closeness to the request)
func deliverMemory(root, scope, request string, pages []notebook.Page, feedback bool, session *hookSession, parent, agent string) (string, error) {
	seen := map[string]bool{}
	for _, previous := range session.Memories {
		if age := time.Since(previous.At); age >= 0 && age < time.Hour {
			seen[previous.Key] = true
		}
	}
	// the person's own past answers on similar requests (0.11): only for a person's request, chosen by closeness to it
	answers := []memoryQuote{}
	if feedback && parent == "" {
		var err error
		if answers, err = personAnswerQuotes(root, request, seen); err != nil {
			return "", err
		}
	}
	// seats: 2 in all. A fitting answer keeps one of them; lessons take the rest, and a seat they leave free goes to a second answer
	lessonSeats := 2
	if len(answers) > 0 {
		lessonSeats = 1
	}
	// the door these quotes come through (0.12): "request" when chosen by the request's words; the always rules have none
	door := ""
	effects := map[string]store.PairEffect{}
	if feedback && len(pages) > 0 {
		door = "request"
		var err error
		if effects, err = memoryEffects(root, scope); err != nil {
			return "", err
		}
	}
	quotes, keys := []memoryQuote{}, []string{}
	for _, p := range pages {
		if len(quotes) == lessonSeats { // after leaving out revisions already delivered (cutting first lets delivered revisions take the seats)
			break
		}
		key := "lesson:" + p.ID + ":" + p.Revision
		if seen[key] || silenced(effects[store.PairKey(key, door)]) { // unused twice and never helpful at the request door: not quoted again (0.12)
			continue
		}
		q := lessonQuote(p)
		// ask the decision model whether the lesson applies to this request (lesson-applies, 0.12): a trusted no skips it. Not for a
		// subagent's delivery: a child started again gets the same record (same id, same quotes), so no new decision is made for it
		if door != "" && parent == "" {
			trace, skip := lessonApplies(root, scope, request, lessonAppliesRequestTarget, p)
			if skip {
				continue
			}
			q.Trace = trace
		}
		quotes = append(quotes, q)
		keys = append(keys, key)
	}
	for _, a := range answers {
		if len(quotes) == 2 {
			break
		}
		quotes, keys = append(quotes, a), append(keys, a.Key)
	}
	// Only for topics with no lesson yet, return the raw text of one conversation with a similar request. Do not overlap with sorted lessons.
	if len(pages) == 0 && feedback && len(quotes) < 2 {
		pending, err := (notebook.Book{Root: root, Scope: scope}).PendingSources()
		if err != nil {
			return "", err
		}
		unprocessed := map[string]bool{}
		for _, f := range pending {
			unprocessed[f.ID] = true
		}
		items, err := (store.Store{Dir: root}).RecallFeedback(scope, request)
		if err != nil {
			return "", err
		}
		for _, f := range items {
			key := "feedback:" + f.ID
			if seen[key] || !unprocessed[f.ID] || store.TextMatch(request, f.Request) < 0.5 {
				continue
			}
			quotes = append(quotes, memoryQuote{Key: key, ID: f.ID, Request: f.Request, Summary: f.Summary, Reply: f.Reply})
			keys = append(keys, key)
			break
		}
	}
	if len(quotes) == 0 {
		return "", nil
	}
	prefix := memoryPrefix
	for _, key := range keys {
		if strings.HasPrefix(key, "decision:") {
			prefix += "A decision: quote is the person's own answer (\"you\" is the person) about a similar earlier request: weigh it, it is not this request's answer.\n"
			break
		}
	}
	for budget := 400; budget >= 25; budget /= 2 {
		clipped := clipQuotes(quotes, budget)
		data, err := json.Marshal(clipped)
		if err != nil {
			return "", err
		}
		if len(prefix)+len(data) > 1800 {
			continue
		}
		// save, per revision, the short quotes actually returned, not the search candidates.
		id := session.TurnID
		if parent != "" {
			sum := sha256.Sum256([]byte(parent + "/" + agent + "/" + strings.Join(keys, ",")))
			id = hex.EncodeToString(sum[:])[:16]
		}
		if id != "" {
			if err := (store.Store{Dir: root}).SaveMemoryDelivery(store.MemoryDelivery{ID: id, Scope: scope, Request: request, Quotes: clipped, Parent: parent, Agent: agent, Phrase: door}); err != nil {
				return "", err
			}
		}
		for _, key := range keys {
			session.Memories = append(session.Memories, memoryDelivery{Key: key, At: time.Now().UTC()})
		}
		if len(session.Memories) > 16 {
			session.Memories = session.Memories[len(session.Memories)-16:]
		}
		return prefix + strings.TrimSpace(string(data)), nil
	}
	return "", nil
}

// lessonQuote is the short quote of a lesson page as it is delivered and recorded (key lesson:<id>:<revision>).
// A delivery record carries at most 16 source ids per quote; a page that kept its earlier sources over many updates has more.
// The correction's source comes first so the record still names it.
func lessonQuote(p notebook.Page) memoryQuote {
	ids := []string{}
	if p.Correction != nil {
		ids = append(ids, p.Correction.SourceID)
	}
	for _, source := range p.Sources {
		if len(ids) < 16 && (p.Correction == nil || source.ID != p.Correction.SourceID) {
			ids = append(ids, source.ID)
		}
	}
	return memoryQuote{Key: "lesson:" + p.ID + ":" + p.Revision, ID: p.ID, Revision: p.Revision, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Sources: ids, Correction: p.Correction}
}

// clipQuotes returns a copy of quotes with each text cut to budget bytes (an ellipsis marks a cut).
func clipQuotes(quotes []memoryQuote, budget int) []memoryQuote {
	clipped := append([]memoryQuote{}, quotes...)
	text := func(s string) string {
		if len(s) > budget {
			return cut(s, budget) + "…"
		}
		return s
	}
	for i := range clipped {
		q := &clipped[i]
		q.Lesson, q.AppliesTo = text(q.Lesson), text(q.AppliesTo)
		q.Request, q.Summary, q.Reply = text(q.Request), text(q.Summary), text(q.Reply)
		if q.Correction != nil {
			c := *q.Correction
			c.Quote, c.Action = text(c.Quote), text(c.Action)
			q.Correction = &c
		}
	}
	return clipped
}

// answerSpecs are the hook questions whose answers the person gives about their own requests (prompt-kind is the AI's sorting, not asked of the person).
var answerSpecs = []string{"needs-plan", "changes-in-scope", "done-as-asked"}

// personAnswerQuotes returns, at most 2 and closest first, the person's own answers (page, spot check, hand label) to the hook
// questions, for past requests similar to request, leaving out those already delivered (seen). Similar means the same bar as a past
// conversation (TextMatch >= 0.5: at least half of this request's words, and 2 or more, appear in the past request). Each quote carries
// the past request and what the person said, in the page's words: English first, the configured language alongside (0.11), together with
// the two answers the person judged between: the agent's own answer with its reason (its organizing before starting, or its provisional
// score; only when the decision was paired under 0.11's rule) and the decision model's answer. A bare "yes" or "no" told the agent
// nothing it could act on; with the reasons it learns how its own reasoning is judged in this project.
func personAnswerQuotes(root, request string, seen map[string]bool) ([]memoryQuote, error) {
	st := store.Store{Dir: root}
	answers, err := st.PersonAnswers(answerSpecs)
	if err != nil {
		return nil, err
	}
	type match struct {
		answer store.PersonAnswer
		score  float64
	}
	matches := []match{}
	for _, a := range answers {
		if seen["decision:"+a.TraceID] {
			continue
		}
		if score := store.TextMatch(request, a.Request); score >= 0.5 {
			matches = append(matches, match{a, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].answer.At.After(matches[j].answer.At) // on a tie, the newer answer
	})
	lang := hookLang()
	specs := map[string]*spec.Spec{}
	reviews := map[string]map[string]store.Review{}
	out := []memoryQuote{}
	for _, m := range matches {
		sp, ok := specs[m.answer.SpecID]
		if !ok {
			sp = loadOrNil(root, m.answer.SpecID)
			specs[m.answer.SpecID] = sp
		}
		var pair *aiPair
		var model modelAnswer
		if rec, err := st.FindRecord(m.answer.SpecID, m.answer.TraceID); err == nil && rec != nil {
			model = modelAnswerOf(*rec)
			if store.ReviewSpec(m.answer.SpecID) {
				if _, ok := reviews[m.answer.SpecID]; !ok {
					r, err := st.Reviews(m.answer.SpecID)
					if err != nil {
						return nil, err
					}
					reviews[m.answer.SpecID] = r
				}
			}
			if p, ok := pairOf(m.answer.SpecID, *rec, reviews[m.answer.SpecID]); ok {
				pair = &p
			}
		}
		said := personAnswerSaid(sp, m.answer.Truth, lang, pair, model)
		if said == "" {
			continue
		}
		out = append(out, memoryQuote{Key: "decision:" + m.answer.TraceID, ID: m.answer.TraceID, Request: m.answer.Request, Said: said})
		if len(out) == 2 {
			break
		}
	}
	return out, nil
}

// personAnswerSaid is what the person answered, in the page's words: the question's title and the chosen answer, never the spec id, then
// the agent's answer with its reason (when it gave one) and the decision model's answer. Empty when the question's answers cannot be
// named (a spec that is gone).
func personAnswerSaid(sp *spec.Spec, truth, lang string, pair *aiPair, model modelAnswer) string {
	if sp == nil {
		return ""
	}
	name := func(l, truth string) string {
		if o := strings.TrimSpace(sp.Display[l].Options[truth]); o != "" {
			return o
		}
		return truth
	}
	text := "In a similar request (above), you answered \"" + name("en", truth) + "\" to \"" + questionTitle(sp, "en") + "\""
	ja := ""
	if strings.TrimSpace(sp.Display["ja"].Options[truth]) != "" {
		ja = "\u4f3c\u305f\u4f9d\u983c(\u4e0a)\u3067\u3001\u3042\u306a\u305f\u306f\u300c" + questionTitle(sp, "ja") + "\u300d\u306b\u300e" + name("ja", truth) + "\u300f\u3068\u7b54\u3048\u305f"
	}
	if pair != nil && pair.Agent != "" {
		reason := cut(strings.TrimSpace(pair.AgentReason), 160)
		text += "; the agent had answered \"" + name("en", pair.Agent) + "\""
		if reason != "" {
			text += " because: " + reason
		}
		if ja != "" {
			ja += "\u3002\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306f\u300e" + name("ja", pair.Agent) + "\u300f"
			if reason != "" {
				ja += "(\u7406\u7531: " + reason + ")"
			}
		}
	}
	if model.Answer != "" {
		text += "; the decision model said \"" + name("en", model.Answer) + "\""
		if ja != "" {
			ja += "\u3001\u5224\u5b9a\u30e2\u30c7\u30eb\u306f\u300e" + name("ja", model.Answer) + "\u300f"
		}
	}
	if ja != "" {
		text = bi(text, ja, hookLoc{Lang: lang})
	}
	return cut(text, 600)
}

// decideKnowledge is what the project knows about a request an agent consults the decision model about (0.11): the person's answers on
// similar requests (with the agent's and the model's answers, as the request hook quotes them) and the closest lessons, at most 2 each.
// Nothing is recorded as a delivery: the agent asked, and the reflection assesses what the hook delivered.
func decideKnowledge(root, scope, request string) (similar, lessons []memoryQuote, err error) {
	if similar, err = personAnswerQuotes(root, request, nil); err != nil {
		return nil, nil, err
	}
	pages, err := (notebook.Book{Root: root, Scope: scope}).Recall(request)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range pages {
		if len(lessons) == 2 {
			break
		}
		lessons = append(lessons, lessonQuote(p))
	}
	return clipQuotes(similar, 400), clipQuotes(lessons, 400), nil
}
