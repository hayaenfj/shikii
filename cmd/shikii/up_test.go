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

// 鍵があって Jev を使う間は、shikii up は手元のモデル(llama-server :8086)を起こさない(本人の決定、2026-10-23)。
// SHIKII_MODEL=local なら起こしてよい。
func TestUpDoesNotStartTheLocalModelWhileJevIsUsed(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("SHIKII_MODEL", "")
	err := cmdUp(nil)
	if err == nil || !strings.Contains(err.Error(), "SHIKII_MODEL=local") || !strings.Contains(err.Error(), "Jev") {
		t.Fatalf("鍵があれば起こさず、手元を使う方法(SHIKII_MODEL=local)を伝える: %v", err)
	}
	t.Setenv("SHIKII_MODEL", "local")
	if msg := upBlockedByHosted(); msg != "" {
		t.Fatalf("SHIKII_MODEL=local なら起こしてよい: %s", msg)
	}
	t.Setenv("SHIKII_MODEL", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	if msg := upBlockedByHosted(); msg != "" {
		t.Fatalf("鍵が無ければ起こしてよい: %s", msg)
	}
}

// 監査 0.8.1 の 2: 開発用の Makefile も手元の標準(shikii up)へ向け、使わない Kev-4B を取りに行かない。
// drift のモデル名は較正の鍵(応答の model。jev-1.14.0)に揃える(鍵が合わないと drift は「no current calibration」で落ちる。監査 0.8.2-2 の A2)。
// 監査 0.8.2-1 の 6: 文字列の有無ではなく、make -n で実際に走る手順を見る(build を先に組む、drift が MODEL を使う)。
func TestMakefileUsesTheLocalStandard(t *testing.T) {
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), "kev-4b") {
		t.Fatal("Makefile が Kev-4B を指している")
	}
	plan := func(target string) []string {
		out, err := exec.Command("make", "-C", "../..", "-n", target).CombinedOutput()
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", target, err, out)
		}
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}
	server := plan("server")
	if !slices.Equal(server, []string{"go build -o bin/shikii ./cmd/shikii", "bin/shikii up"}) {
		t.Fatalf("make server は Go の本体だけを組んで(画面は組み直さない)から bin/shikii up を起こす: %q", server)
	}
	drift := plan("drift")
	if last := drift[len(drift)-1]; !strings.HasPrefix(last, "bin/shikii drift ") || !strings.Contains(last, "--model 'jev-1.14.0'") {
		t.Fatalf("make drift は較正の鍵になる応答の model(jev-1.14.0)で測る: %q", drift)
	}
}
