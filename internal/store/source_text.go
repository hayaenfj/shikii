package store

// Keeps the short input passed to the decision model apart from the original text people read.
import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const RecordTextLimit = 64 << 10

// Under the old 1,500-byte limit, dropping the last UTF-8 character can give this length.
const legacyTextFloor = 1500 - utf8.UTFMax + 1

type recordText struct {
	InputsHash string            `json:"inputs_hash"`
	Inputs     map[string]string `json:"inputs"`
	Limited    []string          `json:"limited,omitempty"`
}

var textTrace = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (s Store) recordTextPath(rec Record) (string, error) {
	if !textTrace.MatchString(rec.TraceID) || rec.InputsHash == "" {
		return "", errors.New("original text needs the decision's identifier and the input hash")
	}
	return filepath.Join(s.Dir, "records", "text", rec.TraceID+".json"), nil
}

func (s Store) SaveRecordText(rec Record, originals map[string]string) error {
	if len(originals) > 4 {
		return errors.New("too many original text fields")
	}
	text := recordText{InputsHash: rec.InputsHash, Inputs: map[string]string{}}
	for key, value := range originals {
		if value == "" {
			continue
		}
		input, ok := rec.Inputs[key]
		if !ok || !utf8.ValidString(value) || !strings.HasPrefix(value, input) {
			return errors.New("the original text does not match the opening saved with the decision")
		}
		if value == input && len(value) < legacyTextFloor {
			continue
		}
		if len(value) > RecordTextLimit {
			end := RecordTextLimit
			for end > 0 && !utf8.RuneStart(value[end]) {
				end--
			}
			value = value[:end]
			text.Limited = append(text.Limited, key)
		}
		text.Inputs[key] = value
	}
	if len(text.Inputs) == 0 {
		return nil
	}
	sort.Strings(text.Limited)
	path, err := s.recordTextPath(rec)
	if err != nil {
		return err
	}
	parent, name, err := s.parentRoot(path, true)
	if err != nil {
		return err
	}
	defer parent.Close()
	data, err := json.Marshal(text)
	if err != nil {
		return err
	}
	tmpName := ".text-" + NewTraceID()
	f, err := parent.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer parent.Remove(tmpName)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := parent.Link(tmpName, name); err != nil {
		if !os.IsExist(err) {
			return err
		}
		old, err := s.readRecordText(path)
		if err != nil || !bytes.Equal(old, data) {
			return errors.New("saved original text cannot be overwritten")
		}
	}
	return nil
}

func (s Store) readRecordText(path string) ([]byte, error) {
	f, err := s.openFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if len(data) > 2<<20 {
		return nil, errors.New("the original text exceeds the storage limit")
	}
	return data, err
}

// RecordText is the original text for display and the state of fields that reached the storage limit.
// Nothing is written back to the original inputs, hashes or truth labels.
func (s Store) RecordText(rec Record) (map[string]string, map[string]string) {
	originals, status := map[string]string{}, map[string]string{}
	if rec.SpecID == "prompt-kind" || rec.SpecID == "needs-plan" || ReviewSpec(rec.SpecID) {
		for key, value := range rec.Inputs {
			if (key == "summary" || key == "request" || key == "previous") && len(value) >= legacyTextFloor {
				status[key] = "legacy"
			}
		}
	}
	path, err := s.recordTextPath(rec)
	if err != nil {
		return originals, status
	}
	data, err := s.readRecordText(path)
	if os.IsNotExist(err) {
		return originals, status
	}
	var text recordText
	if err != nil || json.Unmarshal(data, &text) != nil || text.InputsHash != rec.InputsHash || len(text.Inputs) == 0 || len(text.Inputs) > 4 {
		for key := range status {
			status[key] = "unavailable"
		}
		return originals, status
	}
	for key, value := range text.Inputs {
		input, ok := rec.Inputs[key]
		if !ok || len(value) > RecordTextLimit || !utf8.ValidString(value) || !strings.HasPrefix(value, input) {
			for key := range status {
				status[key] = "unavailable"
			}
			return originals, status
		}
	}
	for key, value := range text.Inputs {
		originals[key] = value
		delete(status, key)
	}
	for _, key := range text.Limited {
		if _, ok := originals[key]; ok {
			status[key] = "limited"
		}
	}
	return originals, status
}
