package store

import (
	"fmt"
	"sync"
	"testing"
)

// まだ records/ の無い置き場へ、同じプロセスから同時に初めての記録を書いても、どれも失われない
// (hook は 2 つの spec を並行して聞き、wiki の差し込み先は記事ごとに並行して聞く。初めての判定でフォルダを同時に作る)。
func TestFirstRecordsCanBeAppendedConcurrently(t *testing.T) {
	for round := 0; round < 20; round++ {
		st := Store{Dir: t.TempDir()}
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = st.AppendRecord(Record{TraceID: fmt.Sprintf("t%d", i), SpecID: fmt.Sprintf("spec-%d", i%3)})
			}(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d writer %d: %v", round, i, err)
			}
		}
		for s := 0; s < 3; s++ {
			recs, err := st.Records(fmt.Sprintf("spec-%d", s), 0)
			if err != nil || len(recs) == 0 {
				t.Fatalf("round %d spec-%d: %d records, %v", round, s, len(recs), err)
			}
		}
	}
}
