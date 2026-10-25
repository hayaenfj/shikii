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
		// The file size limit applies only to this throwaway child process.
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
		t.Fatalf("confirm the synthetic I/O failure in the child process: %v %s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) != 0 {
		t.Fatalf("do not publish a spec that failed to write under its final name: %v %v", files, err)
	}
	n, err := seedAgentSpecs(dir)
	if err != nil || n != len(hookSpecs)+len(wikiSpecs) {
		t.Fatalf("a retry prepares every ready-made question correctly: n=%d err=%v", n, err)
	}
	for _, id := range hookSpecs {
		mustLoad(t, filepath.Join(dir, id+".yaml"))
	}
}
