package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// calibrate は choice / score の行でも raw(選択肢ごとの確率の object)付きなら読める。モデルを呼ばずに線を引き、
// 比較ベンチの LoRA の腕(別の道具が出した raw)を A・B と同じ物差しで並べられる。noul の raw は今までどおり数字 1 つ。
func TestCalibrateReadsChoiceRowsWithRawDistributions(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false") // min_n 2
	rows := `[
	 {"inputs":{"message":"fix a"},"raw":{"change_request":0.9,"question":0.08,"other":0.02},"truth":"change_request"},
	 {"inputs":{"message":"fix b"},"raw":{"change_request":0.85,"question":0.1,"other":0.05},"truth":"change_request"},
	 {"inputs":{"message":"why a"},"raw":{"change_request":0.1,"question":0.88,"other":0.02},"truth":"question"},
	 {"inputs":{"message":"why b"},"raw":{"change_request":0.2,"question":0.75,"other":0.05},"truth":"question"}
	]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// 送れる先は無い(--local 無し): raw があるので呼ばずに済む
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--model", "lora-bench", "--store", filepath.Join(dir, ".shikii"), "--write"})
	})
	for _, must := range []string{"n=4", "top-1 accuracy=1.000", "wrote calibration"} {
		if !strings.Contains(out, must) {
			t.Fatalf("calibrate must score the given raw without a model; missing %q:\n%s", must, out)
		}
	}
	sp := mustLoad(t, specPath)
	if c := sp.CalibrationFor("lora-bench"); c == nil || c.N != 4 {
		t.Fatalf("the line is keyed by the given model name: %+v", sp.Calibration)
	}
	// raw の鍵が spec の選択肢に無ければ誤り(黙って 0 にしない)
	bad := strings.Replace(rows, `"other":0.02},"truth":"change_request"`, `"nope":0.02},"truth":"change_request"`, 1)
	if err := os.WriteFile(labeledPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--model", "lora-bench", "--store", filepath.Join(dir, ".shikii2")})
	stdout = old
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a raw key that is not an option must be an error, got %v", err)
	}
}
