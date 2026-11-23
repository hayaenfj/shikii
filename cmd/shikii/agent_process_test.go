package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeProcessTable is the process table a test gives shikii in place of this machine's: the rows it holds, and for any other pid the
// answer "not there". It writes down the pids it was asked about.
type fakeProcessTable struct {
	mu    sync.Mutex
	rows  map[int]processRow
	state processState // what a pid that is not in rows gets (processAbsent by default)
	asked []int
	blind bool // the table does not show the process that asks (an empty /proc, another pid namespace); by default it does
	// the list of every process (readProcessList): none by default, which is a machine with nothing else running
	list    []processEntry
	listErr error
	listed  int // how many times the list was read
}

func useProcessTable(t *testing.T, rows map[int]processRow) *fakeProcessTable {
	t.Helper()
	table := &fakeProcessTable{rows: map[int]processRow{}, state: processAbsent}
	for pid, row := range rows {
		table.rows[pid] = row
	}
	old := readProcess
	readProcess = func(_ context.Context, pid int) (processRow, processState) {
		table.mu.Lock()
		defer table.mu.Unlock()
		table.asked = append(table.asked, pid)
		if row, ok := table.rows[pid]; ok {
			return row, processFound
		}
		if pid == thisPID() && !table.blind {
			return processRow{Parent: 1, Start: "started-forget", Name: "shikii"}, processFound
		}
		return processRow{}, table.state
	}
	oldList := readProcessList
	readProcessList = func(context.Context) ([]processEntry, error) {
		table.mu.Lock()
		defer table.mu.Unlock()
		table.listed++
		return append([]processEntry(nil), table.list...), table.listErr
	}
	t.Cleanup(func() { readProcess, readProcessList = old, oldList })
	return table
}

func (f *fakeProcessTable) set(pid int, row processRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[pid] = row
}

func (f *fakeProcessTable) drop(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, pid)
}

// thisMachine is the name shikii gives this machine, which is what a record written here names as its host.
func thisMachine(t *testing.T) string {
	t.Helper()
	host, err := machineName()
	if err != nil || host == "" {
		t.Fatalf("premise: this machine has a name: %q %v", host, err)
	}
	return host
}

func ownProcess(t *testing.T) agentProcess {
	t.Helper()
	return agentProcess{PID: os.Getpid(), Host: thisMachine(t)}
}

// useParent makes the hook's parent the pid, as if the hook had been started by it.
func useParent(t *testing.T, pid int) {
	t.Helper()
	old := parentPID
	parentPID = func() int { return pid }
	t.Cleanup(func() { parentPID = old })
}

// A fake family: the hook's parent is a login shell's child shell, above which is a go-between, above which is the agent.
func agentFamily(agentName string) map[int]processRow {
	return map[int]processRow{
		500: {Parent: 400, Start: "s500", Name: "/bin/zsh"},
		400: {Parent: 300, Start: "s400", Name: "-bash"},
		300: {Parent: 200, Start: "s300", Name: "/usr/bin/env"},
		200: {Parent: 100, Start: "Sat Nov 21 11:08:27 2026", Name: agentName},
		100: {Parent: 1, Start: "s100", Name: "-zsh"},
	}
}

