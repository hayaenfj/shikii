package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// shikii uninstall (0.9.3) takes out what shikii put into the agents' settings and stops its pages, and keeps everything else:
//   - shikii's hooks in ~/.claude/settings.json and ~/.codex/hooks.json (other hooks and the files stay)
//   - [mcp_servers.shikii] in ~/.codex/config.toml (only that table and its lines; the file and every other line stay)
//   - the shikii entry in each registered project's .mcp.json (mcpServers) and opencode.json (mcp)
//   - the skill folders ~/.claude/skills/shikii and ~/.agents/skills/shikii (only when they are shikii's skill, not a link)
//   - the running pages (shikii serve) the hooks started, found through their marks in ~/.shikii/agent/pages/
//
// It never touches the data (each project's .shikii/ and ~/.shikii/agent), AGENTS.md / CLAUDE.md, or the binaries: removing a running
// binary does not work the same way on every machine, so it prints the rm command with the real paths instead.

var (
	stopProcess    = signalTerm    // tests swap in a recorder: a test never sends a real signal
	processCommand = psCommand     // the command line of a pid ("" if unknown). Tests swap in a table
	executablePath = os.Executable // the running binary, not resolved through links (the path the person typed or installed)
)

func signalTerm(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func psCommand(pid int) string {
	out, err := exec.Command("/bin/ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// uninstallStep is one thing to remove: what it is (shown in the list) and how to remove it.
type uninstallStep struct {
	what  string
	apply func() error
}

const uninstallUsage = "usage: shikii uninstall [--yes]\n  Without --yes it lists what it would remove and changes nothing. With --yes it removes shikii's hooks, MCP entries and skill and stops its pages.\n  The data (each project's .shikii/ and ~/.shikii/agent) and the binaries stay\n"

func cmdUninstall(args []string) error {
	yes := false
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		case "--help", "-h", "help":
			fmt.Fprint(stdout, uninstallUsage)
			return nil
		default:
			return fmt.Errorf("uninstall: unknown argument %q (only --yes)", a)
		}
	}
	agent, err := agentHome()
	if err != nil {
		return err
	}
	home := filepath.Dir(filepath.Dir(agent))
	tilde := func(p string) string {
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, home)
		}
		return p
	}
	projects := registeredProjects(agent)
	var steps []uninstallStep
	var left []string   // what is left alone and why (not an error: it is not shikii's, or the person chose it)
	var failed []string // shikii's parts that could not be checked or removed: they are still there, so the run is not a success

	// 1) hooks: the same editing as hook uninstall (only handlers whose command is `… shikii hook <event>`)
	for _, t := range hookTargets {
		path, evs, who := filepath.Join(home, t.rel), hookEventsFor(t.who), t.who
		switch changed, err := editHooksTo(path, false, evs, false); {
		case err != nil:
			failed = append(failed, fmt.Sprintf("%s: %v", tilde(path), err))
		case changed:
			steps = append(steps, uninstallStep{fmt.Sprintf("%s: shikii's hooks (%s; other hooks and the file stay)", tilde(path), who), func() error {
				_, err := editHooks(path, false, evs)
				return err
			}})
		}
	}
	// 2) Codex's MCP server
	codex := filepath.Join(home, ".codex", "config.toml")
	switch found, err := removeCodexServer(codex, false); {
	case err != nil:
		failed = append(failed, fmt.Sprintf("%s: %v", tilde(codex), err)) // shikii's own table stays: Codex keeps starting shikii (audit 0.9.3 uninstall)
	case found:
		steps = append(steps, uninstallStep{fmt.Sprintf("%s: the [mcp_servers.shikii] table (Codex; every other line and the file stay)", tilde(codex)), func() error {
			_, err := removeCodexServer(codex, true)
			return err
		}})
	}
	// 3) each registered project's MCP entries (the same files init writes), also in a registered project whose .shikii/ the person removed:
	// init wrote the entries there too (audit 0.9.3 uninstall)
	for _, p := range mcpProjects(agent, projects) {
		for _, f := range []struct{ file, key, who string }{{".mcp.json", "mcpServers", "Claude Code"}, {"opencode.json", "mcp", "OpenCode"}} {
			path, key, file := filepath.Join(p.Path, f.file), f.key, f.file
			switch found, err := removeJSONEntry(path, key, "shikii", false); {
			case err != nil:
				failed = append(failed, fmt.Sprintf("%s: %v", path, err))
			case found:
				steps = append(steps, uninstallStep{fmt.Sprintf("%s: the shikii entry in %s (%s; other entries and the file stay)", path, key, f.who), func() error {
					_, err := removeJSONEntry(path, key, "shikii", true)
					if err != nil {
						return fmt.Errorf("%s: %w", file, err)
					}
					return nil
				}})
			}
		}
	}
	// 4) the skill folders
	for _, rel := range skillDirs {
		dir := filepath.Join(home, rel)
		switch present, err := shikiiSkillDir(dir); {
		case err != nil:
			left = append(left, fmt.Sprintf("%s %v", tilde(dir), err))
		case present:
			steps = append(steps, uninstallStep{fmt.Sprintf("%s: shikii's skill folder", tilde(dir)), func() error {
				if present, err := shikiiSkillDir(dir); err != nil || !present { // check again right before removing
					return err
				}
				if err := os.RemoveAll(dir); err != nil {
					return err
				}
				return nil // init's lock file beside the folder stays: removing it raced with a holder (confirmation audit of 0.9.9)
			}})
		}
	}
	// 5) the running pages the hooks started, with their records in ~/.shikii/agent/pages/ (shikii's, not data; 0.9.9). The lock files stay:
	// removing one raced with a page or init opening it, and an empty lock file is harmless (confirmation audit of 0.9.9)
	for _, pg := range runningPages(agent) {
		pg := pg
		steps = append(steps, uninstallStep{fmt.Sprintf("stop the page of %s (shikii serve, pid %d, port %d)", pg.Scope, pg.PID, pg.Port), func() error {
			if err := stopProcess(pg.PID); err != nil {
				return err
			}
			if r, ok := readPageRecord(agent, pg.Scope); ok && r.PID == pg.PID {
				_ = os.Remove(pagePath(agent, pg.Scope))
			}
			return nil
		}})
	}

	fmt.Fprintln(stdout, "shikii uninstall: removes shikii's hooks, MCP entries and skill, and stops its pages. The data and the binaries stay.")
	if len(steps) == 0 {
		fmt.Fprintln(stdout, "nothing of shikii's is left to remove")
	}
	for i, s := range steps {
		if yes {
			if err := s.apply(); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", s.what, err))
				fmt.Fprintf(stdout, "%d. not done: %s (%v)\n", i+1, s.what, err)
				continue
			}
			fmt.Fprintf(stdout, "%d. done: %s\n", i+1, s.what)
		} else {
			fmt.Fprintf(stdout, "%d. %s\n", i+1, s.what)
		}
	}
	for _, l := range left {
		fmt.Fprintf(stdout, "left alone: %s\n", l)
	}
	for _, f := range failed {
		fmt.Fprintf(stdout, "could not check or change: %s\n", f)
	}

	fmt.Fprintln(stdout, "Kept (never touched): the data. Running `shikii hook install --yes` again (and `shikii init` in a project) continues from it:")
	for _, p := range projects {
		fmt.Fprintf(stdout, "  - %s%c\n", filepath.Join(p.Path, ".shikii"), filepath.Separator)
	}
	fmt.Fprintf(stdout, "  - %s%c\n", agent, filepath.Separator)
	fmt.Fprintln(stdout, "AGENTS.md and CLAUDE.md are not touched. The local model (llama-server) is not stopped: other tools may use it.")
	if len(failed) > 0 {
		fmt.Fprintln(stdout, "Some of shikii is still there (\"could not check or change\" above). Remove it by hand first: once the binaries are gone, an agent that still finds shikii's hooks or MCP entry fails to start it.")
	}
	if exe, err := executablePath(); err == nil {
		// run through a link (~/.local/bin/shikii -> the real binary), rm of the link alone leaves the binaries: name what it points at too
		paths := []string{exe}
		if info, err := os.Lstat(exe); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				paths = append(paths, real)
			}
		}
		for _, p := range append([]string{}, paths...) {
			paths = append(paths, filepath.Join(filepath.Dir(p), "shikii-readout"))
		}
		var bins []string
		var seen []os.FileInfo
	next:
		for _, p := range paths {
			info, err := os.Lstat(p)
			if err != nil {
				continue
			}
			for _, s := range seen {
				if os.SameFile(s, info) {
					continue next // the same file reached through a linked folder (/tmp and /private/tmp) is named once
				}
			}
			seen = append(seen, info)
			bins = append(bins, shellQuote(p))
		}
		fmt.Fprintf(stdout, "The binaries are not removed (removing a running binary does not work the same way everywhere). To remove them:\n  rm %s\n", strings.Join(bins, " "))
	}
	if !yes {
		fmt.Fprintln(stdout, "(shown only; pass --yes to do it)")
		return nil
	}
	fmt.Fprintln(stdout, "Next: reopen any open Claude Code and Codex sessions (and their MCP) so they stop using shikii.")
	if len(failed) > 0 {
		return errors.New("uninstall: some parts could not be removed (see above)")
	}
	return nil
}

