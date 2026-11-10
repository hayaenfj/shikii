package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStrictJSONEntryErrorsRemainDistinct(t *testing.T) {
	for _, text := range []string{`{"operation":"query","unknown":true}`, `{"operation":"query"} {}`, `{"operation":"query"} broken`} {
		r, re := decodeReflect(strings.NewReader(text))
		v, ve := decodeReview(strings.NewReader(text))
		if re == nil || ve == nil || r.Operation != "query" || v.Operation != "query" {
			t.Fatal("partial requests or strict rejection changed", text, re, ve)
		}
		if strings.Contains(text, "unknown") {
			if !strings.Contains(re.Error(), `unknown field "unknown"`) || ve.Error() != "check the scoring JSON" {
				t.Fatal(re, ve)
			}
		} else if re.Error() != "pass reflection JSON one object at a time" || ve.Error() != "pass scoring JSON one object at a time" {
			t.Fatal(re, ve)
		}
	}
	if _, err := decodeReview(strings.NewReader(strings.Repeat(" ", 64<<10+1))); err == nil || err.Error() != "scoring input is JSON of at most 64 KB" {
		t.Fatal(err)
	}
	if _, err := decodeReflect(io.MultiReader(strings.NewReader(`{"operation":"query"}`), failingJSONReader{})); err == nil || !strings.Contains(err.Error(), "at most 1 MiB") {
		t.Fatal(err)
	}
}

type failingJSONReader struct{}

func (failingJSONReader) Read([]byte) (int, error) { return 0, errors.New("synthetic read failure") }
