package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 日本語と英語の似た依頼を探す。追加依頼も原文のままで、正誤を付けない。
// 同名の別プロジェクトには渡さず、同じ依頼の重複は最新だけ、上限は 2 件。
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
		t.Fatalf("近い依頼の新しい観察だけを返す: %+v err=%v", items, err)
	}
	if items, err := s.RecallFeedback("/work/beta/project", request); err != nil || len(items) != 0 {
		t.Fatalf("同名でも別の仕事: %+v err=%v", items, err)
	}
	if items, err := s.RecallFeedback(scope, "network timeout retry policy"); err != nil || len(items) != 0 {
		t.Fatalf("無関係な依頼には返さない: %+v err=%v", items, err)
	}
	items, err = s.RecallFeedback(scope, "database migration index")
	if err != nil || len(items) != 1 || items[0].ID != "0000000000000003" {
		t.Fatalf("英語でも語の順序に依らず探す: %+v err=%v", items, err)
	}
	if ids, err := s.SpecIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("会話を判定や正解に混ぜない: %v %v", ids, err)
	}
	b, err := os.ReadFile(s.feedbackPath(scope))
	if err != nil || strings.Contains(string(b), `"truth"`) || strings.Contains(string(b), `"source":"manual"`) {
		t.Fatalf("会話の原文だけを保管する: err=%v", err)
	}
	if info, err := os.Stat(s.feedbackPath(scope)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("会話のファイルは本人だけが読める: info=%v err=%v", info, err)
	}
}

// 読むのは末尾 4 MB・256 観察だけ。壊れた行や未完の行を引用しない。
func TestFeedbackBoundsHistoryAndRejectsIncompleteData(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const scope = "/work/project"
	path := s.feedbackPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// 読み始めが巨大な行の途中に来ても、その後の正常な行を読める。
	if err := os.WriteFile(path, []byte(strings.Repeat("x", feedbackReadMax+1)+"\n{broken json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 260; i++ {
		if err := s.AppendFeedback(scope, Feedback{ID: fmt.Sprintf("%016x", i), Request: "README install instructions", Summary: "updated", Reply: fmt.Sprintf("feedback %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// 改行の無い最終行はまだ書き込み中かもしれない。
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
		t.Fatalf("履歴の末尾だけを使う: len=%d err=%v", len(recent), err)
	}
	for _, v := range []Feedback{
		{ID: "empty"},
		{ID: "large", Request: strings.Repeat("a", 1501), Summary: "done", Reply: "reply"},
	} {
		if err := s.AppendFeedback(scope, v); err == nil {
			t.Fatal("入力の欠落と大きすぎる本文は保管しない")
		}
	}
}
