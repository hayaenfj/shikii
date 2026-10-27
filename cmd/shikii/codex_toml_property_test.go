package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// The oracle below is written here, apart from codex_toml.go, so a mistake there is not repeated by the test (go-toml is called directly).

// oracleParse reads b as Codex does: a leading byte order mark is skipped, the rest must be TOML.
func oracleParse(b []byte) (map[string]any, bool) {
	m := map[string]any{}
	return m, toml.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &m) == nil
}

func oracleEqual(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !oracleEqual(v, y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !oracleEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
	}
	return reflect.DeepEqual(a, b)
}

// oracleSplit is the settings without mcp_servers.shikii (and without an emptied mcp_servers), and the shikii server if any.
func oracleSplit(m map[string]any) (map[string]any, any, bool) {
	rest := map[string]any{}
	for k, v := range m {
		rest[k] = v
	}
	servers, ok := m["mcp_servers"].(map[string]any)
	if !ok {
		return rest, nil, false
	}
	shikii, has := servers["shikii"]
	others := map[string]any{}
	for k, v := range servers {
		if k != "shikii" {
			others[k] = v
		}
	}
	rest["mcp_servers"] = others
	if len(others) == 0 {
		delete(rest, "mcp_servers")
	}
	return rest, shikii, has
}

// oracleKeeps reports whether after is TOML holding exactly want as the shikii server (nil: none) and before's other settings unchanged.
func oracleKeeps(before, after []byte, want map[string]any) bool {
	b, ok1 := oracleParse(before)
	a, ok2 := oracleParse(after)
	if !ok1 || !ok2 {
		return false
	}
	restB, _, _ := oracleSplit(b)
	restA, shikii, has := oracleSplit(a)
	if want == nil && has || want != nil && (!has || !oracleEqual(shikii, map[string]any(want))) {
		return false
	}
	return oracleEqual(restA, restB)
}

func oracleWant(args []string) map[string]any {
	list := make([]any, len(args))
	for i, a := range args {
		list[i] = a
	}
	return map[string]any{"command": mcpCommand(), "args": list}
}

// codexOutcome is what the properties saw shikii do with one config.toml: whether init added the server, what removing it from the result
// did ("cut", "none" or "refused"), what fixing shikii's own form appended to the person's text did ("fixed", "left", or "skipped" when
// that form cannot be appended), and the text the removal returned.
type codexOutcome struct {
	added   bool
	removal string
	fix     string
	removed string
}

// checkCodexConfigProperties checks A1 to A3 on one config.toml text with the oracle above and reports what happened.
func checkCodexConfigProperties(t *testing.T, path, text string) codexOutcome {
	t.Helper()
	var got codexOutcome
	before, parses := oracleParse([]byte(text))
	_, _, hadShikii := oracleSplit(before)
	// addable: appending shikii's table to the text keeps every other setting (checked here, not by codex_toml.go)
	appended := text
	if appended != "" && !strings.HasSuffix(appended, "\n") {
		appended += "\n"
	}
	appended += "\n[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\nargs = [\"mcp\"]\n"
	addable := parses && !hadShikii && oracleKeeps([]byte(text), []byte(appended), oracleWant([]string{"mcp"}))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	// A1: add
	did, err := addCodexServer(path, []string{"mcp"})
	after, _ := os.ReadFile(path)
	switch {
	case err != nil:
		if string(after) != text {
			t.Fatalf("A1: an error changed the file:\n%q\n->\n%q", text, after)
		}
	case did:
		if !addable || !oracleKeeps([]byte(text), after, oracleWant([]string{"mcp"})) {
			t.Fatalf("A1: the added file is not the person's settings plus one shikii:\n%q\n->\n%q", text, after)
		}
	default:
		if string(after) != text {
			t.Fatalf("A1: not added, yet the file changed:\n%q\n->\n%q", text, after)
		}
		if addable {
			t.Fatalf("A1: addable but not added:\n%q", text)
		}
	}
	got.added = did && err == nil
	// A2: remove (from what is there now)
	current, _ := os.ReadFile(path)
	out, found, rerr := withoutCodexServer(string(current))
	parsed, currentParses := oracleParse(current)
	_, _, has := oracleSplit(parsed)
	switch {
	case rerr != nil:
		if out != string(current) {
			t.Fatalf("A2: a refusal changed the text:\n%q", current)
		}
		got.removal = "refused"
	case found:
		if !oracleKeeps(current, []byte(out), nil) {
			t.Fatalf("A2: the removal changed other settings or left shikii:\n%q\n->\n%q", current, out)
		}
		got.removal = "cut"
	default:
		if currentParses && has {
			t.Fatalf("A2: reported missing while Codex reads a shikii server:\n%q", current)
		}
		if out != string(current) {
			t.Fatalf("A2: not found, yet the text changed:\n%q", current)
		}
		got.removal = "none"
	}
	got.removed = out
	// A3: fix the form shikii wrote (command "shikii"), appended after the person's settings
	got.fix = "skipped"
	if !parses || hadShikii {
		return got
	}
	written := text
	if written != "" && !strings.HasSuffix(written, "\n") {
		written += "\n"
	}
	written += "\n[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexServerArgs) + "]\n"
	if _, ok := oracleParse([]byte(written)); !ok {
		return got
	}
	if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	fixed, err := fixCodexServer(path, codexServerArgs)
	fixedText, _ := os.ReadFile(path)
	if err != nil || (fixed && !oracleKeeps([]byte(written), fixedText, oracleWant(codexServerArgs))) || (!fixed && string(fixedText) != written) {
		t.Fatalf("A3: fixing shikii's own form: fixed=%v err=%v\n%q\n->\n%q", fixed, err, written, fixedText)
	}
	got.fix = "left"
	if fixed {
		got.fix = "fixed"
	}
	return got
}

