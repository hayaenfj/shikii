package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Finds similar requests in Japanese and English. Follow-up requests stay verbatim too, with no right/wrong attached.
// Not handed to a different project with the same name; duplicates of the same request keep only the newest; at most 2.
func TestFeedbackRecallUsesObservedConversations(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const scope = "/work/alpha/project"
	request := "README のインストール手順を初めての人にも分かる形に更新して"
	put := func(id, req, reply string) {
		t.Helper()
		if err := s.AppendFeedback(scope, Feedback{ID: id, Request: req, Summary: "説明を更新しました", Reply: reply}); err != nil {
			t.Fatal(err)
		}
	}
	put("0000000000000001", request, "まだ終わっていない。モデルの起動手順が無い")
	put("0000000000000001", request, "その後の訂正: モデルの起動手順も必要")
	put("0000000000000002", request, "追加で英訳もお願いします")
	put("0000000000000003", "database index migration", "the index is missing")
	items, err := s.RecallFeedback(scope, request)
	if err != nil || len(items) != 2 || items[0].ID != "0000000000000002" || !strings.Contains(items[1].Reply, "その後の訂正") {
		t.Fatalf("returns only the new observations of the close request: %+v err=%v", items, err)
	}
	if items, err := (Store{Dir: t.TempDir()}).RecallFeedback("/work/beta/project", request); err != nil || len(items) != 0 {
		t.Fatalf("same name but different work (each project has its own store): %+v err=%v", items, err)
	}
	if items, err := s.RecallFeedback(scope, "network timeout retry policy"); err != nil || len(items) != 0 {
		t.Fatalf("returns nothing for an unrelated request: %+v err=%v", items, err)
	}
	items, err = s.RecallFeedback(scope, "database migration index")
	if err != nil || len(items) != 1 || items[0].ID != "0000000000000003" {
		t.Fatalf("finds English too regardless of word order: %+v err=%v", items, err)
	}
	if ids, err := s.SpecIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("does not mix conversations into decisions or truths: %v %v", ids, err)
	}
	b, err := os.ReadFile(s.feedbackPath(scope))
	if err != nil || strings.Contains(string(b), `"truth"`) || strings.Contains(string(b), `"source":"manual"`) {
		t.Fatalf("keeps only the conversation's original text: err=%v", err)
	}
	if info, err := os.Stat(s.feedbackPath(scope)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the conversation file is readable only by its owner: info=%v err=%v", info, err)
	}
}

// Reads only the last 4 MB and 256 observations. Does not quote broken or unfinished lines.
func TestFeedbackBoundsHistoryAndRejectsIncompleteData(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const scope = "/work/project"
	path := s.feedbackPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// even if reading starts in the middle of a huge line, the normal lines after it can be read.
	if err := os.WriteFile(path, []byte(strings.Repeat("x", feedbackReadMax+1)+"\n{broken json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 260; i++ {
		if err := s.AppendFeedback(scope, Feedback{ID: fmt.Sprintf("%016x", i), Request: "README install instructions", Summary: "updated", Reply: fmt.Sprintf("feedback %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// a last line without a newline may still be being written.
	b, _ := json.Marshal(Feedback{ID: "partial", Request: "README install instructions", Summary: "updated", Reply: "not written completely"})
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(b)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.recentFeedback(scope)
	if err != nil || len(recent) != 256 || recent[0].Reply != "feedback 259" || recent[255].Reply != "feedback 4" {
		t.Fatalf("uses only the tail of the history: len=%d err=%v", len(recent), err)
	}
	for _, v := range []Feedback{
		{ID: "empty"},
		{ID: "large", Request: strings.Repeat("a", 1501), Summary: "done", Reply: "reply"},
	} {
		if err := s.AppendFeedback(scope, v); err == nil {
			t.Fatal("missing input and overly large text are not stored")
		}
	}
}
