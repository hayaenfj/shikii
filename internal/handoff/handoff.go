// Package handoff は各プロジェクトの .shikii/handoff/ (作業記録の置き場)を扱う。
//
// 3 つの置き場の役割(0.8.1):
//   - handoff: 途中で書き換える作業ファイル(監査の報告、リリースの手順と記録、差分、画像)。人もエージェントも読み書きする
//   - wiki の原資料(records/wiki-sources): 確かめた時点の証拠。書いた後は変えない
//   - wiki の記事: 次の依頼に効く教訓
//
// 固定の規則(オプションは無い): 置き場の外は読まない・書かない(os.Root で閉じる)。リンクは辿らず一覧にも出さない。
// 隠しファイル(. で始まる名)・制御文字や書式の文字を含む名は扱わない。途中のフォルダのリンクとハードリンクも扱わない。書くのは UTF-8 の文字だけで 1 回 1 MiB まで。読むのは文字なら 4 MiB まで。
// gc も update も中身を消さない(0.7.0 を跨ぐ消去の時だけ、ほかの置き場と一緒に消える)。
package handoff

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Dir は .shikii/ の下の作業記録の置き場の名前。
const Dir = "handoff"

const (
	writeMax = 1 << 20 // 1 回に書ける大きさ
	readMax  = 4 << 20 // 文字として返す大きさ
	pathMax  = 256

	staleTempAge = 24 * time.Hour // 落ちて残った一時ファイルを片付けるまでの時間
)

// Entry は一覧の 1 件。Path は置き場からの相対(/ 区切り)。
type Entry struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Kind     string    `json:"kind"` // text / image / other
}

// File は読んだ 1 件。文字でない物は Content を空にし、Kind と Size だけ返す。
type File struct {
	Entry
	Content string `json:"content,omitempty"`
	Note    string `json:"note,omitempty"`
}

var textExt = map[string]bool{".md": true, ".txt": true, ".json": true, ".jsonl": true, ".patch": true, ".diff": true, ".log": true, ".csv": true, ".tsv": true,
	".yaml": true, ".yml": true, ".toml": true, ".go": true, ".js": true, ".mjs": true, ".ts": true, ".py": true, ".sh": true, ".html": true, ".css": true, ".svelte": true, ".sql": true, ".xml": true}

// ImageTypes は画面が画像として出す拡張子と Content-Type。svg は中に script を持てるので画像として出さない。
var ImageTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

// KindOf は拡張子から種類を決める。
func KindOf(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if _, ok := ImageTypes[ext]; ok {
		return "image"
	}
	if textExt[ext] || ext == "" {
		return "text"
	}
	return "other"
}

// Clean は置き場からの相対パスを確かめて正規化する。絶対パス・..・隠し名・\・空の区切りは断る。
func Clean(p string) (string, error) {
	p = strings.TrimPrefix(p, ".shikii/handoff/") // 原資料の reference に書いた形(.shikii/handoff/<パス>)もそのまま読める
	if p == "" || len(p) > pathMax || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("作業記録のパスは置き場(.shikii/handoff/)からの相対で書きます: %q", p)
	}
	for _, c := range p {
		if unicode.IsControl(c) || unicode.Is(unicode.Bidi_Control, c) || c == '\uFEFF' {
			return "", fmt.Errorf("作業記録のパスに制御文字や書式の文字は使えません: %q", p)
		}
	}
	parts := strings.Split(p, "/")
	for _, s := range parts {
		if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") {
			return "", fmt.Errorf("作業記録のパスに空・.・..・隠し名は使えません: %q", p)
		}
	}
	return strings.Join(parts, "/"), nil
}

// Book は 1 つのプロジェクトの置き場。Store は .shikii/ のパス。
type Book struct{ Store string }

func (b Book) dir() string { return filepath.Join(b.Store, Dir) }

// Ensure は置き場を作る(init が呼ぶ)。
func (b Book) Ensure() error { return os.MkdirAll(b.dir(), 0o700) }

func (b Book) open() (*os.Root, error) {
	if info, err := os.Lstat(b.dir()); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("作業記録の置き場(.shikii/handoff)がリンクです。リンクは辿りません")
	}
	r, err := os.OpenRoot(b.dir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fs.ErrNotExist
	}
	return r, err
}

