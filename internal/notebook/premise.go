package notebook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

type Premise struct {
	Path string `json:"path" yaml:"path"`
	Hash string `json:"hash" yaml:"hash"`
}

type PremiseChange struct {
	Path    string `json:"path"`
	Before  string `json:"before"`
	Current string `json:"current,omitempty"`
	State   string `json:"state"`
}

func premisePath(path string) bool {
	if path == "" || len(path) > 300 || !filepath.IsLocal(path) || filepath.ToSlash(filepath.Clean(path)) != path || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return !strings.ContainsFunc(path, unicode.IsControl)
}

// Read only regular, bounded project files. No links, outside paths, commands or URLs.
func (b Book) premiseHash(path string) (string, string) {
	if !premisePath(path) {
		return "", "unreadable"
	}
	root, err := os.OpenRoot(b.Scope)
	if err != nil {
		return "", "unreadable"
	}
	defer root.Close()
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if os.IsNotExist(err) {
			return "", "missing"
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", "unreadable"
		}
	}
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", "unreadable"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return "", "unreadable"
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		return "", "unreadable"
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (1<<20)+1))
	if err != nil || n > 1<<20 {
		return "", "unreadable"
	}
	return hex.EncodeToString(h.Sum(nil)), "ok"
}

func (b Book) snapshotPremises(paths []string) ([]Premise, error) {
	if len(paths) > 4 {
		return nil, errors.New("watch accepts at most four project files")
	}
	var out []Premise
	seen := map[string]bool{}
	for _, path := range paths {
		if !premisePath(path) || seen[path] {
			return nil, errors.New("watch needs distinct, relative project file paths without hidden segments or links")
		}
		seen[path] = true
		hash, state := b.premiseHash(path)
		if state != "ok" {
			return nil, errors.New("a watched file must be readable, regular and at most 1 MiB")
		}
		out = append(out, Premise{Path: path, Hash: hash})
	}
	return out, nil
}

func (b Book) premiseChanges(p Page) []PremiseChange {
	var out []PremiseChange
	for _, ref := range p.Premises {
		hash, state := b.premiseHash(ref.Path)
		if state == "ok" && hash == ref.Hash {
			continue
		}
		if state == "ok" {
			state = "changed"
		}
		out = append(out, PremiseChange{Path: ref.Path, Before: ref.Hash, Current: hash, State: state})
	}
	return out
}
