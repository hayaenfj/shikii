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

// pageRecord marks where the project's page (shikii serve) is currently up (~/.shikii/agent/pages/<project id>.json).
// The port is not fixed; serve takes a free one and writes it. Even when several agents work in the same project, there is one page (0.8.0).
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

// writePageRecord is written by serve right after it starts listening.
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

// pageUp reports whether the recorded page answers: the started child is alive and /v1/status returns 200 (hookProbeTimeout).
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

// livePage returns the URL of this project's page if it is up (http://127.0.0.1:<the free port>), or "" if not.
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
	pageStartGap   = 30 * time.Second // even if a previously started child died, do not start another within this time (do not hammer a serve that keeps failing)
	pageStaleGuard = 5 * time.Minute  // when the pid in the start-right mark is alive but the page does not answer, clear the mark after this time (pid reuse; audit R5)
	openBrowser    = openInBrowser    // tests swap in something that only records
)

// ensureProjectPage starts `shikii serve --store <scope>/.shikii --port 0` in the background if this project's page is not up
// (on the first request in Claude Code or Codex, so the person never types a start command). The hook does not wait for it to answer (to stay within Claude Code's time limit; audit R5):
// serve itself writes the mark and opens the browser right after it opens its port. The right to start is given to only one hook through startOnceWithin's flock,
// and serve itself does not start if a page is already up, so even when several agents work in the same project there is one page.
// It returns the URL (if up) and whether it started one this time.
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

// clearStaleStartGuard removes the start-right mark when its pid is alive but the page does not answer and the mark is older than pageStaleGuard (so a pid reused by another process never blocks starting forever).
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

// waitForPage waits until the page answers (for the CLI's shikii page; not used in the hook).
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

// openInBrowser opens the URL in the default browser (open on macOS, xdg-open on Linux). Does nothing if neither exists. Never makes the hook wait.
func openInBrowser(url string) {
	if runningAsTestBinary() {
		return // never open a real browser from tests (0.9.1)
	}
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

// pageRef is where the page is, for the hook's text: the URL (ending in /) if up, otherwise it starts the page and returns a "not up yet" text.
func pageRef(scope string) string {
	if url := livePage(scope); url != "" {
		return url + "/"
	}
	ensureProjectPage(scope)
	return pageNotUp
}

const pageNotUp = "(the page is not up yet; the next request's hook starts it)"

// cmdPage says where this project's page is up (URL, port, pid). It is the entry point an agent uses to answer the person whenever asked.
// If it is not up, it starts it (as the hook does) and then says.
func cmdPage(args []string) error {
	if len(args) != 0 {
		return errors.New("shikii page takes no arguments or options (run it inside an initialized project)")
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
		url = waitForPage(scope, 5*time.Second) // the CLI can wait (the hook does not)
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
