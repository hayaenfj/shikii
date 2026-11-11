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
// as it is. Only the block shikii wrote itself gets the line: exactly its three lines with shikii as the command, with nothing a
// person added (their own approval choice and a comment included) and not the line already. The edit is read back: Codex must read the same server plus the approval,
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
	if command != "shikii" && command != mcpCommand() && !(filepath.IsAbs(command) && strings.HasPrefix(filepath.Base(command), "shikii")) {
		return nil, false // a command the person wrote (another program, a wrapper): not the name or a path of a shikii binary
	}
	end := i + len(head) + len(line) + 1 + len(argsLine) + 1
	if next := text[end:]; next != "" && !strings.HasPrefix(next, "\n") && !strings.HasPrefix(next, "[") {
		return nil, false // a line the person added to the block: a comment is theirs too, and reading the file back does not show it
	}
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

// codexToolsAllowedMark is where shikii notes that it added the line once. A person who then deletes the line has chosen to be
// asked: the block is back to its three lines, like one an earlier version wrote, and only this note tells the two apart.
func codexToolsAllowedMark() string {
	agent, err := agentHome()
	if err != nil {
		return ""
	}
	return filepath.Join(agent, "codex-tools-allowed")
}

// allowCodexTools adds the line to Codex's config.toml when codexToolsAllowed says so, and tells the person what changed and how
// to be asked again. It adds the line once: after that, a block without it is left as the person made it. A file that cannot be
// written is left as it is, and the person is told which line to add by hand.
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
	mark := codexToolsAllowedMark()
	if mark != "" && fileExists(mark) {
		return nil // shikii added the line before, and the person took it out
	}
	// the note first: without it a later deletion by the person could not be told from an earlier version's block
	if mark != "" {
		if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(mark, []byte(codexToolsAllowedLine+"\n"), 0o644); err != nil {
			return err
		}
	}
	if err := writePersonFile(path, allowed, 0o644); err != nil {
		if mark != "" {
			_ = os.Remove(mark) // nothing was added
		}
		if errors.Is(err, errPersonFileReadOnly) || errors.As(err, new(*personFileError)) {
			fmt.Fprintf(stdout, "%s: left as it is (it cannot be written: %v). To let Codex run shikii's MCP tools without asking each time, add %s to the shikii server by hand.\n", shown, err, codexToolsAllowedLine)
			return nil
		}
		return err
	}
	fmt.Fprintf(stdout, "%s: shikii's MCP tools now run without Codex asking each time (added %s to the shikii server; to be asked, write \"prompt\" in place of \"approve\" or delete the line: shikii does not add it again) (Codex; reopen Codex)\n", shown, codexToolsAllowedLine)
	return nil
}
