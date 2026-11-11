package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/rootfile"
	"github.com/hayaenfj/shikii/internal/store"
)

const reasoningHistoryLimit = 64 << 10

type reasoningHistoryRecord struct {
	Store      string         `json:"store"`
	Session    string         `json:"session"`
	Turn       string         `json:"turn"`
	Unverified bool           `json:"unverified,omitempty"`
	Entry      reasoningEntry `json:"entry"`
}

func bindReasoningOwner(r *reasoningProgress, session string) error {
	if r == nil || session == "" || len(session) > 200 || r.TurnID == "" {
		return errors.New("reasoning history needs its current session and turn")
	}
	if r.SessionID != "" && r.SessionID != session {
		return errors.New("reasoning history belongs to another session")
	}
	r.SessionID = session
	return nil
}

func reasoningHistoryIdentity(book notebook.Book, r *reasoningProgress, key string) (reasoningHistoryRecord, string, error) {
	if r == nil || r.SessionID == "" || r.TurnID == "" || !strings.HasPrefix(key, "reasoning:") || len(key) != len("reasoning:")+64 {
		return reasoningHistoryRecord{}, "", errors.New("the reasoning history identity is incomplete")
	}
	identity := reasoningHistoryRecord{Store: store.StoreName(book.Root, book.Scope), Session: r.SessionID, Turn: r.TurnID}
	name := "work-reasoning-" + workHash([]string{identity.Store, identity.Session, identity.Turn, key}) + ".json"
	return identity, name, nil
}

func readReasoningHistoryFile(dir *os.Root, name string, identity reasoningHistoryRecord, key string) (*reasoningHistoryRecord, error) {
	before, err := dir.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("the reasoning history must be an ordinary file")
	}
	f, err := rootfile.OpenNoFollow(dir, name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() > reasoningHistoryLimit {
		return nil, errors.New("the reasoning history file is unavailable or exceeds its size limit")
	}
	var record reasoningHistoryRecord
	decoder := json.NewDecoder(io.LimitReader(f, reasoningHistoryLimit+1))
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("the reasoning history must contain one complete record")
	}
	if record.Store != identity.Store || record.Session != identity.Session || record.Turn != identity.Turn || record.Entry.Key != key || record.Entry.At.IsZero() {
		return nil, errors.New("the reasoning history belongs to different work")
	}
	if record.Unverified {
		if record.Entry.Generation != 0 || !slices.Equal(record.Entry.Evidence, nil) || record.Entry.Phase != "" {
			return nil, errors.New("the legacy reasoning history is invalid")
		}
	} else if validateReasoning(record.Entry.workReasoning) != nil || "reasoning:"+workHash(record.Entry.workReasoning) != key || record.Entry.Phase == "finish" && record.Entry.Source == "" {
		return nil, errors.New("the reasoning history content does not match its identity")
	}
	return &record, nil
}

func readReasoningHistory(book notebook.Book, r *reasoningProgress, key string) (*reasoningHistoryRecord, error) {
	identity, name, err := reasoningHistoryIdentity(book, r, key)
	if err != nil {
		return nil, err
	}
	dir, err := (store.Store{Dir: book.Root}).OpenRecordDirectory(filepath.Join(book.Root, "records"), false)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readReasoningHistoryFile(dir, name, identity, key)
}

// Each key has one immutable first generation outside the replaceable session.
func saveReasoningHistory(book notebook.Book, r *reasoningProgress, entry reasoningEntry, unverified bool, commit workCommitter) (*reasoningHistoryRecord, error) {
	identity, name, err := reasoningHistoryIdentity(book, r, entry.Key)
	if err != nil {
		return nil, err
	}
	identity.Entry, identity.Unverified = entry, unverified
	data, err := json.Marshal(identity)
	if err != nil {
		return nil, err
	}
	if len(data) > reasoningHistoryLimit {
		return nil, errors.New("the reasoning history record exceeds its size limit")
	}
	var saved *reasoningHistoryRecord
	err = commit(func() error {
		dir, err := (store.Store{Dir: book.Root}).OpenRecordDirectory(filepath.Join(book.Root, "records"), true)
		if err != nil {
			return err
		}
		defer dir.Close()
		previous, err := readReasoningHistoryFile(dir, name, identity, entry.Key)
		if err != nil {
			return err
		}
		if previous != nil {
			saved = previous
			return nil
		}
		temp := ".reasoning-" + store.NewTraceID() + ".tmp"
		if err := writeWorkTemp(dir, temp, data); err != nil {
			return err
		}
		defer dir.Remove(temp)
		// Link publishes without replacing a first record saved by another writer.
		if err = dir.Link(temp, name); err != nil && !os.IsExist(err) {
			return err
		}
		saved, err = readReasoningHistoryFile(dir, name, identity, entry.Key)
		if err != nil {
			return err
		}
		return workAfterWrite("reasoning-history")
	})
	return saved, err
}

// Older sessions may retain only an item, without its first generation. Preserve
// that key as an unverified retry rather than inventing a current generation.
func preserveReasoningHistory(book notebook.Book, w *workState, includeSaved bool, commit workCommitter) error {
	if w == nil || w.Reasoning == nil {
		return nil
	}
	r := w.Reasoning
	known := map[string]bool{}
	for _, entry := range []*reasoningEntry{r.Start, r.Latest, r.Finish} {
		if entry == nil || entry.Key == "" || known[entry.Key] {
			continue
		}
		known[entry.Key] = true
		if _, err := saveReasoningHistory(book, r, *entry, false, commit); err != nil {
			return err
		}
	}
	if includeSaved {
		for key, item := range w.Saved {
			if item.Kind != "reasoning" || known[key] {
				continue
			}
			if _, err := saveReasoningHistory(book, r, reasoningEntry{Key: key, Source: item.ID, At: time.Now().UTC()}, true, commit); err != nil {
				return err
			}
		}
	}
	return nil
}

// Recover a final session-save interruption only while no later judgment exists.
// A historical retry never moves its generation forward or replaces later work.
func restoreReasoningHistory(w *workState, record *reasoningHistoryRecord, save func() error) (workItem, error) {
	entry := record.Entry
	item := workItem{Ref: "reasoning", Kind: "reasoning", ID: entry.Source}
	if record.Unverified {
		return item, nil
	}
	r := w.Reasoning
	changed := false
	if entry.Phase == "start" && r.Start == nil {
		copy := entry
		r.Start = &copy
		changed = true
	}
	if r.Latest == nil || r.Latest.At.Before(entry.At) {
		copy := entry
		r.Latest = &copy
		applyReasoningEffects(r, entry.workReasoning)
		switch entry.Phase {
		case "finish":
			r.Finish = &copy
		case "update":
			r.Finish = nil
		}
		changed = true
	}
	if changed {
		return item, save()
	}
	return item, nil
}
