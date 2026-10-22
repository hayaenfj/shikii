package notebook

import "testing"

func TestSearchKeepsCommandsAndRanksApplicableFields(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var target string
	for i, title := range []string{"Orbit display", "Orbit history", "Orbit update"} {
		d := draft(source, title)
		d.Lesson, d.AppliesTo = "Orbit command reference", "Orbit maintenance"
		body := "Use orbit update to refresh installed packages."
		d.Body = &body
		if i == 2 {
			d.AppliesTo = "orbit update installation"
		}
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			target = r.Page.ID
		}
	}
	for _, q := range []string{"orbit update", "ORBIT UPDATE"} {
		v, err := b.Query(q)
		if err != nil || len(v.Lessons) == 0 || v.Lessons[0].ID != target {
			t.Fatalf("コマンドを保持し題と適用条件を本文だけの一致より優先する: query=%q result=%+v err=%v", q, v.Lessons, err)
		}
	}
	for _, q := range []string{"update", "installed packages"} {
		v, err := b.Query(q)
		if err != nil || len(v.Lessons) == 0 {
			t.Fatalf("本文とコマンド名を検索できる: %q %v", q, err)
		}
	}
	if v, err := b.Query("orbit unrelated"); err != nil || len(v.Lessons) != 0 {
		t.Fatal("一般的な製品名だけの一致を返さない")
	}
	if p, err := b.Recall("installed packages"); err != nil || len(p) != 0 {
		t.Fatal("自動配送は本文だけの一致を使わない")
	}
}
