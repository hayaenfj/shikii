package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// Two of the person's past answers can be too long to fit the delivery together even at the smallest cut: what the person said is
// not cut (here the agent had asked to check first with a long reason, the person answered it was fine to go ahead, and the page's
// Japanese labels are added). The hook must then deliver the one that fits, not stay silent, and leave the other for the next request.
func TestRequestHookDeliversTheAnswerThatFitsWhenTwoDoNot(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	setHookLanguage(t, "Japanese")
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	sp, err := spec.Load(hookSpecPath(st.Dir, "needs-plan"))
	if err != nil {
		t.Fatal(err)
	}
	const request = "update the invoice routing procedure for the billing export"
	reason := strings.Repeat("the export format and the owner of the billing report are not decided yet; ", 4)
	traces := []string{}
	for i := 0; i < 2; i++ {
		trace := store.NewTraceID()
		inputs := map[string]string{"request": request + fmt.Sprintf(" part %d", i), "open": planningPreface + reason, "agreed": "the billing export keeps its columns"}
		at := time.Now().UTC().Add(time.Duration(i) * time.Second)
		rec := store.Record{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", Inputs: inputs, InputsHash: store.HashInputs(inputs), Top: "no", P: .4, Band: "ask", AgentPaired: true, At: at}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabel(store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", InputsHash: rec.InputsHash, Inputs: inputs, Truth: "no", Weight: 1, Source: "ui", At: at}); err != nil {
			t.Fatal(err)
		}
		traces = append(traces, trace)
	}
	quotes, err := personAnswerQuotes(st.Dir, request, nil, nil)
	if err != nil || len(quotes) != 2 {
		t.Fatalf("precondition: both answers fit the request: %v %v", quotes, err)
	}
	for _, q := range quotes {
		if len(q.Said) < 595 { // personAnswerSaid cuts at 600 bytes; two of that length do not fit 1800 bytes with the prefix
			t.Fatalf("precondition: what the person said is at its cap, so the two do not fit together: %d bytes\n%s", len(q.Said), q.Said)
		}
	}
	delivered := func(session, prompt string) string {
		t.Helper()
		ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": prompt}))
		at := strings.Index(ctx, memoryPrefix)
		if at < 0 {
			return ""
		}
		text := ctx[at:] // the memory text ends with its one line of JSON; other notices may follow
		if start := strings.Index(text, "\n["); start >= 0 {
			if end := strings.Index(text[start+1:], "\n"); end >= 0 {
				text = text[:start+1+end]
			}
		}
		return text
	}
	first := delivered("fits", request)
	t.Logf("first request: delivered %d bytes, answers %d", len(first), strings.Count(first, `"key":"decision:`))
	if strings.Count(first, `"key":"decision:`) != 1 || len(first) > 1800 {
		t.Fatalf("the hook delivers the one answer that fits within 1800 bytes: %d bytes\n%s", len(first), first)
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil || len(deliveries) != 1 || len(deliveries[0].Quotes) != 1 {
		t.Fatalf("the delivery record keeps the one quote that was delivered: %+v %v", deliveries, err)
	}
	got := deliveries[0].Quotes[0].ID
	other := traces[0]
	if got == traces[0] {
		other = traces[1]
	} else if got != traces[1] {
		t.Fatalf("the delivered quote is one of the two answers: %s", got)
	}
	// the answer left out was not delivered, so it is not held back as "delivered within the hour": the next request gets it
	second := delivered("fits", request+" again")
	t.Logf("second request: delivered %d bytes, answers %d", len(second), strings.Count(second, `"key":"decision:`))
	if !strings.Contains(second, `"key":"decision:`+other) || strings.Contains(second, `"key":"decision:`+got) {
		t.Fatalf("the next request in the session gets the answer that was left out, and not the delivered one again:\n%s", second)
	}
}

// The newer answer does not fit alone (its reason is markup: every < takes six bytes in the JSON the hook delivers). The older
// one, which fits, must still arrive: quotes used to be left out from the end only, so nothing was delivered at any request.
func TestRequestHookDeliversTheAnswerThatFitsWhenTheOneBeforeItDoesNot(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	setHookLanguage(t, "Japanese")
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	sp, err := spec.Load(hookSpecPath(st.Dir, "needs-plan"))
	if err != nil {
		t.Fatal(err)
	}
	const request = "update the invoice routing procedure for the billing export"
	seed := func(i int, reason string) string {
		trace := store.NewTraceID()
		inputs := map[string]string{"request": request + fmt.Sprintf(" part %d", i), "open": planningPreface + reason, "agreed": "the billing export keeps its columns"}
		at := time.Now().UTC().Add(time.Duration(i) * time.Second)
		rec := store.Record{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", Inputs: inputs, InputsHash: store.HashInputs(inputs), Top: "no", P: .4, Band: "ask", AgentPaired: true, At: at}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabel(store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", InputsHash: rec.InputsHash, Inputs: inputs, Truth: "no", Weight: 1, Source: "ui", At: at}); err != nil {
			t.Fatal(err)
		}
		return trace
	}
	fits := seed(0, "the owner is not decided")
	tooLong := seed(1, strings.Repeat("<", 200))
	quotes, err := personAnswerQuotes(st.Dir, request, nil, nil)
	if err != nil || len(quotes) != 2 || quotes[0].ID != tooLong || quotes[1].ID != fits {
		t.Fatalf("both answers match the request, the newer (long) one first: %v %v", quotes, err)
	}
	if _, _, _, ok, err := fitQuotes(quotes[:1], []string{quotes[0].Key}); ok || err != nil {
		t.Fatalf("this fixture needs a first answer that does not fit alone: ok=%v err=%v", ok, err)
	}
	session := &hookSession{TurnID: "aaaaaaaaaaaaaa01"}
	ctx, err := deliverMemory(st.Dir, scope, request, nil, true, session, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "decision:"+fits) || strings.Contains(ctx, "decision:"+tooLong) || len(ctx) > 1800 {
		t.Fatalf("the answer that fits is delivered and the one that does not is left out (delivered %d bytes): %q", len(ctx), cut(ctx, 300))
	}
	// only what arrived is recorded as delivered
	if len(session.Memories) != 1 || session.Memories[0].Key != "decision:"+fits {
		t.Errorf("the session records the delivered answer alone: %+v", session.Memories)
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil || len(deliveries) != 1 || len(deliveries[0].Quotes) != 1 || deliveries[0].Quotes[0].Key != "decision:"+fits {
		t.Errorf("the delivery record holds the delivered answer alone: %+v %v", deliveries, err)
	}
}
