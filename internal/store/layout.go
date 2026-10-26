package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// projectName is the fixed name of the project's conversations, memory deliveries and memory reviews under records/ (0.9.7). Before, they
// were named by sha256 of the project's path, so moving or renaming the folder hid them. A store belongs to one project.
const projectName = "project"

var hashName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func scopeKey(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])
}

// projectPath is records/<area>/project<ext> (a file, or a folder when ext is ""). The records written before 0.9.7 under the scope's hash,
// or the only hash-named ones there when the fixed name is still empty (written under the path the project had then), are brought in:
// a file's complete lines are copied (merge), never the file moved, because an older shikii still running may be writing to it (moving it
// sealed a half-written line and sent the rest to a new file; found by the property test, 0.9.7); a folder of records written once is moved.
func (s Store) projectPath(area, ext, scope string) string {
	dir := filepath.Join(s.Dir, "records", area)
	from := filepath.Join(dir, scopeKey(scope)+ext)
	fixed := filepath.Join(dir, projectName+ext)
	if _, err := os.Lstat(from); err != nil {
		from = ""
		if empty(fixed) {
			from = onlyHashNamed(dir, ext)
		}
	}
	if from == "" {
		return fixed
	}
	if ext == "" {
		if empty(fixed) {
			return s.adopt(area, ext, from)
		}
		s.merge(from, fixed, ext)
		return fixed
	}
	if _, err := os.Lstat(fixed); os.IsNotExist(err) {
		if f, err := os.OpenFile(fixed, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600); err == nil {
			f.Close()
		}
	}
	s.merge(from, fixed, ext)
	return fixed
}

// merge adds what an older shikii (before 0.9.7, still running) wrote under the scope's hash after the move to the fixed name.
// A file is never moved or removed, since the older shikii may hold it open: only its complete lines (ending in a newline) after the
// offset kept in <file>.merged are appended, so a half-written line waits for its end (re-audit C-2, C-3). If this stops between the
// append and the offset, the lines are appended again next time (a repeated conversation, never a lost one).
// A folder's files are moved in; a file whose name is already in the fixed folder is that same record (records there are written once),
// so the leftover is removed and the folder goes (re-audit C-5).
func (s Store) merge(from, fixed, ext string) {
	info, err := os.Lstat(from)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if ext == "" {
		entries, err := os.ReadDir(from)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") { // an older shikii's file being written (.<random>.tmp) is left to it
				continue
			}
			if _, err := os.Lstat(filepath.Join(fixed, e.Name())); err == nil {
				_ = os.Remove(filepath.Join(from, e.Name()))
			} else if os.IsNotExist(err) {
				_ = os.Rename(filepath.Join(from, e.Name()), filepath.Join(fixed, e.Name()))
			}
		}
		_ = os.Remove(from) // only when emptied
		return
	}
	mark, err := os.OpenFile(from+".merged", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	defer mark.Close()
	if syscall.Flock(int(mark.Fd()), syscall.LOCK_EX) != nil {
		return
	}
	defer syscall.Flock(int(mark.Fd()), syscall.LOCK_UN)
	f, err := os.OpenFile(from, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	// the mark is "<offset> <hash of the bytes just before it>": the file is known by its content, not its inode, so a copied store
	// goes on where it was (audit E-3), and another file (the older file removed and made again, even with the same inode; D-1, E-2)
	// or a broken mark starts from the beginning (D-2). A mark of the number only (9b77d07) is taken as it is (E-4).
	raw, _ := io.ReadAll(io.LimitReader(mark, 128))
	at, sum, _ := strings.Cut(strings.TrimSpace(string(raw)), " ")
	if strings.Contains(at, ":") { // "<device>:<inode> <offset>", written by 855865f: the offset holds only for that same file (audit F-4, G-2)
		same := false
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			same = at == fmt.Sprintf("%d:%d", sys.Dev, sys.Ino)
		}
		at, sum = sum, ""
		if !same {
			at = "0"
		}
	}
	offset, err := strconv.ParseInt(at, 10, 64)
	if err != nil || offset < 0 || offset > st.Size() || (sum != "" && tailHash(f, offset) != sum) {
		offset = 0
	}
	data, err := io.ReadAll(io.NewSectionReader(f, offset, MaxLineBytes*16))
	if err != nil {
		return
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return // nothing complete yet
	}
	next := offset + int64(end+1)
	nextSum := tailHash(f, next)
	if nextSum == "" { // the mark could not be checked next time: do not append now (audit F, S-1)
		return
	}
	out, err := os.OpenFile(fixed, os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return
	}
	_, werr := out.Write(data[:end+1])
	if cerr := out.Close(); werr != nil || cerr != nil {
		return
	}
	_ = mark.Truncate(0)
	_, _ = mark.WriteAt([]byte(strconv.FormatInt(next, 10)+" "+nextSum), 0)
}

// tailHash is the hash of up to 4 KiB of f just before offset.
func tailHash(f *os.File, offset int64) string {
	start := max(0, offset-4096)
	b := make([]byte, offset-start)
	if _, err := f.ReadAt(b, start); err != nil && offset > start {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// MigrateLayout moves the records written before 0.9.7 under scope's hash to the fixed names. Reconnecting a moved project calls it with
// the old path, which tells this project's files apart even when files from still earlier paths sit beside them.
func (s Store) MigrateLayout(scope string) {
	for _, a := range []struct{ area, ext string }{{"feedback", ".jsonl"}, {"memories", ".jsonl"}, {"memory-reviews", ""}} {
		s.projectPath(a.area, a.ext, scope)
	}
}

// adopt moves from to records/<area>/project<ext> unless that already holds records (an empty file or folder made before is replaced).
func (s Store) adopt(area, ext, from string) string {
	fixed := filepath.Join(s.Dir, "records", area, projectName+ext)
	if !empty(fixed) {
		return fixed
	}
	if from == "" {
		return fixed
	}
	if info, err := os.Lstat(from); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return fixed // records are never links (openFile refuses them too)
	}
	_ = os.Remove(from + ".merged")                // the moved file's merge mark belongs to it no more (re-audit D-1)
	if err := os.Rename(from, fixed); err != nil { // replaces an empty file or folder in one step (0.9.7 audit B-a)
		if _, statErr := os.Lstat(fixed); statErr != nil { // not moved by another process either
			return from
		}
	}
	return fixed
}

// empty reports whether path is missing, an empty file or an empty folder.
func empty(path string) bool {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		return err == nil && len(entries) == 0
	}
	return info.Size() == 0
}

func onlyHashNamed(dir, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	only := ""
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ext)
		if !ok || !hashName.MatchString(name) || e.IsDir() != (ext == "") {
			continue
		}
		if only != "" {
			return ""
		}
		only = filepath.Join(dir, e.Name())
	}
	return only
}
