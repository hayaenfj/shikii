package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// shikii up: prepares the local model with one command.
//   - finds llama-server and starts it on the back port (--port + 6, default 8086) (the default is Ternary-Bonsai-27B Q2_g64; the first download takes time)
//   - waits until /health is ok
//   - puts shikii-readout in front on --port (default 8080). If the back end is a plain GGUF it reads out the token probabilities; if it is a trained decision model (OpenJev etc.)
//     it passes requests through. shikii, the hook and MCP always just look at --port and never change where they connect by model type
//   - Ctrl-C stops both
// shikii doctor: says on one screen what is running and what is missing (with how to fix what is missing).

// defaultModel is the default model (a plain chat GGUF, used at decision-model speed with upstream llama.cpp and shikii-readout.
// Measured on 2026-10-17: prompt-intent top-1 0.968, Japanese 1.000. About 7.6 GB).
// "Q2_g64" does not pass as a Hugging Face quantization tag, so the file name goes after ":" (expanded to -hf <repo> --hff <file>).
const defaultModel = "prism-ml/Ternary-Bonsai-27B-gguf:Ternary-Bonsai-27B-Q2_g64.gguf"

// hfArgs turns the -hf value into llama-server arguments. "<repo>:<file>.gguf" is given by file name; anything else ("<repo>[:quant]") is passed as is.
func hfArgs(hf string) []string {
	if i := strings.LastIndex(hf, ":"); i > 0 && strings.HasSuffix(hf, ".gguf") {
		return []string{"-hf", hf[:i], "--hff", hf[i+1:]}
	}
	return []string{"-hf", hf}
}

// llamaPortOffset is the fixed offset from readout's port (--port) to llama-server's back port (there is no option).
const llamaPortOffset = 6

type upOpts struct {
	hf, gguf, alias string
	port            int // the port shikii looks at (shikii-readout)
	ctx             int
}

type plan struct {
	server    []string // llama-server command (started on the back port)
	llamaPort int      // back port
	install   string   // how to install llama-server when missing
}

// upPlan decides what to start (it does not run anything; this is the part tests check).
func upPlan(o upOpts, llamaPath string) plan {
	if llamaPath == "" {
		return plan{install: "llama-server not found. Install llama.cpp (brew install llama.cpp, or https://github.com/ggml-org/llama.cpp/releases) and run shikii up again"}
	}
	if strings.Contains(strings.ToLower(o.hf+" "+o.gguf+" "+o.alias), "kev-4b") {
		return plan{install: "Kev-4B is not supported (project decision, 2026-10-23). Use the default local model (shikii up, Ternary-Bonsai-27B), any other GGUF (shikii up --gguf <file>), or TypeSafe Jev with TYPESAFE_API_KEY set"}
	}
	llamaPort := o.port + llamaPortOffset
	args := []string{llamaPath, "--port", fmt.Sprint(llamaPort), "--host", "127.0.0.1"}
	switch {
	case o.gguf != "":
		args = append(args, "-m", o.gguf, "-c", fmt.Sprint(o.ctx), "-ngl", "99", "-fa", "on", "-ctk", "q8_0", "-ctv", "q8_0", "-np", "1")
		if o.alias != "" {
			args = append(args, "--alias", o.alias)
		}
	default:
		args = append(append(args, hfArgs(o.hf)...), "-fa", "on", "-ctk", "q8_0", "-ctv", "q8_0", "-np", "1")
	}
	return plan{server: args, llamaPort: llamaPort}
}

type probeResult struct {
	up            bool
	decisionModel bool
	model         string
	note          string
}

