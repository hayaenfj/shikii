package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/handoff"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	shikii "github.com/hayaenfj/shikii"
)

// cmdInit prepares the agent in one go (there are no options; the base layout and fixed rules decide everything):
//  1. .shikii/ at the project root (where it runs), with decisions/ wiki/ records/ labels/ under it (nothing else is added)
//  2. a shikii MCP server entry in .mcp.json (read by Claude Code). If a shikii entry already exists it is left alone (kept even if a person added --proposer etc.). Other entries and keys are kept
//  3. puts the skill (working rules) back at ~/.claude/skills/shikii (every time. Not a place to edit by hand; edit skills/shikii in the repo)
//
// Running it again gives the same result and never breaks what a person wrote.
func cmdInit(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: shikii init or shikii --init (no extra arguments needed)")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	if isSharedPlaceScope(scope) {
		return errors.New("the home folder is not initialized (~/.shikii is the shared store). Run this in a project folder")
	}
	version := projectInitVersionNow
	if wipePending(root) { // check before creating the store (once created, it cannot be told apart from a re-initialization in the middle of a wipe)
		version = projectInitVersion(root)
	}
	if err := checkProjectStore(root, true); err != nil {
		return err
	}
	if err := excludeProjectStore(scope); err != nil {
		return err
	}
	for _, d := range []string{"specs", "runs"} {
		if err := os.MkdirAll(filepath.Join(root, "wiki", d), 0700); err != nil {
			return err
		}
	}
	if err := (handoff.Book{Store: root}).Ensure(); err != nil { // the work records folder (0.8.1)
		return err
	}
	fmt.Fprintln(stdout, "Store: .shikii/ (decisions/, wiki/, records/, labels/, handoff/)")
	nSpecs, err := seedAgentSpecs(filepath.Join(root, "decisions"))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Built-in decisions: prepared %d kinds (%d hook questions and %d wiki questions; %d new, existing settings kept)\n", len(hookSpecs)+len(wikiSpecs), len(hookSpecs), len(wikiSpecs), nSpecs)
	// Built-in knowledge: copy the type, pattern and model pages that are missing (tune --propose reads these to make candidates. Pages a person edited are left alone)
	n, err := seedWiki(filepath.Join(root, "wiki"))
	if err != nil {
		return fmt.Errorf("wiki: %w", err)
	}
	if n > 0 {
		fmt.Fprintf(stdout, ".shikii/wiki/: added %d built-in knowledge pages (existing pages kept)\n", n)
	}

	// MCP settings: Claude Code (.mcp.json), OpenCode (mcp in opencode.json), Codex (~/.codex/config.toml). Cursor is not supported from 0.9.2
	// (a .cursor/mcp.json written by an earlier version is left as it is: it is the person's file).
	// If a shikii entry already exists it is left alone (kept even if a person added --proposer etc.). Other entries and keys are kept.
	mcpArgs := codexMCPArgs
	claude := map[string]any{"command": "shikii", "args": mcpArgs}
	for _, f := range []struct {
		path, key string
		entry     map[string]any
		who       string
	}{
		{".mcp.json", "mcpServers", claude, "Claude Code"},
		{"opencode.json", "mcp", map[string]any{"type": "local", "command": append([]string{"shikii"}, mcpArgs...)}, "OpenCode"},
	} {
		did, err := addJSONEntry(filepath.Join(scope, f.path), f.key, "shikii", f.entry)
		if err != nil {
			return err
		}
		if did {
			fmt.Fprintf(stdout, "%s: added shikii (%s)\n", f.path, f.who)
		} else {
			fmt.Fprintf(stdout, "%s: kept the existing shikii settings (%s)\n", f.path, f.who)
		}
	}
	// AGENTS.md is the person's file: shikii adds nothing (0.9.2), and removes only the section an earlier version wrote
	var skipped agentsSkipped
	switch did, err := removeAgentsSection(filepath.Join(scope, "AGENTS.md")); {
	case errors.As(err, &skipped):
		fmt.Fprintln(stdout, "AGENTS.md: "+skipped.reason)
	case err != nil:
		return err
	case did:
		fmt.Fprintln(stdout, "AGENTS.md: removed the shikii section an earlier version wrote (the rest of the file is kept)")
	}
	// Things placed in the home folder (Codex settings, the skill). Skipped where there is no HOME (e.g. CI in a container): the project is already prepared
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stdout, "No HOME, so the shared Codex settings and the skill are skipped; only the project side is prepared")
		return completeProjectInit(root, scope, version)
	}
	codex := filepath.Join(home, ".codex", "config.toml")
	switch did, err := addCodexServer(codex, mcpArgs); {
	case err != nil:
		return err
	case did:
		fmt.Fprintf(stdout, "%s: added shikii (Codex)\n", filepath.Join("~", ".codex", "config.toml"))
	default:
		if fixed, err := fixCodexServer(codex, mcpArgs); err != nil {
			return err
		} else if fixed {
			fmt.Fprintf(stdout, "%s: pointed the shikii MCP server shikii wrote at %s (Codex)\n", filepath.Join("~", ".codex", "config.toml"), mcpCommand())
		} else {
			fmt.Fprintf(stdout, "%s: kept the existing MCP settings (Codex)\n", filepath.Join("~", ".codex", "config.toml"))
		}
	}

	// skill: put it back (old files are deleted too), where Claude Code reads it and where Codex and other agents read skills (~/.agents/skills, 0.9.2:
	// without the AGENTS.md section, Codex without hooks found the habits only there)
	for _, rel := range skillDirs {
		if err := copySkill(filepath.Join(home, rel)); err != nil {
			return fmt.Errorf("skill: %w", err)
		}
		fmt.Fprintf(stdout, "skill: updated %s\n", filepath.Join("~", rel))
	}
	return completeProjectInit(root, scope, version)
}

