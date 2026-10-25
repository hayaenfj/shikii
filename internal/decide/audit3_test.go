package decide

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// audit 9: even if the context step's call fails, the minimal answer (ask) is not thrown away.
func TestContextCallFailureKeepsTheMinimalAnswer(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if atomic.AddInt32(&calls, 1) == 2 || strings.Contains(req.State, "Previous turn") {
			http.Error(w, "boom", 500)
			return
		}
		probs := map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	sp := contextSpec()
	d, err := Decide(context.Background(), sp, map[string]string{"message": "fix a", "previous": "x"}, Session{}, []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}})
	if err != nil || d == nil || d.Rung != "minimal" || d.Band != Ask || d.Minimal != nil || d.SpecHash != sp.Hash() || d.P != 0.65 {
		t.Fatalf("the minimal answer must survive a failed context call: err=%v d=%+v", err, d)
	}
	if calls != 2 {
		t.Fatalf("the context rung was tried once: %d", calls)
	}
}
