package decide

import (
	"context"
	"testing"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// spec の layout は System One の要求に載る(変換役が並べ方を変える。決定モデルは知らない項目として無視する)。
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
