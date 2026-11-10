package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// status counts a spec from one read of its records and labels. The lines it prints are the ones the store's own counts give:
// the number of record lines (a line that is not JSON included) and labels, the records per project, and the rungs.
func TestStatusCountsFromOneReadLikeTheStore(t *testing.T) {
	agent, st, sp := truthsFixture(t)
	path := hookSpecPath(agent, sp.ID)
	records, labels, err := st.Counts(sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.Records(sp.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	byProject := map[string]int{}
	for _, r := range all {
		if r.Project != "" {
			byProject[r.Project]++
		}
	}
	funnel, err := st.Funnel(sp.ID, decide.DiscardUnsure(sp))
	if err != nil {
		t.Fatal(err)
	}
	if records != 10 || len(labels) != 9 || len(all) != 9 || len(byProject) != 2 || funnel.Human == 0 || funnel.Minimal == 0 {
		t.Fatalf("the fixture has a line that is not JSON, two projects and both rungs: %d %d %d %v %+v", records, len(labels), len(all), byProject, funnel)
	}

	before := store.JSONLScans()
	out := capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	scans := store.JSONLScans() - before

	for _, want := range []string{
		fmt.Sprintf("%s: records=%d labels=%d\n", sp.ID, records, len(labels)),
		fmt.Sprintf("  by project: %s\n", fmtCounts(byProject)),
		fmt.Sprintf("  rungs: settled by the minimal question %d, by added context %d, left to a person %d (%.0f per 1000 decisions)\n", funnel.Minimal, funnel.Context, funnel.Human, funnel.HumanPerThousand()),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status prints the store's own count %q:\n%s", want, out)
		}
	}
	if scans != 2 {
		t.Fatalf("status scans the spec's records file once and its labels file once, got %d scans", scans)
	}
}
