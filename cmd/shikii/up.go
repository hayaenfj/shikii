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
//   - llama-server を探して起こす(既定は Kev-4B。初回は取得に時間がかかる)
//   - /health が ok になるまで待ち、/v1/systemone が通るか(決定モデルか)を見る
//   - 普通の GGUF なら shikii-readout を前に立てて、decide に渡す URL を言う
//   - Ctrl-C で両方止める
// shikii doctor: 何が動いていて何が足りないかを 1 画面で言う(足りないものには直し方)。

type upOpts struct {
	hf, gguf, alias string
	port, readout   int
	ctx             int
}

type plan struct {
	server  []string // llama-server のコマンド
	install string   // llama-server が無い時の入れ方
}

// upPlan は何を起こすかを決める(実行はしない。テストで確かめる部分)。
func upPlan(o upOpts, llamaPath string) plan {
	if llamaPath == "" {
		return plan{install: "llama-server not found. Install llama.cpp (brew install llama.cpp, or https://github.com/ggml-org/llama.cpp/releases) and run shikii up again"}
	}
	args := []string{llamaPath, "--port", fmt.Sprint(o.port), "--host", "127.0.0.1"}
	switch {
	case o.gguf != "":
		args = append(args, "-m", o.gguf, "-c", fmt.Sprint(o.ctx), "-ngl", "99", "-fa", "on", "-ctk", "q8_0", "-ctv", "q8_0", "-np", "1")
		if o.alias != "" {
			args = append(args, "--alias", o.alias)
		}
	default:
		args = append(args, "-hf", o.hf)
	}
	return plan{server: args}
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
	fs.StringVar(&o.hf, "hf", "ggml-org/Kev-4B-GGUF:Q4_K_M", "Hugging Face のモデル(llama-server -hf に渡す)")
	fs.StringVar(&o.gguf, "gguf", "", "手元の GGUF ファイル(あれば -hf より優先)")
	fs.StringVar(&o.alias, "alias", "", "GGUF の時の名前(較正の鍵になる)")
	fs.IntVar(&o.port, "port", 8080, "llama-server のポート")
	fs.IntVar(&o.readout, "readout-port", 8090, "普通の GGUF だった時に前に立てる shikii-readout のポート")
	fs.IntVar(&o.ctx, "ctx", 4096, "文脈長(GGUF の時)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	llama, _ := exec.LookPath("llama-server")
	p := upPlan(o, llama)
	if p.install != "" {
		return errors.New(p.install)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", o.port)
	if pr := probe(base); pr.up {
		fmt.Fprintf(stdout, "something already answers on %s (model=%s). Use --port for another one, or just point shikii at it\n", base, pr.model)
		return nil
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
	useURL := base
	var ro *exec.Cmd
	if !pr.decisionModel {
		readout, err := findReadout()
		if err != nil {
			return fmt.Errorf("%s is a plain chat model and shikii-readout is needed in front, but: %w", pr.model, err)
		}
		ro = exec.CommandContext(ctx, readout, "--upstream", base, "--port", fmt.Sprint(o.readout))
		ro.Stdout, ro.Stderr = os.Stderr, os.Stderr
		if err := ro.Start(); err != nil {
			return err
		}
		useURL = fmt.Sprintf("http://127.0.0.1:%d", o.readout)
		fmt.Fprintf(stdout, "%s is a plain chat model → shikii-readout started in front on %s\n", pr.model, useURL)
	}
	fmt.Fprintf(stdout, "\nready. model=%s\n  shikii decide <spec> --local %s -i …\n  shikii serve --local %s\nCtrl-C stops everything.\n", pr.model, useURL, useURL)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		cancel()
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("llama-server stopped: %w", err)
		}
	}
	if ro != nil {
		_ = ro.Wait()
	}
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
			fmt.Fprintf(stdout, "MISSING local model at %s: unreachable → shikii up   (starts Kev-4B; or shikii up --gguf <file> for any GGUF)\n", *local)
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
