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

// shikii up: 手元のモデルを 1 コマンドで用意する。
//   - llama-server を探して裏のポート(--port + 6。既定 8086)で起こす(既定は Ternary-Bonsai-27B Q2_g64。初回は取得に時間がかかる)
//   - /health が ok になるまで待つ
//   - 前に shikii-readout を --port(既定 8080)で立てる。後ろが普通の GGUF なら文字の確率を読み出し、学習済みの決定モデル(OpenJev など)なら
//     そのまま通す。shikii・hook・MCP は常に --port を見ればよく、モデルの種類で接続先を変えない
//   - Ctrl-C で両方止める
// shikii doctor: 何が動いていて何が足りないかを 1 画面で言う(足りないものには直し方)。

// defaultModel は既定のモデル(普通のチャット用 GGUF。本家 llama.cpp と shikii-readout で決定モデルの速さで使う。
// 2026-10-17 の実測で prompt-intent top-1 0.968、日本語 1.000。約 7.6 GB)。
// 「Q2_g64」は Hugging Face の量子化タグとして通らないので、":" の後にファイル名を書く(-hf <repo> --hff <file> に展開)。
const defaultModel = "prism-ml/Ternary-Bonsai-27B-gguf:Ternary-Bonsai-27B-Q2_g64.gguf"

// hfArgs は -hf の値を llama-server の引数にする。"<repo>:<file>.gguf" ならファイル名で指定し、それ以外("<repo>[:quant]")はそのまま渡す。
func hfArgs(hf string) []string {
	if i := strings.LastIndex(hf, ":"); i > 0 && strings.HasSuffix(hf, ".gguf") {
		return []string{"-hf", hf[:i], "--hff", hf[i+1:]}
	}
	return []string{"-hf", hf}
}

// llamaPortOffset は readout の口(--port)から llama-server の裏の口までの固定の差(オプションは無い)。
const llamaPortOffset = 6

type upOpts struct {
	hf, gguf, alias string
	port            int // shikii が見る口(shikii-readout)
	ctx             int
}

type plan struct {
	server    []string // llama-server のコマンド(裏の口で起こす)
	llamaPort int      // 裏の口
	install   string   // llama-server が無い時の入れ方
}

// upPlan は何を起こすかを決める(実行はしない。テストで確かめる部分)。
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

// probe は口が生きているか、モデル名、決定モデルか(/v1/systemone が 501 を返さないか)を見る。
func probe(base string) probeResult {
	c := &http.Client{Timeout: 3 * time.Second}
	var p probeResult
	resp, err := c.Get(strings.TrimRight(base, "/") + "/health")
	if err != nil {
		p.note = err.Error()
		return p
	}
	resp.Body.Close()
	p.up = resp.StatusCode == http.StatusOK // 4xx は「何か別のものがいる」。生きているとは言わない
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

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	var o upOpts
	fs.StringVar(&o.hf, "hf", defaultModel, "Hugging Face のモデル(llama-server -hf に渡す)")
	fs.StringVar(&o.gguf, "gguf", "", "手元の GGUF ファイル(あれば -hf より優先)")
	fs.StringVar(&o.alias, "alias", "", "GGUF の時の名前(較正の鍵になる)")
	fs.IntVar(&o.port, "port", 8080, "shikii が見る口(shikii-readout)。llama-server はその 6 つ上の口で裏に立つ")
	fs.IntVar(&o.ctx, "ctx", 4096, "文脈長(GGUF の時)")
	if err := fs.Parse(args); err != nil {
		return err
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
	go func() { exited <- srv.Wait() }() // 子が死んだらすぐ分かるように(ProcessState は Wait の後にしか立たない)
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
	// 前に readout を立てる。普通の GGUF なら読み出し、決定モデルならそのまま通す(readout が自分で見分ける)
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

// findReadout は shikii-readout を、この実行ファイルの隣 → PATH の順で探す。
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
	specsDir := fs.String("specs", "", "spec のフォルダ(既定は <store>/decisions)")
	storeDir := fs.String("store", ".shikii", "記録の置き場")
	local := fs.String("local", "http://127.0.0.1:8080", "手元のモデルの URL")
	hosted := fs.String("hosted", "https://api.typesafe.ai", "ホスト版の URL(空なら見ない)")
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
	// 1. 道具
	llama, _ := exec.LookPath("llama-server")
	fmt.Fprintf(stdout, "%s llama-server: %s\n", ok(llama != ""), orElse(llama, "not in PATH → brew install llama.cpp"))
	readout, err := findReadout()
	fmt.Fprintf(stdout, "%s shikii-readout: %s\n", ok(err == nil), orElse(readout, "not found → make install"))
	// 2. 手元のモデル
	if *local != "" {
		pr := probe(*local)
		switch {
		case !pr.up:
			fmt.Fprintf(stdout, "MISSING local model at %s: unreachable → shikii up   (starts %s behind shikii-readout; or shikii up --gguf <file> for any GGUF)\n", *local, defaultModel)
		case pr.decisionModel:
			fmt.Fprintf(stdout, "ok   local model at %s: %s (decision model)\n", *local, pr.model)
		default:
			fmt.Fprintf(stdout, "WARN local model at %s: %s is a plain chat model → put shikii-readout in front (shikii up does this)\n", *local, pr.model)
		}
	}
	// 3. ホスト版
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
	// 4. spec と store
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

func orElse(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