// probe checks whether the port is alive, the model name, and whether it is a decision model (/v1/systemone does not return 501).
func probe(base string) probeResult {
	c := &http.Client{Timeout: 3 * time.Second}
	var p probeResult
	resp, err := c.Get(strings.TrimRight(base, "/") + "/health")
	if err != nil {
		p.note = err.Error()
		return p
	}
	resp.Body.Close()
	p.up = resp.StatusCode == http.StatusOK // 4xx means "something else is there". Do not call it alive
	if r, err := c.Get(strings.TrimRight(base, "/") + "/v1/models"); err == nil {
		var m struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		r.Body.Close()
		switch {
		case len(m.Models) > 0:
			p.model = m.Models[0].Name
		case len(m.Data) > 0:
			p.model = m.Data[0].ID
		}
	}
	body := `{"state":"probe","questions":{"q":{"type":"noul","instructions":"Is this a probe?"}}}`
	r, err := c.Post(strings.TrimRight(base, "/")+"/v1/systemone", "application/json", strings.NewReader(body))
	if err != nil {
		p.note = err.Error()
		return p
	}
	r.Body.Close()
	p.decisionModel = r.StatusCode < 400
	if r.StatusCode == 501 {
		p.note = "not a decision model (plain chat GGUF): shikii-readout is needed in front"
	}
	return p
}

// upBlockedByHosted returns the reason not to start the local model when there is a key and decisions go to TypeSafe's Jev (SHIKII_MODEL is not local).
// While Jev is active, the llama-server on :8086 is not started (the owner's decision, 2026-10-23). Empty means it may start.
func upBlockedByHosted() string {
	if !usesHosted() {
		return ""
	}
	return "TYPESAFE_API_KEY is set, so decisions go to TypeSafe's Jev and the local model (llama-server) is not started. To use the local model, set SHIKII_MODEL=local and then run shikii up"
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	var o upOpts
	fs.StringVar(&o.hf, "hf", defaultModel, "Hugging Face model (passed to llama-server -hf)")
	fs.StringVar(&o.gguf, "gguf", "", "local GGUF file (takes priority over -hf)")
	fs.StringVar(&o.alias, "alias", "", "name for a GGUF (becomes the calibration key)")
	fs.IntVar(&o.port, "port", 8080, "the port shikii looks at (shikii-readout). llama-server runs behind it on the port 6 above")
	fs.IntVar(&o.ctx, "ctx", 4096, "context length (for a GGUF)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if msg := upBlockedByHosted(); msg != "" {
		return errors.New(msg)
	}
	llama, _ := exec.LookPath("llama-server")
	p := upPlan(o, llama)
	if p.install != "" {
		return errors.New(p.install)
	}
	readout, err := findReadout()
	if err != nil {
		return err
	}
	front := fmt.Sprintf("http://127.0.0.1:%d", o.port)
	base := fmt.Sprintf("http://127.0.0.1:%d", p.llamaPort)
	if pr := probe(front); pr.up {
		fmt.Fprintf(stdout, "something already answers on %s (model=%s). Use --port for another one, or just point shikii at it\n", front, pr.model)
		return nil
	}
	if pr := probe(base); pr.up {
		return fmt.Errorf("something already answers on %s (model=%s), where llama-server would start. Use --port for another pair of ports", base, pr.model)
	}
	fmt.Fprintln(stdout, "starting:", strings.Join(p.server, " "))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := exec.CommandContext(ctx, p.server[0], p.server[1:]...)
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "waiting for /health (the first run downloads the model; this can take minutes) …")
	exited := make(chan error, 1)
	go func() { exited <- srv.Wait() }() // so we notice right away if the child dies (ProcessState is set only after Wait)
	var pr probeResult
	healthy := false
	deadline := time.After(20 * time.Minute)
wait:
	for {
		select {
		case err := <-exited:
			return fmt.Errorf("llama-server exited before becoming healthy: %v", err)
		case <-deadline:
			break wait
		case <-time.After(2 * time.Second):
			if pr = probe(base); pr.up {
				healthy = true
				break wait
			}
		}
	}
	if !healthy {
		return errors.New("llama-server did not become healthy in 20 minutes")
	}
	// Put readout in front. For a plain GGUF it reads out; for a decision model it passes through (readout tells them apart itself)
	ro := exec.CommandContext(ctx, readout, "--upstream", base, "--port", fmt.Sprint(o.port))
	ro.Stdout, ro.Stderr = os.Stderr, os.Stderr
	if err := ro.Start(); err != nil {
		return err
	}
	roExited := make(chan error, 1)
	go func() { roExited <- ro.Wait() }()
	for ready := false; !ready; {
		select {
		case err := <-roExited:
			return fmt.Errorf("shikii-readout exited before becoming healthy: %v", err)
		case err := <-exited:
			return fmt.Errorf("llama-server stopped: %v", err)
		case <-time.After(time.Second):
			ready = probe(front).up
		}
	}
	kind := "plain chat GGUF, read out by shikii-readout"
	if pr.decisionModel {
		kind = "decision model, passed through by shikii-readout"
	}
	fmt.Fprintf(stdout, "\nready. model=%s (%s)\n  shikii decide <spec> --local %s -i …\n  shikii serve --local %s\nCtrl-C stops everything.\n", probe(front).model, kind, front, front)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		cancel()
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("llama-server stopped: %w", err)
		}
	case err := <-roExited:
		if err != nil {
			return fmt.Errorf("shikii-readout stopped: %w", err)
		}
	}
	<-roExited
	return nil
}

