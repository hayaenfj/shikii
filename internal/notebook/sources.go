package notebook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

// readSourceRecord reads a captured source record. A source has no size limit of its own; only the store's line limit applies.
func readSourceRecord(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(store.MaxLineBytes)+1))
	if err == nil && len(data) > store.MaxLineBytes {
		err = errors.New("the saved source record exceeds the store's line limit")
	}
	return data, err
}

// sourceRecordDigest hashes a source record without holding it in memory (the hook checks every cited source on each request).
func sourceRecordDigest(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, int64(store.MaxLineBytes)+1))
	if err == nil && n > int64(store.MaxLineBytes) {
		err = errors.New("the saved source record exceeds the store's line limit")
	}
	return hex.EncodeToString(h.Sum(nil)), err
}

func readOriginals(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if len(data) > 2<<20 {
		return nil, errors.New("the long source text must be 2MB or less")
	}
	return data, err
}

func immutableLarge(path string, data []byte) error {
	// Even when the same original is sent again, do not replace it with a partial file or different content.
	f, err := os.CreateTemp(filepath.Dir(path), ".source-text-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Link(f.Name(), path); err != nil {
		if !os.IsExist(err) {
			return err
		}
		old, e := readOriginals(path)
		if e != nil || !bytes.Equal(old, data) {
			return errors.New("a saved original cannot be changed")
		}
	}
	return nil
}

