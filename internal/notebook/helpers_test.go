package notebook

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func partsCapture(t *testing.T, b Book, title, body string, at int64) Observation {
	t.Helper()
	o, err := b.Capture(Observation{Kind: "agent", Title: title, Content: body, Reference: "memory/" + title + ".md", Version: "v", ObservedAt: time.Unix(at, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func auditForge(t *testing.T, cursor string, edit func(*continuation)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var c continuation
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	edit(&c)
	data, _ = json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}
