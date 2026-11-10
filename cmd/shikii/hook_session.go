package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

func sessionPath(agent, id string) string {
	id = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
	if id == "" {
		id = "no-session"
	}
	return filepath.Join(agent, "sessions", id+".json")
}

// sessionFilesPath is the kept working tree listing at request time (output of `git ls-files --stage`).
func sessionFilesPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".files"
}

// sessionLockBusy, when set (tests), is called each time lockSessionWithin finds the lock held and is about to wait: the moment another
// hook's read → change → write is in progress.
var sessionLockBusy func()

// lockSessionWithin takes the session lock, waiting at most wait (0: one try). ok is false when it was not taken (unlock is then a no-op).
func lockSessionWithin(agent, id string, wait time.Duration) (unlock func(), ok bool) {
	p := sessionPath(agent, id) + ".lock"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return func() {}, false
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return func() {}, false
	}
	if info, e := f.Stat(); e != nil || !info.Mode().IsRegular() {
		f.Close()
		return func() {}, false
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return func() {}, false
		}
		if sessionLockBusy != nil {
			sessionLockBusy()
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true
}

// updateSession serializes each hook update with planning and prepared recording.
func updateSession(agent, id string, fn func(s *hookSession)) error {
	unlock, ok := lockSessionWithin(agent, id, hookLockWait)
	if !ok {
		return errors.New("the session lock is unavailable; the update was not saved")
	}
	defer unlock()
	s, err := readSession(agent, id)
	if err != nil {
		return err
	}
	fn(s)
	syncDeferral(agent, id, s)
	return saveSession(agent, id, s)
}

func saveHookMutations(agent, id, event string, snapshot *hookSession, out *hookOut, muts []func(*hookSession)) error {
	apply := func(sess *hookSession) {
		for _, m := range muts {
			m(sess)
		}
	}
	// the Stop of a stretch a slash command opened judges nothing of the request, so there is no completion to verify
	if event != "stop" || out.Block || out.Halt != "" || snapshot.Request == "" || snapshot.GuardTurn != nil {
		return updateSession(agent, id, apply)
	}
	// Keep verification and the final session write inside the same commit boundary.
	return commitStopReasoning(agent, id, snapshot, func() error {
		sess, err := readSession(agent, id)
		if err != nil {
			return err
		}
		apply(sess)
		syncDeferral(agent, id, sess)
		return saveSession(agent, id, sess)
	})
}

// readSession distinguishes a new session from an existing record that cannot be verified.
func readSession(agent, id string) (*hookSession, error) {
	s := &hookSession{}
	b, err := os.ReadFile(sessionPath(agent, id))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	restoreWorkReasoning(s)
	return s, nil
}

func loadSession(agent, id string) *hookSession {
	s, err := readSession(agent, id)
	if err != nil {
		hookLog(fmt.Sprintf("session %s: unreadable (%v)", id, err))
		return &hookSession{}
	}
	return s
}

// saveSession writes to tmp and renames (another process never reads a half-written file. Same style as editHooks).
func saveSession(agent, id string, s *hookSession) error {
	s.At = time.Now()
	b, err := encodeHookSession(s)
	if err != nil {
		return err
	}
	return writeAtomic(sessionPath(agent, id), b)
}

func writeAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", p, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// cut cuts s to at most n bytes (without splitting a multibyte character).
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Expired receipts stay invalid, but accepted unresolved work must remain
// recoverable from a fresh preparation. Cleanup shares every writer's lock.
func pruneSessions(agent string) {
	directory := filepath.Join(agent, "sessions")
	root, err := os.OpenRoot(directory)
	if err != nil {
		return
	}
	defer root.Close()
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".json") {
			pruneSessionRecord(root, name)
		} else if strings.HasSuffix(name, ".guard.files") {
			pruneSessionRecord(root, strings.TrimSuffix(name, ".guard.files")+".json")
		} else if strings.HasSuffix(name, ".files") {
			pruneSessionRecord(root, strings.TrimSuffix(name, ".files")+".json")
		}
	}
}

func pruneSessionRecord(root *os.Root, name string) {
	lock, err := root.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err == nil {
		info, statErr := f.Stat()
		_ = f.Close()
		if statErr != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) <= hookSessionTTL {
			return
		}
		session, readErr := readPlanningSession(root, name)
		if readErr != nil {
			return
		} // Unverifiable work is never silently discarded.
		r := session.Reasoning
		if r != nil {
			for _, v := range r.Readings {
				if v == nil {
					return
				}
			}
			for _, v := range r.Trials {
				if v == nil {
					return
				}
			}
			for _, v := range r.Comparisons {
				if v == nil {
					return
				}
			}
			for _, v := range r.PendingResults {
				if v == nil {
					return
				}
			}
			for _, v := range r.PendingProcedures {
				if v == nil {
					return
				}
			}
		}
		if r != nil && (r.WikiPending || len(r.RequiredNotes) > 0 || len(r.PendingCaptures) > 0 || trialPendingCount(r) > 0 || pendingResultCount(r) > 0 || pendingProcedureCount(r) > 0 || r.Result != nil && (!r.Result.Ready || r.Finish == nil || r.Finish.Generation != r.Generation) || r.Procedure != nil && r.Procedure.CloseReport) {
			return
		}
		if session.Planning != nil && session.Planning.Work != nil && reasoningPublicationsPending(session.Planning.Work) {
			return
		}
		if root.Remove(name) != nil {
			return
		}
	} else if !os.IsNotExist(err) {
		return
	}
	// The tree listings (the request's, and the one a slash command's turn kept) are caches. Remove only an old orphan, under the same lock.
	for _, suffix := range []string{".files", ".guard.files"} {
		cache := strings.TrimSuffix(name, ".json") + suffix
		f, err = root.OpenFile(cache, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		info, err = f.Stat()
		_ = f.Close()
		if err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > hookSessionTTL {
			_ = root.Remove(cache)
		}
	}
}