func completeProjectInit(root, scope string, version int) error {
	if err := finishProjectInit(root, scope, version); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Ready: %s\nPage: the hook starts it on the agent's first request (on a free port) and opens it in the browser. Get the URL with shikii page\n", scope)
	fmt.Fprintln(stdout, "Next: make requests in this project as usual. New MCP settings take effect from the next session.")
	fmt.Fprintln(stdout, "To add a business decision, run shikii new <id>. To start the model by hand, run shikii up.")
	return nil
}

// seedWiki copies the embedded built-in knowledge (shikii.WikiFS) into dst (the project's wiki/). Existing files are left alone. Returns how many were copied.
func seedWiki(dst string) (int, error) {
	n := 0
	err := fs.WalkDir(shikii.WikiFS, "wiki", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "wiki/")
		if p == "wiki" {
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := shikii.WikiFS.ReadFile(p)
		if err != nil {
			return err
		}
		created, err := seedFile(target, b)
		if err != nil {
			return err
		}
		if created {
			n++
		}

		return nil
	})
	return n, err
}

// seedFile publishes only fully written content and keeps existing regular files.
func seedFile(path string, data []byte) (bool, error) {
	checkExisting := func() error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("the destination for built-in files cannot be a link")
		}
		return nil
	}
	if err := checkExisting(); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".seed-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0644); err != nil {
		f.Close()
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Link(f.Name(), path); err != nil {
		if os.IsExist(err) {
			return false, checkExisting()
		}
		return false, err
	}
	return true, nil
}

// addJSONEntry adds a name entry under key (an object) in a JSON settings file. If it already exists, leaves it alone and returns false.
// Numbers are not rounded (UseNumber), & < > are not turned into \u, and it writes via a temp file + rename. Broken JSON is not overwritten. Order changes (JSON objects have no order).
func addJSONEntry(path, key, name string, entry map[string]any) (bool, error) {
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("%s: %w (init does not replace a file it cannot read)", path, err)
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			return false, fmt.Errorf("%s: check the JSON (broken settings are not overwritten): %w", path, err)
		}
	}
	section, _ := cfg[key].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	if _, exists := section[name]; exists {
		return false, nil
	}
	section[name] = entry
	cfg[key] = section
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path+".tmp", buf.Bytes(), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(path+".tmp", path)
}

