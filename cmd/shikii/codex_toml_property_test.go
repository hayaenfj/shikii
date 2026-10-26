package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// The oracle below is written here, apart from codex_toml.go, so a mistake there is not repeated by the test (go-toml is called directly).

func oracleParse(b []byte) (map[string]any, bool) {
	m := map[string]any{}
	return m, toml.Unmarshal(b, &m) == nil
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

// Properties A1 to A3 of .shikii/handoff/audit-0.9.7/invariants.md, over generated config.toml files rather than one example per finding
// (0.9.7 quality bar): whatever a person wrote, adding, fixing or removing shikii leaves TOML Codex can read, with exactly the intended
// shikii server (or none), and every other setting as it was; when that cannot be done, the file is not changed.

// tomlPiece returns one random piece of a config.toml, sometimes naming mcp_servers or shikii in the many ways TOML allows.
func tomlPiece(r *rand.Rand) string {
	key := []string{"shikii", `"shikii"`, "'shikii'", `"shikii"`, "mytool", `"my-tool"`, "other"}[r.Intn(7)]
	servers := []string{"mcp_servers", `"mcp_servers"`, "'mcp_servers'"}[r.Intn(3)]
	value := []string{`"x"`, `'x'`, "1", "nan", "true", `["a", 'b']`, `"""` + "\nline [mcp_servers.shikii]\n" + `"""`, `"mcp_servers.shikii # not a comment"`}[r.Intn(8)]
	comment := []string{"", " # mcp_servers.shikii", " # note"}[r.Intn(3)]
	switch r.Intn(11) {
	case 0:
		return fmt.Sprintf("model = %s%s\n", value, comment)
	case 1:
		return fmt.Sprintf("[%s.%s]%s\ncommand = %s\n", servers, key, comment, value)
	case 2:
		return fmt.Sprintf("[%s]\n%s = { command = %s }\n", servers, key, value)
	case 3:
		return fmt.Sprintf("%s.%s.command = %s\n", servers, key, value)
	case 4:
		return fmt.Sprintf("%s = { %s = { command = %s } }\n", servers, key, value)
	case 5:
		return fmt.Sprintf("# %s.%s\n", servers, key)
	case 6:
		return fmt.Sprintf("[%s.%s.env]\nX = %s\n", servers, key, value)
	case 7:
		return fmt.Sprintf("[profiles.p%d]\nmodel = %s\n", r.Intn(3), value)
	case 8:
		return "[broken\n" // not TOML
	case 9:
		return "\r\n"
	default:
		return fmt.Sprintf("notes = %s\n", value)
	}
}

func randomCodexConfig(r *rand.Rand) string {
	var b strings.Builder
	for i := r.Intn(5); i >= 0; i-- {
		b.WriteString(tomlPiece(r))
	}
	if r.Intn(4) == 0 {
		return strings.TrimSuffix(b.String(), "\n") // no newline at the end
	}
	return b.String()
}

func TestCodexConfigPropertiesOverGeneratedFiles(t *testing.T) {
	r := rand.New(rand.NewSource(97))
	dir := t.TempDir()
	runs := 3000
	if testing.Short() {
		runs = 300
	}
	for i := 0; i < runs; i++ {
		text := randomCodexConfig(r)
		before, parses := oracleParse([]byte(text))
		_, _, hadShikii := oracleSplit(before)
		// addable: appending shikii's table to the text keeps every other setting (checked here, not by codex_toml.go)
		appended := text
		if appended != "" && !strings.HasSuffix(appended, "\n") {
			appended += "\n"
		}
		appended += "\n[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\nargs = [\"mcp\"]\n"
		addable := parses && !hadShikii && oracleKeeps([]byte(text), []byte(appended), oracleWant([]string{"mcp"}))
		path := filepath.Join(dir, fmt.Sprintf("config-%d.toml", i))
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
		case found:
			if !oracleKeeps(current, []byte(out), nil) {
				t.Fatalf("A2: the removal changed other settings or left shikii:\n%q\n->\n%q", current, out)
			}
		default:
			if currentParses && has {
				t.Fatalf("A2: reported missing while Codex reads a shikii server:\n%q", current)
			}
			if out != string(current) {
				t.Fatalf("A2: not found, yet the text changed:\n%q", current)
			}
		}
		// A3: fix the form shikii wrote, appended after the person's settings
		if parses {
			if !hadShikii {
				written := text
				if written != "" && !strings.HasSuffix(written, "\n") {
					written += "\n"
				}
				written += "\n[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexMCPArgs) + "]\n"
				if _, ok := oracleParse([]byte(written)); ok {
					if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
						t.Fatal(err)
					}
					fixed, err := fixCodexServer(path, codexServerArgs)
					got, _ := os.ReadFile(path)
					if err != nil || (fixed && !oracleKeeps([]byte(written), got, oracleWant(codexServerArgs))) || (!fixed && string(got) != written) {
						t.Fatalf("A3: fixing shikii's own form: fixed=%v err=%v\n%q\n->\n%q", fixed, err, written, got)
					}
				}
			}
		}
	}
}