// shellQuote quotes a path for the shell with single quotes when it needs them (a path with spaces stays one argument).
// shellPlain is the characters a shell reads as themselves: shellQuote leaves a word of only these bare.
const shellPlain = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./-+@:,%"

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, shellPlain) == "" {
		return s // nothing the shell reads specially
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shikiiSkillDir reports whether dir is shikii's skill folder: a real folder (not a link) whose SKILL.md says name: shikii.
// A missing folder is (false, nil). A link, or a folder that is not shikii's skill, is an error and is never removed.
func shikiiSkillDir(dir string) (bool, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(dir)
		return false, fmt.Errorf("is a link (to %s), not the folder shikii put there; remove it by hand if it is shikii's", target)
	}
	if !info.IsDir() {
		return false, errors.New("is not a folder")
	}
	f, err := os.OpenFile(filepath.Join(dir, "SKILL.md"), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, errors.New("is not shikii's skill (no SKILL.md)")
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	head, ok := strings.CutPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), "---\n")
	if ok {
		if end := strings.Index(head, "\n---"); end >= 0 {
			for _, line := range strings.Split(head[:end], "\n") {
				if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "name" && strings.Trim(strings.TrimSpace(v), `"'`) == "shikii" {
					return true, nil
				}
			}
		}
	}
	return false, errors.New("is not shikii's skill (its SKILL.md does not say name: shikii)")
}

