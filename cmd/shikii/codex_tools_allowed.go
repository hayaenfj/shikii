package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// codexToolsAllowedLine is the line that lets Codex run the tools of shikii's MCP server without asking the person each time.
// Without it Codex asks before every call (Allow / Allow for this session / Always allow / Cancel), and codex exec, which has
// nobody to ask, refuses the call: "MCP tool call requires approval, but approval policy is never" (Codex 0.167.1, 2026-11-12).
// With it the same calls ran in codex exec. A person who wants to be asked writes "prompt" in its place: a block with a line of
// the person's is theirs, and shikii leaves it alone.
const codexToolsAllowedLine = `default_tools_approval_mode = "approve"`

// codexToolsAllowed is config.toml's text b with codexToolsAllowedLine added to the shikii block; ok is false when the file is left
// as it is. Only the block shikii wrote itself gets the line: exactly its three lines, with nothing a person added (their own
// approval choice included) and not the line already. The edit is read back: Codex must read the same server plus the approval,
// and every other setting unchanged.
func codexToolsAllowed(b []byte, args []string) ([]byte, bool) {
	text := string(b)
	if strings.Count(text, "[mcp_servers.shikii]") != 1 {
		return nil, false
	}
	head := "[mcp_servers.shikii]\ncommand = "
	i := strings.Index(text, head)
	if i < 0 {
		return nil, false
	}
	line, after, ok := strings.Cut(text[i+len(head):], "\n")
	if !ok {
		return nil, false
	}
	command, err := strconv.Unquote(line)
	if err != nil {
		return nil, false
	}
	argsLine, _, ok := strings.Cut(after, "\n")
	if !ok || argsLine != "args = ["+quotedList(args)+"]" {
		return nil, false // args a person changed, or an older version's form
	}
	end := i + len(head) + len(line) + 1 + len(argsLine) + 1
	allowed := text[:end] + codexToolsAllowedLine + "\n" + text[end:]
	want := codexServerWant(command, args)
	want["default_tools_approval_mode"] = "approve"
	// Read back: the server must be exactly shikii's three settings plus the approval. A line or a sub-table of the person's, their
	// own approval choice or the line already there (a key written twice is not TOML) all fail here, and the file is left as it is.
	if !codexEditKeepsTheRest(b, []byte(allowed), want) {
		return nil, false
	}
	return []byte(allowed), true
}

// allowCodexTools adds the line to Codex's config.toml when codexToolsAllowed says so, and tells the person what changed and how
// to be asked again. A file the person made read-only, or that cannot be written, is left as it is.
func allowCodexTools(path string) error {
	shown := filepath.Join("~", ".codex", "config.toml")
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // a config.toml kept elsewhere through a link: write the file it points at, so the link stays a link
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil // no file, or unreadable: nothing to add to
	}
	allowed, ok := codexToolsAllowed(b, codexServerArgs)
	if !ok {
		return nil
	}
	if err := writePersonFile(path, allowed, 0o644); err != nil {
		if errors.Is(err, errPersonFileReadOnly) || errors.As(err, new(*personFileError)) {
			return nil
		}
		return err
	}
	fmt.Fprintf(stdout, "%s: shikii's MCP tools now run without Codex asking each time (added %s to the shikii server; write \"prompt\" in place of \"approve\" to be asked) (Codex; reopen Codex)\n", shown, codexToolsAllowedLine)
	return nil
}