// List は置き場の全部のファイルを、更新の新しい順に返す(件数で切らない)。置き場が無ければ空。
func (b Book) List() ([]Entry, error) {
	out := []Entry{}
	r, err := b.open()
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	err = fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() && !d.IsDir() {
			return nil // リンクと特殊なファイルは出さない(辿らない)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Entry{Path: p, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(p)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// parentsAreFolders は途中のフォルダがどれもリンクでないか(置き場の中を指すリンクも辿らない)。
func parentsAreFolders(r *os.Root, clean string) bool {
	dir := path.Dir(clean)
	if dir == "." {
		return true
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		info, err := r.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return errors.Is(err, fs.ErrNotExist) // まだ無いフォルダは書く時に作る
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// beforeOpen はテストが Lstat と Open の間に差し替えを起こすための口(本番では nil)。
var beforeOpen func()

// Open は 1 件を開く(画面がそのまま配る時に使う)。リンクと置き場の外は開かない。
func (b Book) Open(p string) (*os.File, Entry, error) {
	clean, err := Clean(p)
	if err != nil {
		return nil, Entry{}, err
	}
	r, err := b.open()
	if err != nil {
		return nil, Entry{}, fmt.Errorf("作業記録が見つかりません: %s", clean)
	}
	defer r.Close()
	info, err := r.Lstat(clean)
	if err != nil || !info.Mode().IsRegular() || linkCount(info) > 1 || !parentsAreFolders(r, clean) {
		return nil, Entry{}, fmt.Errorf("作業記録が見つかりません: %s", clean)
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	f, err := r.OpenFile(clean, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return nil, Entry{}, fmt.Errorf("作業記録が見つかりません: %s", clean)
	}
	opened, err := f.Stat() // 開いた物が確かめた物と同じか(間で差し替わったら配らない)。大きさは開いた物のもの
	if err != nil || !os.SameFile(opened, info) {
		f.Close()
		return nil, Entry{}, fmt.Errorf("作業記録が見つかりません: %s", clean)
	}
	return f, Entry{Path: clean, Size: opened.Size(), Modified: opened.ModTime().UTC(), Kind: KindOf(clean)}, nil
}

// Read は 1 件を読む。文字は中身を返し、画像などは種類と大きさだけ返す(画面で見る)。
func (b Book) Read(p string) (File, error) {
	f, e, err := b.Open(p)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	out := File{Entry: e}
	if e.Kind != "text" {
		out.Note = "文字ではないので中身は返しません。画面の知識の欄「作業記録」で見るか、ファイルを直接開きます"
		return out, nil
	}
	if e.Size > readMax {
		return File{}, fmt.Errorf("作業記録 %s は %d バイトで、文字として返せる %d バイトを超えます。ファイルを直接読みます", e.Path, e.Size, readMax)
	}
	data, err := io.ReadAll(io.LimitReader(f, readMax+1))
	if err != nil {
		return File{}, err
	}
	if !utf8.Valid(data) {
		out.Note = "UTF-8 の文字ではないので中身は返しません"
		return out, nil
	}
	out.Content = string(data)
	return out, nil
}

// Write は 1 件を書く(appendMode なら末尾に足す)。親のフォルダは作る。書き換えは一時ファイルから置き換える。
func (b Book) Write(p, content string, appendMode bool) (Entry, error) {
	clean, err := Clean(p)
	if err != nil {
		return Entry{}, err
	}
	if len(content) > writeMax {
		return Entry{}, fmt.Errorf("1 回に書ける作業記録は %d バイトまでです(%d バイト)。分けて append します", writeMax, len(content))
	}
	if !utf8.ValidString(content) {
		return Entry{}, errors.New("作業記録に書けるのは UTF-8 の文字だけです。画像などはファイルとして .shikii/handoff/ に置きます")
	}
	if err := b.Ensure(); err != nil {
		return Entry{}, errors.New("作業記録の置き場(.shikii/handoff)を作れません")
	}
	r, err := b.open()
	if err != nil {
		return Entry{}, err
	}
	defer r.Close()
	if !parentsAreFolders(r, clean) {
		return Entry{}, fmt.Errorf("作業記録 %s の途中のフォルダがリンクかファイルです(リンクは辿りません)", clean)
	}
	if dir := path.Dir(clean); dir != "." {
		if err := r.MkdirAll(dir, 0o700); err != nil {
			return Entry{}, fmt.Errorf("作業記録のフォルダ %s を作れません", dir)
		}
	}
	removeStaleTemps(r, path.Dir(clean))
	if info, err := r.Lstat(clean); err == nil && (!info.Mode().IsRegular() || linkCount(info) > 1) {
		return Entry{}, fmt.Errorf("作業記録 %s はふつうのファイルではありません(リンク・ハードリンク・フォルダには書きません)", clean)
	}
	if appendMode {
		f, err := r.OpenFile(clean, os.O_CREATE|os.O_WRONLY|os.O_APPEND|noFollow, 0o600)
		if err != nil {
			return Entry{}, err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			return Entry{}, err
		}
		if err := f.Close(); err != nil {
			return Entry{}, err
		}
	} else {
		var rnd [8]byte // 一時名は毎回ちがう名(同時の書き込みが混ざらない、前もって置かれたリンクを辿らない、長い名前でも上限を超えない)
		if _, err := rand.Read(rnd[:]); err != nil {
			return Entry{}, err
		}
		tmp := path.Join(path.Dir(clean), ".write-"+hex.EncodeToString(rnd[:]))
		f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Entry{}, err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			_ = r.Remove(tmp)
			return Entry{}, err
		}
		if err := f.Close(); err != nil {
			_ = r.Remove(tmp)
			return Entry{}, err
		}
		if err := r.Rename(tmp, clean); err != nil {
			_ = r.Remove(tmp)
			return Entry{}, err
		}
	}
	info, err := r.Stat(clean)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: clean, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(clean)}, nil
}

// isTempName は、置き換えの一時ファイルの名(.write- と 16 桁の 16 進。Write が rand で作る)か。
// 人が置いた .write-up.md のような隠しファイルは一時ファイルとみなさない。
func isTempName(name string) bool {
	hexPart, ok := strings.CutPrefix(name, ".write-")
	if !ok || len(hexPart) != 16 {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil && strings.ToLower(hexPart) == hexPart
}

// removeStaleTemps は、書き換えの途中で落ちて残った一時ファイル(isTempName)のうち 1 日より古いものを消す。
// 隠し名なので一覧にも画面にも出ず、gc も触らないため、次にそのフォルダへ書く時に片付ける。
// 書き込み中の一時ファイル(新しいもの)とリンクには触らない。
func removeStaleTemps(r *os.Root, dir string) {
	entries, err := fs.ReadDir(r.FS(), dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleTempAge)
	for _, e := range entries {
		if !isTempName(e.Name()) || !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = r.Remove(path.Join(dir, e.Name()))
		}
	}
}
