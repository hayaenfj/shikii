package decide

import (
	"context"
	"testing"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// The spec's layout goes into the System One request (the converter changes the arrangement; decision models ignore it as an unknown field).
func TestLayoutTravelsInTheRequest(t *testing.T) {
	f := newFake(t, "m", systemone.Answer{Type: "noul", Noul: 0.9})
	sp := noulSpec("local")
	eps := []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "m")}}
	if _, err := Decide(context.Background(), sp, okInputs, Session{}, eps); err != nil {
		t.Fatal(err)
	}
	if f.last.Layout != "" {
		t.Fatalf("no layout in the spec, none in the request: %q", f.last.Layout)
	}
	sp.Layout = "plain"
	if _, err := Decide(context.Background(), sp, okInputs, Session{}, eps); err != nil {
		t.Fatal(err)
	}
	if f.last.Layout != "plain" {
		t.Fatalf("the spec's layout must reach the model: %q", f.last.Layout)
	}
}
