package main

// The process of the agent a session runs in. The hook notes it in the session's record at every event, and shikii forget goes by it:
// a session whose agent process is gone has ended, and one whose agent process is the one that was noted may still be at work
// (forget_idle.go says when it is not). A time since the last record cannot say that: a long test, a long think and a person away from
// the keyboard all leave the records quiet.
//
// The agent is the first ancestor of the hook whose name is one of the agents shikii knows (agentNames). Whatever stands between the hook
// and the agent (a shell, a wrapper script, python3, sudo) is passed over, and a name that is not known is never taken for the agent: a
// go-between that ends with the hook would be noted instead, and forget would then read the session as ended while its agent still
// runs. When no ancestor has a known name nothing is noted, and forget goes by the time of the records (forgetUnknownQuiet). The process
// table is read through readProcess, so a test gives it a table of its own and never reads this machine's.

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// agentProcess is the record's name for a process. A pid is used again after its process ends, so the pid comes with when the process
// started (Start, which means the same only to the same process on the same machine), and the host says whose process table the pid is in.
// Noted is when the hook first saw this process run the session: a process under the agent that started before then (the servers the
// agent starts with itself) is not the session's, and one that started after may be.
type agentProcess struct {
	PID   int       `json:"pid"`
	Start string    `json:"start"`
	Host  string    `json:"host"`
	Name  string    `json:"name,omitempty"`
	Noted time.Time `json:"noted,omitzero"`
}

// same says whether both name one process: the time it was noted at is not part of that.
func (p agentProcess) same(o agentProcess) bool {
	return p.PID == o.PID && p.Start == o.Start && p.Host == o.Host && p.Name == o.Name
}

// processRow is what the process table says of one pid. Exe is the path of its executable when the table says it (/proc/<pid>/exe on
// Linux, the path ps prints as the name on macOS): the name of a process started by the path of Claude Code's version file is the version.
type processRow struct {
	Parent int
	Start  string
	Name   string
	Exe    string
}

// processState is the answer of the process table: the pid is there, it is not there (it never was, or it ended), or the table could not
// be read, which says nothing of the pid.
type processState int

const (
	processUnknown processState = iota
	processFound
	processAbsent
)

// agentNames are the names (as the process table gives them, without a path) of the agents shikii knows: the first ancestor of the hook
// with one of them is the agent that runs the session. It is a list of what is allowed, not of what is passed over: an ancestor that is
// not on it is passed over whatever it is, and when none is on it the hook notes no process. A runtime such as node or bun is not on it, as
// it cannot be told from a go-between that only lives as long as the hook.
var agentNames = map[string]bool{"claude": true, "codex": true, "opencode": true}

// claudeVersionFile is the path of a file of Claude Code's versions (~/.local/share/claude/versions/2.1.330). ~/.local/bin/claude is a
// link to it: started by the link, the process is named claude, and started by the path of the file, its name is the version. Both are
// Claude Code, and nothing else has this path.
var claudeVersionFile = regexp.MustCompile(`(^|/)claude/versions/[0-9]+(\.[0-9]+)+$`)

