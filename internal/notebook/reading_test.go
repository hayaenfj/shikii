package notebook

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestReadingHistoryRetainsCounterexamplesAtCapacity(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/reading-learning"}
	o, err := b.Capture(Observation{Kind: "test", Title: "Reading fixture", Content: "The fixed source condition was checked.", Reference: "reading fixture", Version: "fixture", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := b.Ingest(Draft{Title: "Reading fixture", Lesson: "Keep the source condition.", AppliesTo: "reading fixture", Kind: "howto", Scene: "reading a source", Sources: []string{o.ID}})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := b.OutcomeSources([]string{o.ID})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 130; i++ {
		d := ReadingDetails{Topic: "reading fixture", Case: fmt.Sprintf("%016x", i), Work: fmt.Sprintf("%016x", i+1000), Check: "Keep the requested condition.", Depth: "brief", Bytes: 1000, Reads: 1, Sufficient: i != 0, Articles: []ArticleReference{{ID: page.Page.ID, Revision: page.Page.Revision}}, Evidence: evidence}
		data, _ := json.Marshal(d)
		source, err := b.Capture(Observation{Kind: "agent", Title: "Measured reading execution", Content: string(data), Reference: "reading fixture", Version: "fixture", ObservedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		refs, err := b.OutcomeSources([]string{source.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err = b.RecordReading(ReadingSample{ReadingDetails: d, Snapshot: refs[0]}); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := b.directory(false, "searches")
	if err != nil {
		t.Fatal(err)
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Readings) > 128 {
		t.Fatal("reading history exceeded its fixed bound")
	}
	for _, s := range j.Readings {
		if !s.Sufficient {
			return
		}
	}
	t.Fatal("capacity pruning must not erase a counterexample and revive its rejected depth")
}

func TestReadingQualityResultIdentityIgnoresTransportAndProvenance(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/reading-identity"}
	identity := func(content, kind string, sufficient bool) (string, error) {
		t.Helper()
		o, err := b.Capture(Observation{Kind: kind, Title: "Quality result", Content: content, Reference: "synthetic", Version: "fixture", ObservedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := b.OutcomeSources([]string{o.ID})
		if err != nil {
			t.Fatal(err)
		}
		d := ReadingDetails{Topic: "identity", Case: "0000000000000001", Work: "0000000000000002", Check: "Keep the condition.", Depth: "brief", Bytes: 100, Reads: 1, Sufficient: sufficient, Articles: []ArticleReference{{ID: "0000000000000003", Revision: "0000000000000004"}}, Evidence: evidence}
		data, _ := json.Marshal(d)
		o, err = b.Capture(Observation{Kind: "agent", Title: "Measured reading execution", Content: string(data), Reference: "synthetic execution", Version: "fixture", ObservedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		source, err := b.OutcomeSources([]string{o.ID})
		if err != nil {
			t.Fatal(err)
		}
		hashes, err := b.checkedReading(ReadingSample{ReadingDetails: d, Snapshot: source[0]})
		if err != nil {
			return "", err
		}
		return hashes[0], nil
	}
	pairs := [][2]string{
		{"Recovered the condition.", `{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":true,"observed":"Recovered the condition."}`},
		{`{"observed":{"a":1,"b":2}}`, `{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":true,"observed":"{\"b\":2,\"a\":1}"}`},
		{"The same checked condition.", "The same checked condition."},
		{`{"observed":{"a":1,"b":2}}`, ` { "observed":{"b":2,"a":1} } `},
		{`{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":true,"observed":"Recovered the condition.","discoveries":null}`, `{"observed":"Recovered  the condition.","sufficient":true,"check":"Keep the condition.","type":"reading-quality/v1","discoveries":[{"ref":"knowledge2"}],"observation":"Unrelated transport note"}`},
	}
	for i, pair := range pairs {
		a, err := identity(pair[0], "test", true)
		if err != nil {
			t.Fatal(err)
		}
		z, err := identity(pair[1], "agent", true)
		if err != nil {
			t.Fatal(err)
		}
		if a != z {
			t.Errorf("the same quality result multiplied support: %d", i)
		}
	}
	for _, content := range []string{`{"type":"reading-quality/v1","check":"A different check.","sufficient":true,"observed":"Recovered the condition."}`, `{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":false,"observed":"Recovered the condition."}`} {
		if _, err := identity(content, "agent", true); err == nil {
			t.Error("accepted typed evidence with a different quality result")
		}
	}
}