// Observation is an agent-supplied source snapshot, never a truth or permission.
// Reference is descriptive: capture does not read files, execute commands or fetch URLs.
type Observation struct {
	ID         string    `json:"id,omitempty"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Reference  string    `json:"reference"`
	Version    string    `json:"version"`
	ObservedAt time.Time `json:"observed_at"`
}

func (o Observation) valid() bool {
	if o.Kind != "human" && o.Kind != "agent" && o.Kind != "code" && o.Kind != "test" && o.Kind != "release" {
		return false
	}
	for _, text := range []string{o.Title, o.Content, o.Reference, o.Version} {
		if !utf8.ValidString(text) || strings.TrimSpace(text) == "" {
			return false
		}
	}
	return len(o.Title) <= 200 && len(o.Reference) <= 1000 && len(o.Version) <= 200 && !o.ObservedAt.IsZero() && o.ObservedAt.Year() >= 1970 && o.ObservedAt.Year() <= 9999
}

func (b Book) Capture(o Observation) (Observation, error) {
	if o.ID != "" || !o.valid() {
		return Observation{}, errors.New("a source record needs kind, title, content, reference, version and observed time; do not give an ID")
	}
	o.ObservedAt = o.ObservedAt.UTC()
	data, _ := json.Marshal(o)
	o.ID = digest(append([]byte(b.Scope+"\n"), data...))[:16]
	dir, err := b.directory(true, "sources")
	if err != nil {
		return Observation{}, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return Observation{}, err
	}
	defer unlock()
	ids, err := capturedIDs(dir)
	if err != nil {
		return Observation{}, err
	}
	kept := []string{}
	for _, id := range ids {
		if id == o.ID {
			return b.readObservation(dir, id)
		}
		if _, err := os.Lstat(filepath.Join(dir, id+".reviewed")); os.IsNotExist(err) {
			kept = append(kept, id)
		} else if err != nil {
			return Observation{}, err
		}
	}
	data, _ = json.Marshal(o)
	if len(data) > store.MaxLineBytes {
		return Observation{}, fmt.Errorf("the source record JSON must be %d MiB or less (the store's line limit)", store.MaxLineBytes>>20)
	}
	if err := immutable(filepath.Join(dir, o.ID+".json"), data); err != nil {
		return Observation{}, err
	}
	kept = append(kept, o.ID)
	data, _ = json.Marshal(kept)
	if err := writeFile(filepath.Join(dir, ".captured.json"), data); err != nil {
		return Observation{}, err
	}
	return o, nil
}

func capturedIDs(dir string) ([]string, error) {
	data, err := readRegistry(filepath.Join(dir, ".captured.json"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if json.Unmarshal(data, &ids) != nil {
		return nil, errors.New("cannot read the source record queue")
	}
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			return nil, errors.New("invalid source record ID")
		}
	}
	return ids, nil
}

func (b Book) readObservation(dir, id string) (Observation, error) {
	data, err := readSourceRecord(filepath.Join(dir, id+".json"))
	if err != nil {
		return Observation{}, err
	}
	var o Observation
	if json.Unmarshal(data, &o) != nil || o.ID != id || !o.valid() {
		return Observation{}, errors.New("the saved source record is invalid")
	}
	want := o.ID
	o.ID = ""
	canonical, _ := json.Marshal(o)
	o.ID = want
	if digest(append([]byte(b.Scope+"\n"), canonical...))[:16] != id {
		return Observation{}, errors.New("the hash of the saved source record does not match")
	}
	return o, nil
}

func (b Book) PendingObservations() ([]Observation, error) {
	dir, err := b.directory(false, "sources")
	if os.IsNotExist(err) {
		return []Observation{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids, err := capturedIDs(dir)
	if err != nil {
		return nil, err
	}
	out := []Observation{}
	for _, id := range ids {
		if _, err := os.Lstat(filepath.Join(dir, id+".reviewed")); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		o, err := b.readObservation(dir, id)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
		if len(out) == 2 {
			break
		}
	}
	return out, nil
}

type Original struct {
	TraceID string            `json:"trace_id"`
	Inputs  map[string]string `json:"original_inputs,omitempty"`
	Status  map[string]string `json:"text_status,omitempty"`
}

func (b Book) originals(f store.Feedback) []Original {
	st := store.Store{Dir: b.Root}
	out := []Original{}
	for _, target := range f.Targets {
		if !store.ReviewSpec(target.Spec) || !idPattern.MatchString(target.TraceID) {
			continue
		}
		rec, err := st.FindRecord(target.Spec, target.TraceID)
		if err != nil || rec.Inputs["request"] != f.Request || rec.Inputs["summary"] != f.Summary {
			continue
		}
		inputs, status := st.RecordText(*rec)
		if len(inputs) > 0 || len(status) > 0 {
			out = append(out, Original{rec.TraceID, inputs, status})
		}
	}
	return out
}

func validSource(data []byte, id string) bool {
	var f store.Feedback
	if json.Unmarshal(data, &f) == nil && f.ID == id && f.Request != "" && f.Summary != "" && f.Reply != "" {
		return true
	}
	var o Observation
	return json.Unmarshal(data, &o) == nil && o.ID == id && o.valid()
}

// OtherPathStores returns, as paths relative to Root, the wiki stores left in this project's store under a different path's name
// (sha256(path) differs from the current path) (0.9.2). Left under the old name after moving or renaming the folder, the wiki silently became empty.
// A store whose files have all been copied to the current store is not counted (the old one may be kept, not deleted).
func (b Book) OtherPathStores() ([]string, error) {
	if b.Scope == "" || b.Root == "" {
		return nil, errors.New("wiki needs a project and a store")
	}
	root, err := filepath.EvalSymlinks(b.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	own := digest([]byte(b.Scope))
	out := []string{}
	for _, area := range []string{"records/wiki-sources", "wiki/lessons"} {
		entries, err := os.ReadDir(filepath.Join(root, area))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == own || !scopeName.MatchString(e.Name()) {
				continue
			}
			left, err := notCopied(filepath.Join(root, area, e.Name()), filepath.Join(root, area, own))
			if err != nil {
				return nil, err
			}
			if left {
				out = append(out, area+"/"+e.Name())
			}
		}
	}
	return out, nil
}

var scopeName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// notCopied reports whether the old store has a file missing at the same place in the current store. Things rebuilt per store (lock, check marker, index, log) are ignored.
func notCopied(old, own string) (bool, error) {
	left := false
	err := filepath.WalkDir(old, func(path string, d os.DirEntry, err error) error {
		if err != nil || left {
			return err
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") || d.Name() == "index.md" || d.Name() == "log.md" {
			return nil
		}
		rel, err := filepath.Rel(old, path)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(own, rel)); os.IsNotExist(err) {
			left = true
		} else if err != nil {
			return err
		}
		return nil
	})
	return left, err
}

// fromOtherPath reports whether a source record a page cites was captured under the previous path (the path in its id differs from now, so it cannot be checked).
// Conversation sources (those without a kind) are not counted.
func (b Book) fromOtherPath(dir, id string) bool {
	data, err := readSourceRecord(filepath.Join(dir, id+".json"))
	if err != nil {
		return false
	}
	var o Observation
	if json.Unmarshal(data, &o) != nil || o.Kind == "" {
		return false
	}
	_, err = b.readObservation(dir, id)
	return err != nil
}

// SourceHeadBytes is how much of a pending source's body the reflection and the wiki listing show.
// A source has no size limit of its own, so a long one is listed by its head; the whole body is read with wiki query <source id>.
const SourceHeadBytes = 1500

// ObservationHead is a pending source as listed for sorting: everything but the body, the body's length, and its head.
type ObservationHead struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	ContentBytes int       `json:"content_bytes"`
	ContentCut   bool      `json:"content_cut,omitempty"` // true: read the whole body with wiki query <id>
	Reference    string    `json:"reference"`
	Version      string    `json:"version"`
	ObservedAt   time.Time `json:"observed_at"`
}

// Heads lists sources by their head.
func Heads(list []Observation) []ObservationHead {
	out := make([]ObservationHead, 0, len(list))
	for _, o := range list {
		h := ObservationHead{ID: o.ID, Kind: o.Kind, Title: o.Title, Content: o.Content, ContentBytes: len(o.Content), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt}
		if len(h.Content) > SourceHeadBytes {
			end := SourceHeadBytes
			for end > 0 && !utf8.RuneStart(h.Content[end]) { // do not cut a character in half
				end--
			}
			h.Content, h.ContentCut = h.Content[:end], true
		}
		out = append(out, h)
	}
	return out
}
