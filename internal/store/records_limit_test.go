package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func recordsLimitFixture(t *testing.T) Store {
	t.Helper()
	s := Store{Dir: t.TempDir()}
	if e := os.MkdirAll(filepath.Join(s.Dir, "records"), 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(s.recordsPath("s"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range 1200 {
		r := Record{TraceID: fmt.Sprint(i), SpecID: "s", At: time.Unix(int64((i*73)%19), 0)}
		if e = enc.Encode(r); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = f.WriteString("broken\n{\"at\":\"2099-01-01T00:00:00Z\",\"inputs\":42}\n{\"trace_id\":"); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestRecordsLimitKeepsStableOrderAndBoundedCapacity(t *testing.T) {
	s := recordsLimitFixture(t)
	all, e := s.Records("s", 0)
	if e != nil || len(all) != 1200 {
		t.Fatal(len(all), e)
	}
	for _, limit := range []int{1, 2, 255, 256, 257, 1200, 1300, 0, -1} {
		got, e := s.Records("s", limit)
		if e != nil {
			t.Fatal(e)
		}
		want := all
		if limit > 0 && len(want) > limit {
			want = want[:limit]
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("\u6642\u523b\u9806\u30fb\u540c\u6642\u523b\u306e\u5143\u306e\u9806\u5e8f\u304c\u7570\u306a\u308b: %d", limit)
		}
		if limit > 0 && cap(got) > 2*limit {
			t.Errorf("\u4e0a\u9650\u3092\u8d85\u3048\u305f\u5168\u5c65\u6b74\u3092\u4fdd\u6301: limit=%d cap=%d", limit, cap(got))
		}
	}
}
