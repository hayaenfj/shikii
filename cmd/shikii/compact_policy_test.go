package main

import (
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

func TestCompactMemoryUnknownNeedsNoInventedExplanation(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "compact-memory")
	id := memoryEffectComplete(t, st, scope, "compact-memory")
	r := memoryEffectSubmit(t, h, id, "lesson:"+draft.ID+":"+revision, "unknown", "", "")
	if r["complete"] != true {
		t.Fatal("unknown needs no fictional use/effect narrative", r)
	}
	stats := memoryEffectStats(t, h)
	if stats["unknown"] != float64(1) || stats["helpful"] != float64(0) {
		t.Fatal("delivery must not become usefulness", stats)
	}
}

func TestAutoTuneSkipsOneSidedEligibleTruths(t *testing.T) {
	root, _ := outcomeFixture(t, 60)
	sp := mustLoad(t, filepath.Join(root, "decisions", "changes-in-scope.yaml"))
	sp.Variants = append(sp.Variants, spec.Variant{Name: "clearer", Question: "Were all changed files necessary for the requested task?"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	old := hookTune
	defer func() { hookTune = old }()
	calls := 0
	hookTune = func(string, string, string) { calls++ }
	if err := hookMaybeTune(root, sp.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("one-sided eligible answers cannot compare discrimination; do not spend model calls", calls)
	}
}
