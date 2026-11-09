package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFeedbackReadMatchesPreviousWindowRules(t *testing.T) {
	for _, prefix := range []int{0, feedbackReadMax + 17} {
		s := Store{Dir: t.TempDir()}
		scope := "/synthetic/audit"
		path := s.feedbackPath(scope)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		var text strings.Builder
		if prefix > 0 {
			text.WriteString(strings.Repeat("x", prefix))
			text.WriteByte('\n')
		}
		for i := range 600 {
			if i%23 == 0 {
				text.WriteString("invalid synthetic JSON\n")
			}
			raw, _ := json.Marshal(Feedback{ID: fmt.Sprintf("%016x", i%17+1), Request: "Synthetic request", Summary: "Synthetic summary", Reply: fmt.Sprintf("Synthetic reply %d", i), At: time.Unix(int64(600-i), 0), Targets: []ReviewTarget{{Spec: "owner", TraceID: "synthetic"}}})
			text.Write(raw)
			text.WriteByte('\n')
		}
		text.WriteString(`{"id":"unfinished"`)
		if e := os.WriteFile(path, []byte(text.String()), 0600); e != nil {
			t.Fatal(e)
		}
		got, e := s.RecentFeedback(scope)
		want, we := s.referenceRecentFeedback(scope)
		if fmt.Sprint(e) != fmt.Sprint(we) || !reflect.DeepEqual(got, want) {
			t.Fatal("feedback tail/order/deduplication changed", prefix, e, we)
		}
		view, e := s.ReadFeedback(scope)
		if e != nil {
			t.Fatal(e)
		}
		fresh, e := view.CurrentHash(make([]byte, 32<<10))
		if e != nil || view.Hash() != fresh {
			t.Fatal("captured tail hash differs", e)
		}
		items := view.Items()
		items[0].Targets[0].TraceID = "caller mutation"
		if view.Items()[0].Targets[0].TraceID != "synthetic" {
			t.Fatal("caller changed shared feedback")
		}
	}
}
func (s Store) referenceRecentFeedback(scope string) ([]Feedback, error) {
	f, err := s.openFile(s.feedbackPath(scope), os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(int64(0), info.Size()-feedbackReadMax)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReader(io.LimitReader(f, feedbackReadMax))
	if start > 0 {
		if _, err := r.ReadString('\n'); err != nil {
			return nil, nil
		}
	}
	var recent []Feedback
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break // use only observations written up to the newline
		}
		var v Feedback
		if len(line) > 32<<10 || json.Unmarshal([]byte(line), &v) != nil || v.ID == "" || v.Request == "" || v.Summary == "" || v.Reply == "" ||
			len(v.Request) > feedbackTextMax || len(v.Summary) > feedbackTextMax || len(v.Reply) > feedbackTextMax || len(v.Targets) > 2 {
			continue
		}
		recent = append(recent, v)
		if len(recent) > feedbackRecent {
			copy(recent, recent[1:])
			recent = recent[:feedbackRecent]
		}
	}
	seen := map[string]bool{}
	out := make([]Feedback, 0, len(recent))
	for i := len(recent) - 1; i >= 0; i-- {
		v := recent[i]
		if !seen[v.ID] {
			seen[v.ID] = true
			out = append(out, v)
		}
	}
	return out, nil
}

func TestFeedbackReadRevalidatesWindowFraming(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/synthetic/framing"
	path := s.feedbackPath(scope)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	first, _ := json.Marshal(Feedback{ID: "0000000000000001", Request: "First original", Summary: "First summary", Reply: "First reply"})
	last, _ := json.Marshal(Feedback{ID: "0000000000000002", Request: "Last original", Summary: "Last summary", Reply: "Last reply"})
	body := string(first) + "\n" + strings.Repeat("x", feedbackReadMax-len(first)-len(last)-3) + "\n" + string(last) + "\n"
	if len(body) != feedbackReadMax {
		t.Fatal("fixture length")
	}
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	view, e := s.ReadFeedback(scope)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte("synthetic prefix\n"+body), 0600); e != nil {
		t.Fatal(e)
	}
	current, e := view.CurrentHash(make([]byte, 32<<10))
	if e != nil {
		t.Fatal(e)
	}
	fresh, e := s.RecentFeedback(scope)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("before_rows=%d after_rows=%d hash_changed=%v", len(view.Items()), len(fresh), current != view.Hash())
	if reflect.DeepEqual(view.Items(), fresh) {
		t.Fatal("fixture did not change effective history")
	}
	if current == view.Hash() {
		t.Fatal("tail framing changed effective history without changing validation hash")
	}
}
