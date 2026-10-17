package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rec(specHash, model string) Record {
	return Record{
		TraceID: NewTraceID(), SpecID: "s", SpecHash: specHash, Type: "noul", Model: model,
		InputsHash: HashInputs(map[string]string{"a": "x"}),
		Raw:        map[string]float64{"yes": 0.8, "no": 0.2}, Top: "yes", P: 0.8, Band: "ask", At: time.Now(),
	}
}

// InputsByHash は本文が残っている記録から、入力のハッシュ → 本文 の対応を作る(聞き直しの材料)。
func TestInputsByHashOnlyFromRecordsThatKeptText(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	withText := rec("h1", "m")
	withText.Inputs = map[string]string{"a": "x"}
	withoutText := rec("h1", "m")
	withoutText.InputsHash = HashInputs(map[string]string{"a": "y"})
	for _, r := range []Record{withText, withoutText} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.InputsByHash("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[withText.InputsHash].Inputs["a"] != "x" {
		t.Fatalf("want only the record that kept its text, got %v", got)
	}
	// ファイルから較正した時のラベルは記録を持たない。ラベル側に本文があればそれも材料になる
	fromFile := Label{TraceID: NewTraceID(), SpecID: "s", SpecHash: "h1", Type: "noul", Model: "m",
		InputsHash: HashInputs(map[string]string{"a": "z"}), Inputs: map[string]string{"a": "z"},
		Raw: map[string]float64{"yes": 0.7, "no": 0.3}, Truth: "yes", Weight: 1, Source: "file", At: time.Now()}
	if err := st.AppendLabel(fromFile); err != nil {
		t.Fatal(err)
	}
	got, _ = st.InputsByHash("s")
	if len(got) != 2 || got[fromFile.InputsHash].Inputs["a"] != "z" {
		t.Fatalf("labels that carry text must count too, got %v", got)
	}
}

