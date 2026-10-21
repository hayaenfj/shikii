package store

// 判定モデルへ渡す短い入力と、人が読む原文を分ける。
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
	"syscall"
	"unicode/utf8"
)

const RecordTextLimit = 64 << 10

// 以前の上限 1,500 バイトで、最後の UTF-8 の文字を除くとこの長さになることがある。
const legacyTextFloor = 1500 - utf8.UTFMax + 1

type recordText struct {
	InputsHash string            `json:"inputs_hash"`
	Inputs     map[string]string `json:"inputs"`
	Limited    []string          `json:"limited,omitempty"`
}

var textTrace = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (s Store) recordTextPath(rec Record) (string, error) {
	if !textTrace.MatchString(rec.TraceID) || rec.InputsHash == "" {
		return "", errors.New("原文には判定の識別子と入力のハッシュが必要です")
	}
	return filepath.Join(s.Dir, "records", "text", rec.TraceID+".json"), nil
}

func (s Store) SaveRecordText(rec Record, originals map[string]string) error {
	if len(originals) > 4 {
		return errors.New("原文の欄が多すぎます")
	}
	text := recordText{InputsHash: rec.InputsHash, Inputs: map[string]string{}}
	for key, value := range originals {
		if value == "" {
			continue
		}
		input, ok := rec.Inputs[key]
		if !ok || !utf8.ValidString(value) || !strings.HasPrefix(value, input) {
			return errors.New("原文と判定に保存した冒頭が一致しません")
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("原文の置き場を確認できません")
	}
	data, err := json.Marshal(text)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".text-*")
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
		old, err := readRecordText(path)
		if err != nil || !bytes.Equal(old, data) {
			return errors.New("保存済みの原文は上書きできません")
		}
	}
	return nil
}

func readRecordText(path string) ([]byte, error) {
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("原文の置き場を確認できません")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if len(data) > 2<<20 {
		return nil, errors.New("原文の保存上限を超えています")
	}
	return data, err
}

// RecordText は表示用の原文と、保存上限に達した欄の状態。
// 元の inputs やハッシュ、正解のラベルには書き戻さない。
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
	data, err := readRecordText(path)
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
