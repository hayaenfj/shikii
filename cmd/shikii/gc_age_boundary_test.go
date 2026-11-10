package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestGCAgeRejectsNegativeFractionsBeforeDeleting(t *testing.T) {
	for _, age := range []string{"-1e-30d", "-1e-30w", "-1e-400d", "-1e-400w", "NaNd", "+Infd", "106752d", "15251w"} {
		t.Run(age, func(t *testing.T) {
			dir := t.TempDir()
			st := store.Store{Dir: dir}
			if err := st.AppendRecord(store.Record{TraceID: "synthetic", SpecID: "synthetic", At: time.Now().Add(-time.Second)}); err != nil {
				t.Fatal(err)
			}
			err := cmdGC([]string{"--store", dir, "--older-than", age, "--apply"})
			rows, readErr := st.Records("synthetic", 0)
			if err == nil || readErr != nil || len(rows) != 1 {
				t.Fatalf("invalid age %s: err=%v rows=%d read=%v", age, err, len(rows), readErr)
			}
		})
	}
}
