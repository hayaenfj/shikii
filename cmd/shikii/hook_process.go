package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// modelUp reports whether the local model answers (/health, 300ms. up.go's probe is 3s × 3 requests, too heavy for a hook).
func modelUp() bool {
	c := &http.Client{Timeout: hookProbeTimeout}
	resp, err := c.Get(strings.TrimRight(hookLocalURL, "/") + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// lookPath is the PATH lookup (tests replace it).
var lookPath = exec.LookPath

// Returns the real path of the same repo as the listing, so conversations of another repo with the same name are not mixed in,
// and requests from subdirectories or symlinks are treated as the same work.
func treeStateWithScope(cwd string) (hash, listing string, listed bool, project, scope string) {
	if cwd == "" {
		return "", "", false, "", ""
	}
	scope = feedbackScope(cwd)
	project = filepath.Base(filepath.Clean(cwd))
	ctx, cancel := context.WithTimeout(context.Background(), hookGitTimeout)
	defer cancel()
	dir := cwd
	run := func(env []string, args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", false
		}
		return strings.TrimSpace(out.String()), true
	}
	top, ok := run(nil, "rev-parse", "--show-toplevel")
	if !ok {
		return "", "", false, project, scope // outside git: the marker is the cwd's name
	}
	project = filepath.Base(top)
	scope = feedbackScope(top)
	dir = top // from here on, at the repo's root (even a session started in a subdirectory sees the whole repo)
	objects, ok := run(nil, "rev-parse", "--git-path", "objects")
	if !ok {
		return "", "", false, project, scope
	}
	if !filepath.IsAbs(objects) {
		objects = filepath.Join(top, objects) // git answered at the repo's root (dir), not at cwd: a subdirectory would name a folder that does not exist
	}
	scratch, err := os.MkdirTemp("", "shikii-git-*")
	if err != nil {
		return "", "", false, project, scope
	}
	defer os.RemoveAll(scratch)
	env := []string{
		"GIT_INDEX_FILE=" + filepath.Join(scratch, "index"),
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(scratch, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + objects,
	}
	if err := os.MkdirAll(filepath.Join(scratch, "objects"), 0o755); err != nil {
		return "", "", false, project, scope
	}
	if _, ok := run(env, "add", "-A", "--", "."); !ok {
		return "", "", false, project, scope
	}
	// remove internal records only from the temporary index. git add errors on a negated pattern for an ignored path, so they are removed in a separate step.
	if _, ok := run(env, "rm", "--cached", "-r", "-f", "--ignore-unmatch", "--", ".shikii"); !ok {
		return "", "", false, project, scope
	}
	tree, ok := run(env, "write-tree")
	if !ok {
		return "", "", false, project, scope
	}
	sum := sha256.Sum256([]byte(tree))
	hash = hex.EncodeToString(sum[:])[:16]
	// stop reading the listing at the upper limit (do not read everything into memory in a huge repo and then throw it away)
	ls := exec.CommandContext(ctx, "git", "ls-files", "--stage", "--full-name")
	ls.Dir = dir
	ls.Env = append(os.Environ(), env...)
	ls.Stderr = io.Discard
	pipe, err := ls.StdoutPipe()
	if err != nil || ls.Start() != nil {
		return hash, "", false, project, scope
	}
	lb, rerr := io.ReadAll(io.LimitReader(pipe, int64(listingMax)+1))
	_ = ls.Wait()
	if rerr != nil || len(lb) > listingMax {
		return hash, "", false, project, scope
	}
	return hash, strings.TrimSpace(string(lb)), true, project, scope
}

func hookLog(msg string) {
	agent, err := agentHome()
	if err != nil {
		return
	}
	p := filepath.Join(agent, "hook.log")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	msg = strings.ReplaceAll(cut(msg, hookLogLineMax), "\n", " ") // do not keep the model's response body (it may contain requests or replies) whole
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}

// spawnShikii starts `shikii <args>` detached with Setsid and appends output to ~/.shikii/agent/<logName>. Neither the hook nor install waits. Returns the child's pid.
func spawnShikii(agent, logName string, args ...string) int {
	exe, err := os.Executable()
	if err != nil {
		hookLog("cannot find shikii: " + err.Error())
		return 0
	}
	if runningAsTestBinary() {
		return 0 // a go test binary runs the whole test suite regardless of its arguments. Starting it in the background makes them pile up (0.9.1: 75 were left on 2026-10-25)
	}
	logf, err := os.OpenFile(filepath.Join(agent, logName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s shikii %s: started in the background\n", time.Now().Format(time.RFC3339), strings.Join(args, " "))
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logf, "%s shikii %s: %v\n", time.Now().Format(time.RFC3339), strings.Join(args, " "), err)
		return 0
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid
}

// runningAsTestBinary reports whether the current executable was built by go test (its name ends in .test). Guards against real background processes and browsers
func runningAsTestBinary() bool {
	exe, err := os.Executable()
	return err == nil && strings.HasSuffix(filepath.Base(exe), ".test")
}

// startOnce gives the right to start to only one hook via a flock on guard (~/.shikii/agent/starting-<what>), and does not start if the previously started child is still alive (pid)
// or within hookStartGap. During a long model download (even past 10 minutes), a live child prevents a double start. fn returns the started child's pid.
func startOnce(agent, what string, fn func() int) bool {
	return startOnceWithin(agent, what, hookStartGap, fn)
}

// startOnceWithin is startOnce with the gap given. The page uses 30 seconds (can be restarted right after stopping), the model 10 minutes (download takes time).
func startOnceWithin(agent, what string, gap time.Duration, fn func() int) bool {
	_ = os.MkdirAll(agent, 0o755)
	guard := filepath.Join(agent, "starting-"+what)
	f, err := os.OpenFile(guard, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false // another hook is starting it right now
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	b, _ := io.ReadAll(f)
	var pid int
	var at string
	fmt.Sscanf(string(b), "%d %s", &pid, &at)
	if pid > 0 && processAlive(pid) {
		return false
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil && time.Since(t) < gap {
		return false
	}
	pid = fn()
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	fmt.Fprintf(f, "%d %s\n", pid, time.Now().Format(time.RFC3339))
	return true
}

// processAlive reports whether the process pid still exists (signal 0).
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// ensureModel starts `shikii up` in the background if the local model (hookLocalURL) does not answer. Without llama-server it does not start and writes one line on how to install to hook.log (once per 10 minutes).
// The first run downloads the model, so for the few turns until it answers there are simply no records. The hook does not wait for this.
func ensureModel(agent string) {
	if usesHosted() || modelUp() {
		return // with a key, Jev is the standard (unless SHIKII_MODEL=local). The local model is not started
	}
	startOnce(agent, "model", func() int {
		if _, err := lookPath("llama-server"); err != nil {
			hookLog("no local model on " + hookLocalURL + " and no llama-server on PATH: install llama.cpp (brew install llama.cpp) and the hooks will start it; until then they record nothing")
			return 0
		}
		return hookSpawn(agent, "up.log", "up")
	})
}

// ensurePage starts the page of this store's (root) project in the background if it is not running (0.8.0: one per project, on a free port).
func ensurePage(root string) {
	ensureProjectPage(filepath.Dir(root))
}

// hookEvent is one entry added to settings. command is the shikii on PATH (same style as the MCP settings). matcher is only for events that select a tool (PreToolUse).
type hookEvent struct {
	event, cmd string
	timeout    int
	claudeOnly bool   // events Codex lacks (SubagentStart, the PreToolUse for child agents) are added only to Claude Code's settings
	matcher    string // tools PreToolUse looks at (Agent / Task for agent-task, the edit tools and Bash for tool-lesson. Other tools do not trigger the hook)
}
