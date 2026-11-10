package main

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestDecisionListsContinuePastOneThousand(t *testing.T) {
	base, path, dir := newServeFixture(t, "false", map[string]float64{"change_request": 1})
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: dir}
	for i := 0; i < 1001; i++ {
		r := store.Record{TraceID: fmt.Sprintf("synthetic-%04d", i), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "synthetic", Raw: map[string]float64{"change_request": 1}, Band: "ask", Inputs: map[string]string{"message": "synthetic"}, At: time.Unix(int64(i), 0)}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	read := func(endpoint string) string {
		t.Helper()
		cursor := ""
		seen := map[string]bool{}
		first := ""
		for page := 0; page < 7; page++ {
			var response struct {
				Items []struct {
					TraceID string `json:"trace_id"`
				} `json:"items"`
				Next string `json:"next_cursor"`
			}
			query := base + "/v1/" + endpoint + "?spec=intent&limit=200"
			if cursor != "" {
				query += "&cursor=" + url.QueryEscape(cursor)
			}
			if code := getJSON(t, query, &response); code != 200 {
				t.Fatalf("%s status=%d", endpoint, code)
			}
			for _, it := range response.Items {
				if seen[it.TraceID] {
					t.Fatalf("duplicate %s", it.TraceID)
				}
				seen[it.TraceID] = true
			}
			if page == 0 {
				first = response.Next
			}
			cursor = response.Next
			if cursor == "" {
				break
			}
		}
		if len(seen) != 1001 {
			t.Errorf("%s: got %d rows, want 1001", endpoint, len(seen))
		}
		if first == "" {
			t.Errorf("%s: missing continuation", endpoint)
		}
		return first
	}
	first := read("queue")
	if code, _ := postJSON(t, base+"/v1/label", map[string]any{"spec": "intent", "trace_id": "synthetic-1000", "truth": "change_request"}); code != 200 {
		t.Fatal(code)
	}
	var result map[string]any
	if first != "" {
		if code := getJSON(t, base+"/v1/queue?spec=intent&limit=200&cursor="+url.QueryEscape(first), &result); code != 409 {
			t.Errorf("changed list: got %d, want 409", code)
		}
	}
	if code := getJSON(t, base+"/v1/queue?spec=intent&cursor=invalid", &result); code != 400 {
		t.Errorf("invalid cursor: got %d", code)
	}
	recs, err := st.Records("intent", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		ls, err := store.LabelsFor(&r, "change_request", 1, "ui")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(ls); err != nil {
			t.Fatal(err)
		}
	}
	read("answered")
}
