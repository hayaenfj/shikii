package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Tell whether the back port is a decision model (upstream llama.cpp rejects /v1/systemone with 501 for a plain GGUF).
func TestProbeDecisionModel(t *testing.T) {
	decision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"kev-4b"}]}`))
		case "/v1/systemone":
			w.Write([]byte(`{"model":"kev-4b","answers":{}}`))
		}
	}))
	t.Cleanup(decision.Close)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"qwen3-8b"}]}`))
		case "/v1/systemone":
			w.WriteHeader(501)
			w.Write([]byte(`{"error":{"message":"This model is not a decision model"}}`))
		}
	}))
	t.Cleanup(plain.Close)
	if p := probe(decision.URL); !p.up || !p.decisionModel || p.model != "kev-4b" {
		t.Fatalf("decision model server: %+v", p)
	}
	if p := probe(plain.URL); !p.up || p.decisionModel || p.model != "qwen3-8b" {
		t.Fatalf("plain chat model server must be up but not a decision model: %+v", p)
	}
	if p := probe("http://127.0.0.1:1"); p.up {
		t.Fatalf("closed port must be down: %+v", p)
	}
}

// up decides first what to start: a decision model as is, a plain GGUF with readout in front. Without llama-server it says how to install it.
func TestUpPlan(t *testing.T) {
	p := upPlan(upOpts{hf: defaultModel, port: 8080}, "/opt/homebrew/bin/llama-server")
	if len(p.server) == 0 || p.server[0] != "/opt/homebrew/bin/llama-server" || !contains(p.server, "-hf") || !contains(p.server, "prism-ml/Ternary-Bonsai-27B-gguf") || !contains(p.server, "--hff") || !contains(p.server, "Ternary-Bonsai-27B-Q2_g64.gguf") || !contains(p.server, "8086") || contains(p.server, "8080") || p.llamaPort != 8086 {
		t.Fatalf("llama-server must start on the back port (--port + 6) with the file named explicitly (Q2_g64 is not a Hugging Face quant tag), leaving --port to shikii-readout: %v", p.server)
	}
	if strings.Contains(defaultModel, "Kev") {
		t.Fatal("the default model is the plain GGUF that measured best (Ternary-Bonsai-27B), not Kev")
	}
	if q := upPlan(upOpts{hf: "ggml-org/OpenJev-GGUF:Q4_K_M", port: 8080}, "/opt/homebrew/bin/llama-server"); !contains(q.server, "ggml-org/OpenJev-GGUF:Q4_K_M") || contains(q.server, "--hff") {
		t.Fatalf("a quant tag is passed to -hf as is: %v", q.server)
	}
	for _, o := range []upOpts{{hf: "ggml-org/Kev-4B-GGUF:Q4_K_M", port: 8080}, {gguf: "/models/kev-4b-q4.gguf", port: 8080}} {
		if q := upPlan(o, "/opt/homebrew/bin/llama-server"); len(q.server) != 0 || !strings.Contains(q.install, "Kev-4B is not supported") {
			t.Fatalf("Kev-4B is refused (project decision 2026-10-23): %+v", q)
		}
	}
	p = upPlan(upOpts{gguf: "/models/x.gguf", port: 9000, alias: "x"}, "/opt/homebrew/bin/llama-server")
	for _, want := range []string{"-m", "/models/x.gguf", "-fa", "on", "-ctk", "q8_0", "-np", "1", "--alias", "x", "9006"} {
		if !contains(p.server, want) {
			t.Fatalf("gguf server command must carry %q: %v", want, p.server)
		}
	}
	p = upPlan(upOpts{hf: "ggml-org/Kev-4B-GGUF:Q4_K_M", port: 8080}, "")
	if p.install == "" || !strings.Contains(p.install, "llama.cpp") {
		t.Fatalf("without llama-server the plan must say how to install it: %+v", p)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// doctor says on one screen what is running and what is missing, with how to fix what is missing.
func TestDoctorReportsWhatIsMissing(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"kev-4b"}]}`))
		case "/v1/systemone":
			w.Write([]byte(`{"model":"kev-4b","answers":{}}`))
		}
	}))
	t.Cleanup(up.Close)
	dir := t.TempDir()
	writeSpec(t, dir, "true")
	out := capture(t, func() error {
		return cmdDoctor([]string{"--specs", dir, "--store", dir + "/.shikii", "--local", up.URL, "--hosted", "http://127.0.0.1:1"})
	})
	for _, must := range []string{"kev-4b", "decision model", "1 spec", "hosted", "unreachable"} {
		if !strings.Contains(out, must) {
			t.Fatalf("doctor must mention %q:\n%s", must, out)
		}
	}
	// If local is down, it says how to start it (shikii up)
	out = capture(t, func() error {
		return cmdDoctor([]string{"--specs", dir, "--store", dir + "/.shikii", "--local", "http://127.0.0.1:1", "--hosted", ""})
	})
	if !strings.Contains(out, "shikii up") {
		t.Fatalf("doctor must tell how to start a local model:\n%s", out)
	}
}

// While there is a key and Jev is used, shikii up does not start the local model (llama-server :8086) (the owner's decision, 2026-10-23).
// With SHIKII_MODEL=local it may start it.
func TestUpDoesNotStartTheLocalModelWhileJevIsUsed(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("SHIKII_MODEL", "")
	err := cmdUp(nil)
	if err == nil || !strings.Contains(err.Error(), "SHIKII_MODEL=local") || !strings.Contains(err.Error(), "Jev") {
		t.Fatalf("with a key it must not start, and must say how to use the local one (SHIKII_MODEL=local): %v", err)
	}
	t.Setenv("SHIKII_MODEL", "local")
	if msg := upBlockedByHosted(); msg != "" {
		t.Fatalf("with SHIKII_MODEL=local it may start: %s", msg)
	}
	t.Setenv("SHIKII_MODEL", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	if msg := upBlockedByHosted(); msg != "" {
		t.Fatalf("without a key it may start: %s", msg)
	}
}

// Audit 0.8.1 item 2: the development Makefile also points to the local default (shikii up) and does not fetch the unused Kev-4B.
// drift's model name matches the calibration key (the response's model, jev-1.14.0) (if the key does not match, drift fails with "no current calibration". Audit 0.8.2-2 A2).
// Audit 0.8.2-1 item 6: look at the steps make -n actually runs, not whether a string is present (build first, drift uses MODEL).
func TestMakefileUsesTheLocalStandard(t *testing.T) {
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), "kev-4b") {
		t.Fatal("the Makefile points to Kev-4B")
	}
	plan := func(target string) []string {
		out, err := exec.Command("make", "--no-print-directory", "-C", "../..", "-n", target).CombinedOutput() // GNU make 4 prints an Entering directory line with -C (this failed on CI's ubuntu)
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", target, err, out)
		}
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}
	server := plan("server")
	if !slices.Equal(server, []string{"go build -o bin/shikii ./cmd/shikii", "bin/shikii up"}) {
		t.Fatalf("make server must build only the Go binary (not rebuild the page) and then start bin/shikii up: %q", server)
	}
	drift := plan("drift")
	if last := drift[len(drift)-1]; !strings.HasPrefix(last, "bin/shikii drift ") || !strings.Contains(last, "--model 'jev-1.14.0'") {
		t.Fatalf("make drift must measure with the response's model (jev-1.14.0), which is the calibration key: %q", drift)
	}
}

// When the binary the hooks and Codex's registration run is gone (a Go version manager removed its per-version folder), doctor says
// so for each of them and how to fix it; after the fix it names was followed (hook install --yes), it reports them as ok.
func TestDoctorSaysWhenTheRegisteredBinaryIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gone := filepath.Join(home, "go-1.27.2", "bin", "shikii")
	for _, target := range hookTargets {
		path := filepath.Join(home, target.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":` + strconv.Quote(hookCommand(gone, "stop")) + `}]}]}}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), codexWithServer(nil, gone, codexServerArgs), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdDoctor([]string{"--specs", home, "--store", home + "/.shikii", "--local", "", "--hosted", ""})
	})
	for _, where := range []string{"hooks in ~/.claude/settings.json", "hooks in ~/.codex/hooks.json", "Codex's MCP server in ~/.codex/config.toml"} {
		if !strings.Contains(out, "MISSING "+where+": they run "+gone+", which does not exist → run ") || !strings.Contains(out, "hook install --yes") {
			t.Fatalf("doctor must say that the %s run a binary that is gone, and how to fix it:\n%s", where, out)
		}
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	for _, line := range registeredBinaryLines(home, mcpCommand()) {
		if !strings.HasPrefix(line, "ok   ") {
			t.Fatalf("after hook install --yes every registration must run a binary that exists: %s", line)
		}
	}
	if n := len(registeredBinaryLines(home, mcpCommand())); n != 3 {
		t.Fatalf("doctor must report the 2 hook files and Codex's registration, reported %d", n)
	}
}

// writeHookFiles writes both hook settings files with one Stop hook running command.
func writeHookFiles(t *testing.T, home, command string) {
	t.Helper()
	for _, target := range hookTargets {
		path := filepath.Join(home, target.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":` + strconv.Quote(command) + `}]}]}}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A Codex block a person edited (here a line added to it) that runs a gone binary is left alone by hook install --yes, so doctor
// does not name that as the fix: it says what to change by hand. The block shikii wrote still gets hook install --yes as its fix.
func TestDoctorNamesAFixForCodexThatWorksForThatBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gone := filepath.Join(home, "go-1.27.2", "bin", "shikii")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, ".codex", "config.toml")
	edited := string(codexWithServer(nil, gone, codexServerArgs)) + "env = { SHIKII_MODEL = \"local\" }\n"
	if err := os.WriteFile(config, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	codexLine := func() string {
		for _, line := range registeredBinaryLines(home, mcpCommand()) {
			if strings.Contains(line, "Codex's MCP server") {
				return line
			}
		}
		return ""
	}
	before := codexLine()
	if !strings.HasPrefix(before, "MISSING ") || strings.Contains(before, "hook install --yes to point") || !strings.Contains(before, "command = "+strconv.Quote(mcpCommand())) {
		t.Fatalf("for a block hook install leaves alone, doctor must say what to write by hand (command = %q), not hook install --yes: %s", mcpCommand(), before)
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if b, _ := os.ReadFile(config); string(b) != edited {
		t.Fatalf("hook install must leave the edited block alone (so the hand edit is the fix):\n%s", b)
	}
	if err := os.WriteFile(config, codexWithServer(nil, gone, codexServerArgs), 0o644); err != nil {
		t.Fatal(err)
	}
	if line := codexLine(); !strings.Contains(line, "hook install --yes") {
		t.Fatalf("for the block shikii wrote, doctor must name hook install --yes: %s", line)
	}
}

// doctor reads only the forms shikii writes (a bare absolute path, or a path in single quotes, then " hook <event>"). A shikii hook a
// person wrote another way (~, $HOME, double quotes, a backslash) gets one line saying it was written by hand and not checked, with its
// command: never ok or MISSING, which doctor cannot know without reading it as a shell would. After hook install --yes the person's
// line is still there next to shikii's own hook, and the line says so (they may now have two). Another tool's hook says nothing.
func TestDoctorSaysAHandWrittenShikiiHookWasNotChecked(t *testing.T) {
	home := filepath.Join(t.TempDir(), "a b")
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin", "shikii")
	spaced := filepath.Join(home, "my dir", "shikii")
	for _, b := range []string{bin, spaced} {
		if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(b, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []string{
		"~/.local/bin/shikii hook stop",
		"$HOME/.local/bin/shikii hook stop", // with a space in HOME the shell splits it: it does not run
		"${HOME}/.local/bin/shikii hook stop",
		`"$HOME/.local/bin/shikii" hook stop`,
		`"$HOME"/go/bin/shikii hook stop`,                    // the binary is gone
		strings.ReplaceAll(spaced, " ", `\ `) + " hook stop", // the binary exists
		strconv.Quote(spaced) + " hook stop",
		"/gone/shikii hook stop 2>/dev/null", // more words after the event (0.10's last audit: it was skipped silently)
	} {
		writeHookFiles(t, home, command)
		lines := registeredBinaryLines(home, mcpCommand())
		if len(lines) != 2 {
			t.Fatalf("%s: doctor must say one line for each hook file, said %q", command, lines)
		}
		for _, line := range lines {
			said := strings.Contains(line, "by hand") && strings.Contains(line, "two")
			if !strings.HasPrefix(line, "note ") || !strings.Contains(line, command) || !said {
				t.Fatalf("%s: doctor must say it did not check the hook, and what hook install does with it: %s", command, line)
			}
		}
	}
	writeHookFiles(t, home, `"$HOME"/go/bin/shikii hook stop`)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	got := readHookCommands(t, filepath.Join(home, ".claude", "settings.json"))
	if !got[`"$HOME"/go/bin/shikii hook stop`] {
		t.Fatalf("hook install must leave the person's hand-written hook, the hooks are %v", got)
	}
	notes, oks := 0, 0
	for _, line := range registeredBinaryLines(home, mcpCommand()) {
		switch {
		case strings.HasPrefix(line, "note "):
			notes++
		case strings.HasPrefix(line, "ok   "):
			oks++
		default:
			t.Fatalf("after hook install --yes doctor must say ok for shikii's hook and note the hand-written one: %s", line)
		}
	}
	if notes != 2 || oks != 2 {
		t.Fatalf("doctor must say ok for shikii's hook and note the hand-written one in each file, said %d ok and %d notes", oks, notes)
	}
	for command, want := range map[string]string{
		`"/opt/tools/shikii-notify" hook done`: "",         // another tool
		"shikii-notify hook done":              "",         // another tool
		"'" + spaced + "' hook stop":           "ok   ",    // shikii's form
		"'/gone dir/shikii' hook stop":         "MISSING ", // shikii's form, gone
	} {
		writeHookFiles(t, home, command)
		lines := registeredBinaryLines(home, mcpCommand())
		if want == "" {
			if len(lines) != 0 {
				t.Fatalf("%s: another tool's hook must say nothing, said %q", command, lines)
			}
			continue
		}
		if len(lines) != 2 {
			t.Fatalf("%s: doctor must report both hook files, said %q", command, lines)
		}
		for _, line := range lines {
			if !strings.HasPrefix(line, want) {
				t.Fatalf("%s: doctor must say %q, says %s", command, want, line)
			}
		}
	}
}

// A hook that points at a folder, or at a file without permission to run, does not run shikii: doctor does not report it as ok.
func TestDoctorDoesNotCallAFolderOrAFileThatCannotRunOk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "shikii-dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(home, "shikii-plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(home, "shikii-runs")
	if err := os.WriteFile(runs, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for bin, want := range map[string]string{dir: "MISSING ", plain: "MISSING ", runs: "ok   "} {
		writeHookFiles(t, home, bin+" hook stop")
		for _, line := range registeredBinaryLines(home, mcpCommand()) {
			if !strings.HasPrefix(line, want) {
				t.Fatalf("a hook running %s: doctor must say %q, says %s", bin, want, line)
			}
			if want == "MISSING " && !strings.Contains(line, "cannot run") {
				t.Fatalf("doctor must say that %s exists but cannot run: %s", bin, line)
			}
		}
	}
}

// A hook settings file doctor cannot read (not JSON, or no permission to read) is named instead of passed over in silence; an empty
// file, which the agents read as no settings, is not.
func TestDoctorSaysWhenItCannotReadTheHookSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeHookFiles(t, home, "/gone/shikii hook stop")
	settings := filepath.Join(home, ".claude", "settings.json")
	b, _ := os.ReadFile(settings)
	if err := os.WriteFile(settings, append([]byte("// my settings\n"), b...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[mcp_servers.shikii\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wants := []string{"WARN hooks in ~/.claude/settings.json: cannot read it", "WARN Codex's MCP server in ~/.codex/config.toml: cannot read it"}
	if os.Getuid() != 0 { // root reads a file without permission bits
		if err := os.Chmod(filepath.Join(home, ".codex", "hooks.json"), 0); err != nil {
			t.Fatal(err)
		}
		wants = append(wants, "WARN hooks in ~/.codex/hooks.json: cannot read it")
	}
	lines := strings.Join(registeredBinaryLines(home, mcpCommand()), "\n")
	for _, want := range wants {
		if !strings.Contains(lines, want) {
			t.Fatalf("doctor must say %q, says:\n%s", want, lines)
		}
	}
	os.Chmod(filepath.Join(home, ".codex", "hooks.json"), 0o644)
	for _, target := range hookTargets {
		if err := os.WriteFile(filepath.Join(home, target.rel), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(filepath.Join(home, ".codex", "config.toml"))
	if lines := registeredBinaryLines(home, mcpCommand()); len(lines) != 0 {
		t.Fatalf("empty settings files have no hooks and nothing to warn about: %q", lines)
	}
}

// A Codex block in the form shikii writes that names a folder is not rewritten by hook install (the path exists), and doctor's reason
// names that case too instead of saying only that it was edited by hand (0.10's audits).
func TestDoctorGivesTheRightReasonForACodexBlockNamingAFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	folder := filepath.Join(home, "go", "bin", "shikii")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), codexWithServer(nil, folder, codexServerArgs), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, line := range registeredBinaryLines(home, mcpCommand()) {
		if strings.Contains(line, "Codex's MCP server") {
			if !strings.Contains(line, "names a path that exists but cannot run") {
				t.Fatalf("doctor gives the reason that fits a folder: %s", line)
			}
			return
		}
	}
	t.Fatal("doctor says something about the Codex block")
}

// doctor names a chained command that runs shikii as written by hand, never as a shikii binary that does not exist.
func TestDoctorNamesAChainedCommandAsWrittenByHand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	command := "/usr/bin/true && " + filepath.Join(home, "bin", "shikii") + " hook stop"
	writeHookFiles(t, home, command)
	for _, line := range registeredBinaryLines(home, mcpCommand()) {
		if strings.HasPrefix(line, "MISSING") || !strings.Contains(line, "by hand") {
			t.Fatalf("doctor names the chained command as written by hand: %s", line)
		}
	}
}
