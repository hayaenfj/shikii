package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 監査 5 の 8: serve の起動は --local があればモデルの口を待ち、無ければ(hosted だけ)待たない。待ちの失敗は起動の失敗。
func TestServeWaitsForTheLocalModelOnlyWhenThereIsOne(t *testing.T) {
	var asked []string
	fake := func(ctx context.Context, url string, every time.Duration, logf func(string, ...any)) error {
		asked = append(asked, url)
		if dl, ok := ctx.Deadline(); !ok || time.Until(dl) < 14*time.Minute {
			t.Fatalf("the wait must have a deadline of about 15 minutes")
		}
		if url == "http://127.0.0.1:1" {
			return errors.New("not ready")
		}
		return nil
	}
	if err := waitForLocalModel("", fake); err != nil || len(asked) != 0 {
		t.Fatalf("hosted only: nothing to wait for, got %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:8080", fake); err != nil || len(asked) != 1 || asked[0] != "http://127.0.0.1:8080" {
		t.Fatalf("a local model is waited for at its URL: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:1", fake); err == nil {
		t.Fatalf("a model that never answers must fail the start")
	}
}