// addCodexServer adds [mcp_servers.shikii] to Codex's ~/.codex/config.toml. It does not parse TOML, so if the word mcp_servers
// appears anywhere (a header, an inline table, a key under [mcp_servers], a quoted key) it leaves the file alone: appending would define it twice and Codex could not read its settings (audit 6 item 1).
func addCodexServer(path string, args []string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(b), "mcp_servers") {
		return false, nil
	}
	// the binary's absolute path, like the hooks: Codex started from a desktop app may not have shikii on its PATH (Codex audit 0.9.2, low 3)
	command := mcpCommand()
	block := "\n[mcp_servers.shikii]\ncommand = " + quotedList([]string{command}) + "\nargs = [" + quotedList(args) + "]\n"
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		block = "\n" + block
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return true, err
}

// codexMCPArgs are the MCP server arguments init writes (Claude Code, OpenCode and Codex).
var codexMCPArgs = []string{"mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}

// mcpCommand is the command written for the MCP server: this binary's own path, like the hooks (os.Executable, not resolved through
// symlinks, so a package manager's link keeps working after an upgrade). The PATH lookup is only a fallback.
func mcpCommand() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	if p, err := exec.LookPath("shikii"); err == nil {
		return p
	}
	return "shikii"
}

// fixCodexServer points Codex's [mcp_servers.shikii] at this binary when the block is still exactly what shikii wrote
// (its three lines, with the same args) and its command is the bare name "shikii" (0.9.1 and earlier) or a path that no longer exists.
// A block a person edited, an existing other binary, or more than one shikii block is left alone (0.9.3).
func fixCodexServer(path string, args []string) (bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	text := string(b)
	if strings.Count(text, "[mcp_servers.shikii]") != 1 {
		return false, nil
	}
	head := "[mcp_servers.shikii]\ncommand = "
	tail := "\nargs = [" + quotedList(args) + "]\n"
	i := strings.Index(text, head)
	rest := text[i+len(head):]
	line, after, ok := strings.Cut(rest, "\n")
	if !ok {
		return false, nil
	}
	cmd, err := strconv.Unquote(line)
	if err != nil || !strings.HasPrefix("\n"+after, tail) {
		return false, nil
	}
	end := i + len(head) + len(line) + len(tail)
	if next := text[end:]; next != "" && !strings.HasPrefix(next, "\n") && !strings.HasPrefix(next, "[") {
		return false, nil // a line a person added to the block
	}
	if cmd != "shikii" {
		if !filepath.IsAbs(cmd) || fileExists(cmd) {
			return false, nil
		}
	}
	command := mcpCommand()
	if command == cmd {
		return false, nil
	}
	fixed := text[:i] + head + quotedList([]string{command}) + tail + text[end:]
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	tmp := path + ".shikii-tmp"
	if err := os.WriteFile(tmp, []byte(fixed), fi.Mode().Perm()); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, fi.Mode().Perm()); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func quotedList(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	return strings.Join(quoted, ", ")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

const agentsMarker = "<!-- shikii -->"

// agentsSkipped is the reason AGENTS.md was not touched (only told to the person; it stops neither init nor the update).
type agentsSkipped struct{ reason string }

func (e agentsSkipped) Error() string { return e.reason }

// removeAgentsSection removes the shikii section an earlier version wrote into AGENTS.md (0.9.2: shikii no longer writes into a person's AGENTS.md;
// the hooks, the MCP instructions and the skill carry the rules). Only the part between the marker-only lines goes; the person's text and the file stay.
//   - A section is a pair that starts with a "marker-only line", has the heading ("## shikii (" or the older "## shikii(") on the next line, and ends at the
//     next "marker-only line". Markers inside a person's text (e.g. inside `…`) do not count.
//   - If AGENTS.md is a link to a file inside the repo (e.g. a symlink to CLAUDE.md), it edits the target and keeps the link; if it points outside, it leaves it alone
//   - With one marker, or three or more marker-only lines, it leaves the file alone and returns the reason (it does not guess what the person meant)
//   - CRLF files stay CRLF; the blank line the earlier version put before the section goes with it. No file is created and none is deleted
func removeAgentsSection(path string) (bool, error) {
	target := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false, agentsSkipped{"AGENTS.md is left alone because its link target cannot be read"}
		}
		dir, _ := filepath.EvalSymlinks(filepath.Dir(path))
		if rel, err := filepath.Rel(dir, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false, agentsSkipped{"AGENTS.md is left alone because it is a link pointing outside the repo"}
		}
		target = real
	}
	b, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	text := string(b)
	start, end, found, ok := agentsSectionSpan(text)
	if !found {
		return false, nil
	}
	if !ok {
		return false, agentsSkipped{"AGENTS.md is left alone because shikii's markers do not form exactly one pair (check the marker-only lines)"}
	}
	before, after := text[:start], text[end:]
	if !strings.HasSuffix(text[start:end], "\n") { // the closing marker had no newline of its own
		after = after[leadingNewline(after):]
	}
	// A person's CRLF file may have had the section added with LF by 0.8, so either line ending counts as the blank line (audit 0.9.2, low 6)
	blankBefore := 0 // length of the newline that ends the blank line just before the section
	if n := trailingNewline(before); n > 0 && trailingNewline(before[:len(before)-n]) > 0 {
		blankBefore = n
	}
	switch {
	case after == "" && blankBefore > 0:
		before = before[:len(before)-blankBefore] // the blank line put before the section at the end of the file
	case blankBefore > 0 && leadingNewline(after) > 0:
		after = after[leadingNewline(after):] // the section sat between two blank lines: keep one
	}
	return true, replaceFile(target, before+after)
}

