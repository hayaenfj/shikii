package main

import (
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"reflect"
	"syscall"
	"testing"
)

// TestMain removes the developer's keys (TYPESAFE_API_KEY / DECISIONS_API_KEY) and SHIKII_MODEL before the tests.
// Otherwise tests that assume the local fake model send requests to the real api.typesafe.ai,
// and the store's records, calibrations and bands change and the tests fail (2026-10-23: 6 failed right after the key was put in an environment variable).
// Tests that try Jev (hook_hosted_test.go) call t.Setenv themselves.
func TestMain(m *testing.M) {
	for _, k := range []string{"TYPESAFE_API_KEY", "DECISIONS_API_KEY", "SHIKII_MODEL"} {
		os.Unsetenv(k)
	}
	// No test starts real background processes (up, serve) or a browser (a test that skipped the harness forgot a stub, and a real one was left running. 0.9.1)
	hookSpawn = recordSpawn
	openBrowser = func(url string) { opened = append(opened, url) }
	stopProcess = func(int) error { return nil } // uninstall never signals a real process from tests (0.9.3)
	processCommand = func(int) string { return "" }
	// Writing files out to the disk (on macOS a flush of the disk's cache) made the eight test groups wait on each other: a test that
	// takes 0.3 s alone took 1 to 2 s. Tests do not need the data to survive a power cut.
	store.SyncTemp = func(*os.File) error { return nil }
	syncPersonFile = func(*os.File) error { return nil }
	os.Exit(m.Run())
}

// Tests never start real background processes (up, serve) and never open a browser (0.9.1). Even a test that runs the hook without the harness (hookEnv)
// started a real `shikii.test up` on a machine with llama-server, which was never cleaned up (2026-10-25: 75 of them kept calling git,
// and each git call ran xcodebuild -license check, pushing the machine's load above 150)
func TestTestsNeverStartRealProcesses(t *testing.T) {
	if reflect.ValueOf(hookSpawn).Pointer() == reflect.ValueOf(spawnShikii).Pointer() {
		t.Fatal("during tests, hookSpawn must be a recorder, not the real spawnShikii")
	}
	if reflect.ValueOf(openBrowser).Pointer() == reflect.ValueOf(openInBrowser).Pointer() {
		t.Fatal("during tests, openBrowser must not open a real browser")
	}
}

// The binary's own guard (0.9.1): from a go test executable (*.test), even with a forgotten stub, never start real background processes or open a browser
func TestSpawnRefusesFromATestBinary(t *testing.T) {
	if pid := spawnShikii(t.TempDir(), "up.log", "up"); pid != 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL) // anything started without the guard is killed with its whole group before it multiplies
		t.Fatalf("a test executable started a real up: pid %d", pid)
	}
	if !runningAsTestBinary() {
		t.Fatal("cannot recognize the go test executable")
	}
}