// The agent is the first ancestor whose name is one of the agents shikii knows (agentNames), whatever stands between it and the hook: a
// shell, the dash of a login shell, a path in front of the name, a script that runs the hook, an interpreter.
func TestTheAgentProcessIsTheFirstAncestorWithTheNameOfAKnownAgent(t *testing.T) {
	host := thisMachine(t)
	started := "Sat Nov 21 11:08:27 2026"
	between := func(names ...string) map[int]processRow { // the hook's parent is 500; the agent is 200 above the go-betweens
		rows := agentFamily("claude")
		for i, name := range names {
			pid := 500 - i*100
			rows[pid] = processRow{Parent: pid - 100, Start: "s" + strconv.Itoa(pid), Name: name}
		}
		return rows
	}
	for _, c := range []struct {
		name string
		rows map[int]processRow
		want *agentProcess
	}{
		{"claude", agentFamily("claude"), &agentProcess{PID: 200, Start: started, Host: host, Name: "claude"}},
		{"a path is not part of the name", agentFamily("/Users/me/.local/bin/claude"), &agentProcess{PID: 200, Start: started, Host: host, Name: "claude"}},
		{"codex", agentFamily("/Users/me/.codex/packages/app-server-daemon/releases/0.171.0/bin/codex"), &agentProcess{PID: 200, Start: started, Host: host, Name: "codex"}},
		{"opencode", agentFamily("opencode"), &agentProcess{PID: 200, Start: started, Host: host, Name: "opencode"}},
		{"python3 between the hook and the agent", between("python3", "-bash", "/usr/bin/env"), &agentProcess{PID: 200, Start: started, Host: host, Name: "claude"}},
		{"a script that wraps the hook", between("logger.sh", "sudo", "gtimeout"), &agentProcess{PID: 200, Start: started, Host: host, Name: "claude"}},
		{"a shell that is not one shikii has heard of", between("nu", "xonsh", "pwsh"), &agentProcess{PID: 200, Start: started, Host: host, Name: "claude"}},
		{"Claude Code started by the path of its version file, as Linux names it", map[int]processRow{
			500: {Parent: 400, Start: "s500", Name: "zsh"},
			400: {Parent: 1, Start: started, Name: "2.1.330", Exe: "/home/me/.local/share/claude/versions/2.1.330"},
		}, &agentProcess{PID: 400, Start: started, Host: host, Name: "claude"}},
		{"Claude Code started by the path of its version file, as macOS names it", map[int]processRow{
			500: {Parent: 400, Start: "s500", Name: "zsh"},
			400: {Parent: 1, Start: started, Name: "/Users/me/.local/share/claude/versions/2.1.330", Exe: "/Users/me/.local/share/claude/versions/2.1.330"},
		}, &agentProcess{PID: 400, Start: started, Host: host, Name: "claude"}},
		{"an agent that runs another agent: the nearer is the one", map[int]processRow{
			500: {Parent: 400, Start: "s500", Name: "zsh"},
			400: {Parent: 300, Start: "s400", Name: "codex"},
			300: {Parent: 200, Start: "s300", Name: "zsh"},
			200: {Parent: 1, Start: started, Name: "claude"},
		}, &agentProcess{PID: 400, Start: "s400", Host: host, Name: "codex"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			useProcessTable(t, c.rows)
			useParent(t, 500)
			if got := findAgentProcess(context.Background()); !reflect.DeepEqual(got, c.want) {
				t.Errorf("the agent process: got %+v, want %+v", got, c.want)
			}
		})
	}
}

// A name that is not a known agent is never taken for the agent, however it stands: a go-between that ends with the hook would be noted
// in its place, and forget would read the session as ended while the agent still runs. Nothing is noted then (forget goes by time).
func TestTheAgentProcessIsNotTakenFromAnAncestorThatIsNotAKnownAgent(t *testing.T) {
	for _, c := range []struct {
		name  string
		names []string // from the hook's parent up
		exes  []string // the path of each one's executable, "" when the table does not say
	}{
		{"an interpreter alone", []string{"python3"}, nil},
		{"a script", []string{"logger.sh", "zsh"}, nil},
		{"node and bun, which cannot be told from a go-between", []string{"zsh", "node", "bun", "zsh"}, nil},
		{"a name that is a version, with no path to say what it is", []string{"zsh", "2.1.330"}, nil},
		{"names that only start like an agent", []string{"claude-wrapper", "codex-code-mode-host", "opencode-pty"}, nil},
		{"a version file of another program", []string{"zsh", "1.2.3"}, []string{"", "/home/me/.local/share/codex/versions/1.2.3"}},
		{"a file of Claude's versions that is no version", []string{"zsh", "latest"}, []string{"", "/home/me/.local/share/claude/versions/latest"}},
		{"a file below a version's folder", []string{"zsh", "helper"}, []string{"", "/home/me/.local/share/claude/versions/2.1.330/bin/helper"}},
		{"a folder that only ends like Claude's", []string{"zsh", "2.1.330"}, []string{"", "/home/me/.local/share/not-claude/versions/2.1.330"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := map[int]processRow{}
			for i, name := range c.names {
				pid := 500 - i*100
				rows[pid] = processRow{Parent: pid - 100, Start: "s" + strconv.Itoa(pid), Name: name}
				if i < len(c.exes) {
					row := rows[pid]
					row.Exe = c.exes[i]
					rows[pid] = row
				}
			}
			rows[500-len(c.names)*100] = processRow{Parent: 1, Start: "s-top", Name: "-zsh"}
			useProcessTable(t, rows)
			useParent(t, 500)
			if got := findAgentProcess(context.Background()); got != nil {
				t.Errorf("an agent was found: %+v", got)
			}
		})
	}
}

