package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/hayaenfj/shikii/internal/handoff"

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
	if notice := relinkMovedProject(scope, true); strings.HasPrefix(notice, "shikii:") { // a moved folder (0.9.7)
		fmt.Fprintln(stdout, "Reconnected this project at its new path (its wiki and records are kept)")
	} else if notice != "" { // a copy, or a .shikii from elsewhere, the person makes a project of its own
		fmt.Fprintln(stdout, notice)
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
		var unwritable *personFileError
		if errors.As(err, &unwritable) { // left as it is, like a read-only AGENTS.md; the rest of init goes on (0.9.7 audit A-1, full audit 2-7)
			fmt.Fprintf(stdout, "%s: shikii did not add itself (%v); add the shikii entry by hand to use it from %s\n", f.path, err, f.who)
			continue
		}
		if err != nil {
			return err
		}
		if did {
			fmt.Fprintf(stdout, "%s: added shikii (%s)\n", f.path, f.who)
		} else {
			fmt.Fprintf(stdout, "%s: kept the existing shikii settings (%s)\n", f.path, f.who)
		}
	}
	// AGENTS.md is the person's file: shikii adds nothing (0.9.2) and changes nothing (0.9.7); a section an older version added is only told
	if agentsSectionLeft(filepath.Join(scope, "AGENTS.md")) {
		fmt.Fprintln(stdout, "AGENTS.md: "+agentsSectionNote)
	}
	// Things placed in the home folder (Codex settings, the skill). Skipped where there is no HOME (e.g. CI in a container): the project is already prepared
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stdout, "No HOME, so the shared Codex settings and the skill are skipped; only the project side is prepared")
		return completeProjectInit(root, scope, version)
	}
	codex := filepath.Join(home, ".codex", "config.toml")
	// Codex reads only this shared registration and starts the server in the folder its daemon started in, so it names no store:
	// each tool call passes its project (0.9.3)
	switch did, err := addCodexServer(codex, codexServerArgs); {
	case errors.As(err, new(*personFileError)): // the rest of init goes on (0.9.7 full audit 2-7)
		fmt.Fprintf(stdout, "%s: shikii did not add itself (%v); add:\n[mcp_servers.shikii]\ncommand = %s\n%s (Codex)\n", filepath.Join("~", ".codex", "config.toml"), err, quotedList([]string{mcpCommand()}), codexServerArgsLine)
	case err != nil:
		return err
	case did:
		fmt.Fprintf(stdout, "%s: added shikii (Codex)\n", filepath.Join("~", ".codex", "config.toml"))
	default:
		if fixed, err := fixCodexServer(codex, codexServerArgs); err != nil {
			return err
		} else if fixed {
			fmt.Fprintf(stdout, "%s: pointed the shikii MCP server shikii wrote at %s with %s (Codex)\n", filepath.Join("~", ".codex", "config.toml"), mcpCommand(), codexServerArgsLine)
		} else if codexServerOlder(codex) {
			fmt.Fprintf(stdout, "%s: %s\n", filepath.Join("~", ".codex", "config.toml"), codexOlderNote())
		} else if codexServerPinsAProject(codex) {
			fmt.Fprintf(stdout, "%s: %s\n", filepath.Join("~", ".codex", "config.toml"), codexPinnedNote)
		} else {
			fmt.Fprintf(stdout, "%s: %s\n", filepath.Join("~", ".codex", "config.toml"), codexServerNote(codex))
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
// readJSONSettings reads a person's JSON settings: one JSON object and nothing after it, whose key, when present, is an object. Anything
// else is broken settings, which are never rewritten (a value after the first, or a key holding an array or a string, was dropped by the
// rewrite; 0.9.7 final audit AE-2).
func readJSONSettings(b []byte, key string) (map[string]any, map[string]any, error) {
	cfg := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		return nil, nil, fmt.Errorf("check the JSON (broken settings are not overwritten): %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, errors.New("check the JSON (broken settings are not overwritten): something follows the settings object")
	}
	if cfg == nil { // a bare null decodes into a nil map (confirmation audit 1)
		return nil, nil, errors.New("check the JSON (broken settings are not overwritten): the settings are not an object")
	}
	raw, ok := cfg[key]
	if !ok || raw == nil {
		return cfg, nil, nil
	}
	section, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("check the JSON (broken settings are not overwritten): %q is not an object", key)
	}
	return cfg, section, nil
}

func addJSONEntry(path, key, name string, entry map[string]any) (bool, error) {
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("%s: %w (init does not replace a file it cannot read)", path, err)
	}
	var section map[string]any
	if err == nil {
		if cfg, section, err = readJSONSettings(b, key); err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
	}
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
	return true, writePersonFile(path, buf.Bytes(), 0o644)
}

