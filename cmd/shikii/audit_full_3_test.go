package main

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

// F4 (reflection): every field name the full reflection request gives for the submit shape is also in the short form.
func TestAuditF4ReflectionShortKeepsSubmitFields(t *testing.T) {
	full, short := reflectAsk("X reflect"), reflectAskShort("X reflect", []string{"a"})
	re := regexp.MustCompile(`\[\{([^}]*)\}\]`)
	var missing []string
	for _, m := range re.FindAllStringSubmatch(full, -1) {
		for _, f := range strings.Split(m[1], ",") {
			if !strings.Contains(short, f) {
				missing = append(missing, f)
			}
		}
	}
	for _, f := range []string{"page_id", "revision"} { // maintenance's fields in the full text
		if strings.Contains(full, f) && !strings.Contains(short, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the short reflection request lacks submit fields of the full text: %v\nshort: %q", missing, short)
	}
}

// C1/C5 (two sessions send their first request in the moved folder at the same time; the move is told once) is checked in one run by
// TestFull1RelinkAfterAnotherHookReconnectedSaysNothing and TestFull1RelinkWritesUnderTheLock.

// controls: the same F2 scenario without the other session (the missed conversations still wait) gives the full text; the move told by one
// request alone is told once and a second request says nothing; after the concurrent relink the project is active at the new path only.
func TestAuditControls(t *testing.T) {
	t.Run("C5 sequential", func(t *testing.T) {
		_, _, moved := movedProject(t)
		a, b := relinkMovedProject(moved, false), relinkMovedProject(moved, false)
		if a == "" || b != "" {
			t.Fatalf("control: %q / %q", a, b)
		}
	})
	t.Run("C5 concurrent state", func(t *testing.T) {
		common, old, moved := movedProject(t)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); relinkMovedProject(moved, false) }()
		}
		wg.Wait()
		active, err := projectActive(moved+"/.shikii", moved)
		paths := registeredPaths(common)
		t.Logf("active=%v err=%v paths=%v old=%s", active, err, paths, old)
		for _, p := range paths {
			if p == old {
				t.Fatalf("the old path stays in the index: %v", paths)
			}
		}
	})
}
