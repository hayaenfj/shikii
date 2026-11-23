package main

// The list of every process of this machine, with its parent and the time it started. shikii forget reads it once to see whether the
// agent of a session has started anything that still runs (forget_idle.go). One question about one pid (readProcess) cannot say that:
// the children of a process are found by asking who has it as a parent.
//
// A row that cannot be read makes the whole list unreadable, and forget then refuses: a process left out might be the one that matters.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processEntry is one running process of the list: a zombie has ended, and is not in it.
type processEntry struct {
	PID     int
	Parent  int
	Started time.Time
	Name    string
}

// procTicksPerSecond is how many ticks of /proc/<pid>/stat make a second (USER_HZ, 100 on every Linux shikii runs on).
const procTicksPerSecond = 100

// readProcessList is what reads the list; tests swap in a table of their own and never read this machine's.
var readProcessList = readProcessTable

// readProcessTable reads the list of this machine in one go: /proc on Linux, one ps elsewhere.
func readProcessTable(ctx context.Context) ([]processEntry, error) {
	if runtime.GOOS == "linux" {
		return readProcList("/proc")
	}
	return readPSList(ctx)
}

// readProcList reads every /proc/<pid>/stat under root once. The start of a process is the boot time of the machine plus its ticks.
func readProcList(root string) ([]processEntry, error) {
	boot, err := bootTime(root)
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var list []processEntry
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, d.Name(), "stat"))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue // it ended between the listing of the folder and the read
		}
		if err != nil {
			return nil, err
		}
		st, state := parseProcStat(data)
		switch state {
		case processAbsent:
			continue
		case processUnknown:
			return nil, fmt.Errorf("the stat of process %d cannot be read", pid)
		}
		ticks, err := strconv.ParseInt(st.ticks, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("the start of process %d is not a number: %q", pid, st.ticks)
		}
		started := boot.Add(time.Duration(ticks) * time.Second / procTicksPerSecond)
		list = append(list, processEntry{PID: pid, Parent: st.parent, Started: started, Name: st.name})
	}
	return list, nil
}

// bootTime is the time the machine started, from the btime line of /proc/stat.
func bootTime(root string) (time.Time, error) {
	data, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "btime "); ok {
			secs, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			if err != nil {
				break
			}
			return time.Unix(secs, 0).UTC(), nil
		}
	}
	return time.Time{}, errors.New("the boot time of the machine is not in /proc/stat")
}

// readPSList asks ps for every process once. The time zone and the language are fixed as for one pid (psQuestion).
func readPSList(ctx context.Context) ([]processEntry, error) {
	out, err := runPS(psQuestion(ctx, "-A", "-o", "pid=", "-o", "ppid=", "-o", "stat=", "-o", "lstart=", "-o", "comm="))
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePSList(string(out))
}

// parsePSList reads the lines of ps -A -o pid= -o ppid= -o stat= -o lstart= -o comm=: the pid, the parent, the state, the start time (five
// words, "Mon Nov 23 20:07:09 2026", in UTC) and the name (a path on macOS, which may hold spaces). Zombies are left out.
func parsePSList(out string) ([]processEntry, error) {
	var list []processEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 9 {
			return nil, fmt.Errorf("a line of ps is not a process: %q", line)
		}
		pid, err1 := strconv.Atoi(f[0])
		parent, err2 := strconv.Atoi(f[1])
		started, err3 := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(f[3:8], " "), time.UTC)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("a line of ps is not a process: %q", line)
		}
		if strings.HasPrefix(f[2], "Z") {
			continue
		}
		list = append(list, processEntry{PID: pid, Parent: parent, Started: started, Name: strings.Join(f[8:], " ")})
	}
	return list, nil
}
