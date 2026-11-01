package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestWorkSavedCapacity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing int
		keys     []string
		allowed  bool
	}{
		{"empty", 0, nil, true},
		{"one below", 127, []string{"new"}, true},
		{"two below", 127, []string{"new-a", "new-b"}, false},
		{"same proposed key twice", 127, []string{"new", "new"}, true},
		{"full replay", 128, []string{"saved-0"}, true},
		{"full multiple replay", 128, []string{"saved-0", "saved-127", "saved-0"}, true},
		{"full new", 128, []string{"new"}, false},
		{"full mixed", 128, []string{"saved-0", "new"}, false},
		{"legacy overfull replay", 129, []string{"saved-128", "saved-0"}, true},
		{"legacy overfull new", 129, []string{"new"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &workState{Saved: map[string]workItem{}}
			for i := 0; i < tc.existing; i++ {
				w.Saved[fmt.Sprintf("saved-%d", i)] = workItem{Kind: "test"}
			}
			err := workSavedCapacity(w, tc.keys...)
			if (err == nil) != tc.allowed {
				t.Fatalf("existing=%d keys=%v allowed=%v error=%v", tc.existing, tc.keys, tc.allowed, err)
			}
			if len(w.Saved) != tc.existing {
				t.Fatal("capacity checking must not mutate stored items")
			}
		})
	}
}

func TestWorkSavedCapacityPublicBoundary(t *testing.T) {
	b, _ := loopBook(t)
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "synthetic fixed capacity"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := prepared["receipt"].(string)
	saved, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "observations": []any{map[string]any{"kind": "test", "title": "Synthetic A", "content": "Synthetic capacity evidence A"}, map[string]any{"kind": "test", "title": "Synthetic B", "content": "Synthetic capacity evidence B"}}})
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	var first map[string]any
	for i := 0; i < 128; i++ {
		refs := make([]string, 8)
		for j := range refs {
			refs[j] = "observed1"
			if i&(1<<j) != 0 {
				refs[j] = "observed2"
			}
		}
		input := map[string]any{"receipt": receipt, "reviewed": refs}
		if i == 0 {
			first = input
		}
		result, err := workCall(t, b, "record", input)
		if err != nil || result["complete"] != true {
			t.Fatalf("batch %d: %v %v", i+1, result, err)
		}
	}
	excess := map[string]any{"receipt": receipt, "reviewed": []string{"observed1", "observed1", "observed1", "observed1", "observed1", "observed1", "observed1", "observed2"}}
	result, err := workCall(t, b, "record", excess)
	if err == nil && result["complete"] == true {
		t.Error("the 129th distinct batch must not succeed")
	}
	replay, err := workCall(t, b, "record", first)
	if err != nil || replay["complete"] != true {
		t.Fatal("a full context must retain its existing reply", replay, err)
	}
}

func TestWorkSavedCapacityReservesPublishedNoteRecovery(t *testing.T) {
	b, _ := loopBook(t)
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := prepared["receipt"]
	payload := map[string]any{"receipt": receipt, "observations": []any{map[string]any{"title": "Checked filters", "content": "The CSV keeps filters."}}, "notes": []any{map[string]any{"title": "CSV filter procedure", "lesson": "Keep filters in CSV.", "applies_to": "CSV export", "kind": "howto", "scene": "exporting CSV", "sources": []string{"new1"}}}}
	old := workAfterWrite
	defer func() { workAfterWrite = old }()
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "note" && !failed {
			failed = true
			return errors.New("synthetic failure after publication")
		}
		return nil
	}
	partial, err := workCall(t, b, "record", payload)
	if err != nil || partial["complete"] != false || !failed {
		t.Fatal(partial, err)
	}
	workAfterWrite = old
	pages, err := b.Live()
	if err != nil || len(pages) != 1 {
		t.Fatal(pages, err)
	}
	for i := 0; i < workSavedLimit; i++ {
		refs := make([]string, 8)
		for j := range refs {
			refs[j] = "observed1"
			if i&(1<<j) != 0 {
				refs[j] = "evidence1"
			}
		}
		value, e := workCall(t, b, "record", map[string]any{"receipt": receipt, "reviewed": refs})
		if e != nil || value["complete"] != true {
			break
		}
	}
	replay, err := workCall(t, b, "record", payload)
	if err != nil || replay["complete"] != true {
		t.Fatal("new batches consumed the recovery capacity of an already published note", replay, err)
	}
	after, err := b.Live()
	if err != nil || len(after) != 1 || after[0].Revision != pages[0].Revision {
		t.Fatal("recovery must retain the published revision", after, err)
	}
}

func TestWorkSavedCapacityCountsPublicationReservations(t *testing.T) {
	reserved := func(count int) []string {
		keys := make([]string, count)
		for i := range keys {
			keys[i] = fmt.Sprintf("reserved-%d", i)
		}
		return keys
	}
	for _, tc := range []struct {
		name     string
		saved    int
		expected []string
		keys     []string
		allowed  bool
	}{
		{"reserved full blocks new", 0, reserved(128), []string{"new"}, false},
		{"reserved full allows recovery", 0, reserved(128), []string{"reserved-127"}, true},
		{"mixed occupied full blocks new", 127, []string{"reserved-0"}, []string{"new"}, false},
		{"mixed occupied full allows recovery", 127, []string{"reserved-0"}, []string{"reserved-0", "saved-0"}, true},
		{"saved reservation overlap counted once", 127, []string{"saved-0"}, []string{"new"}, true},
		{"reserved duplicate proposal counted once", 126, []string{"reserved-0"}, []string{"new", "new"}, true},
		{"two new keys exceed occupied union", 126, []string{"reserved-0"}, []string{"new-a", "new-b"}, false},
		{"legacy occupied overflow allows recovery", 128, []string{"reserved-0"}, []string{"reserved-0"}, true},
		{"legacy occupied overflow allows saved reply", 128, []string{"reserved-0"}, []string{"saved-0"}, true},
		{"legacy reservations overflow blocks new", 0, reserved(129), []string{"new"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &workState{Saved: map[string]workItem{}, Expected: map[string]store.KnowledgeReference{}}
			for i := 0; i < tc.saved; i++ {
				w.Saved[fmt.Sprintf("saved-%d", i)] = workItem{Kind: "test"}
			}
			for _, key := range tc.expected {
				w.Expected[key] = store.KnowledgeReference{ID: "fixed", Revision: "retained"}
			}
			savedBefore, expectedBefore := len(w.Saved), len(w.Expected)
			err := workSavedCapacity(w, tc.keys...)
			if (err == nil) != tc.allowed {
				t.Fatalf("saved=%d reserved=%d keys=%v allowed=%v error=%v", tc.saved, len(tc.expected), tc.keys, tc.allowed, err)
			}
			if len(w.Saved) != savedBefore || len(w.Expected) != expectedBefore {
				t.Fatal("capacity checks must preserve stored and reserved keys")
			}
		})
	}
}