func TestTheAgentProcessIsNotFoundWhenTheWalkCannotSayWhoItIs(t *testing.T) {
	for name, c := range map[string]struct {
		rows   map[int]processRow
		parent int
		state  processState
	}{
		"only shells up to the top":      {map[int]processRow{500: {Parent: 400, Start: "a", Name: "zsh"}, 400: {Parent: 1, Start: "b", Name: "sh"}}, 500, processAbsent},
		"a pid that is not in the table": {map[int]processRow{500: {Parent: 400, Start: "a", Name: "zsh"}}, 500, processAbsent},
		"a table that cannot be read":    {nil, 500, processUnknown},
		"an agent with no start time":    {map[int]processRow{500: {Parent: 1, Start: "", Name: "claude"}}, 500, processAbsent},
		"the hook's parent is the top":   {nil, 1, processAbsent},
	} {
		t.Run(name, func(t *testing.T) {
			table := useProcessTable(t, c.rows)
			table.state = c.state
			useParent(t, c.parent)
			if got := findAgentProcess(context.Background()); got != nil {
				t.Errorf("an agent was found: %+v", got)
			}
		})
	}
	t.Run("a chain of shells longer than the walk", func(t *testing.T) {
		rows := map[int]processRow{}
		for pid := 500; pid > 500-agentProcessHops-3; pid-- {
			rows[pid] = processRow{Parent: pid - 1, Start: "s", Name: "sh"}
		}
		rows[500-agentProcessHops-3] = processRow{Parent: 1, Start: "s", Name: "claude"}
		useProcessTable(t, rows)
		useParent(t, 500)
		if got := findAgentProcess(context.Background()); got != nil {
			t.Errorf("the walk went past its limit: %+v", got)
		}
	})
	t.Run("the name of this machine is not known", func(t *testing.T) {
		useProcessTable(t, agentFamily("claude"))
		useParent(t, 500)
		old := machineName
		machineName = func() (string, error) { return "", os.ErrNotExist }
		defer func() { machineName = old }()
		if got := findAgentProcess(context.Background()); got != nil {
			t.Errorf("a process was noted with no host: %+v", got)
		}
	})
}

func hookProcessOf(t *testing.T, root, session string) *agentProcess {
	t.Helper()
	s, err := readSession(root, session)
	if err != nil {
		t.Fatal(err)
	}
	return s.Process
}

