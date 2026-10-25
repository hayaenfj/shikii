package store

import (
	"strings"
	"testing"
	"time"
)

// 0.9.2 re-check with real agents: a provisional score sent for a decision whose truth came from the outcome was refused with
// "a decision a person answered ...", which is not what happened. The refusal names where the truth came from.
func TestReviewRefusalNamesWhereTheTruthCameFrom(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if err := st.AppendLabel(Label{TraceID: "0000000000000001", SpecID: "changes-in-scope", SpecHash: "test-hash", Model: "test-model", Truth: "yes", Source: "outcome", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := st.SaveReview(Review{TraceID: "0000000000000001", Spec: "changes-in-scope", Scope: "/work/a", InputsHash: "x", Truth: "yes", Reason: "checked", Source: Feedback{ID: "0000000000000002", Request: "r", Summary: "s", Reply: "p"}})
	if err == nil || !strings.Contains(err.Error(), "outcome") || strings.Contains(err.Error(), "person answered") {
		t.Fatalf("the refusal says the truth came from the outcome: %v", err)
	}
}

// A truth sent by an agent through MCP (source mcp) is neither a person's answer nor the outcome; the refusal must not say it is.
func TestReviewRefusalDoesNotCallAnMCPTruthAPerson(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if err := st.AppendLabel(Label{TraceID: "0000000000000001", SpecID: "changes-in-scope", SpecHash: "test-hash", Model: "test-model", Truth: "yes", Source: "mcp", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := st.SaveReview(Review{TraceID: "0000000000000001", Spec: "changes-in-scope", Scope: "/work/a", InputsHash: "x", Truth: "yes", Reason: "checked", Source: Feedback{ID: "0000000000000002", Request: "r", Summary: "s", Reply: "p"}})
	if err == nil || !strings.Contains(err.Error(), "source mcp") || strings.Contains(err.Error(), "a person or the outcome") {
		t.Fatalf("the refusal names source mcp without calling it a person or the outcome: %v", err)
	}
}
