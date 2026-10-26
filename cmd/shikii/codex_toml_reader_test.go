package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// 0.9.7: Codex's config.toml is read with a TOML parser, not with patterns. Before, every audit found another way of writing the same
// settings that the patterns missed (quotes, \u escapes, dotted keys, comments), and words in a value looked like a shikii server.
// Whatever init does, the file must still be TOML that Codex reads, hold at most one shikii, and keep the person's settings as they were.

func readTOML(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("not TOML Codex can read: %v\n%s", err, b)
	}
	return m
}

// withoutShikiiServer is the parsed settings without mcp_servers.shikii (and without mcp_servers when that was all it held).
func withoutShikiiServer(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	if servers, ok := out["mcp_servers"].(map[string]any); ok {
		rest := map[string]any{}
		for k, v := range servers {
			if k != "shikii" {
				rest[k] = v
			}
		}
		out["mcp_servers"] = rest
		if len(rest) == 0 {
			delete(out, "mcp_servers")
		}
	}
	return out
}

func TestCodexServerIsAddedOnlyWhenATOMLReaderAgrees(t *testing.T) {
	add := map[string]string{
		"words in a value":           "description = \"mcp_servers.shikii is set up by shikii init\"\n",
		"a table name in a string":   "notes = '''\n[mcp_servers.shikii]\n'''\n",
		"another server, odd quotes": "[mcp_servers.'my-tool']\ncommand = 'my-tool' # mcp_servers.shikii later\n",
	}
	for name, text := range add {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || !did {
			t.Fatalf("%s: there is no shikii server, so it is added: did=%v err=%v\n%s", name, did, err, b)
		}
		got := readTOML(t, b)
		server, _ := got["mcp_servers"].(map[string]any)["shikii"].(map[string]any)
		if !reflect.DeepEqual(server["args"], []any{"mcp"}) || server["command"] != mcpCommand() {
			t.Fatalf("%s: shikii's server as written: %v", name, server)
		}
		if !reflect.DeepEqual(withoutShikiiServer(got), readTOML(t, []byte(text))) {
			t.Fatalf("%s: the person's settings stay as they were:\n%s", name, b)
		}
	}
	keep := map[string]string{
		"escaped name":      "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n",
		"dotted keys":       "mcp_servers.shikii.command = \"x\"\n",
		"inline table":      "mcp_servers = { mytool = { command = \"mytool\" } }\n",
		"not TOML":          "[mcp_servers\ncommand = \"x\"\n",
		"key under servers": "[mcp_servers]\n\"shikii\" = { command = \"x\" }\n",
	}
	for name, text := range keep {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || did || string(b) != text {
			t.Fatalf("%s: the file is left as it was: did=%v err=%v\n%s", name, did, err, b)
		}
		if note := codexServerNote(p); !strings.Contains(note, "[mcp_servers.shikii]") {
			t.Fatalf("%s: init shows the block to add by hand: %q", name, note)
		}
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(keep["not TOML"]), 0o644); err != nil {
		t.Fatal(err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "could not be read as TOML") {
		t.Fatalf("a file Codex cannot read is named as such: %q", note)
	}
}

// Uninstall removes shikii however it is written, or refuses; it never reports "not there" while Codex still reads a shikii server,
// and what it leaves is the person's settings exactly.
func TestUninstallRemovesShikiiOnlyWhenATOMLReaderAgrees(t *testing.T) {
	for name, text := range map[string]string{
		"escaped name": "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n\n[other]\na = 1\n",
		"plain":        "model = \"m\"\n\n[mcp_servers.shikii]\ncommand = \"x\"\nargs = [\"mcp\"]\n\n[mcp_servers.mine]\ncommand = \"y\"\n",
		"inline":       "mcp_servers = { shikii = { command = \"x\" }, mine = { command = \"y\" } }\n",
	} {
		before := readTOML(t, []byte(text))
		out, found, err := withoutCodexServer(text)
		if err != nil {
			if out != text {
				t.Fatalf("%s: a refusal changes nothing", name)
			}
			continue
		}
		if !found {
			t.Fatalf("%s: Codex reads a shikii server here, so it must be removed or refused, not reported missing", name)
		}
		if !reflect.DeepEqual(readTOML(t, []byte(out)), withoutShikiiServer(before)) {
			t.Fatalf("%s: only shikii goes:\n%s", name, out)
		}
	}
}

// A shared registration that pins one project is found however the args are quoted.
func TestCodexPinnedProjectFoundInAnyQuoting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	text := "[mcp_servers.shikii]\ncommand = 'shikii'\nargs = ['mcp', '--store', '/work/app/.shikii']\n"
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if !codexServerPinsAProject(p) {
		t.Fatal("args in literal strings still pin a project")
	}
}
