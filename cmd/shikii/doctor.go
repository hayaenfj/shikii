package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/store"

	"github.com/hayaenfj/shikii/internal/systemone"
)

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	specsDir := fs.String("specs", "", "spec folder (default <store>/decisions)")
	storeDir := fs.String("store", ".shikii", "where records are stored")
	local := fs.String("local", "http://127.0.0.1:8080", "URL of the local model")
	hosted := fs.String("hosted", "https://api.typesafe.ai", "hosted URL (empty to skip)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := resolveStoreFlag(fs); err != nil {
		return err
	}
	ok := func(b bool) string {
		if b {
			return "ok  "
		}
		return "MISSING"
	}
	// 1. tools
	llama, _ := exec.LookPath("llama-server")
	fmt.Fprintf(stdout, "%s llama-server: %s\n", ok(llama != ""), orElse(llama, "not in PATH → brew install llama.cpp"))
	readout, err := findReadout()
	fmt.Fprintf(stdout, "%s shikii-readout: %s\n", ok(err == nil), orElse(readout, "not found → install again with the one line in the README (it puts it in ~/.local/bin; make install from a source checkout)"))
	if home, err := os.UserHomeDir(); err == nil {
		for _, line := range registeredBinaryLines(home, mcpCommand()) {
			fmt.Fprintln(stdout, line)
		}
	}
	// 2. the local model (with a key Jev is used, so a missing one is not treated as lacking)
	if usesHosted() {
		fmt.Fprintf(stdout, "ok   model: TypeSafe Jev (TYPESAFE_API_KEY is set, so the local model is not started. To use it, set SHIKII_MODEL=local)\n")
	}
	if *local != "" {
		pr := probe(*local)
		shownLocal := systemone.RedactURL(*local) // never the credentials in the URL
		switch {
		case !pr.up && usesHosted():
			fmt.Fprintf(stdout, "ok   local model at %s: not running (not needed while Jev is used)\n", shownLocal)
		case !pr.up:
			fmt.Fprintf(stdout, "MISSING local model at %s: unreachable → shikii up   (starts %s behind shikii-readout; or shikii up --gguf <file> for any GGUF)\n", shownLocal, defaultModel)
		case pr.decisionModel:
			fmt.Fprintf(stdout, "ok   local model at %s: %s (decision model)\n", shownLocal, pr.model)
		default:
			fmt.Fprintf(stdout, "WARN local model at %s: %s is a plain chat model → put shikii-readout in front (shikii up does this)\n", shownLocal, pr.model)
		}
	}
	// 3. hosted
	if *hosted != "" {
		key := hostedKey()
		shownHosted := systemone.RedactURL(*hosted)
		c := &http.Client{Timeout: 4 * time.Second}
		_, herr := c.Get(strings.TrimRight(*hosted, "/") + "/")
		switch {
		case herr != nil:
			fmt.Fprintf(stdout, "WARN hosted %s: unreachable (%v)\n", shownHosted, systemone.ScrubError(herr, *hosted))
		case key == "":
			fmt.Fprintf(stdout, "WARN hosted %s: reachable, but no key in DECISIONS_API_KEY / TYPESAFE_API_KEY (hosted will not be used)\n", shownHosted)
		default:
			fmt.Fprintf(stdout, "ok   hosted %s: reachable, key present\n", shownHosted)
		}
	}
	// 4. spec and store
	if *specsDir == "" {
		*specsDir = specsDirOf(*storeDir)
	}
	specs, err := loadSpecs(*specsDir)
	if err != nil {
		fmt.Fprintf(stdout, "MISSING specs in %s: %v → shikii new <id>\n", *specsDir, err)
	} else {
		fmt.Fprintf(stdout, "ok   %d spec(s) in %s\n", len(specs), *specsDir)
		st := store.Store{Dir: *storeDir}
		for _, sp := range specs {
			n, labs, _ := st.Counts(sp.ID)
			state := fmt.Sprintf("%d records, %d truths", n, len(labs))
			if c := sp.CalibrationFor(""); c != nil {
				state += ", calibrated"
			}
			fmt.Fprintf(stdout, "     %-24s %s (pii=%v, send_to=%v)\n", sp.ID, state, sp.PIISensitive(), sp.Policy.SendTo)
		}
	}
	if fi, err := os.Stat(*storeDir); err != nil {
		fmt.Fprintf(stdout, "note store %s does not exist yet (created on first decision)\n", *storeDir)
	} else if !fi.IsDir() {
		fmt.Fprintf(stdout, "MISSING store %s is not a directory\n", *storeDir)
	} else {
		fmt.Fprintf(stdout, "ok   store %s\n", *storeDir)
	}
	return nil
}

