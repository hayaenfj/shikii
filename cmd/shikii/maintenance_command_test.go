package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMaintenanceCommandsPreserveArgumentsAndLogOwnership(t *testing.T) {
	old := startMaintenance
	defer func() { startMaintenance = old }()
	for _, calibration := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "question.yaml")
		store := filepath.Join(dir, "store")
		called := false
		var log *os.File
		startMaintenance = func(cmd *exec.Cmd) error {
			called = true
			want := []string{"tune", path, "--store", store, "--local", hookLocalURL}
			if calibration {
				want = []string{"calibrate", path, "--store", store, "--local", hookLocalURL, "--model", "synthetic-model", "--write"}
			}
			if !reflect.DeepEqual(cmd.Args[1:], want) {
				t.Fatalf("args=%q want %q", cmd.Args[1:], want)
			}
			if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid || cmd.Stdout != cmd.Stderr {
				t.Fatal("detached output ownership changed")
			}
			log = cmd.Stdout.(*os.File)
			return errors.New("synthetic start failure")
		}
		if calibration {
			spawnCalibrate(dir, path, store, "synthetic-model")
		} else {
			spawnTune(dir, path, store)
		}
		if !called {
			t.Fatal("command not prepared")
		}
		if _, err := log.WriteString("must be closed"); !errors.Is(err, os.ErrClosed) {
			t.Fatal("log descriptor leaked", err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "hook.log"))
		if err != nil || !strings.Contains(string(b), "synthetic start failure") {
			t.Fatal("start failure not logged", err)
		}
	}
}