// Records は新しい順に limit 件。LabeledTraces はラベルの付いた trace の集合(待ち行列の「済み」判定に使う)。
func TestRecentRecordsAndLabeledTraces(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	var recs []Record
	for i := 0; i < 3; i++ {
		r := rec("h1", "m")
		r.At = time.Now().Add(time.Duration(i) * time.Second)
		recs = append(recs, r)
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Records("s", 2)
	if err != nil || len(got) != 2 || got[0].TraceID != recs[2].TraceID || got[1].TraceID != recs[1].TraceID {
		t.Fatalf("want the 2 newest first, got %v %v", got, err)
	}
	l, _ := LabelFor(&recs[1], "yes", 1, "ui")
	if err := st.AppendLabel(l); err != nil {
		t.Fatal(err)
	}
	labeled, err := st.LabeledTraces("s")
	if err != nil || len(labeled) != 1 || labeled[recs[1].TraceID].Truth != "yes" {
		t.Fatalf("labeled traces: %v %v", labeled, err)
	}
	if none, err := st.Records("no-such", 5); err != nil || len(none) != 0 {
		t.Fatalf("missing spec must be empty, got %v %v", none, err)
	}
}

// 監査 S2: 書く側に上限が無く読む側に 1 MiB の上限があり、巨大な 1 行で store 全体が読めなくなった。
// 書く側で弾き、読む側は余裕を持つ。
func TestOversizedRecordIsRejectedNotWritten(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	r := rec("h1", "m")
	r.Inputs = map[string]string{"a": strings.Repeat("x", MaxLineBytes)}
	if err := st.AppendRecord(r); err == nil {
		t.Fatal("a record larger than MaxLineBytes must be rejected")
	}
	ok := rec("h1", "m")
	ok.Inputs = map[string]string{"a": strings.Repeat("x", 1<<20+100)} // 1 MiB を少し超える(読める)
	if err := st.AppendRecord(ok); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Records("s", 0); err != nil || len(got) != 1 {
		t.Fatalf("a record just above 1 MiB must still be readable: %d %v", len(got), err)
	}
}

// 監査 S9: 本文を含む store は持ち主だけが読める(0600 / 0700)。
func TestStoreFilesAreOwnerOnly(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "s")}
	if err := st.AppendRecord(rec("h1", "m")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(st.recordsPath("s"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("record file must be 0600, got %o", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Join(st.Dir, "records"))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("store dirs must be 0700, got %o", di.Mode().Perm())
	}
}

func TestHashInputsIsOrderIndependent(t *testing.T) {
	a := HashInputs(map[string]string{"x": "1", "y": "2"})
	b := HashInputs(map[string]string{"y": "2", "x": "1"})
	c := HashInputs(map[string]string{"x": "1", "y": "3"})
	if a != b || a == c {
		t.Fatalf("hash: a=%s b=%s c=%s", a, b, c)
	}
}

func TestRecordRoundTripAndLookup(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	r1, r2 := rec("h1", "m"), rec("h1", "m")
	for _, r := range []Record{r1, r2} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.FindRecord("", r2.TraceID)
	if err != nil || got.TraceID != r2.TraceID || got.Raw["yes"] != 0.8 {
		t.Fatalf("find: %v %+v", err, got)
	}
	if _, err := st.FindRecord("s", "nope"); err == nil {
		t.Fatal("unknown trace must be an error")
	}
}

func TestLabelForValidatesTruthByType(t *testing.T) {
	r := rec("h1", "m")
	if _, err := LabelFor(&r, "maybe", 1, "ask"); err == nil {
		t.Fatal("noul truth other than yes/no must be rejected")
	}
	l, err := LabelFor(&r, "no", 0, "")
	if err != nil || l.Truth != "no" || l.Weight != 1 || l.Source != "manual" || l.SpecHash != "h1" || l.Model != "m" {
		t.Fatalf("label: %v %+v", err, l)
	}

	ch := r
	ch.Type = "choice"
	ch.Raw = map[string]float64{"change_request": 1, "question": 0}
	if _, err := LabelFor(&ch, "operation", 1, "ask"); err == nil {
		t.Fatal("choice truth must be one of the recorded options")
	}
	if _, err := LabelFor(&ch, "question", 1, "ask"); err != nil {
		t.Fatal(err)
	}
	// 部分ラベル: 知っている選択肢の否定だけ受ける
	if l, err := LabelFor(&ch, "!change_request", 0.7, "outcome"); err != nil || l.Truth != "!change_request" || l.Weight != 0.7 {
		t.Fatalf("partial label must be accepted: %v %+v", err, l)
	}
	if _, err := LabelFor(&ch, "!operation", 1, "outcome"); err == nil {
		t.Fatal("partial label of an unknown option must be rejected")
	}
	if _, err := LabelFor(&ch, "!", 1, "outcome"); err == nil {
		t.Fatal("bare '!' must be rejected")
	}
	if !Correct("question", "!change_request") || Correct("change_request", "!change_request") || !Correct("a", "a") || Correct("a", "b") {
		t.Fatal("Correct must treat !x as 'anything but x'")
	}

	sc := r
	sc.Type = "score"
	sc.Raw = map[string]float64{"0": 0.5, "1": 0.5}
	if _, err := LabelFor(&sc, "2", 1, "ask"); err == nil {
		t.Fatal("score truth must be a recorded level index")
	}
}

// 較正に使うラベルは (spec_hash, model) が一致するものだけ。
func TestLabelsFilterByHashAndModel(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	add := func(hash, model, truth string) {
		r := rec(hash, model)
		l, err := LabelFor(&r, truth, 1, "ask")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	add("h1", "jev-1.13.0", "yes")
	add("h1", "jev-1.13.0", "no")
	add("stale", "jev-1.13.0", "yes") // 質問文を変える前のラベル
	add("h1", "jev-1.14.0", "yes")    // 別版のラベル

	got, err := st.Labels("s", "h1", "jev-1.13.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want exactly the 2 labels for (h1, jev-1.13.0), got %d: %+v", len(got), got)
	}
	none, err := st.Labels("s", "h9", "jev-1.13.0")
	if err != nil || len(none) != 0 {
		t.Fatalf("no labels for unknown hash, got %v %v", none, err)
	}
	// 無いファイルはエラーではなく空
	if l, err := st.Labels("other-spec", "h1", "m"); err != nil || len(l) != 0 {
		t.Fatalf("missing label file must be empty, got %v %v", l, err)
	}
}

// GC は「古くてラベルの無い記録」だけを消す。ラベル付きは年齢に関係なく残り、ラベル自体は触らない。
// 読めない行は消さない(読めないものを捨てない)。既定は表示だけで、apply の時だけ書き換える。
func TestGCRemovesOnlyOldUnlabeledRecords(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	now := time.Now()
	oldUnlabeled, oldLabeled, fresh := rec("h1", "m"), rec("h1", "m"), rec("h1", "m")
	oldUnlabeled.At = now.Add(-40 * 24 * time.Hour)
	oldLabeled.At = now.Add(-40 * 24 * time.Hour)
	fresh.At = now.Add(-1 * time.Hour)
	for _, r := range []Record{oldUnlabeled, oldLabeled, fresh} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	l, err := LabelFor(&oldLabeled, "yes", 1, "ask")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(l); err != nil {
		t.Fatal(err)
	}
	// 壊れた行を 1 つ混ぜる
	f, err := os.OpenFile(st.recordsPath("s"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cutoff := now.Add(-30 * 24 * time.Hour)
	dry, err := st.GC("s", cutoff, false)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Removed != 1 || dry.KeptLabeled != 1 || dry.KeptRecent != 1 || dry.KeptUnparsed != 1 {
		t.Fatalf("dry run counts: %+v", dry)
	}
	if n, _, _ := st.Counts("s"); n != 4 {
		t.Fatalf("dry run must not touch the file, records=%d", n)
	}

	got, err := st.GC("s", cutoff, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 1 {
		t.Fatalf("apply counts: %+v", got)
	}
	if n, labs, _ := st.Counts("s"); n != 3 || len(labs) != 1 {
		t.Fatalf("after apply: records=%d labels=%d (want 3 and 1)", n, len(labs))
	}
	if _, err := st.FindRecord("s", oldUnlabeled.TraceID); err == nil {
		t.Fatal("old unlabeled record must be gone")
	}
	for _, keep := range []Record{oldLabeled, fresh} {
		if _, err := st.FindRecord("s", keep.TraceID); err != nil {
			t.Fatalf("record %s must survive: %v", keep.TraceID, err)
		}
	}
	b, _ := os.ReadFile(st.recordsPath("s"))
	if !strings.Contains(string(b), "{not json") {
		t.Fatal("unparsable line must be kept, not silently dropped")
	}
	// 2 回目は何も消さない(冪等)
	again, err := st.GC("s", cutoff, true)
	if err != nil || again.Removed != 0 {
		t.Fatalf("second gc must remove nothing: %+v %v", again, err)
	}
	// 無い spec はエラーではなく 0
	none, err := st.GC("no-such-spec", cutoff, true)
	if err != nil || none.Removed != 0 {
		t.Fatalf("missing spec must be a no-op: %+v %v", none, err)
	}
}

func TestAppendLabelRequiresKeys(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if err := st.AppendLabel(Label{TraceID: "t", SpecID: "s", SpecHash: "h"}); err == nil {
		t.Fatal("label without model must be rejected; it could not be keyed for calibration")
	}
}
