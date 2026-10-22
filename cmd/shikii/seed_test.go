package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSeedFailureDoesNotPublishPartialSpec(t *testing.T) {
	if dir := os.Getenv("SHIKII_SEED_FAILURE_DIR"); dir != "" {
		// ファイルサイズの制限はこの使い捨ての子プロセスにだけ適用する。
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 0, Max: 0}); err != nil {
			os.Exit(2)
		}
		if _, err := seedAgentSpecs(dir); err == nil {
			os.Exit(3)
		}
		return
	}
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestSeedFailureDoesNotPublishPartialSpec$")
	cmd.Env = append(os.Environ(), "SHIKII_SEED_FAILURE_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("子プロセスの合成I/O失敗を確認する: %v %s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) != 0 {
		t.Fatalf("書けなかった spec を最終名で公開しない: %v %v", files, err)
	}
	n, err := seedAgentSpecs(dir)
	if err != nil || n != len(hookSpecs)+len(wikiSpecs) {
		t.Fatalf("再試行で既製の質問を全部正常に準備できる: n=%d err=%v", n, err)
	}
	for _, id := range hookSpecs {
		mustLoad(t, filepath.Join(dir, id+".yaml"))
	}
}
