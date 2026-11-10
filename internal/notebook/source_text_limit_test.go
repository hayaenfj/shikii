package notebook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// A conversation cited by a lesson archives the long text its decisions kept.
// Whatever the store can keep for one conversation must be archived so that it
// reads back: a lesson saved with an archive nobody can read is never delivered.
func TestArchivedOriginalsOfAConversationReadBack(t *testing.T) {
	// JSON writes "<" as six bytes: the largest archive a field of this length can need.
	worst := strings.Repeat("<", store.RecordTextLimit)
	opening := worst[:store.FeedbackTextLimit]
	cases := []struct {
		name   string
		fields []string
	}{
		{"what the hook keeps: the request and the closing message", []string{"request", "summary"}},
		{"the most the store keeps: four fields for each of two decisions", []string{"request", "summary", "previous", "files"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/project"}
			st := store.Store{Dir: b.Root}
			f := store.Feedback{ID: "0000000000000020", Request: opening, Summary: opening, Reply: "Read the whole text."}
			for i, spec := range []string{"done-as-asked", "changes-in-scope"} {
				rec := store.Record{TraceID: fmt.Sprintf("%016x", 0x10+i), SpecID: spec, Inputs: map[string]string{}, At: time.Now()}
				originals := map[string]string{}
				for _, field := range c.fields {
					rec.Inputs[field], originals[field] = opening, worst
				}
				rec.InputsHash = store.HashInputs(rec.Inputs)
				if err := st.AppendRecord(rec); err != nil {
					t.Fatal(err)
				}
				if err := st.SaveRecordText(rec, originals); err != nil {
					t.Fatal(err)
				}
				f.Targets = append(f.Targets, store.ReviewTarget{Spec: spec, TraceID: rec.TraceID})
			}
			if err := st.AppendFeedback(b.Scope, f); err != nil {
				t.Fatal(err)
			}
			result, err := b.Ingest(draft(f.ID, "Long conversation"))
			if err != nil {
				t.Fatalf("the conversation cannot be cited: %v", err)
			}
			dir, _ := b.directory(false, "sources")
			archive, err := os.Stat(filepath.Join(dir, f.ID+".text.json"))
			if err != nil || result.Page.Sources[0].TextHash == "" {
				t.Fatalf("the fixture changed: the long text was not archived: %v", err)
			}
			t.Logf("the archive file is %d bytes", archive.Size())
			v, err := b.Query("")
			if err != nil || len(v.Lessons) != 1 || len(v.Issues) != 0 {
				t.Errorf("the save succeeded, but the lesson is not listed: %d lessons, issues %+v, %v", len(v.Lessons), v.Issues, err)
			}
			material, err := b.ReadSources([]string{f.ID})
			if err != nil {
				t.Fatalf("the save succeeded, but the archived source cannot be read: %v", err)
			}
			archived := material["originals"].(map[string][]Original)[f.ID]
			if len(archived) != 2 {
				t.Fatalf("archived %d decisions, want 2", len(archived))
			}
			bytes := 0
			for _, o := range archived {
				for _, field := range c.fields {
					if o.Inputs[field] != worst {
						t.Errorf("the archived %s of %s is %d bytes, want %d", field, o.TraceID, len(o.Inputs[field]), len(worst))
					}
					bytes += len(o.Inputs[field])
				}
			}
			t.Logf("%s: %d bytes of long text archived and read back", c.name, bytes)
		})
	}
}

// The case above is the largest archive only while the store keeps at most two
// decisions for a conversation, four fields for a decision and RecordTextLimit
// bytes for a field.
func TestStoreBoundsTheLongTextOfAConversation(t *testing.T) {
	st := store.Store{Dir: t.TempDir()}
	three := []store.ReviewTarget{{Spec: "done-as-asked", TraceID: "0000000000000010"}, {Spec: "changes-in-scope", TraceID: "0000000000000011"}, {Spec: "done-as-asked", TraceID: "0000000000000012"}}
	if err := st.AppendFeedback("/project", store.Feedback{ID: "0000000000000020", Request: "request", Summary: "summary", Reply: "reply", Targets: three}); err == nil {
		t.Error("a conversation with three decisions was accepted")
	}
	rec := store.Record{TraceID: "0000000000000010", SpecID: "done-as-asked", Inputs: map[string]string{"a": "a", "b": "b", "c": "c", "d": "d", "e": "e"}, At: time.Now()}
	rec.InputsHash = store.HashInputs(rec.Inputs)
	long := func(prefix string) string { return prefix + strings.Repeat("x", 2000) }
	if err := st.SaveRecordText(rec, map[string]string{"a": long("a"), "b": long("b"), "c": long("c"), "d": long("d"), "e": long("e")}); err == nil {
		t.Error("five long fields were kept for one decision")
	}
	if err := st.SaveRecordText(rec, map[string]string{"a": "a" + strings.Repeat("x", store.RecordTextLimit)}); err != nil {
		t.Fatal(err)
	}
	if kept, status := st.RecordText(rec); len(kept["a"]) != store.RecordTextLimit || status["a"] != "limited" {
		t.Errorf("a field longer than the limit was kept as %d bytes (%q), want it cut to %d", len(kept["a"]), status["a"], store.RecordTextLimit)
	}
}
