//go:build !race

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRecordsWithinLimitAvoidsExtraAllocation(t *testing.T) {
	for _, c := range []struct{ n, limit int }{{8, 256}, {256, 256}, {4000, 1 << 30}} {
		t.Run(fmt.Sprintf("rows%d_limit%d", c.n, c.limit), func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			if err := os.MkdirAll(filepath.Join(s.Dir, "records"), 0700); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(s.recordsPath("s"))
			if err != nil {
				t.Fatal(err)
			}
			enc := json.NewEncoder(f)
			for i := range c.n {
				if err = enc.Encode(Record{TraceID: fmt.Sprint(i), At: time.Unix(int64(i*73%19), 0), Inputs: map[string]string{"request": strings.Repeat("x", 128)}}); err != nil {
					f.Close()
					t.Fatal(err)
				}
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			measure := func(limit int) (uint64, []Record) {
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				var out []Record
				for range 10 {
					var e error
					out, e = s.Records("s", limit)
					if e != nil {
						t.Fatal(e)
					}
				}
				runtime.ReadMemStats(&after)
				runtime.KeepAlive(out)
				return (after.TotalAlloc - before.TotalAlloc) / 10, out
			}
			base, want := measure(0)
			limited, got := measure(c.limit)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("\u4ef6\u6570\u5185\u306e\u6642\u523b\u3068\u540c\u6642\u523b\u9806\u5e8f\u304c\u7570\u306a\u308b")
			}
			t.Logf("all=%d limited=%d B/op", base, limited)
			if limited > base*110/100 {
				t.Errorf("\u4e0a\u9650\u5185\u306e\u5c65\u6b74\u3067\u4e0d\u8981\u306a\u5272\u5f53: all=%d limited=%d B/op", base, limited)
			}
		})
	}
}
