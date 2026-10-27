package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A suspicion the 0.9.9 audit left: on a disk without hard links a built-in file is written in place, so a stop in the middle left a part
// of it, and every later init kept that part as if the person had written it (a cut spec or wiki page). A left-over part of the built-in
// text is now written again in full; a part still being written, and any other text, stay.
func TestPatch0910LeftOverPartOfABuiltInFileIsWrittenAgain(t *testing.T) {
	data := []byte("name: needs-plan\nquestion: Should the plan be agreed first?\n")
	old := time.Now().Add(-time.Minute)
	for _, c := range []struct {
		name, text string
		age        time.Time
		want       string
	}{
		{"a left-over part", "name: needs-pl", old, string(data)},
		{"a left-over empty file", "", old, string(data)},
		{"a part another init is writing now", "name: needs-pl", time.Now(), "name: needs-pl"},
		{"the person's own text", "name: mine\n", old, "name: mine\n"},
		{"the person's text that starts like it and goes on", string(data) + "# mine\n", old, string(data) + "# mine\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "needs-plan.yaml")
			if err := os.WriteFile(path, []byte(c.text), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, c.age, c.age); err != nil {
				t.Fatal(err)
			}
			if _, err := seedFile(path, data); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(path); string(b) != c.want {
				t.Fatalf("got %q, want %q", b, c.want)
			}
		})
	}
}
