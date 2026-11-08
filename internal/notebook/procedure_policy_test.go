package notebook

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestProcedurePolicyRejectsCyclesAndUnsafeCheckpointFiles(t *testing.T) {
	dir := t.TempDir()
	base := DefaultProcedure()
	config := ProcedureConfig{ID: "loop-0123456789abcdef", Parent: "loop-0123456789abcdef", RepeatAfter: 1, Compact: 1}
	p := procedureProposal{Config: config, Origin: "0123456789abcdef", Required: 2, Exposures: map[string]string{}, Status: "adopted"}
	j := procedureJournal{Version: 1, Lanes: map[string]*procedureLane{"scope": {Active: config, History: []procedureProposal{p}, Seen: map[string]string{}}}}
	if e := saveProcedureJournal(dir, &j); e != nil {
		t.Fatal(e)
	}
	if _, e := readProcedureJournal(dir); e == nil {
		t.Fatal("a policy cannot be its own ancestor")
	}
	j.Lanes["scope"].Active = base
	j.Lanes["scope"].History = nil
	if e := saveProcedureJournal(dir, &j); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, ".procedure-policy-v1.json")
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	other := filepath.Join(t.TempDir(), "checkpoint.json")
	if e = os.WriteFile(other, original, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(other, path); e != nil {
		t.Fatal(e)
	}
	if _, e = readProcedureJournal(dir); e == nil {
		t.Fatal("do not follow a policy checkpoint link")
	}
}

func TestProcedurePolicyStopsBeforeItsStorageBudget(t *testing.T) {
	dir := t.TempDir()
	old := procedureJournal{Version: 1, Lanes: map[string]*procedureLane{"retained": {Active: DefaultProcedure(), Seen: map[string]string{"0123456789abcdef": "retained-evidence"}}}}
	if e := saveProcedureJournal(dir, &old); e != nil {
		t.Fatal(e)
	}
	j := procedureJournal{Version: 1, Lanes: map[string]*procedureLane{}}
	for lane := 0; lane < 32; lane++ {
		l := &procedureLane{Active: DefaultProcedure(), Seen: map[string]string{}}
		for entry := 0; entry < 64; entry++ {
			l.Seen[fmt.Sprintf("%016x", entry)] = fmt.Sprintf("%064x", entry)
		}
		j.Lanes[fmt.Sprintf("%064x", lane)] = l
	}
	if e := saveProcedureJournal(dir, &j); e != nil {
		t.Fatal(e)
	}
	read, e := readProcedureJournal(dir)
	if e != nil || !read.Limited || read.Lanes["retained"] == nil || len(read.Lanes) != 1 {
		t.Fatal("stop new optimization without discarding prior evidence", read, e)
	}
	info, e := os.Stat(filepath.Join(dir, ".procedure-policy-v1.json"))
	if e != nil || info.Size() > 64<<10 {
		t.Fatal("respect the hard file bound", e)
	}
}
