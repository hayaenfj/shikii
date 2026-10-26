package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Codex's ~/.codex/config.toml is read with a TOML parser (0.9.7). Before, shikii read it with patterns, and every audit found another way
// of writing the same settings that they missed (quotes, \u escapes, dotted keys, comments, words in a value), so init could write a second
// shikii server and Codex would refuse the whole file. Now every change shikii makes is checked by reading the result back: it must be
// TOML, hold the shikii server shikii meant (or none, for uninstall), and keep everything else Codex reads exactly as it was.
// The text itself is still edited by appending or cutting lines, so the person's comments and layout stay.

func readCodexConfig(b []byte) (map[string]any, error) {
	m := map[string]any{}
	if err := toml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// codexShikiiServer is mcp_servers.shikii, however it was written.
func codexShikiiServer(m map[string]any) (any, bool) {
	servers, ok := m["mcp_servers"].(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := servers["shikii"]
	return v, ok
}

// withoutCodexShikii is the settings without mcp_servers.shikii, and without mcp_servers when that was all it held.
func withoutCodexShikii(m map[string]any) map[string]any {
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

func stringsAsTOML(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// codexEditKeepsTheRest reports whether after is TOML whose shikii server is want (nil: no shikii server) and whose other settings equal before's.
func codexEditKeepsTheRest(before, after []byte, want map[string]any) bool {
	b, err := readCodexConfig(before)
	if err != nil {
		return false
	}
	a, err := readCodexConfig(after)
	if err != nil {
		return false
	}
	got, has := codexShikiiServer(a)
	if want == nil && has || want != nil && (!has || !reflect.DeepEqual(got, want)) {
		return false
	}
	return reflect.DeepEqual(withoutCodexShikii(a), withoutCodexShikii(b))
}

func codexServerWant(command string, args []string) map[string]any {
	return map[string]any{"command": command, "args": stringsAsTOML(args)}
}

// codexWithServer is the file with shikii's server appended as its own table.
func codexWithServer(b []byte, command string, args []string) []byte {
	block := "\n[mcp_servers.shikii]\ncommand = " + quotedList([]string{command}) + "\nargs = [" + quotedList(args) + "]\n"
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		block = "\n" + block
	}
	return append(append([]byte{}, b...), block...)
}

// codexServerState says what init can do with Codex's config.toml: "add" (no shikii server, and appending one keeps every other setting),
// "has" (a shikii server is defined, in any form), "inline" (mcp_servers cannot be extended by a table, e.g. mcp_servers = { … }) or
// "unreadable" (not TOML; Codex cannot read it either). Only "add" changes the file; otherwise init shows the block to add by hand.
func codexServerState(b []byte, command string, args []string) string {
	m, err := readCodexConfig(b)
	if err != nil {
		return "unreadable"
	}
	if _, ok := codexShikiiServer(m); ok {
		return "has"
	}
	if !codexEditKeepsTheRest(b, codexWithServer(b, command, args), codexServerWant(command, args)) {
		if _, ok := m["mcp_servers"]; ok {
			return "inline"
		}
		return "unreadable"
	}
	return "add"
}

// addCodexServer adds [mcp_servers.shikii] to Codex's ~/.codex/config.toml when codexServerState says "add".
func addCodexServer(path string, args []string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	// the binary's absolute path, like the hooks: Codex started from a desktop app may not have shikii on its PATH (Codex audit 0.9.2, low 3)
	command := mcpCommand()
	if codexServerState(b, command, args) != "add" {
		return false, nil
	}
	return true, writePersonFile(path, codexWithServer(b, command, args), 0o644)
}

// codexServerPinsAProject reports whether the shikii server in Codex's config names a store by its absolute path
// (--store /... or --store=/...). Codex shares that one registration among all sessions, so every session then uses that one project.
func codexServerPinsAProject(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	m, err := readCodexConfig(b)
	if err != nil {
		return false
	}
	server, _ := codexShikiiServer(m)
	table, _ := server.(map[string]any)
	list, _ := table["args"].([]any)
	var args []string
	for _, a := range list {
		if s, ok := a.(string); ok {
			args = append(args, s)
		}
	}
	for k, a := range args {
		if (a == "--store" || a == "-store") && k+1 < len(args) && filepath.IsAbs(args[k+1]) {
			return true
		}
		if v, ok := strings.CutPrefix(strings.TrimLeft(a, "-"), "store="); ok && strings.HasPrefix(a, "-") && filepath.IsAbs(v) {
			return true
		}
	}
	return false
}

// codexServerNote is what init says about Codex's config.toml when it did not add shikii there.
func codexServerNote(path string) string {
	b, _ := os.ReadFile(path)
	block := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + " (Codex)"
	switch codexServerState(b, mcpCommand(), codexServerArgs) {
	case "inline":
		return "did not add shikii: mcp_servers is written as an inline table (mcp_servers = { ... }), which cannot be extended. Add shikii to it, or rewrite it as tables and add:\n" + block
	case "unreadable":
		_, err := readCodexConfig(b)
		return fmt.Sprintf("did not add shikii: the file could not be read as TOML (%v), so it was left as it is. Fix it, then add:\n%s", err, block)
	}
	return "kept the existing MCP settings; if they do not register shikii, add:\n" + block
}

// withoutCodexServer removes shikii's server from config.toml text (cutCodexServer cuts the lines) and checks the cut by reading it back:
// no shikii server is left and every other setting is unchanged. A file that is not TOML, or a shikii server that cannot be cut out line by
// line (an inline mcp_servers = { shikii = … }), is an error and nothing is changed. found is false only when Codex reads no shikii server.
func withoutCodexServer(text string) (string, bool, error) {
	before, err := readCodexConfig([]byte(text))
	if err != nil {
		return text, false, fmt.Errorf("could not be read as TOML (%v), so it was left as it is; remove [mcp_servers.shikii] by hand", err)
	}
	if _, ok := codexShikiiServer(before); !ok {
		return text, false, nil
	}
	out, _, err := cutCodexServer(text)
	if err != nil {
		return text, false, err
	}
	if !codexEditKeepsTheRest([]byte(text), []byte(out), nil) {
		return text, false, errors.New("holds shikii's server in a form that cannot be cut out line by line without changing other settings (such as an inline mcp_servers = { … } table); remove shikii from it by hand")
	}
	return out, true, nil
}
