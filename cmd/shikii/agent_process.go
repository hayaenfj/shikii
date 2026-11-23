package main

// The process of the agent a session runs in. The hook notes it in the session's record at every event, and shikii forget goes by it:
// a session whose agent process is the one that was noted is still running, however long its records have been quiet, and one whose
// process is gone has ended. A time since the last record cannot say that: a long test, a long think and a person away from the
// keyboard all leave the records quiet.
//
// The agent is the first ancestor of the hook that is not a shell or a short go-between (agentShells). The process table is read
// through readProcess, so a test gives it a table of its own and never reads this machine's.

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// agentProcess is the record's name for a process. A pid is used again after its process ends, so the pid comes with when the process
// started (Start, which means the same only to the same process on the same machine), and the host says whose process table the pid is in.
type agentProcess struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	Host  string `json:"host"`
	Name  string `json:"name,omitempty"`
}

// processRow is what the process table says of one pid.
type processRow struct {
	Parent int
	Start  string
	Name   string
}

// processState is the answer of the process table: the pid is there, it is not there (it never was, or it ended), or the table could not
// be read, which says nothing of the pid.
type processState int

const (
	processUnknown processState = iota
	processFound
	processAbsent
)

// agentShells are the names (as the process table gives them, without a path or the leading dash of a login shell) that the walk up from
// the hook passes over: a hook is started through a shell, and the agent is the process above it. Any other name is the agent.
var agentShells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true, "ksh": true, "csh": true, "tcsh": true,
	"env": true, "nohup": true, "timeout": true,
}

// agentProcessHops is how many ancestors the walk looks at before it gives up.
const agentProcessHops = 8

// hookProcessTimeout is the upper limit for asking the process table about the agent, the hook's and forget's (every question about a
// process goes through one limit, as every question to git goes through hookGitTimeout).
var hookProcessTimeout = 2 * time.Second

var (
	readProcess = readProcessRow // tests swap in a table
	parentPID   = os.Getppid
	machineName = os.Hostname
)

// processName is the name of an executable as the walk compares it: its base, without the dash a login shell has in front.
func processName(name string) string {
	return filepath.Base(strings.TrimPrefix(strings.TrimSpace(name), "-"))
}

// findAgentProcess walks up from the hook's parent to the first ancestor that is not a shell. It gives nil when the table cannot say,
// when the walk reaches the top, or when a process has no start time to tell it from a later one with the same pid.
func findAgentProcess(ctx context.Context) *agentProcess {
	host, err := machineName()
	if err != nil || host == "" {
		return nil
	}
	pid := parentPID()
	for hop := 0; hop < agentProcessHops && pid > 1; hop++ {
		row, state := readProcess(ctx, pid)
		if state != processFound {
			return nil
		}
		name := processName(row.Name)
		if !agentShells[name] {
			if row.Start == "" {
				return nil
			}
			return &agentProcess{PID: pid, Start: row.Start, Host: host, Name: name}
		}
		pid = row.Parent
	}
	return nil
}

// currentAgentProcess is the agent process of this hook, found within hookProcessTimeout.
func currentAgentProcess() *agentProcess {
	ctx, cancel := context.WithTimeout(context.Background(), hookProcessTimeout)
	defer cancel()
	return findAgentProcess(ctx)
}

// noteAgentProcess keeps the agent process in the session's record when the record does not have it yet or has another: the first event
// of a session, and the first after the agent was started again with the same session (a resume). Otherwise it writes nothing.
func noteAgentProcess(agent, id string) {
	if id == "" {
		return
	}
	p := currentAgentProcess()
	if p == nil {
		return
	}
	if s, err := readSession(agent, id); err == nil && s.Process != nil && *s.Process == *p {
		return
	}
	if err := updateSession(agent, id, func(s *hookSession) { s.Process = p }); err != nil {
		hookLog("process: " + err.Error())
	}
}

// readProcessRow reads one pid from the process table of this machine: /proc on Linux, ps elsewhere.
func readProcessRow(ctx context.Context, pid int) (processRow, processState) {
	if runtime.GOOS == "linux" {
		return readProcStat("/proc", pid)
	}
	return readPS(ctx, pid)
}

// readProcStat reads /proc/<pid>/stat under root. The start time there counts from the boot of the machine, so the boot's id goes in
// front of it: the same number after a reboot is another process.
func readProcStat(root string, pid int) (processRow, processState) {
	data, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return processRow{}, processAbsent
	}
	if err != nil {
		return processRow{}, processUnknown
	}
	// "pid (name) state ppid ...": the name may hold spaces and brackets, so the fields are counted from the last bracket
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 0 || end < open {
		return processRow{}, processUnknown
	}
	rest := strings.Fields(string(data[end+1:]))
	if len(rest) < 20 {
		return processRow{}, processUnknown
	}
	if strings.HasPrefix(rest[0], "Z") {
		return processRow{}, processAbsent // a zombie has ended; only its parent has not collected it
	}
	parent, err := strconv.Atoi(rest[1])
	if err != nil {
		return processRow{}, processUnknown
	}
	start := rest[19]
	if boot, err := os.ReadFile(filepath.Join(root, "sys", "kernel", "random", "boot_id")); err == nil {
		start = strings.TrimSpace(string(boot)) + "/" + start
	}
	return processRow{Parent: parent, Start: start, Name: string(data[open+1 : end])}, processFound
}

// readPS asks ps about one pid. The time zone and the language are fixed so that the start time reads the same whoever asks: the hook
// of an agent and a person's shell do not have the same TZ.
func readPS(ctx context.Context, pid int) (processRow, processState) {
	cmd := exec.CommandContext(ctx, "/bin/ps", "-o", "ppid=", "-o", "stat=", "-o", "lstart=", "-o", "comm=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 {
			return processRow{}, processAbsent // ps exits 1 and prints nothing for a pid that is not there
		}
		return processRow{}, processUnknown
	}
	return parsePS(string(out))
}

// parsePS reads the line of ps -o ppid= -o stat= -o lstart= -o comm=: the parent, the state, the start time (five words, "Mon Nov 23
// 20:07:09 2026") and the name (a path on macOS, which may hold spaces).
func parsePS(out string) (processRow, processState) {
	f := strings.Fields(out)
	if len(f) == 0 {
		return processRow{}, processAbsent
	}
	if len(f) < 8 {
		return processRow{}, processUnknown
	}
	parent, err := strconv.Atoi(f[0])
	if err != nil {
		return processRow{}, processUnknown
	}
	if strings.HasPrefix(f[1], "Z") {
		return processRow{}, processAbsent
	}
	return processRow{Parent: parent, Start: strings.Join(f[2:7], " "), Name: strings.Join(f[7:], " ")}, processFound
}

// agentProcessOf is the process a session's record names (the "process" field), or nil when it names none that can be told from another.
func agentProcessOf(doc map[string]any) *agentProcess {
	m := jmap(doc, "process")
	pid, _ := jget(m, "pid").(float64)
	p := &agentProcess{PID: int(pid), Start: jstr(m, "start"), Host: jstr(m, "host"), Name: jstr(m, "name")}
	if p.PID <= 0 || float64(p.PID) != pid || p.Start == "" || p.Host == "" {
		return nil
	}
	return p
}