// registeredBinaryLines says, for the hooks (hookTargets) and Codex's MCP registration (~/.codex/config.toml), which shikii binary they
// run, and when that binary is gone (a Go version manager removed its per-version folder, or the binary was moved), how to point them
// at one that exists: hook install --yes rewrites the hooks, and the Codex block shikii wrote, to the binary that runs it.
// Only the hooks in a form shikii writes are checked (hookBinary: nothing is read as a shell would). A folder or a file without
// permission to run is not ok, and a settings file that cannot be read is named, not skipped. A shikii hook a person wrote another way
// (handWrittenShikiiHook) gets one note with its command: doctor did not check it, and hook install leaves it next to shikii's own.
// A Codex block that hook install leaves alone (a person edited it, or an older version's form) gets the hand edit as its fix.
func registeredBinaryLines(home, exe string) []string {
	fix := "run " + shellQuote(exe) + " hook install --yes to point them at this shikii, or install again with the one line in the README (it puts shikii in ~/.local/bin)"
	var lines []string
	report := func(where, shown, fix string) {
		switch exists, runs := hookBinaryRuns(shown); {
		case runs:
			lines = append(lines, fmt.Sprintf("ok   %s: %s", where, shown))
		case exists:
			lines = append(lines, fmt.Sprintf("MISSING %s: they run %s, which cannot run (a folder, or a file without permission to run) → %s", where, shown, fix))
		default:
			lines = append(lines, fmt.Sprintf("MISSING %s: they run %s, which does not exist → %s", where, shown, fix))
		}
	}
	for _, target := range hookTargets {
		where := "hooks in ~/" + filepath.ToSlash(target.rel)
		body, err := os.ReadFile(filepath.Join(home, target.rel))
		if os.IsNotExist(err) {
			continue
		}
		var config map[string]any
		if err == nil && strings.TrimSpace(string(body)) != "" { // an empty file is the same as {} (as editHooks reads it)
			err = json.NewDecoder(strings.NewReader(string(body))).Decode(&config)
		}
		if err != nil {
			lines = append(lines, fmt.Sprintf("WARN %s: cannot read it (%v) → fix it by hand; hook install --yes stops on it too", where, err))
			continue
		}
		var bins, relative, hand []string
		seen := map[string]bool{}
		hooks, _ := config["hooks"].(map[string]any)
		for _, groups := range hooks {
			groups, _ := groups.([]any)
			for _, group := range groups {
				group, _ := group.(map[string]any)
				handlers, _ := group["hooks"].([]any)
				for _, handler := range handlers {
					handler, _ := handler.(map[string]any)
					command, _ := handler["command"].(string)
					bin, _ := hookBinary(command)
					switch {
					case isShikiiHook(command) && (filepath.IsAbs(bin) || !strings.Contains(bin, "/")):
						if !seen[bin] {
							seen[bin] = true
							bins = append(bins, bin)
						}
					case isShikiiHook(command): // ~/… or another relative path: the shell decides what it runs, and install rewrites it
						if !seen[command] {
							seen[command] = true
							relative = append(relative, command)
						}
					case handWrittenShikiiHook(command):
						if !seen[command] {
							seen[command] = true
							hand = append(hand, command)
						}
					}
				}
			}
		}
		sort.Strings(bins) // the same order on every run
		for _, bin := range bins {
			report(where, bin, fix)
		}
		sort.Strings(relative)
		for _, command := range relative {
			lines = append(lines, fmt.Sprintf("note %s: %s names shikii by a relative path, so doctor did not check what it runs; hook install --yes replaces it with this shikii's absolute path", where, command))
		}
		sort.Strings(hand)
		for _, command := range hand {
			lines = append(lines, fmt.Sprintf("note %s: %s was written by hand, so doctor did not check what it runs; hook install --yes leaves it as it is and adds shikii's own hook, so you may have two: remove yours if so", where, command))
		}
	}
	codex := "Codex's MCP server in ~/.codex/config.toml"
	b, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	var m map[string]any
	if err == nil {
		m, err = readCodexConfig(b)
	}
	switch {
	case os.IsNotExist(err):
	case err != nil:
		lines = append(lines, fmt.Sprintf("WARN %s: cannot read it (%v) → fix it by hand; Codex cannot read it either", codex, err))
	default:
		server, _ := codexShikiiServer(m)
		t, _ := server.(map[string]any)
		if bin, ok := t["command"].(string); ok {
			codexFix := fix
			if _, fixable := codexServerFixed(b, codexServerArgs, exe); !fixable {
				codexFix = "hook install --yes does not rewrite this block (it was edited by hand, is in an older version's form, or names a path that exists but cannot run): set command = " + quotedList([]string{exe}) + " under [mcp_servers.shikii] in ~/.codex/config.toml by hand (Codex; reopen Codex)"
			}
			report(codex, bin, codexFix)
		}
	}
	return lines
}

// hookBinaryRuns says whether bin exists, and whether it is a file the shell can run: a regular file with permission to run. A bare name
// is looked up on PATH.
func hookBinaryRuns(bin string) (exists, runs bool) {
	if !strings.Contains(bin, "/") {
		_, err := exec.LookPath(bin)
		return err == nil, err == nil
	}
	info, err := os.Stat(bin)
	if err != nil {
		return false, false
	}
	return true, info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func orElse(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
