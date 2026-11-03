package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func collectiveReply(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	c, ok := v["collective"].(map[string]any)
	if !ok {
		t.Fatal("preparation must supply bounded collective knowledge", v)
	}
	return c
}

func TestCollectiveInspectionCannotAcknowledgeANewerChange(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"body": "First version."})
	w, _ := workCall(t, b, "prepare", map[string]any{"request": "Check the condition", "query": "lesson:" + p.ID})
	second := loopPage(t, b, source, map[string]any{"id": p.ID, "body": "Second version."})
	v, _ := workCall(t, b, "prepare", map[string]any{"receipt": w["receipt"], "query": "lesson:" + p.ID})
	u := collectiveReply(t, v)["updates"].([]any)[0].(map[string]any)
	third := loopPage(t, b, source, map[string]any{"id": p.ID, "body": "Third version."})
	r, e := workCall(t, b, "record", map[string]any{"receipt": w["receipt"], "reviewed": []string{u["ref"].(string)}})
	if e != nil || r["complete"] != true {
		t.Fatal(r, e)
	}
	v, e = workCall(t, b, "prepare", map[string]any{"receipt": w["receipt"], "query": "lesson:" + p.ID})
	if e != nil {
		t.Fatal(e)
	}
	next := collectiveReply(t, v)["updates"].([]any)[0].(map[string]any)
	if next["before"].(map[string]any)["revision"] != second.Revision || next["after"].(map[string]any)["revision"] != third.Revision || next["ref"] == u["ref"] {
		t.Fatal(next)
	}
}

func TestCollectiveOutOfOrderInspectionDoesNotRegress(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"body": "First version."})
	first, e := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID})
	if e != nil {
		t.Fatal(e)
	}
	changes := []string{}
	for _, body := range []string{"Second version.", "Third version."} {
		loopPage(t, b, source, map[string]any{"id": p.ID, "body": body})
		v, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID})
		if e != nil {
			t.Fatal(e)
		}
		changes = append(changes, collectiveReply(t, v)["updates"].([]any)[0].(map[string]any)["ref"].(string))
	}
	r, e := workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "reviewed": []string{changes[1], changes[0], "knowledge1"}})
	if e != nil || r["complete"] != true {
		t.Fatal(r, e)
	}
	v, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID})
	if e != nil {
		t.Fatal(e)
	}
	if len(collectiveReply(t, v)["updates"].([]any)) != 0 {
		t.Fatal("older inspection must not regress the confirmed baseline")
	}
	token, _ := workDecode(first["receipt"].(string))
	dir, e := openWorkSessions(b.Root)
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	s, e := readPlanningSession(dir, filepath.Base(sessionPath("", token.Session)))
	if e != nil {
		t.Fatal(e)
	}
	live, _ := b.Query("lesson:" + p.ID)
	if len(s.KnowledgeSeen) != 1 || s.KnowledgeSeen[0].Revision != live.Lessons[0].Revision {
		t.Fatal("an old initial article must not replace a later confirmed revision", s.KnowledgeSeen)
	}
}

func TestCollectiveInitialPreparationIgnoresUnrelatedSessionKnowledge(t *testing.T) {
	b, source := loopBook(t)
	a := loopPage(t, b, source, map[string]any{"title": "Quartz invoices"})
	z := loopPage(t, b, source, map[string]any{"title": "Indigo compiler"})
	base := b.CollectiveBaseline(notebook.ArticleReference{ID: a.ID, Revision: a.Revision})
	loopPage(t, b, source, map[string]any{"id": a.ID, "body": "A peer changed invoices."})
	w := &workState{Articles: map[string]store.KnowledgeReference{"knowledge1": {ID: z.ID, Revision: z.Revision}}}
	v, e := preparedCollective(b, w, []notebook.CollectiveBaseline{base}, []any{map[string]any{"id": z.ID, "revision": z.Revision}})
	if e != nil || len(v.Updates) != 0 {
		t.Fatal("unrelated previous work must not be delivered", v, e)
	}
}

