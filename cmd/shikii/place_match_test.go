package main

// 0.8.3 の再々監査で 0.9 へ回した件(新-B とテストの穴: 同点の向き、小文字にする)を、突き合わせ(matchPlacedDraft)だけで確かめる。

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func placedRecs(now time.Time, page string, placed ...string) []store.Record {
	recs := []store.Record{}
	for i, p := range placed {
		recs = append(recs, store.Record{TraceID: "t" + string(rune('a'+i)), At: now.Add(time.Duration(i-len(placed)) * time.Minute), Inputs: map[string]string{"new_lesson": p, "lesson": page}})
	}
	return recs
}

// 新-B: 記事の教訓を言い直しただけの草稿 B が、本当に足した草稿 A より上の点になって B に付いていた。合流先の今の教訓に
// 既にある語の並びは、比べる対象から除く。
func TestPlaceMatchPrefersTheDraftThatAddedSomething(t *testing.T) {
	now := time.Now()
	page := "変更履歴は英語と日本語を揃える。"
	restated := "英語と日本語を揃える"              // B: 今の教訓の一部を言い直しただけ(合流した文に全部含まれる)
	added := "見出しは版と題の形にし、未公開の見出しのまま出さない" // A: 足した中身(合流の時に少し書き直される)
	recs := placedRecs(now, page, added, restated)
	d := notebook.Draft{ID: "p1", Title: "変更履歴", Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にし、未公開のまま出さない。"}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != added {
		t.Fatalf("足した草稿 A に付く: got %q", got)
	}
}

// 同点の向き: 被覆が同じなら新しく置いた方に付く。
func TestPlaceMatchTieGoesToTheNewerDraft(t *testing.T) {
	now := time.Now()
	page := "古い教訓"
	older, newer := "コミットの本文に検証を書く", "コミットの件名に対象を書く"
	recs := placedRecs(now, page, older, newer) // newer の方が後
	d := notebook.Draft{ID: "p1", Title: "コミット", Lesson: "コミットの本文に検証を書く。コミットの件名に対象を書く。"}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != newer {
		t.Fatalf("同点は新しい方: got %q", got)
	}
}

// 小文字にする: 英語の大文字と小文字の違いだけで外さない。
func TestPlaceMatchIgnoresLetterCase(t *testing.T) {
	now := time.Now()
	page := "Old lesson"
	placed := "Read The Status Page before changing a spec"
	recs := placedRecs(now, page, placed)
	d := notebook.Draft{ID: "p1", Title: "Status", Lesson: "read the status page before changing a spec, then decide."}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != placed {
		t.Fatalf("大文字小文字の違いで外さない: got %q", got)
	}
}
