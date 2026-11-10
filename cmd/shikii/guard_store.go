package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

func guardsDir(root string) string { return filepath.Join(root, "guards") }

func guardPath(root, id string) string { return filepath.Join(guardsDir(root), id+".yaml") }

func guardRecordsPath(root string) string { return filepath.Join(root, "records", "guards.jsonl") }

func loadGuards(root string) ([]guard, error) {
	files, _ := filepath.Glob(filepath.Join(guardsDir(root), "*.yaml"))
	sort.Strings(files)
	var out []guard
	for _, f := range files {
		g, err := loadGuard(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out = append(out, g)
	}
	return out, nil
}

func loadGuard(path string) (guard, error) {
	var g guard
	b, err := os.ReadFile(path)
	if err != nil {
		return g, err
	}
	if err := yaml.Unmarshal(b, &g); err != nil {
		return g, err
	}
	if err := g.validate(); err != nil {
		return g, err
	}
	if filepath.Base(path) != g.ID+".yaml" {
		return g, errors.New("the file name and the id differ")
	}
	return g, nil
}

func saveGuard(root string, g guard) error {
	if err := g.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(guardsDir(root), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# shikii check that stops the turn (" + guardKinds + "). Change enabled with shikii guard enable/disable (enable confirms that the negative example stops)\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(g); err != nil {
		return err
	}
	return writeAtomic(guardPath(root, g.ID), buf.Bytes())
}

// enabledGuards are the checks the hooks run. A file that does not load (edited by hand and left invalid) is skipped with one line in
// hook.log, so it never switches off the other checks; guard list, guard try and status still report it (loadGuards).
func enabledGuards(root string) ([]guard, error) {
	files, _ := filepath.Glob(filepath.Join(guardsDir(root), "*.yaml"))
	sort.Strings(files)
	var out []guard
	for _, f := range files {
		g, err := loadGuard(f)
		if err != nil {
			hookLog(fmt.Sprintf("guard: %s does not load and was skipped (the other checks still run): %v", f, err))
			continue
		}
		if g.Enabled {
			out = append(out, g)
		}
	}
	return out, nil
}

// keepStopRecords keeps the records of a Stop: written at once, or, while the Stop holds them back (hookInput.stopRecords), with
// the commit of the session record, once it is known that the next request did not overtake this Stop.
//
// Held back, they are still refused here when the records file cannot be written at all: the Stop then ends the turn incomplete
// before it judges anything, as it did when the records were written at once.
func keepStopRecords(root string, in *hookInput, recs []guardRecord) error {
	if in.stopRecords != nil {
		if len(recs) == 0 {
			return nil
		}
		if err := guardRecordsWritable(root); err != nil {
			return err
		}
		*in.stopRecords = append(*in.stopRecords, recs...)
		return nil
	}
	return appendGuardRecords(root, recs)
}

// guardRecordsWritable checks that records/guards.jsonl can be appended to, and leaves nothing behind: the file is opened when
// it exists, and where it does not exist yet a probe file is created next to its place and removed (a Stop the next request
// overtakes keeps nothing, not even an empty records file).
func guardRecordsWritable(root string) error {
	path := guardRecordsPath(root)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err == nil {
		return f.Close()
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	probe, err := os.CreateTemp(filepath.Dir(path), ".guards-probe-*")
	if err != nil {
		return err
	}
	probe.Close()
	return os.Remove(probe.Name())
}

func appendGuardRecords(root string, recs []guardRecord) error {
	if len(recs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(guardRecordsPath(root)), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(guardRecordsPath(root), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func readGuardRecords(root string) []guardRecord {
	b, err := os.ReadFile(guardRecordsPath(root))
	if err != nil {
		return nil
	}
	var out []guardRecord
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r guardRecord
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// shikiiStoppedSession reports whether shikii itself stopped a turn of this session (a failed check or ritual in records/guards.jsonl).
// stop_hook_active only says that some Stop hook continued the turn; without a kept turn, only such a record shows that the
// continuation was shikii's own and that the session record it needs is gone.
func shikiiStoppedSession(root, session string) bool {
	for _, r := range readGuardRecords(root) {
		if r.Session == session && !r.Pass && !r.Unverified && !r.Tool { // an unverified check stopped nothing; a refused command (Tool) stopped no Stop
			return true
		}
	}
	return false
}

// guardStats is for status: the number enabled, the number of turns stopped, and the number of turns fixed by the Stop after the block.
type guardStats struct{ Enabled, Stopped, Fixed int }
