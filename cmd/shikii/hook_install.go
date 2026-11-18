package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/handoff"
)

// hookEvents are 6, for Claude Code and Codex alike: before thinking (UserPromptSubmit), after thinking (Stop), at the session's start
// and after a summary (SessionStart. 0.11), when a subagent starts and ends (SubagentStart, SubagentStop: the rules for every request
// for the subagent and its own report of what it did with its lessons), and right before a file edit or a command (PreToolUse for the
// edit tools and the shell. One lesson naming that file or command, and the forbidden-commands guards. 0.11). Up to 0.37 Claude Code
// had a seventh, PreToolUse for Agent / Task, that kept the task text passed to the child (0.9.1); install removes it.
// Measured on Codex 0.169.0: SubagentStart, SubagentStop and PreToolUse pass additionalContext to the model (PreToolUse could not be
// shown to in 0.11, and Codex was left out then), and a subagent's hooks carry the parent's session and the subagent's own agent_id.
// Every change to hooks.json makes Codex users trust the hooks again. OpenCode's plugin runs the subagent commands itself.
// command is the current executable's absolute path + " hook <event>": it works even if the agent's shell PATH has no `shikii` (e.g. ~/go/bin right after go install).
// If the executable moves, rerunning `shikii hook install --yes` removes the handlers with the old path and adds new ones.
func hookEvents() []hookEvent {
	exe := shikiiBinaryPath()
	return []hookEvent{
		{"UserPromptSubmit", hookCommand(exe, "user-prompt-submit"), hookTimeout, ""},
		{"SessionStart", hookCommand(exe, "session-start"), hookTimeout, ""}, // the project's brief at the start and right after a summary (source compact). Both agents read its additionalContext (0.11)
		{"Stop", hookCommand(exe, "stop"), hookTimeout + 5, ""},
		{"SubagentStart", hookCommand(exe, "subagent-start"), hookTimeout, ""},          // the rules for every request, to subagents too (0.7.3; lessons chosen by the task until 0.37)
		{"SubagentStop", hookCommand(exe, "subagent-stop"), hookTimeout, ""},            // the child's own report of what it did with its lessons
		{"PreToolUse", hookCommand(exe, "tool-lesson"), hookTimeout, toolLessonMatcher}, // a lesson naming the file or command, right before it (0.11)
	}
}

// shikiiBinaryPath is the absolute path of the running shikii (links resolved), or the bare name when it cannot be found: the binary the hooks
// of every agent run, including OpenCode's plugin (opencode_plugin.go).
func shikiiBinaryPath() string {
	exe := "shikii"
	if p, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		exe = p
	}
	return exe
}

// hookCommand is the command line of one hook. The agents run it through a shell, so a path with a space or a quote is quoted
// (0.9.3 and earlier wrote it bare: with a space in the path the shell ran only the part before it, and the hook never worked).
func hookCommand(exe, event string) string { return shellQuote(exe) + " hook " + event }

// hookBinary is the binary of a hook command `<binary> hook <event>` in exactly a form shikii writes and that runs (0.10.1): a path in
// single quotes as hookCommand writes it (quoting it again gives the same text, and it is absolute), or one bare word of only the
// characters a path has (letters, digits, non-ASCII letters and ./-_+@:,%; inside an absolute path also ~ = #, which the shell keeps
// as they are, as 0.9.2 and earlier wrote paths such as iCloud's com~apple). No space: a space means another command wraps or comes
// before shikii (nice, timeout, a chain), and the paths with spaces 0.9.3 and earlier wrote bare never ran (the shell split them).
// Anything else (; & | $ quotes, a backslash, a leading ~) means a person wrote it, and ok is false: install and uninstall then leave
// the whole command as it is. Reading more forms kept deleting people's commands (0.10's audits), so shikii reads only its own.
func hookBinary(command string) (string, bool) {
	c := strings.TrimSpace(command)
	i := strings.LastIndex(c, " hook ")
	if i <= 0 || strings.TrimSpace(c[i+len(" hook "):]) == "" {
		return "", false
	}
	bin := strings.TrimSpace(c[:i])
	if len(bin) >= 2 && bin[0] == '\'' && bin[len(bin)-1] == '\'' {
		path := strings.ReplaceAll(bin[1:len(bin)-1], `'\''`, "'")
		if shellQuote(path) != bin || !filepath.IsAbs(path) {
			return "", false
		}
		return path, true
	}
	for _, r := range bin {
		if r < 0x80 && !strings.ContainsRune(shellPlain, r) && !(strings.ContainsRune("~=#", r) && filepath.IsAbs(bin)) {
			return "", false
		}
	}
	return bin, true
}

