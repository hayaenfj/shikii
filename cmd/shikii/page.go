package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// pageRecord は、プロジェクトの画面(shikii serve)が今どこで立っているかの印(~/.shikii/agent/pages/<プロジェクト id>.json)。
// ポートは固定せず、serve が空いているものを取って書く。複数のエージェントが同じプロジェクトで動いても、画面は 1 つ(0.8.0)。
type pageRecord struct {
	Port  int       `json:"port"`
	PID   int       `json:"pid"`
	Scope string    `json:"scope"`
	At    time.Time `json:"at"`
}

func (r pageRecord) url() string { return fmt.Sprintf("http://127.0.0.1:%d", r.Port) }

func pagePath(common, scope string) string {
	return filepath.Join(common, "pages", projectID(scope)+".json")
}

// writePageRecord は serve が待ち受けを始めた直後に書く。
func writePageRecord(common, scope string, port, pid int) error {
	dir := filepath.Join(common, "pages")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(pageRecord{Port: port, PID: pid, Scope: scope, At: time.Now()})
	tmp := filepath.Join(dir, "."+projectID(scope)+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, pagePath(common, scope))
}

func readPageRecord(common, scope string) (pageRecord, bool) {
	var r pageRecord
	b, err := os.ReadFile(pagePath(common, scope))
	if err != nil || json.Unmarshal(b, &r) != nil || r.Port <= 0 {
		return pageRecord{}, false
	}
	return r, true
}

// pageUp は印の画面が応えるか: 起こした子が生きていて、/v1/status が 200(hookProbeTimeout)。
func pageUp(r pageRecord) bool {
	if r.Port <= 0 || r.PID <= 0 || !processAlive(r.PID) {
		return false
	}
	c := &http.Client{Timeout: hookProbeTimeout}
	resp, err := c.Get(r.url() + "/v1/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// livePage は、このプロジェクトの画面が立っていればその URL(http://127.0.0.1:<空いていたポート>)、無ければ ""。
func livePage(scope string) string {
	common, err := agentHome()
	if err != nil || scope == "" {
		return ""
	}
	if r, ok := readPageRecord(common, scope); ok && pageUp(r) {
		return r.url()
	}
	return ""
}

var (
	pageStartGap   = 30 * time.Second // 前に起こした子が死んでいても、この間は起こし直さない(失敗し続ける serve を連打しない)
	pageStaleGuard = 5 * time.Minute  // 起こす権利の印の pid が生きているのに画面が応えない時、この時間を過ぎたら印を片付ける(pid の再利用。監査 R5)
	openBrowser    = openInBrowser    // テストは記録するだけの物に差し替える
)

// ensureProjectPage は、このプロジェクトの画面が立っていなければ `shikii serve --store <scope>/.shikii --port 0` を裏で起こす
// (Claude Code・Codex の最初の依頼で。人に起動コマンドを打たせない)。hook は応えるのを待たない(Claude Code の制限時間の中に収める。監査 R5):
// 印とブラウザは serve 自身が口を開けた直後に書き・開く。起こす権利は startOnceWithin の flock で 1 つの hook だけに渡し、
// serve 自身も立っている画面があれば起きないので、複数のエージェントが同じプロジェクトで動いても画面は 1 つ。
// 返すのは URL(立っていれば)と、今回起こしたか。
func ensureProjectPage(scope string) (string, bool) {
	common, err := agentHome()
	if err != nil || scope == "" {
		return "", false
	}
	if url := livePage(scope); url != "" {
		return url, false
	}
	id := projectID(scope)
	root := filepath.Join(scope, ".shikii")
	clearStaleStartGuard(common, "page-"+id)
	started := startOnceWithin(common, "page-"+id, pageStartGap, func() int {
		return hookSpawn(common, "serve-"+id+".log", "serve", "--store", root, "--port", "0", "--local", hookLocalURL)
	})
	return "", started
}

// clearStaleStartGuard は、起こす権利の印の pid が生きているのに画面が応えず、印が pageStaleGuard より古ければ印を消す(別のプロセスが pid を再利用した時に永久に起こせなくなるのを避ける)。
func clearStaleStartGuard(common, what string) {
	path := filepath.Join(common, "starting-"+what)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var pid int
	var at string
	fmt.Sscanf(string(b), "%d %s", &pid, &at)
	if t, err := time.Parse(time.RFC3339, at); err == nil && time.Since(t) > pageStaleGuard {
		_ = os.Remove(path)
	}
}

// waitForPage は画面が応えるまで待つ(CLI の shikii page 用。hook では使わない)。
func waitForPage(scope string, limit time.Duration) string {
	deadline := time.Now().Add(limit)
	for {
		if url := livePage(scope); url != "" {
			return url
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// openInBrowser は URL を標準のブラウザで開く(macOS は open、Linux は xdg-open)。無ければ何もしない。hook を待たせない。
func openInBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	exe, err := lookPath(name)
	if err != nil {
		return
	}
	cmd := exec.Command(exe, url)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}

// pageRef は hook の文に入れる画面の場所: 立っていれば URL(末尾に /)、無ければ起こしておき「まだ立っていない」の文(英日)。
func pageRef(scope string) string {
	if url := livePage(scope); url != "" {
		return url + "/"
	}
	ensureProjectPage(scope)
	return pageNotUp
}

const pageNotUp = "(the page is not up yet; the next request's hook starts it / 画面はまだ立っていません。次の依頼で hook が起こします)"

// cmdPage は、このプロジェクトの画面がどこで立っているか(URL・ポート・pid)を言う。エージェントが人に聞かれた時にいつでも答えるための口。
// 立っていなければ起こして(hook と同じ)から言う。
func cmdPage(args []string) error {
	if len(args) != 0 {
		return errors.New("shikii page に引数やオプションは不要です(初期化したプロジェクトの中で実行)")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	if active, err := projectActive(root, scope); err != nil || !active {
		return errProjectInactive
	}
	common, err := agentHome()
	if err != nil {
		return err
	}
	url, started := ensureProjectPage(scope)
	if url == "" {
		url = waitForPage(scope, 5*time.Second) // CLI は待てる(hook は待たない)
	}
	if url == "" {
		fmt.Fprintln(stdout, "page: not running (could not start it; see ~/.shikii/agent/serve-"+projectID(scope)+".log)")
		return nil
	}
	rec, _ := readPageRecord(common, scope)
	state := "up"
	if started {
		state = "started now and opened in the browser"
	}
	fmt.Fprintf(stdout, "page: %s/ (port %d, pid %d, %s)\n", url, rec.Port, rec.PID, state)
	return nil
}
