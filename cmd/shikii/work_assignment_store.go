package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/rootfile"
	"github.com/hayaenfj/shikii/internal/store"
)

func assignmentDir(root, scope string) (*os.Root, error) {
	if root == "" || scope == "" {
		return nil, errors.New("assignment needs a project")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	path, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	for _, part := range []string{"records", "assignments", store.StoreName(root, scope)} {
		path = filepath.Join(path, part)
		if err = os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		st, e := os.Lstat(path)
		if e != nil {
			return nil, e
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("assignment directories must not be links")
		}
	}
	return os.OpenRoot(path)
}

func assignmentName(id string) string { return workHash(id)[:32] + ".json" }

func readAssignment(dir *os.Root, name string) (assignmentRecord, error) {
	var r assignmentRecord
	f, err := rootfile.OpenNoFollow(dir, name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return r, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return r, errors.New("assignment record must be regular")
	}
	data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil {
		return r, err
	}
	if len(data) > 256<<10 || json.Unmarshal(data, &r) != nil || !validAssignment(r.Context) || r.Bindings == nil || r.Used == nil || r.Events == nil || len(r.Bindings) > 8 || len(r.Order) > 64 {
		return r, errors.New("invalid assignment record")
	}
	return r, nil
}

func saveAssignment(dir *os.Root, r assignmentRecord) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(data) > 256<<10 {
		return errors.New("assignment record exceeds 256 KiB; begin a new assignment")
	}
	name := ".assignment-" + store.NewTraceID() + ".tmp"
	f, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(name)
	_, err = f.Write(data)
	if err == nil {
		err = store.SyncTemp(f)
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return dir.Rename(name, assignmentName(r.Context.ID))
}

func assignmentRecords(dir *os.Root) ([]assignmentRecord, error) {
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var out []assignmentRecord
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if len(out) >= 512 {
			return nil, errors.New("assignment inventory exceeds 512 records")
		}
		r, e := readAssignment(dir, entry.Name())
		if e != nil {
			return nil, e
		}
		if assignmentName(r.Context.ID) != entry.Name() {
			return nil, errors.New("assignment identity changed")
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].Context.ID < out[j].Context.ID
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}

func assignmentState(r assignmentRecord) (string, *assignmentSavedEvent) {
	state := "active"
	var last *assignmentSavedEvent
	for _, id := range r.Order {
		e := r.Events[id]
		if e.Source == "" {
			continue
		}
		copy := e
		last = &copy
		if e.Event.State != "observed" {
			state = e.Event.State
		}
	}
	if state == "active" && time.Since(r.At) > 24*time.Hour {
		state = "stale"
	}
	return state, last
}