// agentOf says which of the agents shikii knows a process is, by its name or by the path of its executable; "" when it is none.
func agentOf(row processRow) string {
	if name := processName(row.Name); agentNames[name] {
		return name
	}
	if claudeVersionFile.MatchString(row.Exe) {
		return "claude"
	}
	return ""
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

// findAgentProcess walks up from the hook's parent to the first ancestor that has the name of a known agent (agentNames). It gives nil
// when the table cannot say, when no ancestor within agentProcessHops is a known agent, or when the agent has no start time to tell it
// from a later process with the same pid.
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
		if name := agentOf(row); name != "" {
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
// of a session, and the first after the agent was started again with the same session (a resume). Otherwise it writes nothing, so the
// time it was noted at stays that of the first event under this process. A record of an earlier shikii that has the process without a
// time is not given one: a later time would put processes the session started before it among the agent's own.
func noteAgentProcess(agent, id string) {
	if id == "" {
		return
	}
	p := currentAgentProcess()
	if p == nil {
		return
	}
	if s, err := readSession(agent, id); err == nil && s.Process != nil && s.Process.same(*p) {
		return
	}
	p.Noted = time.Now().UTC()
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
	st, state := parseProcStat(data)
	if state != processFound {
		return processRow{}, state
	}
	start := st.ticks
	if boot, err := os.ReadFile(filepath.Join(root, "sys", "kernel", "random", "boot_id")); err == nil {
		start = strings.TrimSpace(string(boot)) + "/" + start
	}
	row := processRow{Parent: st.parent, Start: start, Name: st.name}
	if exe, err := os.Readlink(filepath.Join(root, strconv.Itoa(pid), "exe")); err == nil {
		row.Exe = strings.TrimSuffix(exe, " (deleted)") // a version that an update replaced while it runs
	}
	return row, processFound
}

// procStat is what /proc/<pid>/stat says that shikii goes by.
type procStat struct {
	parent int
	ticks  string // the start time, in clock ticks since the boot
	name   string
}

// parseProcStat reads the text of a stat file. A zombie has ended (only its parent has not collected it): it is absent.
func parseProcStat(data []byte) (procStat, processState) {
	// "pid (name) state ppid ...": the name may hold spaces and brackets, so the fields are counted from the last bracket
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 0 || end < open {
		return procStat{}, processUnknown
	}
	rest := strings.Fields(string(data[end+1:]))
	if len(rest) < 20 {
		return procStat{}, processUnknown
	}
	if strings.HasPrefix(rest[0], "Z") {
		return procStat{}, processAbsent
	}
	parent, err := strconv.Atoi(rest[1])
	if err != nil {
		return procStat{}, processUnknown
	}
	return procStat{parent: parent, ticks: rest[19], name: string(data[open+1 : end])}, processFound
}

// psQuestion is the one place a question to ps is made. The time zone and the language are fixed so that the start time reads the same
// whoever asks (the hook of an agent and a person's shell do not have the same TZ): they come last, so they win over the environment.
func psQuestion(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/ps", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	return cmd
}

// runPS runs a command made by psQuestion; a test swaps it to see the command and give an answer, without starting ps.
var runPS = func(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }

// readPS asks ps about one pid.
func readPS(ctx context.Context, pid int) (processRow, processState) {
	out, err := runPS(psQuestion(ctx, "-o", "ppid=", "-o", "stat=", "-o", "lstart=", "-o", "comm=", "-p", strconv.Itoa(pid)))
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 {
			return processRow{}, processAbsent // ps exits 1 and prints nothing for a pid that is not there
		}
		return processRow{}, processUnknown
	}
	row, state := parsePS(string(out))
	if state == processFound && row.Exe == "" && versionName.MatchString(processName(row.Name)) {
		row.Exe = readPSExe(ctx, pid, processName(row.Name)) // the name is a version: the path it was started by says whether it is Claude Code's
	}
	return row, state
}

// versionName is the name of a process started by the path of one of Claude Code's version files.
var versionName = regexp.MustCompile(`^[0-9]+(\.[0-9]+)+$`)

// readPSExe is the path of the executable of a process whose name is the version number version, from the first word of its arguments
// ("" when they do not start with a path that ends with claude/versions/<version>, or ps cannot say). It asks only when the name is a
// version number, so that the hook asks ps once more only for an agent started that way.
func readPSExe(ctx context.Context, pid int, version string) string {
	out, err := runPS(psQuestion(ctx, "-o", "args=", "-p", strconv.Itoa(pid)))
	if err != nil {
		return ""
	}
	// the path may hold spaces (a home folder named "John Smith"), so it is cut where the version ends, not at the first space
	m := regexp.MustCompile(`^(/(?:.*/)?claude/versions/` + regexp.QuoteMeta(version) + `)(?: |$)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return ""
	}
	return m[1]
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
	name := strings.Join(f[7:], " ")
	row := processRow{Parent: parent, Start: strings.Join(f[2:7], " "), Name: name}
	if strings.HasPrefix(name, "/") {
		row.Exe = name // ps prints the path of the executable as the name
	}
	return row, processFound
}

// agentProcessOf is the process a session's record names (the "process" field), or nil when it names none that can be told from another.
func agentProcessOf(doc map[string]any) *agentProcess {
	m := jmap(doc, "process")
	pid, _ := jget(m, "pid").(float64)
	p := &agentProcess{PID: int(pid), Start: jstr(m, "start"), Host: jstr(m, "host"), Name: jstr(m, "name")}
	p.Noted, _ = time.Parse(time.RFC3339Nano, jstr(m, "noted")) // zero when the record has none
	if p.PID <= 0 || float64(p.PID) != pid || p.Start == "" || p.Host == "" {
		return nil
	}
	return p
}
