package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookEnvCopyUsable has two conditions, and a copy is made only where neither holds. Each condition names a place where a real hook
// install --yes leaves something other than what the template holds, so the test makes that place, runs prepareHookEnv with the copy
// allowed, and checks two things: the real command ran (copied is false), and the real command left what the copy cannot have.
// Take either condition out and the copy is made there, which is the first thing checked.

// copyConditionHome makes the folders a prepareHookEnv run needs, with the template made before the test changes the environment (the
// template is made by a real install in folders of its own, with this process's environment as it is when the first test asks).
func copyConditionHome(t *testing.T) (home, root string, tmpl *hookEnvTemplate) {
	t.Helper()
	base := t.TempDir()
	home, root = filepath.Join(base, "home"), filepath.Join(base, "root")
	for _, dir := range []string{home, root} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	hookEnvGlobals(t, nil) // the model port is closed, nothing real is started
	tmpl = hookEnvTemplateOf(t)
	return home, root, tmpl
}

func hasTemplatePath(entries []treeEntry, rel string) bool {
	for _, e := range entries {
		if e.rel == filepath.FromSlash(rel) || strings.HasPrefix(e.rel, filepath.FromSlash(rel)+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// With neither condition, the copy is used: the check that the two tests below fail for their condition and not for something else.
func TestPrepareHookEnvCopiesWhereNothingDiffers(t *testing.T) {
	home, root, _ := copyConditionHome(t)
	t.Setenv("XDG_CONFIG_HOME", "relative/dir") // a relative value is not followed, so it is the same as none
	if _, copied := prepareHookEnv(t, home, root, true); !copied {
		t.Fatal("the copy was not used where neither condition holds: the two tests below prove nothing")
	}
}

// An absolute XDG_CONFIG_HOME sends OpenCode's plugin outside the home: the real install writes it under $XDG_CONFIG_HOME/opencode,
// and the template (made without one) holds it under the home's .config. A copy would put the plugin in the wrong folder and leave the
// folder the real run writes to empty.
func TestPrepareHookEnvRunsTheRealInstallWhereXDGConfigHomeIsAbsolute(t *testing.T) {
	home, root, tmpl := copyConditionHome(t)
	if !hasTemplatePath(tmpl.home, filepath.Join(".config", "opencode", "plugins")) {
		t.Fatal("the template holds no OpenCode plugin under the home's .config: the test proves nothing")
	}
	xdg := filepath.Join(filepath.Dir(home), "xdg")
	if err := os.Mkdir(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if _, copied := prepareHookEnv(t, home, root, true); copied {
		t.Fatal("the template was copied although XDG_CONFIG_HOME is an absolute path: the plugin would go to the home's .config")
	}
	if _, err := os.Stat(filepath.Join(xdg, "opencode", "plugins")); err != nil {
		t.Fatalf("the real install did not write the plugin under XDG_CONFIG_HOME: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "opencode")); !os.IsNotExist(err) {
		t.Fatalf("the home's .config holds a plugin although XDG_CONFIG_HOME names another folder: %v", err)
	}
}

// Where llama-server is on PATH and no model answers, the real install starts the model and writes its guard file (starting-model).
// The template, made where llama-server is not found, has none, so a copy would leave the home without the file a real install leaves.
func TestPrepareHookEnvRunsTheRealInstallWhereLlamaServerIsFound(t *testing.T) {
	home, root, tmpl := copyConditionHome(t)
	if hasTemplatePath(tmpl.home, filepath.Join(".shikii", "agent", "starting-model")) {
		t.Fatal("the template holds the guard file of a started model: the test proves nothing")
	}
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil } // as if on a machine with llama-server
	if _, copied := prepareHookEnv(t, home, root, true); copied {
		t.Fatal("the template was copied although llama-server is on PATH: the home would lack the guard file of the started model")
	}
	if _, err := os.Stat(filepath.Join(home, ".shikii", "agent", "starting-model")); err != nil {
		t.Fatalf("the real install did not write the guard file of the model it started: %v", err)
	}
}
