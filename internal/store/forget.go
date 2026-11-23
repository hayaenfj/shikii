package store

// What shikii forget removes does not vanish at once: it is moved into records/forgotten/<UTC time>-<short id>/, each file under the
// same path it had in the store (a line or an element of a JSON file is appended to the file of that path there, as it stood). A person
// can read it back from there; nothing in shikii reads that folder.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/rootfile"
)

// ForgottenFolder is the folder under records/ that holds what forget removed.
const ForgottenFolder = "forgotten"

// Forgotten is one run's folder under records/forgotten/.
type Forgotten struct {
	s   Store
	Dir string
}

// NewForgotten makes the folder of a run: <UTC time>-<short>, a number added when the name is taken.
func (s Store) NewForgotten(at time.Time, short string) (*Forgotten, error) {
	base := filepath.Join(s.Dir, "records", ForgottenFolder, at.UTC().Format("20060102T150405Z")+"-"+short)
	dir := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			break
		} else if err != nil {
			return nil, err
		}
		dir = fmt.Sprintf("%s-%d", base, n)
	}
	root, err := s.OpenRecordDirectory(dir, true)
	if err != nil {
		return nil, err
	}
	root.Close()
	return &Forgotten{s: s, Dir: dir}, nil
}

// SaveFile keeps a file's content at rel in the folder. A name already kept is refused: a run keeps each path once.
func (f *Forgotten) SaveFile(rel string, data []byte) error {
	file, err := f.s.openFile(filepath.Join(f.Dir, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// AppendLines keeps removed lines of a JSONL file at rel, as they were.
func (f *Forgotten) AppendLines(rel string, lines [][]byte) error {
	file, err := f.s.openFile(filepath.Join(f.Dir, rel), os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	var out []byte
	for _, line := range lines {
		out = append(append(out, line...), '\n')
	}
	_, err = file.Write(out)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// WriteFile writes a file of the folder whole, replacing what was there (the run's plan, which is written again when the run ends).
func (f *Forgotten) WriteFile(rel string, data []byte) error {
	parent, name, err := f.s.parentRoot(filepath.Join(f.Dir, rel), true)
	if err != nil {
		return err
	}
	defer parent.Close()
	tmp := name + ".tmp-" + NewTraceID()
	file, err := parent.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = parent.Rename(tmp, name)
	}
	if err != nil {
		parent.Remove(tmp)
	}
	return err
}

// Rel is path relative to the store, the name a kept file has in the folder.
func (s Store) Rel(path string) (string, error) {
	rel, err := filepath.Rel(s.Dir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("a file outside the store is not kept here")
	}
	return rel, nil
}

// MoveOut keeps the file at path in the folder under its path relative to the store, then removes it. A link or another kind of file
// is refused, and nothing is removed unless the copy was kept.
func (s Store) MoveOut(path string, f *Forgotten) error {
	rel, err := s.Rel(path)
	if err != nil {
		return err
	}
	file, err := s.openFile(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		return err
	}
	if err := f.SaveFile(rel, data); err != nil {
		return err
	}
	parent, name, err := s.parentRoot(path, false)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Remove(name)
}

// ReadFileWhole reads a regular file of the store, never through a link.
func (s Store) ReadFileWhole(path string) ([]byte, error) {
	file, err := s.openFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// JSONLLines reads the lines of a JSONL file that hold something, as they are, however long they are. A file that is not there has
// none.
func (s Store) JSONLLines(path string) ([][]byte, error) {
	data, err := s.ReadFileWhole(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, line)
		}
	}
	return out, nil
}

// RewriteJSONL removes the lines of the JSONL file at path that drop names. It takes path+".lock", the lock every writer of such a
// file takes, so no line is appended while the file is read and replaced. saved gets the removed lines before anything is changed: when
// it fails, the file is as it was. The kept lines stay as they are (a line that holds nothing too); the file is replaced by renaming a
// complete copy. It returns the number of lines removed; a file that is not there, or has nothing to remove, is not touched.
func (s Store) RewriteJSONL(path string, drop func(line []byte) bool, saved func(removed [][]byte) error) (int, error) {
	parent, name, err := s.parentRoot(path, false)
	if err == nil {
		_, err = parent.Lstat(name)
		parent.Close()
	}
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	err = s.withJSONLAppendLock(path, func() error {
		var err error
		n, err = s.rewriteJSONLLocked(path, drop, saved)
		return err
	})
	return n, err
}

// rewriteJSONLLocked is RewriteJSONL for a caller that holds path+".lock".
func (s Store) rewriteJSONLLocked(path string, drop func(line []byte) bool, saved func(removed [][]byte) error) (int, error) {
	data, err := s.ReadFileWhole(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var kept, removed [][]byte
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // the newline that ends the last line
	}
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) > 0 && drop(line) {
			removed = append(removed, line)
		} else {
			kept = append(kept, line)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := saved(removed); err != nil {
		return 0, err
	}
	parent, name, err := s.parentRoot(path, false)
	if err != nil {
		return 0, err
	}
	defer parent.Close()
	tmpName := name + ".forget-" + NewTraceID()
	tmp, err := parent.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	var out []byte
	for _, line := range kept {
		out = append(append(out, line...), '\n')
	}
	_, err = tmp.Write(out)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = parent.Rename(tmpName, name)
	}
	if err != nil {
		parent.Remove(tmpName)
		return 0, err
	}
	return len(removed), nil
}

// RewriteKept is RewriteJSONL for a run that keeps the removed lines in f at the file's path relative to the store.
func (s Store) RewriteKept(path string, drop func(line []byte) bool, f *Forgotten) (int, error) {
	rel, err := s.Rel(path)
	if err != nil {
		return 0, err
	}
	return s.RewriteJSONL(path, drop, func(removed [][]byte) error { return f.AppendLines(rel, removed) })
}

// The paths of the files forget reads and rewrites, for the project named scope.
func (s Store) FeedbackFile(scope string) string { return s.feedbackPath(scope) }
func (s Store) MemoriesFile(scope string) string { return s.memoryPath(scope) }
func (s Store) CoverageFile(scope string) string { return s.coveragePath(scope) }
func (s Store) MemoryReviewsDir(scope string) string {
	return s.recordPath("memory-reviews", "", scope)
}
func (s Store) RecordsFile(spec string) string { return s.recordsPath(spec) }
func (s Store) LabelsFile(spec string) string  { return s.labelsPath(spec) }

// ReviewsFile is the file of the provisional scores of a question (changes-in-scope, done-as-asked).
func (s Store) ReviewsFile(spec string) string {
	return filepath.Join(s.Dir, "records", "reviews", spec+".jsonl")
}

// LockMemory takes the lock the memory deliveries and their assessments are saved under (2 seconds at most).
func (s Store) LockMemory(scope string) (func(), error) { return s.memoryLock(scope) }

// WithAppendLock runs fn holding path+".lock", the lock every writer of the JSONL file at path takes before it appends.
func (s Store) WithAppendLock(path string, fn func() error) error {
	return s.withJSONLAppendLock(path, fn)
}

// machineLabelSources are the sources of a truth no person gave: forget removes such a label with its decision. Any other source (ask,
// manual, ui, mcp, undo, file, audit, a source this list does not know) is a person's, and keeps the decision.
var machineLabelSources = map[string]bool{"outcome": true, "replay": true, "context": true, "judge": true, SourceAgreed: true}

// PersonLabelSource reports whether a label of this source may be a person's answer.
func PersonLabelSource(source string) bool { return !machineLabelSources[source] }

// DecisionForgetting is what forget does to the decisions of one question.
type DecisionForgetting struct {
	Spec    string
	Removed []string          // the traces of the decision lines removed
	Labels  int               // the lines of machine labels removed with them
	Kept    map[string]string // a decision kept, and why: the source of the person's label on it
}

// ForgetDecisions removes the decision lines of the question spec whose trace is in want, with the labels no person gave, unless a
// person labeled the decision: then it stays with all its labels. With apply false it only reports. The labels are read and both files
// replaced under the labels lock and then the records lock, the order GC takes them in.
func (s Store) ForgetDecisions(spec string, want map[string]bool, f *Forgotten, apply bool) (DecisionForgetting, error) {
	res := DecisionForgetting{Spec: spec, Kept: map[string]string{}}
	records, labels := s.recordsPath(spec), s.labelsPath(spec)
	decide := func() (dropRecord, dropLabel func([]byte) bool, err error) {
		res.Removed, res.Labels, res.Kept = nil, 0, map[string]string{}
		lines, err := s.JSONLLines(labels)
		if err != nil {
			return nil, nil, err
		}
		for _, line := range lines {
			var l struct {
				TraceID string `json:"trace_id"`
				Source  string `json:"source"`
			}
			if json.Unmarshal(line, &l) == nil && want[l.TraceID] && PersonLabelSource(l.Source) {
				res.Kept[l.TraceID] = l.Source
			}
		}
		traceOf := func(line []byte) string {
			var r struct {
				TraceID string `json:"trace_id"`
			}
			if json.Unmarshal(line, &r) != nil {
				return ""
			}
			return r.TraceID
		}
		dropRecord = func(line []byte) bool { t := traceOf(line); return want[t] && res.Kept[t] == "" }
		dropLabel = func(line []byte) bool {
			var l struct {
				TraceID string `json:"trace_id"`
				Source  string `json:"source"`
			}
			return json.Unmarshal(line, &l) == nil && want[l.TraceID] && res.Kept[l.TraceID] == "" && !PersonLabelSource(l.Source)
		}
		seen := map[string]bool{}
		recLines, err := s.JSONLLines(records)
		if err != nil {
			return nil, nil, err
		}
		for _, line := range recLines {
			if t := traceOf(line); dropRecord(line) && !seen[t] {
				seen[t] = true
				res.Removed = append(res.Removed, t)
			}
		}
		for _, line := range lines {
			if dropLabel(line) {
				res.Labels++
			}
		}
		sort.Strings(res.Removed)
		return dropRecord, dropLabel, nil
	}
	if !apply {
		_, _, err := decide()
		return res, err
	}
	err := s.withJSONLAppendLock(labels, func() error {
		return s.withJSONLAppendLock(records, func() error {
			dropRecord, dropLabel, err := decide()
			if err != nil {
				return err
			}
			for _, one := range []struct {
				path string
				drop func([]byte) bool
			}{{records, dropRecord}, {labels, dropLabel}} {
				rel, err := s.Rel(one.path)
				if err != nil {
					return err
				}
				if _, err := s.rewriteJSONLLocked(one.path, one.drop, func(removed [][]byte) error { return f.AppendLines(rel, removed) }); err != nil {
					return err
				}
			}
			return nil
		})
	})
	return res, err
}

// RecordTraces reads the traces of every question's decision lines (records/<question>.jsonl): trace -> the questions that hold it.
// unnamed is true when a line holds no trace: it may be any decision, so no original text is taken as unused.
func (s Store) RecordTraces() (traces map[string][]string, unnamed bool, err error) {
	traces = map[string][]string{}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "records"))
	if os.IsNotExist(err) {
		return traces, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || name == "guards.jsonl" {
			continue
		}
		lines, err := s.JSONLLines(filepath.Join(s.Dir, "records", name))
		if err != nil {
			return nil, false, err
		}
		for _, line := range lines {
			var r struct {
				TraceID string `json:"trace_id"`
			}
			if json.Unmarshal(line, &r) != nil || r.TraceID == "" {
				unnamed = true
				continue
			}
			traces[r.TraceID] = append(traces[r.TraceID], strings.TrimSuffix(name, ".jsonl"))
		}
	}
	return traces, unnamed, nil
}

