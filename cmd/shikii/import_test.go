package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 冷たい出発を温める: 手元の過去の表(CSV: 入力の列 + truth [+ weight])を、calibrate / tune が読む正解の貯金(JSON)にする。
// 列名は spec の入力名と一致していること。truth は spec の選択肢(noul は yes/no/true/false)。
func TestImportTurnsACSVIntoTruths(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	csvPath := filepath.Join(dir, "past.csv")
	src := "message,truth,weight\nfix the typo,change_request,\n\"why, though?\",question,0.5\nok thanks,other,\n"
	if err := os.WriteFile(csvPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "truths.json")
	got := capture(t, func() error { return cmdImport([]string{csvPath, "--spec", specPath, "--out", out}) })
	if !strings.Contains(got, "3 truths") {
		t.Fatalf("import must say how many rows it wrote:\n%s", got)
	}
	var rows []labeled
	b, _ := os.ReadFile(out)
	if err := json.Unmarshal(b, &rows); err != nil || len(rows) != 3 {
		t.Fatalf("the output is the labeled JSON calibrate reads: %v %s", err, b)
	}
	if rows[1].Inputs["message"] != "why, though?" || string(rows[1].Truth) != `"question"` || rows[1].Weight != 0.5 || rows[0].Weight != 0 {
		t.Fatalf("rows: %+v", rows)
	}
	// 知らない選択肢・足りない列は行番号付きで誤り(黙って捨てない)
	bad := "message,truth\nfix,nope\n"
	if err := os.WriteFile(csvPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdImport([]string{csvPath, "--spec", specPath, "--out", out}); err == nil || !strings.Contains(err.Error(), "row 1") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown truth must fail with the row number: %v", err)
	}
	if err := os.WriteFile(csvPath, []byte("text,truth\nfix,change_request\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdImport([]string{csvPath, "--spec", specPath, "--out", out}); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("a missing required input column must fail naming the column: %v", err)
	}
}