// TestCodexConfigShapes checks properties A1 to A3 of .shikii/handoff/audit-0.9.7/invariants.md (whatever a person wrote, adding, fixing
// or removing shikii leaves TOML Codex can read, with exactly the intended shikii server or none, and every other setting as it was; when
// that cannot be done, the file is not changed) on one fixed config.toml per way of writing it that an audit found to matter, and pins what
// shikii does with each (added or not, cut / none / refused, fixed / left / skipped). A text shikii added to and then removed from comes back
// as it was (only a missing last newline is added). Every case runs on every run, so a broken shape fails on the first run.
// It replaces 3000 random texts: those missed a byte order mark, a \u escape, multi-line values a cut must follow, dotted keys under
// [mcp_servers] and the blank line shikii adds, because a refusal always passed and the generator never wrote the first two.
func TestCodexConfigShapes(t *testing.T) {
	dir := t.TempDir()
	bom := "\xef\xbb\xbf"
	cases := []struct {
		name, text string
		want       codexOutcome // removed is not compared here; the round trip below covers it
	}{
		{"empty file", "", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"plain setting", "model = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"no newline at the end", "model = \"x\"", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"comment without newline at the end", "model = \"x\" # note", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"CRLF line ends", "model = \"x\"\r\n[profiles.p0]\r\nmodel = 'x'\r\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"byte order mark", bom + "model = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"nan value", "model = nan\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"nan in another server", "[mcp_servers.mytool]\ncommand = nan\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"comment naming shikii's table", "model = \"x\" # mcp_servers.shikii\n# [mcp_servers.shikii]\n", codexOutcome{added: true, removal: "cut", fix: "left"}},
		{"string naming shikii's table", "notes = \"mcp_servers.shikii # not a comment\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"multi-line string holding shikii's header", "notes = \"\"\"\n[mcp_servers.shikii]\n\"\"\"\n", codexOutcome{added: true, removal: "cut", fix: "left"}},
		{"array value", "notes = [\"a\", 'b']\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"not TOML", "[broken\n", codexOutcome{removal: "refused", fix: "skipped"}},
		{"another server as a table", "[mcp_servers.mytool]\ncommand = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"another server under [mcp_servers]", "[mcp_servers]\nmytool = { command = \"x\" }\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"another server as dotted keys", "mcp_servers.mytool.command = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"another server inline", "mcp_servers = { mytool = { command = \"x\" } }\n", codexOutcome{removal: "none", fix: "skipped"}},
		{"mcp_servers not a table", "mcp_servers = 1\n", codexOutcome{removal: "none", fix: "skipped"}},
		{"shikii as a table", "[mcp_servers.shikii]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with a basic quoted key", "[mcp_servers.\"shikii\"]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with literal quoted keys", "['mcp_servers'.'shikii']\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with a \\u escape", "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii as dotted keys", "mcp_servers.shikii.command = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii as dotted keys with a multi-line value", "mcp_servers.shikii.args = [\n  \"a\",\n]\nmodel = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii under [mcp_servers]", "[mcp_servers]\nshikii = { command = \"x\" }\nmytool = { command = \"y\" }\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii only by its sub-table", "[mcp_servers.shikii.env]\nX = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with multi-line values", "[mcp_servers.shikii]\ncommand = \"\"\"\nline [mcp_servers.other]\n\"\"\"\nargs = [\n  \"a\",\n]\n[profiles.p0]\nmodel = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii in an inline mcp_servers", "mcp_servers = { shikii = { command = \"x\" } }\n", codexOutcome{removal: "refused", fix: "skipped"}},
		{"shikii after a byte order mark", bom + "[mcp_servers.shikii]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii between other tables", "[profiles.p0]\nmodel = 1\n['mcp_servers'.\"shikii\"] # mcp_servers.shikii\ncommand = 'x'\n[mcp_servers.shikii.env]\nX = nan\n\r\n[mcp_servers.mytool]\ncommand = \"y\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"not TOML after a table", "[mcp_servers.mytool]\ncommand = \"x\"\n[broken\n", codexOutcome{removal: "refused", fix: "skipped"}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkCodexConfigProperties(t, filepath.Join(dir, fmt.Sprintf("config-%d.toml", i)), c.text)
			if got.added != c.want.added || got.removal != c.want.removal || got.fix != c.want.fix {
				t.Fatalf("added=%v removal=%s fix=%s, want added=%v removal=%s fix=%s\n%q", got.added, got.removal, got.fix, c.want.added, c.want.removal, c.want.fix, c.text)
			}
			restored := c.text
			if restored != "" && !strings.HasSuffix(restored, "\n") {
				restored += "\n"
			}
			if got.added && got.removed != restored {
				t.Fatalf("adding and then removing shikii must give the text back:\n%q\n->\n%q", c.text, got.removed)
			}
		})
	}
}