// ForgetRecordTexts moves out the original texts (records/text/<trace>.json) of the traces in gone. The caller names only the traces
// that no decision line holds any more. It returns how many were moved.
func (s Store) ForgetRecordTexts(gone map[string]bool, f *Forgotten, apply bool) (int, error) {
	n := 0
	for trace := range gone {
		path := filepath.Join(s.Dir, "records", "text", trace+".json")
		if !textTrace.MatchString(trace) {
			continue
		}
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if apply {
			if err := s.MoveOut(path, f); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}

// ForgetAssessments moves out the memory assessments (records/memory-reviews/<project>/<hash>.json) of the deliveries named. The
// caller holds LockMemory. A file that cannot be read as an assessment is left alone.
func (s Store) ForgetAssessments(scope string, deliveries map[string]bool, f *Forgotten, apply bool) (int, error) {
	dir := s.MemoryReviewsDir(scope)
	root, _, err := s.parentRoot(filepath.Join(dir, "entry"), false)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	folder, err := rootfile.OpenNoFollow(root, ".", os.O_RDONLY, 0)
	root.Close()
	if err != nil {
		return 0, err
	}
	entries, err := folder.ReadDir(-1)
	folder.Close()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || !e.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := s.ReadFileWhole(path)
		if err != nil {
			return n, err
		}
		var a struct {
			SourceID string `json:"source_id"`
		}
		if json.Unmarshal(data, &a) != nil || !deliveries[a.SourceID] {
			continue
		}
		if apply {
			if err := s.MoveOut(path, f); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}

// RegularFiles lists the names of the regular files of a folder that end with suffix, sorted. A folder that is not there has none.
func RegularFiles(dir, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), suffix) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// Unlocks holds the unlock functions of locks taken one after another, and releases them the other way round.
type Unlocks []func()

// Add keeps the unlock function of a lock just taken.
func (u *Unlocks) Add(unlock func()) { *u = append(*u, unlock) }

// Release releases the locks, the last taken first.
func (u Unlocks) Release() {
	for i := len(u) - 1; i >= 0; i-- {
		u[i]()
	}
}
