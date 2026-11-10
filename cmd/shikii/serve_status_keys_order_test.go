package main

import (
	"fmt"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// The lines per model and wording in /v1/status come in one order on every request: by model, then rung, then wording hash.
// The labels name four wordings of one model in descending order; all four are stale lines of the same rung, so only the hash
// can order them. No rotation of a descending list is ascending, so a walk over the map in its own order cannot pass.
func TestStatusEndpointListsKeysInTheSameOrderEveryRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	url, _, storeDir := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	st := store.Store{Dir: storeDir}
	for i, hash := range []string{"wording-d", "wording-c", "wording-b", "wording-a"} {
		if err := st.AppendLabel(store.Label{TraceID: fmt.Sprintf("trace-%d", i), SpecID: "intent", SpecHash: hash, Model: "m", Truth: "question", Weight: 1, Source: "ui"}); err != nil {
			t.Fatal(err)
		}
	}
	var stat struct {
		Specs []struct {
			ID   string
			Keys []struct{ Model, Hash, Rung, State string }
		}
	}
	if code := getJSON(t, url+"/v1/status", &stat); code != 200 {
		t.Fatalf("status: %d", code)
	}
	if len(stat.Specs) != 1 || stat.Specs[0].ID != "intent" || len(stat.Specs[0].Keys) != 4 {
		t.Fatalf("status lists the four keys of the one spec: %+v", stat.Specs)
	}
	got := []string{}
	for _, k := range stat.Specs[0].Keys {
		if k.Model != "m" || k.Rung != "minimal" || k.State != "stale" {
			t.Fatalf("precondition: the four keys differ only in the wording hash: %+v", stat.Specs[0].Keys)
		}
		got = append(got, k.Hash)
	}
	if want := []string{"wording-a", "wording-b", "wording-c", "wording-d"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("status lists keys of one model and rung sorted by wording hash: got %v, want %v", got, want)
	}
}