// runningPages are the pages (shikii serve) the hooks started that are still running: a mark in ~/.shikii/agent/pages/ whose pid is alive and
// runs `shikii serve --store <that project>/.shikii`. A pid now used by something else (or by another project's serve) is not listed.
func runningPages(agent string) []pageRecord {
	var out []pageRecord
	files, _ := filepath.Glob(filepath.Join(agent, "pages", "*.json"))
	for _, path := range files {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		b, err := os.ReadFile(path)
		var r pageRecord
		if err != nil || json.Unmarshal(b, &r) != nil || r.PID <= 0 || r.PID == os.Getpid() || !filepath.IsAbs(r.Scope) || filepath.Base(path) != projectID(r.Scope)+".json" {
			continue
		}
		if !processAlive(r.PID) || !isShikiiServe(processCommand(r.PID), filepath.Join(r.Scope, ".shikii")) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// isShikiiServe reports whether a command line is `<…/>shikii serve --store <root> …`, the way ensureProjectPage starts the page.
func isShikiiServe(command, root string) bool {
	marker := " serve --store " + root
	i := strings.Index(command, marker)
	if i <= 0 {
		return false
	}
	if rest := command[i+len(marker):]; rest != "" && !strings.HasPrefix(rest, " ") {
		return false
	}
	base := filepath.Base(command[:i])
	return base == "shikii" || strings.HasPrefix(base, "shikii.") // shikii, or a build such as shikii.exe
}

// removeJSONEntry removes the name entry under key (an object) in a JSON settings file, the reverse of addJSONEntry, written the same way
// (numbers kept, & < > not escaped, 2-space indent, temp file + rename). Other entries, other keys and the file stay (an emptied key stays as {}).
// A missing file or entry is (false, nil); broken JSON is not overwritten. With write false it only reports whether there is one.
// A file kept elsewhere through a link is written where it points, and its permissions stay.
func removeJSONEntry(path, key, name string, write bool) (bool, error) {
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
	cfg, section, err := readJSONSettings(b, key)
	if jc := (*jsoncSettings)(nil); errors.As(err, &jc) && jc.section[name] == nil {
		return false, nil // a file with comments and no shikii entry: nothing of shikii's to remove (confirmation audit of 0.9.9)
	}
	if err != nil {
		return false, err
	}
	if _, ok := section[name]; !ok {
		return false, nil
	}
	if !write {
		return true, nil
	}
	delete(section, name)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return false, err
	}
	return true, writePersonFile(path, buf.Bytes(), 0o600)
}

// removeCodexServer removes [mcp_servers.shikii] from Codex's config.toml (withoutCodexServer). A config.toml kept elsewhere through a link
// is written where it points (the link stays a link, like fixCodexServer), and its permissions stay. A read-only file is refused.
// With write false it only reports whether there is one.
func removeCodexServer(path string, write bool) (bool, error) {
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
	out, found, err := withoutCodexServer(string(b))
	if err != nil || !found {
		return false, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if fi.Mode().Perm()&0o200 == 0 || !personFileWritable(path) {
		return false, errors.New("is read-only, so its [mcp_servers.shikii] table was left; remove it by hand")
	}
	if !write {
		return true, nil
	}
	return true, writePersonFile(path, []byte(out), 0o600)
}

// cutCodexServer returns the TOML text without shikii's MCP server, in any form shikii or a person wrote: the [mcp_servers.shikii]
// table and its sub-tables ([mcp_servers.shikii.env] …, quoted keys and spaces too), a shikii key under [mcp_servers], and dotted keys
// mcp_servers.shikii.… at any level, with their multi-line values. Every other line stays, comments and blank lines included; only the blank
// line shikii put before the table it appended (followed by the end of the file or another blank line) goes with it.
// withoutCodexServer checks the cut with a TOML reader; an inline mcp_servers = { shikii = … } is not cut here.
func cutCodexServer(text string) (string, bool, error) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	remove := make([]bool, len(lines))
	header := make([]bool, len(lines))
	var table []string
	inShikii := false
	depth, multi, cont := 0, "", false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if depth > 0 || multi != "" { // inside a multi-line value of the line before
			remove[i] = cont
			depth, multi = tomlScan(line, depth, multi)
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inner := strings.TrimPrefix(trimmed, "[")
			inner = strings.TrimPrefix(inner, "[") // [[array of tables]]
			if keys, rest, ok := tomlKey(inner); ok && strings.HasPrefix(strings.TrimSpace(rest), "]") {
				table = keys
				inShikii = isShikiiServerKey(keys)
				remove[i], header[i] = inShikii, inShikii
				continue
			}
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // blank lines and comments stay (a comment may belong to the next table)
		}
		keys, rest, ok := tomlKey(trimmed)
		if !ok || !strings.HasPrefix(strings.TrimSpace(rest), "=") {
			cont = inShikii
			remove[i] = inShikii
			continue
		}
		full := append(append([]string{}, table...), keys...)
		value := strings.TrimSpace(rest)[1:]
		cont = inShikii || isShikiiServerKey(full)
		remove[i] = cont
		depth, multi = tomlScan(value, 0, "")
	}
	found := false
	for i := range lines {
		if !remove[i] {
			continue
		}
		found = true
		if !header[i] || i == 0 || (i > 0 && remove[i-1]) || strings.TrimSpace(lines[i-1]) != "" {
			continue
		}
		j := i
		for j < len(lines) && remove[j] {
			j++
		}
		if j == len(lines) || strings.TrimSpace(lines[j]) == "" {
			remove[i-1] = true // the blank line before the table shikii appended
		}
	}
	if !found {
		return text, false, nil
	}
	var b strings.Builder
	for i, line := range lines {
		if !remove[i] {
			b.WriteString(line)
		}
	}
	out := b.String()
	if out != "" && !strings.HasSuffix(out, "\n") { // the last kept line had its newline cut off by a removed last line without one
		out += "\n"
	}
	return out, true, nil
}

