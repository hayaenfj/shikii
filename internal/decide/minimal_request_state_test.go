package decide

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// What goes to the model for the minimal question is the template's inputs only: the context input reaches the
// model in the second call, never in the first, also when the message quotes its placeholder.
func TestTheMinimalRequestNeverCarriesTheContextInput(t *testing.T) {
	var states []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req systemone.Request
		_ = json.Unmarshal(body, &req)
		states = append(states, req.State)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"choice","probabilities":{"a":0.65,"b":0.3,"c":0.05}}}}`))
	}))
	t.Cleanup(srv.Close)
	sp := &spec.Spec{
		ID: "c", Type: spec.Choice, Question: "which?",
		Options:    []spec.Option{{ID: "a", Description: "A"}, {ID: "b", Description: "B"}, {ID: "c", Description: "C"}},
		State:      spec.State{Template: "Message:\n{{ message }}", Required: []string{"message"}, Context: "Message:\n{{ message }}\nPrevious turn:\n{{ previous }}"},
		Policy:     spec.Policy{SendTo: []string{"local"}, PII: boolPtr(false)},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
	}
	in := map[string]string{"message": "fix it like {{ previous }} said", "previous": "PREVIOUS-TURN-TEXT"}
	eps := []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
	for i := 0; i < 20; i++ {
		states = nil
		d, err := Decide(context.Background(), sp, in, Session{}, eps)
		if err != nil {
			t.Fatal(err)
		}
		if len(states) != 2 || d.Rung != "context" {
			t.Fatalf("want the minimal call and the context call, got %d calls, rung %s", len(states), d.Rung)
		}
		if states[0] != "Message:\nfix it like {{ previous }} said" {
			t.Fatalf("run %d: the minimal request is not the template's inputs alone: %q", i, states[0])
		}
		if !strings.HasSuffix(states[1], "Previous turn:\nPREVIOUS-TURN-TEXT") || strings.Count(states[1], "PREVIOUS-TURN-TEXT") != 1 {
			t.Fatalf("run %d: the context request must carry the context input once, where the template puts it: %q", i, states[1])
		}
	}
}
