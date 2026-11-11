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

// codexCommandIsShikii reports whether the command of a registration is a shikii binary. Three forms pass and nothing else: the bare
// name, the path shikii writes into a registration (mcpCommand), and an absolute path to a file named exactly shikii (another
// install). Every other command is one the person wrote, a wrapper named after shikii included (shikii-wrapper.sh, shikii.sh,
// shikii_with_key, shikiiX: 0.31.0 audit C, A-7). codexServerFixed leaves an existing command of the person's alone in the same way.
func codexCommandIsShikii(command string) bool {
	return command == "shikii" || command == mcpCommand() || (filepath.IsAbs(command) && filepath.Base(command) == "shikii")
}

// codexShikiiBlock reads the head of the one [mcp_servers.shikii] block in config.toml's text as shikii writes it: the command line
// and the args line. end is where the args line ends. ok is false when there is no such block, more than one, or these two lines are
// not in shikii's form.
func codexShikiiBlock(text string, args []string) (command string, end int, ok bool) {
	if strings.Count(text, "[mcp_servers.shikii]") != 1 {
		return "", 0, false
	}
	head := "[mcp_servers.shikii]\ncommand = "
	i := strings.Index(text, head)
	if i < 0 {
		return "", 0, false
	}
	line, after, ok := strings.Cut(text[i+len(head):], "\n")
	if !ok {
		return "", 0, false
	}
	command, err := strconv.Unquote(line)
	if err != nil {
		return "", 0, false
	}
	argsLine, _, ok := strings.Cut(after, "\n")
	if !ok || argsLine != "args = ["+quotedList(args)+"]" {
		return "", 0, false // args a person changed, or an older version's form
	}
	return command, i + len(head) + len(line) + 1 + len(argsLine) + 1, true
}

// codexToolsAllowed is config.toml's text b with codexToolsAllowedLine added to the shikii block; ok is false when the file is left
// as it is. Only the block shikii wrote itself gets the line: exactly its three lines with a shikii binary as the command
// (codexCommandIsShikii), with nothing a person added (their own approval choice and a comment included) and not the line already.
// The edit is read back: Codex must read the same server plus the approval, and every other setting unchanged.
func codexToolsAllowed(b []byte, args []string) ([]byte, bool) {
	text := string(b)
	command, end, ok := codexShikiiBlock(text, args)
	if !ok {
		return nil, false
	}
	if !codexCommandIsShikii(command) {
		return nil, false // a command the person wrote (another program, a wrapper)
	}
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

// codexToolsAlreadyAllowed reports whether b holds shikii's own block with the approval line, as codexToolsAllowed writes it. The
// line right after shikii's args line is taken out, and what is left must be a block codexToolsAllowed adds the line to, giving b
// back: so a block with a command or a line of the person's, or in an older form, does not count, by the same checks.
func codexToolsAlreadyAllowed(b []byte, args []string) bool {
	text := string(b)
	_, end, ok := codexShikiiBlock(text, args)
	if !ok || !strings.HasPrefix(text[end:], codexToolsAllowedLine+"\n") {
		return false
	}
	allowed, ok := codexToolsAllowed([]byte(text[:end]+text[end+len(codexToolsAllowedLine)+1:]), args)
	return ok && string(allowed) == text
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
	mark := codexToolsAllowedMark()
	if !ok {
		// The line is in shikii's own block and the note is not: ~/.shikii was emptied and shikii set up again while ~/.codex stayed,
		// or the person wrote the line themselves. Note it now. Without the note, the block the person later takes the line out of
		// looks like an earlier version's, and the line would come back once (0.31.0 audit C, U-3).
		if mark != "" && !fileExists(mark) && codexToolsAlreadyAllowed(b, codexServerArgs) {
			return noteCodexToolsAllowed(mark)
		}
		return nil
	}
	if mark != "" && fileExists(mark) {
		return nil // shikii added the line before, and the person took it out
	}
	// the note first: without it a later deletion by the person could not be told from an earlier version's block
	if mark != "" {
		if err := noteCodexToolsAllowed(mark); err != nil {
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

// noteCodexToolsAllowed writes the note that the shikii block has had the approval line.
func noteCodexToolsAllowed(mark string) error {
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		return err
	}
	return os.WriteFile(mark, []byte(codexToolsAllowedLine+"\n"), 0o644)
}
