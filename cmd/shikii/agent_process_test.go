package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// fakeProcessTable is the process table a test gives shikii in place of this machine's: the rows it holds, and for any other pid the
// answer "not there". It writes down the pids it was asked about.
type fakeProcessTable struct {
	mu    sync.Mutex
	rows  map[int]processRow
	state processState // what a pid that is not in rows gets (processAbsent by default)
	asked []int
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
		return processRow{}, table.state
	}
	t.Cleanup(func() { readProcess = old })
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

// The agent is the first ancestor that is not a shell or a go-between named in agentShells: the shells are passed over (with a path or
// the dash of a login shell in front of the name), the first other name is the agent, whatever it is.
func TestTheAgentProcessIsTheFirstAncestorThatIsNotAShell(t *testing.T) {
	host := thisMachine(t)
	for _, c := range []struct {
		name  string
		agent string
		want  *agentProcess
	}{
		{"claude", "claude", &agentProcess{PID: 200, Start: "Sat Nov 21 11:08:27 2026", Host: host, Name: "claude"}},
		{"a path is not part of the name", "/Users/me/.local/share/claude/versions/2.1.7", &agentProcess{PID: 200, Start: "Sat Nov 21 11:08:27 2026", Host: host, Name: "2.1.7"}},
		{"codex", "codex", &agentProcess{PID: 200, Start: "Sat Nov 21 11:08:27 2026", Host: host, Name: "codex"}},
		{"a runtime that is not in the list is the agent", "bun", &agentProcess{PID: 200, Start: "Sat Nov 21 11:08:27 2026", Host: host, Name: "bun"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			useProcessTable(t, agentFamily(c.agent))
			useParent(t, 500)
			if got := findAgentProcess(context.Background()); !reflect.DeepEqual(got, c.want) {
				t.Errorf("the agent process: got %+v, want %+v", got, c.want)
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
			in := map[string]any{"session_id": "ses_noted", "cwd": repo, "prompt": "Explain how the page lists waiting answers.", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"},
				"last_assistant_message": "Done.", "agent_id": "agent_noted", "agent_type": "Explore"}
			runHook(t, event, in)
			want := &agentProcess{PID: 200, Start: "Sat Nov 21 11:08:27 2026", Host: thisMachine(t), Name: "claude"}
			if got := hookProcessOf(t, root, "ses_noted"); !reflect.DeepEqual(got, want) {
				t.Errorf("the process in the record after %s: got %+v, want %+v", event, got, want)
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
	if got := hookProcessOf(t, root, "ses_resumed"); got == nil || got.Start != "Mon Nov 23 09:00:00 2026" {
		t.Errorf("the record keeps the process of the agent that was started before: %+v", got)
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
	put("78/stat", stat(78, "gone", "Z", 12, 5))
	put("79/stat", "79 (short) S 1\n")
	put("80/stat", "no brackets here\n")
	if row, state := readProcStat(proc, 77); state != processFound || row.Parent != 12 || row.Start != "6c1f0a7e-boot/987654" || row.Name != "my (agent) x" {
		t.Errorf("a stat with brackets in its name: %v %+v", state, row)
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
func TestThePSLineIsReadAsTheParentTheStartAndTheName(t *testing.T) {
	row, state := parsePS(" 1352 S+   Sat Nov 21 11:08:27 2026     /Applications/Some Agent.app/agent\n")
	if state != processFound || row.Parent != 1352 || row.Start != "Sat Nov 21 11:08:27 2026" || row.Name != "/Applications/Some Agent.app/agent" {
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