// trailingNewline and leadingNewline are the length of the line ending ("\r\n" or "\n") at the end or the start of s (0 if none).
func trailingNewline(s string) int {
	switch {
	case strings.HasSuffix(s, "\r\n"):
		return 2
	case strings.HasSuffix(s, "\n"):
		return 1
	}
	return 0
}

func leadingNewline(s string) int {
	switch {
	case strings.HasPrefix(s, "\r\n"):
		return 2
	case strings.HasPrefix(s, "\n"):
		return 1
	}
	return 0
}

// agentsSectionSpan is the range of shikii's section (from the start of the opening marker line to after the newline of the closing marker line). found says whether any marker-only line exists,
// isAgentsHeading says whether a line is the heading of shikii's section: "## shikii (" from 0.9.2, "## shikii(" before (so older sections are still found and replaced).
func isAgentsHeading(line string) bool {
	return strings.HasPrefix(line, "## shikii (") || strings.HasPrefix(line, "## shikii(")
}

// ok says whether there was exactly one pair of an opening (next line is the heading) and a closing marker.
func agentsSectionSpan(text string) (start, end int, found, ok bool) {
	type line struct{ start, end int } // end includes the newline
	var marks []line
	var lines []line
	for pos := 0; pos < len(text); {
		e := strings.IndexByte(text[pos:], '\n')
		next := len(text)
		if e >= 0 {
			next = pos + e + 1
		}
		lines = append(lines, line{pos, next})
		pos = next
	}
	body := func(l line) string { return strings.TrimRight(text[l.start:l.end], "\r\n") }
	pairs := 0
	for i, l := range lines {
		if body(l) != agentsMarker {
			continue
		}
		marks = append(marks, l)
		if i+1 >= len(lines) || !isAgentsHeading(body(lines[i+1])) {
			continue
		}
		for _, c := range lines[i+1:] {
			if body(c) == agentsMarker {
				pairs++
				start, end = l.start, c.end
				break
			}
		}
	}
	if len(marks) == 0 {
		return 0, 0, false, false
	}
	return start, end, true, pairs == 1 && len(marks) == 2
}

// replaceFile rewrites path's content by replacing it from a temp file (keeping the original permissions; the temp file gets a new name each time).
func replaceFile(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm() // it is a repo file, so keep the original permissions
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shikii-agents-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// copySkill puts the embedded skills/shikii back at dst (the old content is deleted so stale references do not mix in).
// skillDirs are where the skill is put, relative to the home folder: Claude Code's, and the shared one Codex and other agents read.
var skillDirs = []string{filepath.Join(".claude", "skills", "shikii"), filepath.Join(".agents", "skills", "shikii")}

// Two shikii --init at once (two projects, one HOME) rewrite the same folder, so the copy holds a lock next to it (audit 0.9.2 English, medium 6:
// without it, a copy failed with "directory not empty" or left the skill without SKILL.md).
func copySkill(dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(dst), ".shikii-skill.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return fs.WalkDir(shikii.SkillFS, "skills/shikii", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("skills/shikii", p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := shikii.SkillFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
