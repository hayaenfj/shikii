package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The list of every process (process_list.go): the text of ps and the folders of /proc are given to the readers, and the one test that
// reads this machine's list asks for the test's own process and its parent, which are certain to be there. No process is started.

func TestThePSListIsReadAsPidParentStartAndName(t *testing.T) {
	out := strings.Join([]string{
		"    1     0 Ss   Mon Nov 23 06:00:01 2026     /sbin/launchd",
		"  527     1 S    Mon Nov  9 08:09:10 2026     /Applications/Some Agent.app/Contents/MacOS/Some Agent",
		"  528   527 Z    Mon Nov 23 06:30:00 2026     <defunct>",
		"  529   527 S+   Mon Nov 23 20:07:09 2026     -zsh",
		"",
	}, "\n")
	got, err := parsePSList(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []processEntry{
		{PID: 1, Parent: 0, Started: time.Date(2026, 11, 23, 6, 0, 1, 0, time.UTC), Name: "/sbin/launchd"},
		{PID: 527, Parent: 1, Started: time.Date(2026, 11, 9, 8, 9, 10, 0, time.UTC), Name: "/Applications/Some Agent.app/Contents/MacOS/Some Agent"},
		{PID: 529, Parent: 527, Started: time.Date(2026, 11, 23, 20, 7, 9, 0, time.UTC), Name: "-zsh"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the list of ps:\n got %+v\nwant %+v", got, want)
	}
}

// A line that is not a process makes the whole list unreadable: a process left out might be the one that matters.
func TestAPSListWithALineThatIsNotAProcessCannotBeRead(t *testing.T) {
	good := "  529   527 S+   Mon Nov 23 20:07:09 2026     -zsh"
	for name, line := range map[string]string{
		"too few words":   "  530   527 S+   Mon Nov 23 20:07:09 2026",
		"no pid":          "  x   527 S+   Mon Nov 23 20:07:09 2026     -zsh",
		"no parent":       "  530   y S+   Mon Nov 23 20:07:09 2026     -zsh",
		"a start of ps's": "  530   527 S+   Mon Nov 23 25:07:09 2026     -zsh",
	} {
		if _, err := parsePSList(good + "\n" + line + "\n"); err == nil {
			t.Errorf("a list with %s was read", name)
		}
	}
}

// fakeProc writes the folders of a /proc: the boot time and one stat per process.
type fakeProc struct {
	t    *testing.T
	root string
}

func newFakeProc(t *testing.T, btime string) *fakeProc {
	t.Helper()
	p := &fakeProc{t: t, root: t.TempDir()}
	if btime != "" {
		p.put("stat", "cpu  1 2 3\nbtime "+btime+"\nprocesses 99\n")
	}
	return p
}

func (p *fakeProc) put(rel, text string) {
	p.t.Helper()
	path := filepath.Join(p.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

// process writes /proc/<pid>/stat: the name, the state, the parent and the start in ticks.
func (p *fakeProc) process(pid int, name, state string, parent, ticks int) {
	p.t.Helper()
	fields := []string{state, strconv.Itoa(parent)}
	for i := 3; i < 22; i++ {
		fields = append(fields, "0")
	}
	fields[19] = strconv.Itoa(ticks)
	p.put(strconv.Itoa(pid)+"/stat", strconv.Itoa(pid)+" ("+name+") "+strings.Join(fields, " ")+" 0 0\n")
}

func TestTheProcListIsReadFromTheBootTimeAndTheTicks(t *testing.T) {
	proc := newFakeProc(t, "1700000000")
	proc.process(1, "systemd", "S", 0, 10)
	proc.process(77, "my (agent) x", "S", 1, 150)
	proc.process(78, "gone", "Z", 77, 5)
	proc.put("sys/kernel/random/boot_id", "boot\n") // not a process
	proc.put("self/note", "not a process either")
	if err := os.MkdirAll(filepath.Join(proc.root, "79"), 0o755); err != nil { // ended between the listing and the read: no stat
		t.Fatal(err)
	}
	got, err := readProcList(proc.root)
	if err != nil {
		t.Fatal(err)
	}
	boot := time.Unix(1700000000, 0).UTC()
	want := []processEntry{
		{PID: 1, Parent: 0, Started: boot.Add(100 * time.Millisecond), Name: "systemd"},
		{PID: 77, Parent: 1, Started: boot.Add(1500 * time.Millisecond), Name: "my (agent) x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the list of /proc:\n got %+v\nwant %+v", got, want)
	}
}

// What /proc cannot say makes the list unreadable.
func TestAProcListThatCannotBeReadIsAnError(t *testing.T) {
	for name, setup := range map[string]func(p *fakeProc){
		"no boot time":           func(p *fakeProc) { p.put("stat", "cpu 1 2 3\n"); p.process(77, "a", "S", 1, 5) },
		"a boot time not a time": func(p *fakeProc) { p.put("stat", "btime soon\n"); p.process(77, "a", "S", 1, 5) },
		"a stat of no shape":     func(p *fakeProc) { p.process(77, "a", "S", 1, 5); p.put("78/stat", "78 (short) S 1\n") },
		"a start that is no number": func(p *fakeProc) {
			p.process(77, "a", "S", 1, 5)
			p.put("78/stat", strings.Replace(strings.TrimSpace(readFile(t, filepath.Join(p.root, "77", "stat"))), " 5 ", " later ", 1)+"\n")
		},
		"a folder that cannot be read": func(p *fakeProc) {
			p.process(77, "a", "S", 1, 5)
			_ = os.Remove(filepath.Join(p.root, "77", "stat"))
			_ = os.Mkdir(filepath.Join(p.root, "77", "stat"), 0o755)
		},
	} {
		t.Run(name, func(t *testing.T) {
			proc := newFakeProc(t, "1700000000")
			setup(proc)
			if got, err := readProcList(proc.root); err == nil {
				t.Errorf("a list of %s was read: %+v", name, got)
			}
		})
	}
	if _, err := readProcList(filepath.Join(t.TempDir(), "no-proc")); err == nil {
		t.Errorf("a /proc that is not there was read")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The list of this machine, read for real (the one other test that does is TestTheProcessTableOfThisMachineHasThisProcess): the test's own
// process and the parent that started it are in it, with the start time the one-pid reader gives for the same process.
func TestTheProcessListOfThisMachineHasThisProcessAndItsParent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), hookProcessTimeout)
	defer cancel()
	list, err := readProcessTable(ctx)
	if err != nil {
		t.Fatalf("the list of this machine cannot be read: %v", err)
	}
	byPID := map[int]processEntry{}
	for _, e := range list {
		byPID[e.PID] = e
	}
	self, ok := byPID[os.Getpid()]
	if !ok {
		t.Fatalf("this process is not in the list of %d processes", len(list))
	}
	if self.Parent != os.Getppid() {
		t.Errorf("the parent of this process is %d, the list says %d", os.Getppid(), self.Parent)
	}
	if _, ok := byPID[os.Getppid()]; !ok {
		t.Errorf("the parent of this process is not in the list")
	}
	if self.Started.IsZero() || self.Started.After(time.Now().Add(2*time.Second)) || self.Started.Before(time.Now().Add(-24*time.Hour)) {
		t.Errorf("the start of this process is not within the last day: %v", self.Started)
	}
	if runtime.GOOS != "linux" {
		row, state := readProcessRow(ctx, os.Getpid())
		if state != processFound || row.Start != self.Started.Format("Mon Jan 2 15:04:05 2006") {
			t.Errorf("the list and the one-pid reader disagree on the start of this process: %v %q %v", state, row.Start, self.Started)
		}
	}
}
