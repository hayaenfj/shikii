package notebook

import (
	"bytes"
	"encoding/json"
	"errors"
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

func readOriginals(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if len(data) > 2<<20 {
		return nil, errors.New("原資料の長文は2MB以内です")
	}
	return data, err
}

func immutableLarge(path string, data []byte) error {
	// 同じ原文の再送でも、途中のファイルや別の内容で置き換えない。
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
			return errors.New("保存済みの原文は変更できません")
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
	return len(o.Title) <= 200 && len(o.Content) <= 32<<10 && len(o.Reference) <= 1000 && len(o.Version) <= 200 && !o.ObservedAt.IsZero() && o.ObservedAt.Year() >= 1970 && o.ObservedAt.Year() <= 9999
}

func (b Book) Capture(o Observation) (Observation, error) {
	if o.ID != "" || !o.valid() {
		return Observation{}, errors.New("原資料には種類・題・本文（32KB以内）・参照箇所・版・確認時点が必要です。IDは指定しません")
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
	if len(data) > 64<<10 {
		return Observation{}, errors.New("原資料のJSONは64KB以内です")
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
		return nil, errors.New("原資料の待ち行列を読み取れません")
	}
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			return nil, errors.New("原資料のIDが不正です")
		}
	}
	return ids, nil
}

func (b Book) readObservation(dir, id string) (Observation, error) {
	data, err := readFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return Observation{}, err
	}
	var o Observation
	if json.Unmarshal(data, &o) != nil || o.ID != id || !o.valid() {
		return Observation{}, errors.New("保存された原資料が不正です")
	}
	want := o.ID
	o.ID = ""
	canonical, _ := json.Marshal(o)
	o.ID = want
	if digest(append([]byte(b.Scope+"\n"), canonical...))[:16] != id {
		return Observation{}, errors.New("保存された原資料のハッシュが一致しません")
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

// OtherPathStores は、このプロジェクトの置き場に残った、別のパスの名(sha256(パス) が今のパスと違う)の wiki の置き場を、
// Root からの相対パスで返す(0.9.2)。フォルダを移した・名前を変えた後に古い名のまま残ると、wiki が黙って空になっていた。
// 中のファイルが全部今の置き場に写してある置き場は数えない(古い方は消さずに残してよい)。
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

// notCopied は、古い置き場に、今の置き場の同じ場所に無いファイルがあるか。置き場ごとに作り直す物(錠・点検の印・索引・記録)は見ない。
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

// fromOtherPath は、記事が出典にした原資料のうち、前のパスで控えた物(id に入るパスが今と違い、確かめられない)か。
// 会話の出典(種類の無い物)は数えない。
func (b Book) fromOtherPath(dir, id string) bool {
	data, err := readFile(filepath.Join(dir, id+".json"))
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
