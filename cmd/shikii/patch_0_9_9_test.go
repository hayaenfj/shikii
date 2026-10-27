package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.9: the small items the 0.9.7 audits left (.shikii/handoff/plan-0.9.9/triage.md, C). Each test is checked to fail before its fix.

// C confirm1-5: a key written twice in a person's JSON settings. Go keeps the last one, so a rewrite dropped the person's first text.
// Claude Code reads the last one too, so the file is valid for its agent: shikii leaves it as written, and init goes on with the entry to
// add by hand, as for a read-only file.
func TestPatch099DuplicateKeysAreLeftAsWritten(t *testing.T) {
	for name, text := range map[string]string{
		"top level": `{"mcpServers": {"a": {"command": "x"}}, "mcpServers": {"b": {"command": "y"}}}` + "\n",
		"nested":    `{"mcpServers": {"a": {"command": "x"}, "a": {"command": "y"}}}` + "\n",
	} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Fatalf("%s: a file with a key written twice is left as written, got %q", name, b)
		}
		if !errors.As(err, new(*personFileError)) || !strings.Contains(err.Error(), "twice") {
			t.Fatalf("%s: init is told to add the entry by hand and goes on: %v", name, err)
		}
		withShikii := strings.Replace(text, `"command": "y"`, `"command": "y"}, "shikii": {"command": "shikii"`, 1)
		if err := os.WriteFile(p, []byte(withShikii), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := removeJSONEntry(p, "mcpServers", "shikii", true); err == nil {
			t.Fatalf("%s: uninstall says it left the file", name)
		}
		if b, _ := os.ReadFile(p); string(b) != withShikii {
			t.Fatalf("%s: uninstall leaves it as written, got %q", name, b)
		}
	}
	// control: the same key in two different objects is not a key written twice
	p := filepath.Join(t.TempDir(), ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers": {"a": {"command": "x"}, "b": {"command": "x"}}}`), 0o644)
	if did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"}); err != nil || !did {
		t.Fatalf("control: the same key in two objects is fine: %v %v", did, err)
	}
}

// C confirm1-6: OpenCode reads comments in opencode.json (JSONC). shikii cannot keep them when it rewrites the file, so it leaves the file
// as written, and init goes on with the entry to add by hand instead of stopping as if the settings were broken.
func TestPatch099CommentsInOpencodeJSONLetInitGoOn(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	text := "{\n  // my servers\n  \"mcp\": {\"other\": {\"type\": \"local\", \"command\": [\"x\"]}} /* end */\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "opencode.json"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	out := capture(t, func() error { return cmdInit(nil) })
	if b, _ := os.ReadFile(filepath.Join(repo, "opencode.json")); string(b) != text {
		t.Fatalf("opencode.json with comments is left as written: %q", b)
	}
	if !strings.Contains(out, "opencode.json: shikii did not add itself") || !strings.Contains(out, "comments") || !strings.Contains(out, "Ready:") {
		t.Fatalf("init says why and goes on:\n%s", out)
	}
	// control: a "//" inside a string is not a comment
	p := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(p, []byte(`{"$schema": "https://opencode.ai/config.json", "mcp": {}}`), 0o644)
	if did, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"}); err != nil || !did {
		t.Fatalf("control: a URL in a string is not a comment: %v %v", did, err)
	}
}

// C confirm2-1: an empty settings file (touch .mcp.json) is refused with only "EOF". It is still refused (not JSON), but the message says
// the file is empty.
func TestPatch099EmptySettingsFileSaysItIsEmpty(t *testing.T) {
	for _, text := range []string{"", "  \n\t\n"} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		os.WriteFile(p, []byte(text), 0o644)
		_, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("an empty file is refused and the message says it is empty: %v", err)
		}
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Fatalf("and left as it is: %q", b)
		}
	}
}

// C confirm1-2: records/store-id was made with O_EXCL and then written; a stop in between left an empty file that was never made again
// (read gave "" and O_EXCL failed), so the store kept its path-named entry and a later move was not reconnected.
func TestPatch099EmptyStoreIDIsMadeAgain(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	if err := os.MkdirAll(filepath.Join(root, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "records", "store-id"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id := storeID(root, true)
	if b, _ := os.ReadFile(filepath.Join(root, "records", "store-id")); id == "" || strings.TrimSpace(string(b)) != id {
		t.Fatalf("an empty store-id is made again: id=%q, file=%q", id, b)
	}
	if again := storeID(root, true); again != id {
		t.Fatalf("and then kept: %q, then %q", id, again)
	}
}

// C confirm1-1: on a disk without hard links, init stopped at the built-in decisions with "operation not supported" (seedFile used
// os.Link). It now writes the file in place there.
func TestPatch099InitOnADiskWithoutHardLinks(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { store.Link = os.Link })
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	t.Chdir(repo)
	if err := cmdInit(nil); err != nil {
		t.Fatalf("init works on a disk without hard links: %v", err)
	}
	for _, f := range []string{"decisions/needs-plan.yaml", "records/store-id"} {
		if b, err := os.ReadFile(filepath.Join(repo, ".shikii", f)); err != nil || len(b) == 0 {
			t.Fatalf("%s is written: %v", f, err)
		}
	}
}
