package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 後ろの口が決定モデルか(本家 llama.cpp は普通の GGUF だと /v1/systemone を 501 で蹴る)を見分ける。
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

// up は何を起こすかを先に決める: 決定モデルならそのまま、普通の GGUF なら readout を前に立てる。llama-server が無ければ入れ方を言う。
func TestUpPlan(t *testing.T) {
	p := upPlan(upOpts{hf: "ggml-org/Kev-4B-GGUF:Q4_K_M", port: 8080}, "/opt/homebrew/bin/llama-server")
	if len(p.server) == 0 || p.server[0] != "/opt/homebrew/bin/llama-server" || !contains(p.server, "-hf") || !contains(p.server, "ggml-org/Kev-4B-GGUF:Q4_K_M") || !contains(p.server, "8080") {
		t.Fatalf("server command: %v", p.server)
	}
	p = upPlan(upOpts{gguf: "/models/x.gguf", port: 8086, alias: "x"}, "/opt/homebrew/bin/llama-server")
	for _, want := range []string{"-m", "/models/x.gguf", "-fa", "on", "-ctk", "q8_0", "-np", "1", "--alias", "x"} {
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

// doctor は、何が動いていて何が足りないかを 1 画面で言う。足りないものには直し方を添える。
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
	// local が落ちていれば、起こし方(shikii up)を言う
	out = capture(t, func() error {
		return cmdDoctor([]string{"--specs", dir, "--store", dir + "/.shikii", "--local", "http://127.0.0.1:1", "--hosted", ""})
	})
	if !strings.Contains(out, "shikii up") {
		t.Fatalf("doctor must tell how to start a local model:\n%s", out)
	}
}
