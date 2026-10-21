package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestRecordTextPreservesOriginalAndDecisionBinding(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	input := strings.Repeat("あ", 500)
	rec := Record{TraceID: "0000000000000001", SpecID: "done-as-asked", Inputs: map[string]string{"summary": input}, InputsHash: HashInputs(map[string]string{"summary": input})}
	if _, status := st.RecordText(rec); status["summary"] != "legacy" {
		t.Fatal("以前の保存上限付近の文章には続きが無い可能性を示す")
	}
	full := input + "\nここまでが原文の末尾です"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.SaveRecordText(rec, map[string]string{"summary": full}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	original, status := st.RecordText(rec)
	if original["summary"] != full || len(status) != 0 {
		t.Fatal("原文を末尾まで読み、保存上限の表示を外す")
	}
	if err := st.SaveRecordText(rec, map[string]string{"summary": full + "別の続き"}); err == nil {
		t.Fatal("保存した原文を後から上書きしない")
	}
	path, _ := st.recordTextPath(rec)
	data, _ := os.ReadFile(path)
	var changed recordText
	_ = json.Unmarshal(data, &changed)
	changed.InputsHash = "別の入力"
	broken, _ := json.Marshal(changed)
	_ = os.WriteFile(path, broken, 0o600)
	if original, status := st.RecordText(rec); len(original) != 0 || status["summary"] != "unavailable" {
		t.Fatal("違う判定の原文を表示しない")
	}
	_ = os.WriteFile(path, data, 0o600)
	changed.InputsHash = rec.InputsHash
	changed.Inputs["summary"] = "別の冒頭"
	broken, _ = json.Marshal(changed)
	_ = os.WriteFile(path, broken, 0o600)
	if original, _ := st.RecordText(rec); len(original) != 0 {
		t.Fatal("ハッシュだけを合わせても冒頭が違う文章は使わない")
	}
	_ = os.WriteFile(path, data, 0o600)
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatal("会話の原文は本人だけが読める")
	}
	long := rec
	long.TraceID = "0000000000000002"
	if err := st.SaveRecordText(long, map[string]string{"summary": strings.Repeat("あ", 30000)}); err != nil {
		t.Fatal(err)
	}
	original, status = st.RecordText(long)
	if len(original["summary"]) > RecordTextLimit || !utf8.ValidString(original["summary"]) || status["summary"] != "limited" {
		t.Fatal("長文の保存上限と UTF-8 の境界を守り、省略を示す")
	}
	outside := t.TempDir()
	linkStore := Store{Dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(linkStore.Dir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(linkStore.Dir, "records", "text")); err != nil {
		t.Fatal(err)
	}
	if err := linkStore.SaveRecordText(rec, map[string]string{"summary": full}); err == nil {
		t.Fatal("原文の置き場の symlink をたどらない")
	}
}