// mcpProjects are the projects whose .mcp.json and opencode.json uninstall looks at: the registered ones, and the projects still in
// ~/.shikii/agent/projects/ whose .shikii/ is gone or not initialized (registeredProjects leaves those out). Only shikii's entry in those two
// files is touched there; the folder itself is not.
func mcpProjects(agent string, registered []projectEntry) []projectEntry {
	out := append([]projectEntry{}, registered...)
	seen := map[string]bool{}
	for _, p := range registered {
		seen[p.Path] = true
	}
	files, _ := filepath.Glob(filepath.Join(agent, "projects", "*.json"))
	for _, path := range files {
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			continue
		}
		var p projectEntry
		err = json.NewDecoder(io.LimitReader(f, 8192)).Decode(&p)
		f.Close()
		if err != nil || !filepath.IsAbs(p.Path) || seen[p.Path] || feedbackScope(p.Path) != p.Path || projectID(p.Path) != p.ID || !entryFileName(path, p) {
			continue
		}
		seen[p.Path] = true
		out = append(out, p)
	}
	return out
}

// isShikiiServerKey reports whether a dotted key path is mcp_servers.shikii or below it.
func isShikiiServerKey(keys []string) bool {
	return len(keys) >= 2 && keys[0] == "mcp_servers" && keys[1] == "shikii"
}