func TestCollectiveProbeIdentityAndEvidenceRemainStable(t *testing.T) {
	b, source := loopBook(t)
	q := map[string]any{"question": "Can Quartz handle Linux input?", "method": "Check synthetic input on Linux.", "reconsider_when": "Input policy changes."}
	p := loopPage(t, b, source, map[string]any{"learning": map[string]any{"probe": q}})
	view, e := b.Collective(nil, []notebook.ArticleReference{{ID: p.ID, Revision: p.Revision}})
	if e != nil || len(view.Investigations) != 1 {
		t.Fatal(view, e)
	}
	key := view.Investigations[0].Key
	q["results"] = []any{map[string]any{"conditions": "Synthetic Linux input", "conclusion": "The guarded case completed.", "assessment": "supports", "sources": []string{source}}}
	current := loopPage(t, b, source, map[string]any{"id": p.ID, "learning": map[string]any{"probe": q}})
	view, e = b.Collective(nil, []notebook.ArticleReference{{ID: current.ID, Revision: current.Revision}})
	if e != nil || view.Investigations[0].Key != key || view.Investigations[0].Results[0].Sources[0] != source {
		t.Fatal(view, e)
	}
	history, e := b.Collective(nil, []notebook.ArticleReference{{ID: p.ID, Revision: p.Revision}})
	if e != nil || len(history.Investigations) != 0 {
		t.Fatal("a historical query must not become a current testing opportunity", history, e)
	}
}

func TestCollectiveProbeIsNotRepeatedWithinTheSameWork(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"learning": map[string]any{"probe": map[string]any{"question": "Does Quartz work?", "method": "Check one synthetic case.", "reconsider_when": "The conditions change."}}})
	first, e := workCall(t, b, "prepare", map[string]any{"request": "Check Quartz", "query": "lesson:" + p.ID})
	if e != nil {
		t.Fatal(e)
	}
	if len(collectiveReply(t, first)["investigations"].([]any)) != 1 {
		t.Fatal(first)
	}
	next, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID})
	if e != nil {
		t.Fatal(e)
	}
	if len(collectiveReply(t, next)["investigations"].([]any)) != 0 {
		t.Fatal("the same question must not recur during one work")
	}
}

func TestCollectiveDeletedArticleCanBeAcknowledged(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, nil)
	first, e := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.Manage(notebook.Change{ID: p.ID, Revision: p.Revision, Action: "delete"}); e != nil {
		t.Fatal(e)
	}
	read := func() map[string]any {
		v, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID})
		if e != nil {
			t.Fatal(e)
		}
		return collectiveReply(t, v)
	}
	u := read()["updates"].([]any)[0].(map[string]any)
	if u["status"] != "unavailable" {
		t.Fatal(u)
	}
	r, e := workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "reviewed": []any{u["ref"]}})
	if e != nil || r["complete"] != true {
		t.Fatal(r, e)
	}
	if len(read()["updates"].([]any)) != 0 {
		t.Fatal("an inspected deletion must settle without reviving its text")
	}
}

func TestCollectiveContrastIncludesReportedConditionsWithoutVoting(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"title": "Quartz delivery procedure", "kind": "howto"})
	for i, condition := range []string{"Linux synthetic input", "macOS synthetic input"} {
		work, e := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz delivery"})
		if e != nil {
			t.Fatal(e)
		}
		result := "Guarded input completed."
		failure := ""
		if i == 1 {
			result = "An unchecked input was rejected."
			failure = "Unchecked input"
		}
		r, e := workCall(t, b, "record", map[string]any{"receipt": work["receipt"], "observations": []any{map[string]any{"kind": "test", "title": condition, "content": result}}, "outcomes": []any{map[string]any{"ref": "knowledge1", "conditions": condition, "action": "Check delivery with synthetic input", "result": result, "failure": failure, "sources": []string{"new1"}}}})
		if e != nil || r["complete"] != true {
			t.Fatal(r, e)
		}
	}
	v, e := b.Collective(nil, []notebook.ArticleReference{{ID: p.ID, Revision: p.Revision}})
	if e != nil || len(v.Contrasts) != 1 || len(v.Contrasts[0].Reports) != 2 || v.Contrasts[0].Judgment != "uninterpreted" || v.Contrasts[0].Independence != "not_established" || v.Contrasts[0].Conditions != "different_text" {
		t.Fatal(v, e)
	}
	raw, _ := json.Marshal(v)
	if len(raw) > 4096 {
		t.Fatal(len(raw))
	}
}