// Every kind of event notes the process: a hook that is only a lesson before a tool, or a subagent starting, writes it as the request
// does. The record keeps the process of the agent, found by walking up from the hook through the shells.
func TestTheHookNotesTheAgentProcessInTheSessionRecord(t *testing.T) {
	for _, event := range []string{"user-prompt-submit", "tool-lesson", "subagent-start", "subagent-stop", "stop"} {
		t.Run(event, func(t *testing.T) {
			repo := gitRepo(t)
			root := hookEnv(t, nil, repo)
			root, _, err := projectStoreAt(repo)
			if err != nil {
				t.Fatal(err)
			}
			useProcessTable(t, agentFamily("claude"))
			useParent(t, 500)
			// the event is the first of its session: nothing before it wrote the process
			before := time.Now().Add(-time.Second)
			in := map[string]any{"session_id": "ses_noted", "cwd": repo, "prompt": "Explain how the page lists waiting answers.", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"},
				"last_assistant_message": "Done.", "agent_id": "agent_noted", "agent_type": "Explore"}
			runHook(t, event, in)
			want := &agentProcess{PID: 200, Start: "Sat Nov 21 11:08:27 2026", Host: thisMachine(t), Name: "claude"}
			got := hookProcessOf(t, root, "ses_noted")
			if got == nil || !got.same(*want) {
				t.Fatalf("the process in the record after %s: got %+v, want %+v", event, got, want)
			}
			// the time the process was first seen running the session is the time of this event
			if got.Noted.Before(before) || got.Noted.After(time.Now().Add(time.Second)) {
				t.Errorf("the time the process was noted at after %s is not the time of the event: %v", event, got.Noted)
			}
		})
	}
}

// A resumed session runs in another process: the next event writes the new one. An event of the same process writes nothing.
func TestTheHookWritesTheProcessOnlyWhenItChanged(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	root, _, err := projectStoreAt(repo)
	if err != nil {
		t.Fatal(err)
	}
	table := useProcessTable(t, agentFamily("claude"))
	useParent(t, 500)
	noteAgentProcess(root, "ses_resumed")
	firstNoted := hookProcessOf(t, root, "ses_resumed").Noted
	if firstNoted.IsZero() {
		t.Fatalf("the first event did not note the time")
	}
	first, err := os.ReadFile(sessionPath(root, "ses_resumed"))
	if err != nil {
		t.Fatalf("the first event did not write the record: %v", err)
	}
	noteAgentProcess(root, "ses_resumed")
	if again, _ := os.ReadFile(sessionPath(root, "ses_resumed")); !bytes.Equal(first, again) {
		t.Errorf("an event of the same process wrote the record again:\n%s\n%s", first, again)
	}
	// the agent is started again with the same session: another pid, another start time
	table.set(200, processRow{Parent: 100, Start: "Mon Nov 23 09:00:00 2026", Name: "claude"})
	noteAgentProcess(root, "ses_resumed")
	got := hookProcessOf(t, root, "ses_resumed")
	if got == nil || got.Start != "Mon Nov 23 09:00:00 2026" {
		t.Fatalf("the record keeps the process of the agent that was started before: %+v", got)
	}
	// the processes under the new agent started after the old one's time, so the time is the new process's own
	if !got.Noted.After(firstNoted) {
		t.Errorf("the new process was noted at the old process's time: %v then %v", firstNoted, got.Noted)
	}
}

// A record of an earlier shikii has the process and no time: the hook does not give it one, because a time of a later event would put
// what the session started before it among the agent's own processes.
func TestTheHookGivesNoTimeToAProcessAnEarlierShikiiNoted(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	root, _, err := projectStoreAt(repo)
	if err != nil {
		t.Fatal(err)
	}
	useProcessTable(t, agentFamily("claude"))
	useParent(t, 500)
	noteAgentProcess(root, "ses_before")
	s, err := readSession(root, "ses_before")
	if err != nil {
		t.Fatal(err)
	}
	s.Process.Noted = time.Time{}
	if err := saveSession(root, "ses_before", s); err != nil {
		t.Fatal(err)
	}
	noteAgentProcess(root, "ses_before")
	if got := hookProcessOf(t, root, "ses_before"); got == nil || !got.Noted.IsZero() {
		t.Errorf("a record with a process and no time was given a time: %+v", got)
	}
}

