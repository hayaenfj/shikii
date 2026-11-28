package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// hookSettingsFile is the part of a hook settings file (~/.claude/settings.json, ~/.codex/hooks.json) that tells which commands run at
// which event: the events, their groups, and the commands of each group.
type hookSettingsFile struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// codexHookKey is the name Codex gives a hook in ~/.codex/config.toml: the hooks file, the event in snake case, the index of the group
// in the event's list and the index of the hook in the group (as of Codex 0.172.0: the tables [hooks.state."<key>"] of a config in which
// the hooks run).
func codexHookKey(hooksFile, event string, group, handler int) string {
	var snake strings.Builder
	for i, r := range event {
		if unicode.IsUpper(r) && i > 0 {
			snake.WriteByte('_')
		}
		snake.WriteRune(unicode.ToLower(r))
	}
	return hooksFile + ":" + snake.String() + ":" + strconv.Itoa(group) + ":" + strconv.Itoa(handler)
}

// codexHookStates reads the [hooks.state."<key>"] tables of Codex's config: which keys have a trusted_hash, and which are turned off
// (enabled = false). Only that form is read, line by line; a config in another form says nothing, which doctor then reports as no record.
func codexHookStates(config string) (trusted, off map[string]bool) {
	trusted, off = map[string]bool{}, map[string]bool{}
	current := ""
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "enabled") {
			line, _, _ = strings.Cut(line, "#") // a comment after the value; a trusted_hash is hexadecimal and a table name may hold "#", so only here
			line = strings.TrimSpace(line)
		}
		switch {
		case strings.HasPrefix(line, "[hooks.state.\"") && strings.HasSuffix(line, "\"]"):
			current = strings.TrimSuffix(strings.TrimPrefix(line, "[hooks.state.\""), "\"]")
		case strings.HasPrefix(line, "["):
			current = ""
		case current != "" && strings.HasPrefix(line, "trusted_hash"):
			if _, value, ok := strings.Cut(line, "="); ok && strings.Trim(strings.TrimSpace(value), "\"") != "" {
				trusted[current] = true
			}
		case current != "" && strings.HasPrefix(line, "enabled"):
			if _, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(value) == "false" {
				off[current] = true
			}
		}
	}
	return trusted, off
}

// codexTrustLines says whether Codex has a trust record for each hook shikii wrote to ~/.codex/hooks.json. Codex runs only the hooks it
// was told to trust (/hooks in Codex) and skips the others without a word: in a real Codex 0.172.0 with shikii's hooks installed and not
// trusted, codex exec printed nothing and no hook ran. Only that a record is there is checked: the hash in it is Codex's own, so a record
// from before a change of the hook (a new path or timeout) still counts here, and /hooks shows that hook as changed.
func codexTrustLines(home string) []string {
	file := filepath.Join(home, ".codex", "hooks.json")
	body, err := os.ReadFile(file)
	if err != nil {
		return nil // no Codex hooks, or one that cannot be read: registeredBinaryLines says so
	}
	var config hookSettingsFile
	if json.Unmarshal(body, &config) != nil {
		return nil
	}
	// Codex names the file as it opened it: the path as written, or the one with its links resolved
	files := []string{file}
	if resolved, err := filepath.EvalSymlinks(file); err == nil && resolved != file {
		files = append(files, resolved)
	}
	settings, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	trusted, off := codexHookStates(string(settings))
	hasKey := func(states map[string]bool, event string, group, handler int) bool {
		for _, f := range files {
			if states[codexHookKey(f, event, group, handler)] {
				return true
			}
		}
		return false
	}
	total := 0
	missing, turnedOff := map[string]bool{}, map[string]bool{}
	for event, groups := range config.Hooks {
		for g, group := range groups {
			for h, handler := range group.Hooks {
				if !isShikiiHook(handler.Command) {
					continue
				}
				total++
				if !hasKey(trusted, event, g, h) {
					missing[event] = true
				} else if hasKey(off, event, g, h) {
					turnedOff[event] = true
				}
			}
		}
	}
	names := func(set map[string]bool) string {
		var list []string
		for event := range set {
			list = append(list, event)
		}
		sort.Strings(list)
		return strings.Join(list, ", ")
	}
	var lines []string
	if len(missing) > 0 {
		lines = append(lines, fmt.Sprintf("WARN Codex has no trust record for shikii's hooks of %s in ~/.codex/hooks.json, so it skips them without a word (codex exec prints nothing) → open Codex, run /hooks and trust shikii's hooks", names(missing)))
	}
	if len(turnedOff) > 0 {
		lines = append(lines, fmt.Sprintf("WARN shikii's hooks of %s are turned off in Codex's /hooks → open Codex, run /hooks and turn them on", names(turnedOff)))
	}
	if total > 0 && len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("ok   Codex has a trust record for the %d shikii hooks in ~/.codex/hooks.json (/hooks shows whether it still holds: Codex asks again when a hook changes)", total))
	}
	return lines
}