func TestCollectiveSharedBudgetKeepsRecoveryForLargeQuotes(t *testing.T) {
	b, source := loopBook(t)
	a := loopPage(t, b, source, map[string]any{"title": "Quartz support", "body": strings.Repeat("\u7532", 190)})
	z := loopPage(t, b, source, map[string]any{"title": "Quartz counterexample", "body": strings.Repeat("\u4e59", 190)})
	links := learningConnections(a, z)
	links[1].(map[string]any)["relation"] = "counterexample"
	for _, v := range links {
		c := v.(map[string]any)
		c["conditions"] = strings.Repeat("\u6761", 130)
		c["reason"] = strings.Repeat("\u7531", 130)
	}
	p := loopPage(t, b, source, map[string]any{"learning": map[string]any{"connections": links, "probe": map[string]any{"question": strings.Repeat("\u554f", 190), "method": strings.Repeat("\u6cd5", 330), "reconsider_when": strings.Repeat("\u5909", 190)}}})
	base := b.CollectiveBaseline(notebook.ArticleReference{ID: p.ID, Revision: p.Revision})
	current := loopPage(t, b, source, map[string]any{"id": p.ID, "body": strings.Repeat("\u6539", 3000)})
	v, e := b.Collective([]notebook.CollectiveBaseline{base}, []notebook.ArticleReference{{ID: p.ID, Revision: current.Revision}})
	raw, _ := json.Marshal(v)
	if e != nil || len(raw) > 4096 || len(v.Updates) != 1 || len(v.Contrasts) != 1 || len(v.Investigations) != 1 || !v.Investigations[0].Omitted || v.Investigations[0].Query != "lesson:"+p.ID+"@"+current.Revision {
		t.Fatal(v, e, len(raw))
	}
}

func TestCollectiveChangedKnowledge(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"title": "Quartz invoice procedure", "body": "Keep the original condition.", "kind": "howto"})
	first, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz invoices"})
	if err != nil {
		t.Fatal(err)
	}
	next := loopPage(t, b, source, map[string]any{"id": p.ID, "title": p.Title, "body": "Keep the revised condition with a counterexample."})
	read := func() map[string]any {
		v, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID})
		if e != nil {
			t.Fatal(e)
		}
		return collectiveReply(t, v)
	}
	c := read()
	u := c["updates"].([]any)
	if len(u) != 1 {
		t.Fatal("one actually supplied article changed", c)
	}
	change := u[0].(map[string]any)
	if change["before"].(map[string]any)["revision"] != p.Revision || change["after"].(map[string]any)["revision"] != next.Revision {
		t.Fatal("both exact revisions must remain inspectable", change)
	}
	if read()["updates"].([]any)[0].(map[string]any)["ref"] != change["ref"] {
		t.Fatal("an unacknowledged change needs the same retry reference")
	}
	recorded, err := workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "reviewed": []any{change["ref"]}})
	if err != nil || recorded["complete"] != true {
		t.Fatal(recorded, err)
	}
	if len(read()["updates"].([]any)) != 0 {
		t.Fatal("acknowledgement must settle only the supplied change")
	}
}

func TestCollectiveConditionsAndInvestigation(t *testing.T) {
	b, source := loopBook(t)
	a := loopPage(t, b, source, map[string]any{"title": "Quartz successful condition", "body": "Use the guarded input."})
	z := loopPage(t, b, source, map[string]any{"title": "Quartz counterexample condition", "body": "An unchecked input failed."})
	links := learningConnections(a, z)
	links[1].(map[string]any)["relation"] = "counterexample"
	p := loopPage(t, b, source, map[string]any{"title": "Quartz deployment investigation", "applies_to": "Quartz deployment on Linux", "learning": map[string]any{"connections": links, "probe": map[string]any{"question": "Does Quartz deployment work on Linux?", "method": "Check an isolated Linux input.", "reconsider_when": "The deployment policy changes."}}})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz deployment on Linux"})
	if err != nil {
		t.Fatal(err)
	}
	c := collectiveReply(t, v)
	if len(c["contrasts"].([]any)) != 1 || len(c["investigations"].([]any)) != 1 {
		t.Fatal("explicit opposing roles and the related probe must arrive", c)
	}
	pair := c["contrasts"].([]any)[0].(map[string]any)
	if pair["independence"] != "shared_sources" || pair["judgment"] != "uninterpreted" {
		t.Fatal("shared evidence and interpretation must stay explicit", pair)
	}
	raw, _ := json.Marshal(c)
	if len(raw) > 4096 {
		t.Fatal("all collective fields share one response budget", len(raw))
	}
}