// With no agent process to find, nothing is written: no record is made, and a record that names a process keeps it.
func TestTheHookNotesNothingWhenItFindsNoAgent(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	root, _, err := projectStoreAt(repo)
	if err != nil {
		t.Fatal(err)
	}
	table := useProcessTable(t, agentFamily("claude"))
	useParent(t, 500)
	table.state = processUnknown
	table.drop(500)
	noteAgentProcess(root, "ses_none")
	if _, err := os.Stat(sessionPath(root, "ses_none")); !os.IsNotExist(err) {
		t.Errorf("a record was made though no agent was found: %v", err)
	}
	table.state = processAbsent
	table.set(500, agentFamily("claude")[500])
	noteAgentProcess(root, "ses_none")
	table.drop(500)
	table.state = processUnknown
	noteAgentProcess(root, "ses_none")
	if got := hookProcessOf(t, root, "ses_none"); got == nil || got.PID != 200 {
		t.Errorf("a hook that found no agent took the process out of the record: %+v", got)
	}
	noteAgentProcess(root, "")
}

// The process table of Linux (/proc/<pid>/stat): the name may hold spaces and brackets, the boot's id comes before the start time, a
// pid that is not there is absent, a zombie is absent, and a file that is not a stat says nothing.
func TestTheProcStatIsReadByItsFieldsFromTheLastBracket(t *testing.T) {
	proc := t.TempDir()
	put := func(rel, text string) {
		path := filepath.Join(proc, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stat := func(pid int, name, state string, ppid, start int) string {
		fields := []string{state, strconv.Itoa(ppid)}
		for i := 3; i < 22; i++ { // fields 5 to 21
			fields = append(fields, "0")
		}
		fields[19] = strconv.Itoa(start) // field 22
		return strconv.Itoa(pid) + " (" + name + ") " + strings.Join(fields, " ") + " 0 0\n"
	}
	put("sys/kernel/random/boot_id", "6c1f0a7e-boot\n")
	put("77/stat", stat(77, "my (agent) x", "S", 12, 987654))
	if err := os.Symlink("/home/me/.local/share/claude/versions/2.1.330", filepath.Join(proc, "77", "exe")); err != nil {
		t.Fatal(err)
	}
	put("80/note", "a folder with an exe that is replaced while it runs")
	put("81/stat", stat(81, "2.1.329", "S", 12, 5))
	if err := os.Symlink("/home/me/.local/share/claude/versions/2.1.329 (deleted)", filepath.Join(proc, "81", "exe")); err != nil {
		t.Fatal(err)
	}
	put("78/stat", stat(78, "gone", "Z", 12, 5))
	put("79/stat", "79 (short) S 1\n")
	put("80/stat", "no brackets here\n")
	if row, state := readProcStat(proc, 77); state != processFound || row.Parent != 12 || row.Start != "6c1f0a7e-boot/987654" || row.Name != "my (agent) x" || row.Exe != "/home/me/.local/share/claude/versions/2.1.330" {
		t.Errorf("a stat with brackets in its name: %v %+v", state, row)
	}
	if row, state := readProcStat(proc, 81); state != processFound || row.Exe != "/home/me/.local/share/claude/versions/2.1.329" {
		t.Errorf("an executable that an update replaced is named by its path, without the mark the kernel adds: %v %+v", state, row)
	}
	if _, state := readProcStat(proc, 78); state != processAbsent {
		t.Errorf("a zombie has ended: %v", state)
	}
	if _, state := readProcStat(proc, 4242); state != processAbsent {
		t.Errorf("a pid that is not there: %v", state)
	}
	for _, pid := range []int{79, 80} {
		if _, state := readProcStat(proc, pid); state != processUnknown {
			t.Errorf("a stat that cannot be read says nothing of pid %d: %v", pid, state)
		}
	}
	if err := os.Remove(filepath.Join(proc, "sys", "kernel", "random", "boot_id")); err != nil {
		t.Fatal(err)
	}
	if row, state := readProcStat(proc, 77); state != processFound || row.Start != "987654" {
		t.Errorf("with no boot id the start time stands alone: %v %+v", state, row)
	}
}

// The line of ps -o ppid= -o stat= -o lstart= -o comm=, as macOS prints it.
// On macOS a process started by the path of Claude Code's version file is named by the version (the ps name is not always a path), so
// the path is asked from its arguments, once, and only for a name that is a version number.
func TestThePathOfAnAgentNamedByAVersionIsAskedFromItsArguments(t *testing.T) {
	line := " 1352 S+   Sat Nov 21 11:08:27 2026     %s\n"
	for _, c := range []struct {
		name, comm, args, exe string
		asked                 int
	}{
		{"the path of a version file", "2.1.330", "/Users/me/.local/share/claude/versions/2.1.330 --resume abc", "/Users/me/.local/share/claude/versions/2.1.330", 2},
		{"a home folder with a space", "2.1.330", "/Users/John Smith/.local/share/claude/versions/2.1.330 --dangerously-skip-permissions", "/Users/John Smith/.local/share/claude/versions/2.1.330", 2},
		{"no arguments after the path", "2.1.330", "/Users/me/.local/share/claude/versions/2.1.330", "/Users/me/.local/share/claude/versions/2.1.330", 2},
		{"arguments that name the path but do not start with it", "2.1.330", "node /Users/me/.local/share/claude/versions/2.1.330", "", 2},
		{"another folder", "2.1.330", "/Users/me/.local/share/codex/versions/2.1.330 --resume abc", "", 2},
		{"the version of another process in the path", "2.1.330", "/Users/me/.local/share/claude/versions/2.1.3300 --resume abc", "", 2},
		{"a path that goes on after the version", "2.1.330", "/Users/me/.local/share/claude/versions/2.1.330/bin/helper", "", 2},
		{"a name that is a word, so ps is not asked again", "claude", "claude --resume abc", "", 1},
		{"a name that is not a version, whatever its arguments say", "node", "/Users/me/.local/share/claude/versions/2.1.330", "", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			asked := 0
			old := runPS
			runPS = func(cmd *exec.Cmd) ([]byte, error) {
				asked++
				if slices.Contains(cmd.Args, "args=") {
					return []byte(c.args + "\n"), nil
				}
				return []byte(fmt.Sprintf(line, c.comm)), nil
			}
			defer func() { runPS = old }()
			row, state := readPS(context.Background(), 1352)
			if state != processFound || row.Exe != c.exe || asked != c.asked {
				t.Errorf("exe=%q asked=%d, want exe=%q asked=%d (%v)", row.Exe, asked, c.exe, c.asked, state)
			}
		})
	}
	t.Run("ps cannot say", func(t *testing.T) {
		old := runPS
		runPS = func(cmd *exec.Cmd) ([]byte, error) {
			if slices.Contains(cmd.Args, "args=") {
				return []byte("/Users/me/.local/share/claude/versions/2.1.330 --resume abc\n"), errors.New("ps failed") // what it printed before it failed is not used
			}
			return []byte(fmt.Sprintf(line, "2.1.330")), nil
		}
		defer func() { runPS = old }()
		if row, state := readPS(context.Background(), 1352); state != processFound || row.Exe != "" {
			t.Errorf("a failed question for the arguments gave a path: %v %+v", state, row)
		}
	})
}

func TestThePSLineIsReadAsTheParentTheStartAndTheName(t *testing.T) {
	row, state := parsePS(" 1352 S+   Sat Nov 21 11:08:27 2026     /Applications/Some Agent.app/agent\n")
	if state != processFound || row.Parent != 1352 || row.Start != "Sat Nov 21 11:08:27 2026" || row.Name != "/Applications/Some Agent.app/agent" || row.Exe != row.Name {
		t.Errorf("a ps line: %v %+v", state, row)
	}
	if _, state := parsePS("\n"); state != processAbsent {
		t.Errorf("an empty answer is a pid that is not there: %v", state)
	}
	if _, state := parsePS(" 1352 Z   Sat Nov 21 11:08:27 2026 claude\n"); state != processAbsent {
		t.Errorf("a zombie has ended: %v", state)
	}
	for _, line := range []string{"1352 S Sat Nov 21", "x S Sat Nov 21 11:08:27 2026 claude"} {
		if _, state := parsePS(line); state != processUnknown {
			t.Errorf("a line that is not a ps line says nothing: %q %v", line, state)
		}
	}
}

// The table of this machine, read for real (the one place a test does): this very process is there with a start time that is the same
// when it is asked twice, and its parent is the parent the system names; a pid nobody uses is absent. The process is the test's own, so
// nothing is started.
func TestTheProcessTableOfThisMachineHasThisProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), hookProcessTimeout)
	defer cancel()
	first, state := readProcessRow(ctx, os.Getpid())
	if state != processFound {
		t.Fatalf("this process is not in the table: %v", state)
	}
	second, _ := readProcessRow(ctx, os.Getpid())
	if first.Start == "" || first.Start != second.Start || first.Name == "" {
		t.Errorf("the start time or the name of this process is not stable: %+v %+v", first, second)
	}
	if first.Parent != os.Getppid() {
		t.Errorf("the parent of this process is %d, the table says %d", os.Getppid(), first.Parent)
	}
	free := 0
	for pid := 99990; pid > 90000; pid-- {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			free = pid
			break
		}
	}
	if free == 0 {
		t.Skip("every pid from 90001 to 99990 is in use")
	}
	if _, state := readProcessRow(ctx, free); state != processAbsent {
		t.Errorf("pid %d is used by nobody: %v", free, state)
	}
}