// tomlKey reads a dotted TOML key (bare, "basic" or 'literal' parts, spaces around dots) from the start of s and returns its parts and the rest.
func tomlKey(s string) ([]string, string, bool) {
	var keys []string
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return nil, "", false
		}
		switch s[0] {
		case '"':
			end := 1
			for end < len(s) && s[end] != '"' {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(s) {
				return nil, "", false
			}
			k, err := strconv.Unquote(s[:end+1])
			if err != nil {
				return nil, "", false
			}
			keys, s = append(keys, k), s[end+1:]
		case '\'':
			end := strings.IndexByte(s[1:], '\'')
			if end < 0 {
				return nil, "", false
			}
			keys, s = append(keys, s[1:end+1]), s[end+2:]
		default:
			end := 0
			for end < len(s) && (s[end] == '_' || s[end] == '-' || s[end] >= 'a' && s[end] <= 'z' || s[end] >= 'A' && s[end] <= 'Z' || s[end] >= '0' && s[end] <= '9') {
				end++
			}
			if end == 0 {
				return nil, "", false
			}
			keys, s = append(keys, s[:end]), s[end:]
		}
		s = strings.TrimLeft(s, " \t")
		if !strings.HasPrefix(s, ".") {
			return keys, s, true
		}
		s = s[1:]
	}
}

// tomlScan follows a value over one line: how many brackets and braces stay open, and whether a multi-line string (""" or ”') stays open.
func tomlScan(s string, depth int, multi string) (int, string) {
	for i := 0; i < len(s); i++ {
		if multi != "" {
			if strings.HasPrefix(s[i:], multi) {
				i += len(multi) - 1
				multi = ""
			} else if multi == `"""` && s[i] == '\\' {
				i++
			}
			continue
		}
		switch c := s[i]; {
		case strings.HasPrefix(s[i:], `"""`), strings.HasPrefix(s[i:], "'''"):
			multi = s[i : i+3]
			i += 2
		case c == '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case c == '\'':
			for i++; i < len(s) && s[i] != '\''; i++ {
			}
		case c == '#':
			return depth, multi
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth, multi
}
