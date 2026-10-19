package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Save は別のプロセスがその間に足した候補(variants)を消さない: ファイルの候補と自分の候補を名前で併合する。
// 名前は区切り文字や空白を受けない(wiki/runs のファイル名に入る)。
func TestSaveMergesVariantsWrittenByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	src := "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := Load(path) // 古い写し(例: 裏の tune や calibrate --write)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(path) // 画面の口: 人が候補 A を足す
	if err != nil {
		t.Fatal(err)
	}
	fresh.Variants = append(fresh.Variants, Variant{Name: "a-from-page", Question: "Does it ask for something?"})
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	// 古い写しが候補 B を足して保存 → A が消えてはいけない
	stale.Variants = append(stale.Variants, Variant{Name: "b-from-tune", Question: "Is this a request for work?"})
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, v := range got.Variants {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "a-from-page,b-from-tune" {
		t.Fatalf("both candidates must survive, in file order then new ones: %v", names)
	}
	// 同じ名前は自分のが勝つ(採用後の書き換えなど)
	got.Variants[0].Question = "Does it ask for anything?"
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(path)
	if again.Variants[0].Question != "Does it ask for anything?" || len(again.Variants) != 2 {
		t.Fatalf("same name must be replaced, not duplicated: %+v", again.Variants)
	}
	// 名前の検証
	for _, bad := range []string{"", "x/../../outside", "with space", "a\nb", strings.Repeat("n", 61)} {
		again.Variants = []Variant{{Name: bad, Question: "q"}}
		if err := again.Validate(); err == nil {
			t.Fatalf("variant name %q must be refused", bad)
		}
	}
}