// shikiiHookEvent reports whether event is one of shikii's own hook events (hookEvents), or one older versions added and install now
// removes: pre-tool-use, and agent-task (up to 0.37, it kept the task passed to the Agent tool).
func shikiiHookEvent(event string) bool {
	if event == "pre-tool-use" || event == "agent-task" {
		return true
	}
	for _, ev := range hookEvents() {
		if strings.HasSuffix(ev.cmd, " hook "+event) {
			return true
		}
	}
	return false
}

// hookEventOf is the event a hook command `… hook <event>` runs, or "".
func hookEventOf(command string) string {
	c := strings.TrimSpace(command)
	i := strings.LastIndex(c, " hook ")
	if i <= 0 {
		return ""
	}
	return c[i+len(" hook "):]
}

// hookEventsFor is the hooks added per settings file. Codex gets the same hooks, and its tool-lesson looks at Codex's own tools (see
// hookEvents).
func hookEventsFor(who string) []hookEvent {
	var evs []hookEvent
	for _, ev := range hookEvents() {
		if who != "Claude Code" && ev.matcher == toolLessonMatcher {
			ev.matcher = toolLessonMatcherCodex
		}
		evs = append(evs, ev)
	}
	return evs
}

func hookEventNames(evs []hookEvent) string {
	names := make([]string, 0, len(evs))
	for _, ev := range evs {
		names = append(names, ev.event)
	}
	return strings.Join(names, ", ")
}

// isShikiiHook reports whether a handler's command is a shikii hook, the only ones install and uninstall change: in a form shikii
// writes (hookBinary), a binary named exactly shikii (or like the running binary, as a build named otherwise writes its own hooks),
// and one of shikii's own events (shikiiHookEvent). Another tool named shikii-…, even one using the same event names, is not shikii's.
func isShikiiHook(command string) bool {
	bin, ok := hookBinary(command)
	if !ok || !shikiiHookEvent(hookEventOf(command)) {
		return false
	}
	name := filepath.Base(bin)
	if name == "shikii" {
		return true
	}
	exe, err := os.Executable()
	return err == nil && name == filepath.Base(exe)
}

// shikiiBefore finds a shikii binary right before " hook " (…/shikii, 'shikii', "…/shikii"), not a folder or a tool named shikii-….
var shikiiBefore = regexp.MustCompile(`(^|[/'"\s])shikii['"]?$`)

// handWrittenShikiiHook reports whether a command runs a shikii binary for one of shikii's events (the first word after " hook ") but
// is not in a form shikii writes (~, $HOME, quotes, chained after another command, more words after the event…). install and
// uninstall leave it as it is, and doctor and uninstall name it, since shikii cannot tell what it runs.
func handWrittenShikiiHook(command string) bool {
	c := strings.TrimSpace(command)
	i := strings.LastIndex(c, " hook ")
	if i <= 0 || isShikiiHook(c) {
		return false
	}
	event, _, _ := strings.Cut(strings.TrimSpace(c[i+len(" hook "):]), " ")
	return shikiiBefore.MatchString(strings.TrimSpace(c[:i])) && shikiiHookEvent(event)
}

