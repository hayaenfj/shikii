package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
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
