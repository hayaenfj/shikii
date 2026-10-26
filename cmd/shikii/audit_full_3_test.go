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

// F2 (reflection): this session was asked, skipped it (the Stop recorded the miss), and the same conversations were then handled by another
// session; this session's next ask is for other conversations. Its agent skipped the reflection, so the next notice is the full text.
func TestAuditF2ReflectionMissedThenHandledElsewhere(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	const full = "keep the short lesson and the detailed body apart"
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	stop := func() {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		stop()
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	if !strings.Contains(first, full) {
		t.Fatalf("precondition: the first ask is the full text: %q", first)
	}
	// this session does not reflect; its Stop is the miss (the ritual check blocks once, the recheck records it)
	stop()
	stop()
	// another session handles exactly those conversations: what this session asked is no longer waiting (the record as that leaves it)
	if err := updateSession(st.Dir, "s1", func(s *hookSession) { s.ReflectLastAsked = []string{"ffffffffffffffff"} }); err != nil {
		t.Fatal(err)
	}
	next := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if strings.Contains(next, "wrap up the reflection") && !strings.Contains(next, full) {
		t.Fatalf("F2: this session skipped the reflection, yet its next ask is the short form: %q", next)
	}
}

// C1/C5: two sessions send their first request in the moved folder at the same time. The move is told once.
func TestAuditC5MovedNoticeOnceUnderTwoSessions(t *testing.T) {
	for round := 0; round < 20; round++ {
		_, _, moved := movedProject(t)
		var wg sync.WaitGroup
		var mu sync.Mutex
		told := 0
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if relinkMovedProject(moved, false) != "" {
					mu.Lock()
					told++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if told != 1 {
			t.Fatalf("round %d: the move was told %d times", round, told)
		}
	}
}

// controls: the same F2 scenario without the other session (the missed conversations still wait) gives the full text; the move told by one
// request alone is told once and a second request says nothing; after the concurrent relink the project is active at the new path only.
func TestAuditControls(t *testing.T) {
	t.Run("F2 without the other session", func(t *testing.T) {
		_, scope, _, _, _ := memoryEffectFixture(t)
		const full = "keep the short lesson and the detailed body apart"
		prompt := func(text string) string {
			return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
		}
		stop := func() {
			runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
		}
		memoryEffectPrompt(t, scope, "s1")
		var first string
		for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
			stop()
			first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
		}
		stop()
		stop()
		if next := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044"); !strings.Contains(next, full) {
			t.Fatalf("control: %q", next)
		}
	})
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