// handWrittenShikiiHooksIn lists the hand-written shikii hooks (handWrittenShikiiHook) in a hook settings file, sorted; none when the
// file cannot be read.
func handWrittenShikiiHooksIn(path string) []string {
	body, err := os.ReadFile(path)
	var config map[string]any
	if err != nil || json.Unmarshal(body, &config) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	hooks, _ := config["hooks"].(map[string]any)
	for _, groups := range hooks { // read leniently, as editHooks does: an event of another shape is skipped, not the whole file
		groups, _ := groups.([]any)
		for _, g := range groups {
			g, _ := g.(map[string]any)
			handlers, _ := g["hooks"].([]any)
			for _, h := range handlers {
				h, _ := h.(map[string]any)
				c, _ := h["command"].(string)
				if handWrittenShikiiHook(c) && !seen[c] {
					seen[c] = true
					out = append(out, c)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// hookTargets are the settings files the hooks are written to (under home, fixed). The same 2 are added to both. Settings files for tools that are not installed are also created (same style as init; delete them if not needed).
var hookTargets = []struct {
	rel, who, note string
}{
	{filepath.Join(".claude", "settings.json"), "Claude Code", ""},
	{filepath.Join(".codex", "hooks.json"), "Codex", " Codex runs only hooks you trusted, and asks again whenever ~/.codex/hooks.json changes (a new path or timeout): open Codex, run /hooks and trust shikii's hooks; until then Codex skips them (codex exec says nothing)"},
}

func hookInstall(args []string, install bool) error {
	yes := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			yes = true
		} else {
			return fmt.Errorf("hook %s: unknown argument %q (only --yes)", map[bool]string{true: "install", false: "uninstall"}[install], a)
		}
	}
	agent, err := agentHome()
	if err != nil {
		return err
	}
	home := filepath.Dir(filepath.Dir(agent))
	tilde := homePathDisplay(agent)
	if install {
		fmt.Fprintf(stdout, "1. %s: prepare the shared templates and wiki. Run shikii --init once in each project that uses it; from then on conversations, decisions, scores, truths and calibrations are saved in its .shikii/. The hooks do not run in uninitialized places. Prepares the 4 ready-made questions (%s) and the skill, and keeps existing settings.\n", tilde(agent), strings.Join(hookSpecs, ", "))
		for i, t := range hookTargets {
			evs := hookEventsFor(t.who)
			fmt.Fprintf(stdout, "%d. %s: add %d hooks (%s) running `%s hook <event>` (%s; the binary's own path, so the agent's PATH does not matter); shikii hooks from an older version or an older path are removed; a backup of the file as it was goes to %s.bak-shikii (once, only if it existed). Other hooks are kept; a second install adds nothing.%s\n", i+2, tilde(filepath.Join(home, t.rel)), len(evs), hookEventNames(evs), tilde(func() string { bin, _ := hookBinary(evs[0].cmd); return bin }()), t.who, filepath.Base(t.rel), t.note)
		}
		fmt.Fprintf(stdout, "%d. %s\n", len(hookTargets)+2, opencodePluginPreview(home, shikiiBinaryPath(), true))
		fmt.Fprintf(stdout, "   The hooks measure the agent's own thinking before a turn (will this request change files? should it agree on an approach first?) and after it (were the changes within what was asked? was the request completed?). Model-based advice waits for calibration; past exchanges from the same project can be quoted before similar requests without labeling them as truths. They never stop a tool, except that a guard the person enabled refuses a command right before it runs. Claude Code, Codex and OpenCode 2.x.\n")
		fmt.Fprintf(stdout, "   Recommended model: TypeSafe Jev. With TYPESAFE_API_KEY in the environment the hooks send there and start no local model. Without a key, the local model (%s) is started in the background by the hooks when it is not running (install starts it now if llama-server is on PATH). The answer page is one per project: the first request's hook starts it on a free port, records it in ~/.shikii/agent/pages/, and opens it in the browser\n", hookLocalURL)
		// shown before it is added: Codex will no longer ask before each call of shikii's MCP tools (codex_tools_allowed.go). The
		// preview decides as the install below does: on the registration as fixCodexServer leaves it (a command that is gone is
		// pointed at this binary first), on whether the file can be written (personFileWritable, as the write finds it: the mode
		// bits alone say nothing of a disk mounted read-only, an ACL or an immutable flag; 0.31.0 audit D, U-3), and on the note
		if config := filepath.Join(home, ".codex", "config.toml"); personFileWritable(config) && !fileExists(codexToolsAllowedMark()) {
			if b, err := os.ReadFile(config); err == nil {
				if fixed, ok := codexServerFixed(b, codexServerArgs, mcpCommand()); ok {
					b = fixed
				}
				if _, ok := codexToolsAllowed(b, codexServerArgs); ok {
					fmt.Fprintf(stdout, "   %s: Codex will run shikii's MCP tools without asking each time (adds %s to the shikii server shikii wrote; write \"prompt\" in its place to be asked)\n", tilde(config), codexToolsAllowedLine)
				}
			}
		}
	} else {
		for i, t := range hookTargets {
			fmt.Fprintf(stdout, "%d. %s: remove the handlers whose command is `… shikii hook <event>` (other hooks stay)\n", i+1, tilde(filepath.Join(home, t.rel)))
		}
		fmt.Fprintf(stdout, "%d. %s\n", len(hookTargets)+1, opencodePluginPreview(home, "", false))
		fmt.Fprintf(stdout, "   %s and each project's .shikii/ are kept\n", tilde(agent))
	}
	if install {
		from, broken := installedVersion(agent)
		to := currentVersion()
		switch {
		case broken:
			fmt.Fprintf(stdout, "%d. %s: the version marker cannot be read, so no project's .shikii/ is removed (check its contents and fix it. Until it is fixed, --yes stops too)\n", len(hookTargets)+3, tilde(installedVersionPath(agent)))
		case crossesStoreReset(from, to):
			plan := planWipe(registeredProjects(agent))
			wipes := 0
			for _, p := range plan {
				if p.Action == wipeDo {
					wipes++
				}
			}
			if wipes > 0 {
				fmt.Fprintf(stdout, "%d. update to %s (previous version %s): everything inside .shikii/ of %d initialized projects is removed and initialized again (project.json is kept). Save anything you want to keep first:\n", len(hookTargets)+3, versionString(to), versionString(from), wipes)
			} else if len(plan) > 0 {
				fmt.Fprintf(stdout, "%d. update to %s (previous version %s): no projects to remove (already initialized in this version, or not removed):\n", len(hookTargets)+3, versionString(to), versionString(from))
			}
			for _, p := range plan {
				switch p.Action {
				case wipeDo:
					note := ""
					if files, err := (handoff.Book{Store: filepath.Join(p.Project.Path, ".shikii")}).List(); err == nil && len(files) > 0 {
						note = fmt.Sprintf(" (%d work records (.shikii/handoff/) are removed too)", len(files))
					}
					fmt.Fprintf(stdout, "   - remove: %s%s\n", p.Project.Path, note)
				case wipeDone:
					fmt.Fprintf(stdout, "   - done: %s (%s)\n", p.Project.Path, p.Reason)
				case wipeRefuse:
					fmt.Fprintf(stdout, "   - not removed: %s (%s)\n", p.Project.Path, p.Reason)
				}
			}
		}
	}
	if !yes {
		fmt.Fprintln(stdout, "(shown only; pass --yes to do it)")
		return nil
	}
	if install {
		for _, d := range []string{"decisions", "wiki", filepath.Join("wiki", "specs"), filepath.Join("wiki", "runs")} {
			if err := os.MkdirAll(filepath.Join(agent, d), 0o755); err != nil {
				return err
			}
		}
		n, err := seedAgentSpecs(filepath.Join(agent, "decisions"))
		if err != nil {
			return err
		}
		w, err := seedWiki(filepath.Join(agent, "wiki"))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %d specs and %d wiki pages added\n", tilde(agent), n, w)
		for _, rel := range skillDirs {
			if err := copySkill(filepath.Join(home, rel)); err != nil {
				return fmt.Errorf("skill: %w", err)
			}
			fmt.Fprintf(stdout, "%s: the agent's rules put in place\n", tilde(filepath.Join(home, rel)))
		}
		// convert, once, the index entries released versions wrote (named by the path) into the store id's entries (0.9.7)
		for _, p := range upgradeIndex(agent) {
			fmt.Fprintf(stdout, "%s: %s\n", tilde(p), olderShikiiNote)
		}
		// point the Codex MCP server shikii wrote at this binary while the block is still the current form and its binary is gone (0.9.3).
		// A block a person edited, or in an older version's form, is left alone and only told (0.9.7)
		codex := filepath.Join(home, ".codex", "config.toml")
		if fixed, err := fixCodexServer(codex, codexServerArgs); err != nil {
			fmt.Fprintf(stdout, "%s: %v\n", tilde(codex), err)
		} else if fixed {
			fmt.Fprintf(stdout, "%s: pointed the shikii MCP server shikii wrote at %s with %s (Codex; reopen Codex)\n", tilde(codex), mcpCommand(), codexServerArgsLine)
		} else if codexServerOlder(codex) {
			fmt.Fprintf(stdout, "%s: %s\n", tilde(codex), codexOlderNote())
		} else if codexServerPinsAProject(codex) {
			fmt.Fprintf(stdout, "%s: %s\n", tilde(codex), codexPinnedNote)
		}
		// an earlier version's registration gets the approval line too, while the block is still shikii's own (codex_tools_allowed.go)
		if err := allowCodexTools(codex); err != nil {
			fmt.Fprintf(stdout, "%s: %v\n", tilde(codex), err)
		}
		// a section an older version added to a registered project's AGENTS.md is only told: shikii does not change the person's file (0.9.7)
		for _, p := range registeredProjects(agent) {
			if path := filepath.Join(p.Path, "AGENTS.md"); agentsSectionLeft(path) {
				fmt.Fprintf(stdout, "%s: %s\n", tilde(path), agentsSectionNote)
			}
		}
		// Update all shared writers before they encounter this version's metadata.
		fmt.Fprintf(stdout, "Note: reopen any open agent sessions and restart the page and MCP. Move every machine that uses the same wiki or active session to %s or later before saving learning or using the new work context. Older writers do not preserve the new work context. Binaries older than 0.22 cannot read articles carrying learning; existing sources and history are kept.\n", shikii.Version)
		// on the first run that crosses 0.7.0, remove and reinitialize each project's .shikii/ (this is where the new binary is called at the end of update).
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if _, err := resetStoresOnFirstRun(agent, currentVersion(), reinitWith(context.Background(), exe)); err != nil {
			return err
		}
	}
	for _, t := range hookTargets {
		settings := filepath.Join(home, t.rel)
		changed, err := editHooks(settings, install, hookEventsFor(t.who))
		if err != nil {
			return err
		}
		switch {
		case changed && install:
			fmt.Fprintf(stdout, "%s: hooks added (%s). Start a new terminal for them to take effect.%s\n", tilde(settings), t.who, t.note)
		case changed:
			fmt.Fprintf(stdout, "%s: hooks removed (%s). Start a new terminal for it to take effect\n", tilde(settings), t.who)
		case install:
			fmt.Fprintf(stdout, "%s: hooks were already there, nothing changed (%s)\n", tilde(settings), t.who)
		default:
			fmt.Fprintf(stdout, "%s: no shikii hooks found, nothing changed (%s)\n", tilde(settings), t.who)
		}
	}
	if err := hookInstallOpencodePlugin(home, install); err != nil {
		return err
	}
	if install {
		if usesHosted() {
			fmt.Fprintf(stdout, "model: TypeSafe Jev (%s; the key is in the environment). No local model is started. SHIKII_MODEL=local selects the local model instead\n", hookHostedURL)
		} else if modelUp() {
			fmt.Fprintf(stdout, "local model: already answering on %s\n", hookLocalURL)
		} else if _, err := lookPath("llama-server"); err != nil {
			fmt.Fprintf(stdout, "local model: none on %s and no llama-server on PATH. Install llama.cpp (brew install llama.cpp); the hooks start the model by themselves after that. Until then they record nothing\n", hookLocalURL)
		} else {
			ensureModel(agent)
			fmt.Fprintf(stdout, "local model: starting in the background (the first run downloads it; a few minutes). Log: %s\n", tilde(filepath.Join(agent, "up.log")))
		}
		fmt.Fprintln(stdout, "answer page: one per project, started by the first request's hook on a free port and opened in the browser (shikii page prints the URL)")
	}
	return nil
}

// seedAgentSpecs copies only the embedded specs that are missing. Returns how many were copied.
func seedAgentSpecs(dst string) (int, error) {
	n := 0
	err := fs.WalkDir(shikii.AgentSpecsFS, "integrations/claude-code/specs", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		target := filepath.Join(dst, filepath.Base(p))
		b, err := shikii.AgentSpecsFS.ReadFile(p)
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
		if filepath.Base(p) == "needs-plan.yaml" {
			// a copy still holding an earlier built-in text is brought to the current one (the question's answers changed). The refresh is best
			// effort: a copy that cannot be read or written (permissions, a read-only folder) is kept, and the next seeding tries again. Seeding runs
			// on every hook event, so a refresh error must not stop a hook.
			_, _ = refreshNeedsPlanCopy(target, b)
		}

		return nil
	})
	return n, err
}

// editHooks fixes the hooks in settings.json. For install, it aligns shikii's handlers to the current set (adds to missing events, removes `shikii hook …` not in this version, such as the old PreToolUse).
// For uninstall, it removes every handler starting with `shikii hook `. Other hooks and other keys stay. Before writing, it backs up to .bak-shikii. Returns whether it changed anything.
func editHooks(path string, install bool, hookEvents []hookEvent) (bool, error) {
	return editHooksTo(path, install, hookEvents, true)
}

// editHooksTo is editHooks; with write false it only reports whether it would change anything (shikii uninstall without --yes).
func editHooksTo(path string, install bool, hookEvents []hookEvent, write bool) (bool, error) {
	// a settings file kept elsewhere through a link (dotfiles): write the file it points at, so the link stays a link and its mode stays
	// (the backup stays next to the link). Before 0.9.3 the rename below replaced the link with a regular 0600 file (audit 0.9.3 uninstall)
	backup := path + ".bak-shikii"
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil && strings.TrimSpace(string(b)) != "" { // an empty file is the same as {}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			return false, fmt.Errorf("%s: %w (fix it by hand; shikii does not overwrite a broken file)", path, err)
		}
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	want := map[string]bool{}
	matcher := map[string]string{} // the matcher this version registers each of its commands under
	for _, ev := range hookEvents {
		want[ev.cmd] = install
		matcher[ev.cmd] = ev.matcher
		if v, has := hooks[ev.event]; has {
			if _, ok := v.([]any); !ok {
				return false, fmt.Errorf("%s: hooks.%s is not a list (fix it by hand; shikii does not replace what it cannot read)", path, ev.event)
			}
		}
	}
	command := func(h any) string {
		m, _ := h.(map[string]any)
		c, _ := m["command"].(string)
		return c
	}
	// to remove: shikii handlers not in this version (all of them for uninstall. Old handlers whose executable path changed are removed
	// here too), and a handler of this version under another matcher than it is registered with (written by another build, or edited
	// by hand): it would not run for the tools it is for, so it is taken out here and added again below with its matcher
	stale := func(h any, group map[string]any) bool {
		c := command(h)
		if !isShikiiHook(c) {
			return false
		}
		m, _ := group["matcher"].(string)
		return !want[c] || m != matcher[c]
	}
	changed := false
	// 1) sweep all events
	for name, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			continue // someone else's event in an unreadable shape is not touched (for our own events it is an error above)
		}
		var kept []any
		removed := false
		for _, g := range groups {
			gm, isGroup := g.(map[string]any)
			if !isGroup {
				kept = append(kept, g) // not a group of hooks: it stays as the person wrote it
				continue
			}
			hs, _ := gm["hooks"].([]any)
			var rest []any
			for _, h := range hs {
				if stale(h, gm) {
					changed, removed = true, true
				} else {
					rest = append(rest, h)
				}
			}
			if len(rest) > 0 {
				gm["hooks"] = rest
				kept = append(kept, gm)
			} else if len(hs) == 0 {
				kept = append(kept, gm)
			}
		}
		if !removed {
			continue // someone else's event stays as it is (an empty array is not touched either)
		}
		if len(kept) > 0 {
			hooks[name] = kept
		} else {
			delete(hooks, name)
		}
	}
	// 2) install: add what is missing
	if install {
		for _, ev := range hookEvents {
			groups, _ := hooks[ev.event].([]any)
			present := false
			for _, g := range groups {
				gm, _ := g.(map[string]any)
				hs, _ := gm["hooks"].([]any)
				for _, h := range hs {
					if command(h) == ev.cmd {
						present = true
					}
				}
			}
			if present {
				continue
			}
			group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": ev.cmd, "timeout": ev.timeout}}}
			if ev.matcher != "" {
				group["matcher"] = ev.matcher
			}
			hooks[ev.event] = append(groups, group)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if !write {
		return true, nil
	}
	if len(hooks) > 0 {
		cfg["hooks"] = hooks
	} else {
		delete(cfg, "hooks")
	}
	if len(b) > 0 && !fileExists(backup) && !strings.Contains(string(b), " hook user-prompt-submit") && !strings.Contains(string(b), " hook pre-tool-use") && !strings.Contains(string(b), " hook stop") { // the backup is made only the first time, of a version without shikii hooks (a settings.json created by install is not backed up)
		if err := os.WriteFile(backup, b, 0o600); err != nil {
			return false, err
		}
	}
	return true, writePersonJSON(path, cfg)
}

// pct is a probability as a whole percent, as the page shows the AI's confidence.
func pct(p float64) int { return int(math.Round(p * 100)) }

// opencodePluginPreview is the line the preview prints for OpenCode's plugin (opencode_plugin.go). For an install it asks
// installOpencodePlugin what it would do (without writing), so the preview says what the install will do: add, update, or change nothing.
func opencodePluginPreview(home, exe string, install bool) string {
	shown := displayUnder(home)(opencodePluginDir(home))
	temps := ""
	if n := len(opencodeTempFiles(home)); n > 0 {
		temps = "; " + opencodeTempNote(n)
	}
	if install {
		res, _ := installOpencodePlugin(home, exe, false)
		switch res {
		case "left":
			return fmt.Sprintf("%s: %s is there, so the OpenCode plugin is left as it is (not written)", shown, opencodeNotShikiis)
		case "same":
			return fmt.Sprintf("%s: shikii's OpenCode plugin is already there as this install would write it, so nothing changes%s", shown, temps)
		case "updated":
			return fmt.Sprintf("%s: update shikii's OpenCode plugin (package.json and index.ts) to the one that runs `%s hook <event>`; anything else in the folder stays%s", shown, exe, temps)
		}
		return fmt.Sprintf("%s: add the OpenCode plugin (package.json and index.ts) that runs `%s hook <event>` for OpenCode 2.x (user-prompt-submit and session-start before a turn, stop after it); OpenCode 1.x does not read it and keeps using only the MCP server. A folder of yours with that name is never overwritten; a second install changes nothing%s", shown, exe, temps)
	}
	switch state, _ := removeOpencodePlugin(home, false); state {
	case opencodePluginShikii:
		return fmt.Sprintf("%s: remove shikii's OpenCode plugin (its two files; anything else in the folder and your other plugins stay)%s", shown, temps+opencodeFolderGoes)
	case opencodePluginForeign:
		return fmt.Sprintf("%s: not shikii's plugin (%s), so it stays", shown, opencodeNotShikiis)
	}
	if temps != "" {
		return fmt.Sprintf("%s: no shikii OpenCode plugin found%s%s", shown, temps, opencodeFolderGoes)
	}
	return fmt.Sprintf("%s: no shikii OpenCode plugin found, nothing to remove", shown)
}

// opencodeTempNote says what is done with the n temporary files an install of the plugin left when it stopped before the rename.
func opencodeTempNote(n int) string {
	return fmt.Sprintf("remove the %d temporary file(s) (.write-<digits>.tmp) an install of shikii's OpenCode plugin left when it stopped", n)
}

// opencodeFolderGoes is added to opencodeTempNote when the plugin is taken out.
const opencodeFolderGoes = "; the folder goes when nothing else is in it"

// hookInstallOpencodePlugin puts OpenCode's plugin in place (install) or takes it out (uninstall) and says what happened.
func hookInstallOpencodePlugin(home string, install bool) error {
	shown := displayUnder(home)(opencodePluginDir(home))
	if !install {
		temps := len(opencodeTempFiles(home))
		state, err := removeOpencodePlugin(home, true)
		switch {
		case err != nil:
			return err
		case state == opencodePluginShikii:
			fmt.Fprintf(stdout, "%s: OpenCode plugin removed. Reopen OpenCode for it to take effect\n", shown)
		case state == opencodePluginAbsent && temps > 0:
			fmt.Fprintf(stdout, "%s: removed the %d temporary file(s) an install of shikii's OpenCode plugin left when it stopped\n", shown, temps)
		default:
			fmt.Fprintf(stdout, "%s: no shikii OpenCode plugin found, nothing changed\n", shown)
		}
		return nil
	}
	temps := len(opencodeTempFiles(home))
	res, err := installOpencodePlugin(home, shikiiBinaryPath(), true)
	if err != nil {
		// as when Codex's registration cannot be fixed: one line, and the install goes on. A config folder that cannot be written (owned
		// by root after a sudo run) must not fail the hooks of Claude Code and Codex, which are already written (install.sh runs this
		// under set -eu, and shikii update stops on a failing hook install)
		fmt.Fprintf(stdout, "%s: the OpenCode plugin could not be written (%v); Claude Code and Codex are not affected. Make the folder writable and run shikii hook install --yes again to get lessons in OpenCode 2.x\n", shown, err)
		return nil
	}
	switch res {
	case "added":
		fmt.Fprintf(stdout, "%s: OpenCode plugin added (OpenCode 2.x). Reopen OpenCode for it to take effect\n", shown)
	case "updated":
		fmt.Fprintf(stdout, "%s: OpenCode plugin updated (OpenCode 2.x). Reopen OpenCode for it to take effect\n", shown)
	case "same":
		fmt.Fprintf(stdout, "%s: OpenCode plugin was already there, nothing changed (OpenCode 2.x)\n", shown)
	default:
		fmt.Fprintf(stdout, "%s: %s is there, so the OpenCode plugin is left as it is. Move it away and run shikii hook install --yes again to get lessons in OpenCode 2.x\n", shown, opencodeNotShikiis)
		return nil
	}
	if temps > 0 {
		fmt.Fprintf(stdout, "%s: removed the %d temporary file(s) an earlier install left when it stopped\n", shown, temps)
	}
	return nil
}
