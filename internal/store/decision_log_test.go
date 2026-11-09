package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestDecisionLogMatchesSeparateReaders(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for i, band := range []string{"ask", "discard", "act"} {
		if err := s.AppendRecord(Record{TraceID: band, SpecID: "s", Band: band, At: time.Unix(int64(3-i), 0), Rung: "context"}); err != nil {
			t.Fatal(err)
		}
	}
	f, err := s.openFile(s.recordsPath("s"), os.O_APPEND|os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("broken\n  \n{\"trace_id\":"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, l := range []Label{
		{TraceID: "ask", SpecID: "s", Source: "ui", Truth: "yes", At: time.Unix(1, 0)},
		{TraceID: "ask", SpecID: "s", Source: "judge", Truth: "no", At: time.Unix(9, 0)},
		{TraceID: "ask", SpecID: "s", Source: "ui", Truth: "no", At: time.Unix(2, 0)},
		{TraceID: "ask", SpecID: "s", Source: "ui", Truth: "yes", At: time.Unix(2, 0)},
		{SpecID: "s", Source: "ui", Truth: "yes"},
	} {
		f, err := s.openFile(s.labelsPath("s"), os.O_CREATE|os.O_APPEND|os.O_WRONLY)
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewEncoder(f).Encode(l)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	view, err := s.ReadDecisionLog(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	n, labs, err := s.Counts("s")
	if err != nil {
		t.Fatal(err)
	}
	if view.RawRecords != n || n != 5 || !reflect.DeepEqual(view.Labels, labs) {
		t.Fatalf("raw lines/labels differ: raw=%d want=%d", view.RawRecords, n)
	}
	labels, err := s.LabeledTraces("s")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Labeled, labels) || view.Labeled["ask"].Truth != "no" {
		t.Fatal("latest human truth differs")
	}
	view.NewestFirst()
	records, err := s.Records("s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Records, records) {
		t.Fatal("stable newest-first order differs")
	}
	for _, discard := range []bool{false, true} {
		want, err := s.Funnel("s", discard)
		if err != nil {
			t.Fatal(err)
		}
		if view.Funnel(discard) != want {
			t.Fatal("funnel differs")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.ReadDecisionLog(ctx, "s"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled reads must stop")
	}
}