// envOf is what a process started with the environment gets for the key: the last entry of the key wins (os/exec drops the earlier ones).
func envOf(env []string, key string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, key+"="); ok {
			value, found = v, true
		}
	}
	return value, found
}

// A start time that ps prints depends on the time zone and the language of whoever asks (the hook of an agent and a person's shell do not
// have the same TZ): ps is always started with TZ=UTC and LC_ALL=C. The test gives the environment of a person in Tokyo with a Japanese
// system, takes the command that shikii makes before ps is run, and reads the environment it would get. It does not depend on the language
// of the machine it runs on.
func TestPSIsAlwaysStartedWithTheTimeZoneAndTheLanguageFixed(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	t.Setenv("LANG", "ja_JP.UTF-8")
	t.Setenv("LC_ALL", "ja_JP.UTF-8")
	t.Setenv("LC_TIME", "ja_JP.UTF-8")
	var seen []*exec.Cmd
	old := runPS
	runPS = func(cmd *exec.Cmd) ([]byte, error) {
		seen = append(seen, cmd)
		if slices.Contains(cmd.Args, "-A") {
			return []byte("  529   527 S+   Mon Nov 23 20:07:09 2026     -zsh\n"), nil
		}
		return []byte(" 1352 S+   Sat Nov 21 11:08:27 2026     claude\n"), nil
	}
	defer func() { runPS = old }()
	if row, state := readPS(context.Background(), 1352); state != processFound || row.Start != "Sat Nov 21 11:08:27 2026" {
		t.Fatalf("premise: the one-pid question is answered: %v %+v", state, row)
	}
	if list, err := readPSList(context.Background()); err != nil || len(list) != 1 {
		t.Fatalf("premise: the list question is answered: %v %v", list, err)
	}
	if len(seen) != 2 {
		t.Fatalf("ps was asked %d times, want once for a pid and once for the list", len(seen))
	}
	for i, cmd := range seen {
		if cmd.Path != "/bin/ps" {
			t.Errorf("question %d runs %q, not /bin/ps", i, cmd.Path)
		}
		for key, want := range map[string]string{"TZ": "UTC", "LC_ALL": "C"} {
			if got, ok := envOf(cmd.Env, key); !ok || got != want {
				t.Errorf("question %d gives ps %s=%q, want %s=%s (the environment of the person's own is %q)", i, key, got, key, want, os.Getenv(key))
			}
		}
	}
}