// findReadout looks for shikii-readout next to this executable, then on PATH.
func findReadout() (string, error) {
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "shikii-readout")
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("shikii-readout"); err == nil {
		return p, nil
	}
	return "", errors.New("shikii-readout not found next to shikii or in PATH (make install puts both in ~/.local/bin)")
}

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
	fmt.Fprintf(stdout, "%s shikii-readout: %s\n", ok(err == nil), orElse(readout, "not found → make install"))
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
		switch {
		case !pr.up && usesHosted():
			fmt.Fprintf(stdout, "ok   local model at %s: not running (not needed while Jev is used)\n", *local)
		case !pr.up:
			fmt.Fprintf(stdout, "MISSING local model at %s: unreachable → shikii up   (starts %s behind shikii-readout; or shikii up --gguf <file> for any GGUF)\n", *local, defaultModel)
		case pr.decisionModel:
			fmt.Fprintf(stdout, "ok   local model at %s: %s (decision model)\n", *local, pr.model)
		default:
			fmt.Fprintf(stdout, "WARN local model at %s: %s is a plain chat model → put shikii-readout in front (shikii up does this)\n", *local, pr.model)
		}
	}
	// 3. hosted
	if *hosted != "" {
		key := hostedKey()
		c := &http.Client{Timeout: 4 * time.Second}
		_, herr := c.Get(strings.TrimRight(*hosted, "/") + "/")
		switch {
		case herr != nil:
			fmt.Fprintf(stdout, "WARN hosted %s: unreachable (%v)\n", *hosted, herr)
		case key == "":
			fmt.Fprintf(stdout, "WARN hosted %s: reachable, but no key in DECISIONS_API_KEY / TYPESAFE_API_KEY (hosted will not be used)\n", *hosted)
		default:
			fmt.Fprintf(stdout, "ok   hosted %s: reachable, key present\n", *hosted)
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
func registeredBinaryLines(home, exe string) []string {
	type use struct{ where, bin string }
	var uses []use
	for _, target := range hookTargets {
		body, err := os.ReadFile(filepath.Join(home, target.rel))
		if err != nil {
			continue
		}
		var config struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(body, &config) != nil {
			continue
		}
		seen := map[string]bool{}
		for _, groups := range config.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if bin, ok := hookBinary(hook.Command); ok && isShikiiHook(hook.Command) && !seen[bin] {
						seen[bin] = true
						uses = append(uses, use{"hooks in ~/" + filepath.ToSlash(target.rel), bin})
					}
				}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml")); err == nil {
		if m, err := readCodexConfig(b); err == nil {
			if server, ok := codexShikiiServer(m); ok {
				if t, ok := server.(map[string]any); ok {
					if bin, ok := t["command"].(string); ok {
						uses = append(uses, use{"Codex's MCP server in ~/.codex/config.toml", bin})
					}
				}
			}
		}
	}
	fix := "run " + shellQuote(exe) + " hook install --yes to point them at this shikii, or install again with the one line in the README (it puts shikii in ~/.local/bin)"
	var lines []string
	for _, u := range uses {
		found := fileExists(u.bin)
		if !filepath.IsAbs(u.bin) {
			_, err := exec.LookPath(u.bin)
			found = err == nil
		}
		if found {
			lines = append(lines, fmt.Sprintf("ok   %s: %s", u.where, u.bin))
		} else {
			lines = append(lines, fmt.Sprintf("MISSING %s: they run %s, which does not exist → %s", u.where, u.bin, fix))
		}
	}
	return lines
}

func orElse(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
