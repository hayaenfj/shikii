package notebook

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// Behaviour check 0.9.2 (low 8 / low 4): agents without hooks captured the person's rule as a kind:"human" source, cited it as
// correction.source_id and were refused three times by "the correction quote must be copied verbatim ...", which did not say why.
// The check stays as strict as before (a correction quotes the person's next message in a conversation the hook recorded);
// the refusal now says what is needed and what to do without hooks.
func TestCorrectionRefusalSaysWhatIsNeeded(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	conversation := seed(t, b, 1)
	captured, err := b.Capture(Observation{Kind: "human", Title: "the person's rule", Content: "Always write invoices in yen.", Reference: "chat", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		sources []string
		c       store.Correction
		want    []string
	}{
		{"a captured source is not a recorded message", []string{captured.ID}, store.Correction{SourceID: captured.ID, Quote: "Always write invoices in yen.", Action: "write yen"},
			[]string{captured.ID, "conversation recorded by the hook", "without correction", "sources"}},
		{"the quote is not in the person's reply", []string{conversation}, store.Correction{SourceID: conversation, Quote: "words nobody said", Action: "check"},
			[]string{conversation, "verbatim", "reply"}},
		{"the correction source is not cited", []string{conversation}, store.Correction{SourceID: captured.ID, Quote: "Always write invoices in yen.", Action: "write yen"},
			[]string{captured.ID, "sources"}},
	}
	for _, tc := range cases {
		d := draft(tc.sources[0], "invoice rule")
		d.Sources, d.Correction = tc.sources, &tc.c
		_, err := b.Ingest(d)
		if err == nil {
			t.Fatalf("%s: a correction that does not quote a recorded person's message is refused", tc.name)
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal says %q: %v", tc.name, w, err)
			}
		}
	}
	// the right quote from the recorded conversation is still accepted (the check is not weakened, and not closed either)
	d := draft(conversation, "startup rule")
	d.Correction = &store.Correction{SourceID: conversation, Quote: "model startup instructions are missing", Action: "check model startup instructions"}
	if _, err := b.Ingest(d); err != nil {
		t.Fatalf("a verbatim quote from the person's reply is accepted: %v", err)
	}
}