// codexMCPArgs are the MCP server arguments init writes for Claude Code and OpenCode (per-project files, started in the project),
// and the form 0.9.1 and 0.9.2 wrote for Codex (codexServerOlder).
var codexMCPArgs = []string{"mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}

// codexServerArgs are the arguments of Codex's shared registration (0.9.3): no store, because Codex reads only ~/.codex/config.toml and starts the
// server in the folder its daemon started in; each tool call names its project with project.
// It keeps --local like every other registration and the hook: without it (and without a key) shikii_decide on Codex had no endpoint (audit 0.9.3, second).
var codexServerArgs = []string{"mcp", "--local", hookLocalURL}

// codexServerArgsLine is that registration's args line, as written and as told to the person.
var codexServerArgsLine = "args = [" + quotedList(codexServerArgs) + "]"

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

// fixCodexServer points Codex's [mcp_servers.shikii] at this binary while the block is still exactly the current form shikii writes (its
// three lines) and its command is the bare name or a path that no longer exists. A block a person edited, an older version's form (told by
// codexServerOlder, 0.9.7), the current form on another existing binary, or more than one shikii block is left alone.
func fixCodexServer(path string, args []string) (bool, error) {
	// a config.toml kept elsewhere through a link (dotfiles): write the file it points at, so the link stays a link
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
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
	i := strings.Index(text, head)
	if i < 0 {
		return false, nil
	}
	rest := text[i+len(head):]
	line, after, ok := strings.Cut(rest, "\n")
	if !ok {
		return false, nil
	}
	cmd, err := strconv.Unquote(line)
	if err != nil {
		return false, nil
	}
	argsLine, _, ok := strings.Cut(after, "\n")
	if !ok {
		return false, nil
	}
	if argsLine != "args = ["+quotedList(args)+"]" {
		return false, nil // args a person changed, or an older version's form (codexServerOlder; only told, 0.9.7)
	}
	end := i + len(head) + len(line) + 1 + len(argsLine) + 1
	if next := text[end:]; next != "" && !strings.HasPrefix(next, "\n") && !strings.HasPrefix(next, "[") {
		return false, nil // a line a person added to the block
	}
	switch {
	case cmd == "shikii":
	case !filepath.IsAbs(cmd):
		return false, nil
	case fileExists(cmd):
		return false, nil // the current form on another binary someone chose
	}
	command := mcpCommand()
	if command == cmd {
		return false, nil
	}
	fixed := text[:i] + head + quotedList([]string{command}) + "\nargs = [" + quotedList(args) + "]\n" + text[end:]
	if !codexEditKeepsTheRest(b, []byte(fixed), map[string]any{"command": command, "args": stringsAsTOML(args)}) {
		return false, nil // the text edit would change something else Codex reads: leave it
	}
	if err := writePersonFile(path, []byte(fixed), 0o644); err != nil {
		if errors.Is(err, errPersonFileReadOnly) {
			return false, nil // the person made it read-only: do not replace it
		}
		return false, err
	}
	return true, nil
}

// codexPinnedNote tells a person whose hand-edited Codex block names one project's store what that does and what to write instead.
var codexPinnedNote = "the shikii MCP server was edited by hand, so it was left as it is. It names one project's store (--store with an absolute path), so every Codex session uses that one project. To let each session use its own project, set " + codexServerArgsLine + ": the agent then passes its working folder as project on each call (Codex)"

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

// agentsSectionLeft reports whether AGENTS.md still has the section shikii 0.9.1 and earlier added: a marker-only line followed by the
// heading "## shikii (" or "## shikii(". shikii no longer writes or edits AGENTS.md (0.9.7): init and the update only tell the person.
func agentsSectionLeft(path string) bool {
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for i := 0; i+1 < len(lines); i++ {
		if lines[i] == agentsMarker && (strings.HasPrefix(lines[i+1], "## shikii (") || strings.HasPrefix(lines[i+1], "## shikii(")) {
			return true
		}
	}
	return false
}

// agentsSectionNote tells the person how to remove that section.
const agentsSectionNote = "an older shikii added a section between two " + agentsMarker + " lines; shikii no longer reads or changes AGENTS.md. Remove that part by hand, from the " + agentsMarker + " line above \"## shikii\" to the next " + agentsMarker + " line (the rest of the file is yours)"

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
