// Package notebook は原資料から育てる wiki。文章を書くのは利用中のエージェントで、
// shikii は出典・索引・変更履歴を管理する。教訓は正解や実行許可にはならない。
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
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

type Book struct{ Root, Scope string }

type Source struct {
	ID       string `json:"id" yaml:"id"`
	Hash     string `json:"hash" yaml:"hash"`
	TextHash string `json:"text_hash,omitempty" yaml:"text_hash,omitempty"`
}

type Page struct {
	ID           string            `json:"id" yaml:"id"`
	Title        string            `json:"title" yaml:"title"`
	Lesson       string            `json:"lesson" yaml:"lesson"`
	AppliesTo    string            `json:"applies_to" yaml:"applies_to"`
	Status       string            `json:"status" yaml:"status"`
	Sources      []Source          `json:"sources" yaml:"sources"`
	Replaces     []string          `json:"replaces,omitempty" yaml:"replaces,omitempty"`
	Revision     string            `json:"revision" yaml:"revision"`
	Previous     string            `json:"previous,omitempty" yaml:"previous,omitempty"`
	Correction   *store.Correction `json:"correction,omitempty" yaml:"correction,omitempty"`
	Body         string            `json:"body,omitempty" yaml:"body,omitempty"`
	Related      []string          `json:"related,omitempty" yaml:"related,omitempty"`
	ChangeReason string            `json:"change_reason,omitempty" yaml:"change_reason,omitempty"`
}

type Draft struct {
	ID           string            `json:"id,omitempty"`
	Title        string            `json:"title,omitempty"`
	Lesson       string            `json:"lesson,omitempty"`
	AppliesTo    string            `json:"applies_to,omitempty"`
	Sources      []string          `json:"sources"`
	Replaces     []string          `json:"replaces,omitempty"`
	Correction   *store.Correction `json:"correction,omitempty"`
	Body         *string           `json:"body,omitempty"`
	Related      []string          `json:"related,omitempty"`
	ChangeReason string            `json:"change_reason,omitempty"`
}

type Issue struct {
	Kind   string `json:"kind"`
	Page   string `json:"page,omitempty"`
	Source string `json:"source,omitempty"`
}

type View struct {
	Lessons      []Page            `json:"lessons"`
	Pending      []store.Feedback  `json:"pending_sources"`
	Issues       []Issue           `json:"issues"`
	Matches      []Match           `json:"matches,omitempty"`
	Observations []Observation     `json:"observations,omitempty"`
	Maintenance  []MaintenanceTask `json:"maintenance,omitempty"`
	Search       *SearchResult     `json:"search,omitempty"`
	NextCursor   string            `json:"next_cursor,omitempty"`
}

type Result struct {
	Page     *Page    `json:"page"`
	Reviewed []string `json:"reviewed"`
}

type Report struct {
	Issues   []Issue  `json:"issues"`
	Repaired []string `json:"repaired"`
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// directory は固定の配下だけを使う。生成領域の symlink はたどらない。
func (b Book) directory(create bool, area string) (string, error) {
	if b.Scope == "" || b.Root == "" {
		return "", errors.New("wiki needs a project and a store")
	}
	parts := []string{"wiki", "lessons", digest([]byte(b.Scope))}
	if area == "sources" {
		parts = []string{"records", "wiki-sources", digest([]byte(b.Scope))}
	}
	if create {
		if err := os.MkdirAll(b.Root, 0o700); err != nil {
			return "", err
		}
	}
	path, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		return "", err
	}
	for _, part := range parts {
		path = filepath.Join(path, part)
		if create {
			if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
				return "", err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("wiki generated directories must not be symbolic links")
		}
	}
	return path, nil
}

func readFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err == nil && len(b) > 64<<10 {
		err = errors.New("wiki page exceeds the size limit")
	}
	return b, err
}

func writeFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".wiki-*")
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
	return os.Rename(f.Name(), path)
}

func immutable(path string, data []byte) error {
	// 完全な一時ファイルを link して公開する。存在している原資料は置き換えない。
	f, err := os.CreateTemp(filepath.Dir(path), ".source-*")
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
		old, rerr := readFile(path)
		if rerr != nil || !bytes.Equal(old, data) {
			return errors.New("an immutable wiki source or revision has changed")
		}
	}
	return nil
}

func lock(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	until := time.Now().Add(2 * time.Second)
	for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if time.Now().After(until) {
			f.Close()
			return nil, errors.New("wiki is busy; retry later")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

func (b Book) pageBytes(p Page) ([]byte, error) {
	front, err := yaml.Marshal(p)
	if err != nil {
		return nil, err
	}
	var links strings.Builder
	for _, source := range p.Sources {
		fmt.Fprintf(&links, "- [%s](../../../records/wiki-sources/%s/%s.json)\n", source.ID, digest([]byte(b.Scope)), source.ID)
	}
	correction := ""
	if p.Correction != nil {
		correction = "\n\n人の訂正の引用: " + p.Correction.Quote + "\n\n次に変える行動（仮説）: " + p.Correction.Action
	}
	article := ""
	if p.ChangeReason != "" {
		article = "\n\n変更理由: " + p.ChangeReason
	}
	if p.Body != "" {
		article += "\n\n" + p.Body
	}
	if len(p.Related) > 0 {
		article += "\n\n## 関連する知識\n\n"
		for _, id := range p.Related {
			article += fmt.Sprintf("- [%s](%s.md)\n", id, id)
		}
	}
	return []byte("---\n" + string(front) + "---\n\n# " + p.Title + "\n\n" + p.Lesson + "\n\n適用する場面: " + p.AppliesTo + article + correction + "\n\n## 出典\n\n" + links.String() + "\nこの教訓は出典付きの仮説です。今回の依頼と規則を優先します。\n"), nil
}

func parsePage(data []byte) (Page, error) {
	var p Page
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return p, errors.New("not a generated wiki lesson")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 {
		return p, errors.New("wiki lesson has no metadata boundary")
	}
	if err := yaml.Unmarshal(data[4:4+end], &p); err != nil {
		return p, err
	}
	if !idPattern.MatchString(p.ID) || p.Status != "hypothesis" || p.Title == "" || p.Lesson == "" || p.AppliesTo == "" || len(p.Sources) == 0 || len(p.Body) > 16<<10 {
		return p, errors.New("invalid wiki lesson metadata")
	}
	if p.Previous != "" && !idPattern.MatchString(p.Previous) {
		return p, errors.New("invalid previous wiki revision")
	}
	for _, id := range p.Related {
		if !idPattern.MatchString(id) || id == p.ID {
			return p, errors.New("invalid related wiki page")
		}
	}
	return p, nil
}

func revision(p Page) string {
	p.Revision = ""
	b, _ := json.Marshal(p)
	return digest(b)[:16]
}

func (b Book) pages() ([]Page, []Issue, error) {
	pages, issues := []Page{}, []Issue{}
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return pages, issues, nil
	}
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	// 全ての生成ページを対象にし、応答の分割はQueryPageで行う。
	entries, err := f.ReadDir(-1)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".md")
		if !idPattern.MatchString(id) || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		data, err := readFile(filepath.Join(dir, e.Name()))
		if err != nil {
			issues = append(issues, Issue{Kind: "unreadable-page", Page: id})
			continue
		}
		p, err := parsePage(data)
		if err != nil || p.ID != id || p.Revision != revision(p) {
			issues = append(issues, Issue{Kind: "changed-page", Page: id})
			continue
		}
		expected, _ := b.pageBytes(p)
		if !bytes.Equal(data, expected) {
			issues = append(issues, Issue{Kind: "changed-page", Page: id})
			continue
		}
		pages = append(pages, p)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	return pages, issues, nil
}

func (b Book) sourceIssues(p Page) []Issue {
	issues := []Issue{}
	dir, dirErr := b.directory(false, "sources")
	for _, s := range p.Sources {
		if !idPattern.MatchString(s.ID) {
			issues = append(issues, Issue{Kind: "invalid-source", Page: p.ID})
			continue
		}
		var data []byte
		err := dirErr
		if err == nil {
			data, err = readFile(filepath.Join(dir, s.ID+".json"))
		}
		if err != nil {
			issues = append(issues, Issue{Kind: "missing-source", Page: p.ID, Source: s.ID})
		} else if digest(data) != s.Hash {
			issues = append(issues, Issue{Kind: "changed-source", Page: p.ID, Source: s.ID})
		}
		if s.TextHash != "" {
			var text []byte
			err := dirErr
			if err == nil {
				text, err = readOriginals(filepath.Join(dir, s.ID+".text.json"))
			}
			if err != nil || digest(text) != s.TextHash {
				issues = append(issues, Issue{Kind: "changed-source-text", Page: p.ID, Source: s.ID})
			}
		}
	}
	return issues
}

func (b Book) Query(query string) (View, error) {
	return b.query(query, false)
}

// Recall は自動で届ける教訓。本文の偶然の一致より、題と適用条件を重視する。
// 手で探す Query は従来どおり本文も検索する。
// Live は検索に使える記事(出典が揃い、置き換えられていないもの)を、検索語で並べずに返す。差し込み先の判定の候補。
func (b Book) Live() ([]Page, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	superseded := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			superseded[id] = true
		}
	}
	live := []Page{}
	for _, p := range pages {
		if !superseded[p.ID] && len(b.sourceIssues(p)) == 0 {
			live = append(live, p)
		}
	}
	return live, nil
}

// RecallCandidates は自動配送の候補の上限。届けるのは呼ぶ側が既に届けた版を除いてから 2 件(監査 2026-10-23: 除く前に切ると席を塞ぐ)。
const RecallCandidates = 6

func (b Book) Recall(query string) ([]Page, error) {
	v, err := b.query(query, true)
	return v.Lessons, err
}

func (b Book) query(query string, recall bool) (View, error) {
	v := View{Lessons: []Page{}, Pending: []store.Feedback{}, Issues: []Issue{}}
	if !recall && strings.HasPrefix(query, "lesson:") && strings.Contains(query, "@") {
		p, err := b.readRevision(query)
		if err != nil {
			return v, err
		}
		v.Lessons = append(v.Lessons, p)
		return v, nil
	}
	pages, issues, err := b.pages()
	if err != nil {
		return v, err
	}
	v.Issues = append(v.Issues, issues...)
	superseded := map[string]bool{}
	valid := []Page{}
	exact := !recall && strings.HasPrefix(query, "lesson:")
	for _, p := range pages {
		bad := b.sourceIssues(p)
		v.Issues = append(v.Issues, bad...)
		if len(bad) > 0 {
			continue
		}
		for _, id := range p.Replaces {
			superseded[id] = true
		}
		valid = append(valid, p)
		// 直接参照は置き換え前のページも読める。出典の検証は省略しない。
		if !recall && query == "lesson:"+p.ID {
			return View{Lessons: []Page{p}, Pending: v.Pending, Issues: v.Issues}, nil
		}
	}
	for _, p := range valid {
		if !superseded[p.ID] && !exact {
			v.Lessons = append(v.Lessons, p)
		}
	}
	if query != "" || recall {
		v.Lessons, v.Matches = rankPages(v.Lessons, query, recall)
	}
	if query != "" && !recall {
		v.Search = &SearchResult{Total: len(v.Lessons), Candidates: []SearchCandidate{}}
		for i, page := range v.Lessons {
			v.Search.Candidates = append(v.Search.Candidates, SearchCandidate{Match: v.Matches[i], Title: page.Title, AppliesTo: page.AppliesTo})
		}
	}
	limit := len(v.Lessons)
	if query != "" && !recall {
		limit = 2
	}
	if recall {
		limit = RecallCandidates // 呼ぶ側(memoryContext)が、既に届けた版を除いてから 2 件に切る
	}
	if query == "" && !recall {
		v.Observations, err = b.PendingObservations()
		if err != nil {
			return v, err
		}
		v.Maintenance, err = b.Maintenance()
		if err != nil {
			return v, err
		}
	}
	if len(v.Lessons) > limit {
		v.Lessons = v.Lessons[:limit]
		if len(v.Matches) > limit {
			v.Matches = v.Matches[:limit]
		}
	}
	if recall {
		return v, nil
	}
	v.Pending, err = b.PendingSources()
	if len(v.Pending) > 2 {
		v.Pending = v.Pending[:2]
	}
	return v, err
}

// PendingSources は原資料を古いものから返す。読み出す履歴の量は Store が制限する。
func (b Book) PendingSources() ([]store.Feedback, error) {
	recent, err := (store.Store{Dir: b.Root}).RecentFeedback(b.Scope)
	if err != nil {
		return nil, err
	}
	dir, err := b.directory(false, "sources")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	pending := []store.Feedback{}
	for i := len(recent) - 1; i >= 0; i-- {
		f := recent[i]
		if !idPattern.MatchString(f.ID) {
			continue
		}
		if dir != "" {
			if _, err := os.Lstat(filepath.Join(dir, f.ID+".reviewed")); err == nil {
				continue
			}
		}
		pending = append(pending, f)
	}
	return pending, nil
}

func (b Book) Ingest(d Draft) (Result, error) {
	result := Result{Reviewed: []string{}}
	if d.Correction != nil && (!d.Correction.Valid() || d.Lesson == "") {
		return result, errors.New("訂正には人の発言の出典・引用と、次に変える行動の教訓が必要です")
	}
	d.Title, d.Lesson, d.AppliesTo = strings.TrimSpace(d.Title), strings.TrimSpace(d.Lesson), strings.TrimSpace(d.AppliesTo)
	body := ""
	if d.Body != nil {
		body = strings.TrimSpace(*d.Body)
	}
	if len(d.Sources) < 1 {
		return result, errors.New("wiki ingest needs source ids")
	}
	if d.Lesson != "" && (strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.AppliesTo) == "") {
		return result, errors.New("a lesson needs a title and the conditions where it applies")
	}
	for _, s := range []string{d.Title, d.Lesson, d.AppliesTo, body, d.ChangeReason} {
		if !utf8.ValidString(s) {
			return result, errors.New("wiki text must be UTF-8")
		}
	}
	if len(d.Title) > 200 || len(d.Lesson) > 2000 || len(d.AppliesTo) > 1000 || len(body) > 16<<10 || len(d.ChangeReason) > 1000 {
		return result, errors.New("wiki lesson exceeds the text limits")
	}
	if d.Lesson == "" && (d.Title != "" || d.AppliesTo != "" || d.ID != "" || len(d.Replaces) > 0 || d.Body != nil || d.Related != nil || d.ChangeReason != "") {
		return result, errors.New("reviewing a source without a lesson accepts only source ids")
	}
	for _, id := range append(append(append([]string{}, d.Sources...), d.Replaces...), d.Related...) {
		if !idPattern.MatchString(id) {
			return result, errors.New("wiki ids must be 16 lowercase hexadecimal characters")
		}
	}
	if d.ID != "" && !idPattern.MatchString(d.ID) {
		return result, errors.New("invalid wiki lesson id")
	}
	d.Sources = unique(d.Sources)
	dir, err := b.directory(true, "pages")
	if err != nil {
		return result, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return result, err
	}
	defer unlock()
	sourceDir, err := b.directory(true, "sources")
	if err != nil {
		return result, err
	}
	recent, err := (store.Store{Dir: b.Root}).RecentFeedback(b.Scope)
	if err != nil {
		return result, err
	}
	available := map[string]store.Feedback{}
	for _, f := range recent {
		available[f.ID] = f
	}
	refs := []Source{}
	raw := map[string][]byte{}
	texts := map[string][]byte{}
	for _, id := range d.Sources {
		data, err := readFile(filepath.Join(sourceDir, id+".json"))
		if os.IsNotExist(err) {
			f, ok := available[id]
			if !ok {
				return result, errors.New("wiki source does not belong to this project's observations")
			}
			data, err = json.Marshal(f)
		}
		if err != nil {
			return result, err
		}
		if !validSource(data, id) {
			return result, errors.New("invalid archived wiki source")
		}
		var observation Observation
		if json.Unmarshal(data, &observation) == nil && observation.Kind != "" {
			if _, err := b.readObservation(sourceDir, id); err != nil {
				return result, err
			}
		}
		raw[id] = data
		ref := Source{ID: id, Hash: digest(data)}
		text, err := readOriginals(filepath.Join(sourceDir, id+".text.json"))
		if os.IsNotExist(err) {
			var f store.Feedback
			_ = json.Unmarshal(data, &f)
			if originals := b.originals(f); len(originals) > 0 {
				text, _ = json.Marshal(originals)
			}
		} else if err != nil {
			return result, err
		}
		if len(text) > 0 {
			texts[id] = text
			ref.TextHash = digest(text)
		}
		refs = append(refs, ref)
	}
	if d.Correction != nil {
		var observed store.Feedback
		data, ok := raw[d.Correction.SourceID]
		if !ok || json.Unmarshal(data, &observed) != nil || !strings.Contains(observed.Reply, d.Correction.Quote) {
			return result, errors.New("訂正の引用は指定した出典の人の返答から、そのまま抜き出します")
		}
	}
	pages, issues, err := b.pages()
	if err != nil || len(issues) > 0 {
		return result, errors.New("wiki has unreadable or changed pages; inspect wiki lint before ingesting")
	}
	byID := map[string]Page{}
	for _, p := range pages {
		byID[p.ID] = p
		for _, ref := range p.Sources {
			if data, ok := raw[ref.ID]; ok && digest(data) != ref.Hash {
				return result, errors.New("a wiki source differs from its existing citation")
			}
		}
	}
	if d.Lesson != "" {
		id := d.ID
		if id == "" {
			id = digest([]byte(strings.TrimSpace(d.Title)))[:16]
		} else if _, ok := byID[id]; !ok {
			return result, errors.New("the lesson to update does not exist in this project")
		}
		for _, replaced := range d.Replaces {
			if _, ok := byID[replaced]; !ok || replaced == id {
				return result, errors.New("a replaced lesson must be another page in this project")
			}
		}
		p := Page{ID: id, Title: d.Title, Lesson: d.Lesson, AppliesTo: d.AppliesTo, Status: "hypothesis", Sources: refs, Replaces: d.Replaces, Correction: d.Correction, Body: body, Related: d.Related}
		p.ChangeReason = d.ChangeReason
		if old, ok := byID[id]; ok {
			if d.ChangeReason == "" {
				p.ChangeReason = old.ChangeReason
			}
			if bad := b.sourceIssues(old); len(bad) > 0 {
				return result, errors.New("the existing lesson has missing or changed sources")
			}
			for _, ref := range old.Sources {
				found := false
				for _, r := range p.Sources {
					if ref.ID == r.ID {
						if ref.Hash != r.Hash || (ref.TextHash != "" && ref.TextHash != r.TextHash) {
							return result, errors.New("an existing lesson's source hash changed")
						}
						found = true
					}
				}
				if !found {
					p.Sources = append(p.Sources, ref)
				}
			}
			if d.Replaces == nil {
				p.Replaces = append([]string{}, old.Replaces...)
			}
			if d.Body == nil {
				p.Body = old.Body
			}
			if d.Related == nil {
				p.Related = old.Related
			}
			p.Previous = old.Previous
			if p.Correction == nil {
				p.Correction = old.Correction
			}
		}
		sort.Slice(p.Sources, func(i, j int) bool { return p.Sources[i].ID < p.Sources[j].ID })
		p.Replaces = unique(p.Replaces)
		p.Related = unique(p.Related)
		for _, related := range p.Related {
			linked, ok := byID[related]
			if !ok || related == id || len(b.sourceIssues(linked)) > 0 {
				return result, errors.New("a related page must be another page with valid sources in this project")
			}
		}
		if old, ok := byID[id]; ok && revision(p) != old.Revision {
			// 内容を元に戻す更新も、前の版へのリンクを持つ新しい版にする。
			p.Previous = old.Revision
		}
		p.Revision = revision(p)
		if data, err := b.pageBytes(p); err != nil || len(data) > 64<<10 {
			return result, errors.New("wiki page exceeds the size limit")
		}
		byID[id] = p
		if cyclic(byID) {
			return result, errors.New("replaced wiki lessons must not form a cycle")
		}
		result.Page = &p
	}
	for id, data := range raw {
		if err := immutable(filepath.Join(sourceDir, id+".json"), data); err != nil {
			return result, err
		}
		if text := texts[id]; len(text) > 0 {
			if err := immutableLarge(filepath.Join(sourceDir, id+".text.json"), text); err != nil {
				return result, err
			}
		}
	}
	if p := result.Page; p != nil {
		data, err := b.pageBytes(*p)
		if err != nil {
			return result, err
		}
		history := filepath.Join(dir, "revisions")
		if err := os.Mkdir(history, 0o700); err != nil && !os.IsExist(err) {
			return result, err
		}
		if info, err := os.Lstat(history); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, errors.New("wiki revisions must be an ordinary directory")
		}
		if err := immutable(filepath.Join(history, p.ID+"-"+p.Revision+".md"), data); err != nil {
			return result, err
		}
		if err := writeFile(filepath.Join(dir, p.ID+".md"), data); err != nil {
			return result, err
		}
	}
	pages, _, err = b.pages()
	if err != nil {
		return result, err
	}
	if err := writeFile(filepath.Join(dir, "index.md"), index(pages)); err != nil {
		return result, err
	}
	key := "review " + strings.Join(unique(d.Sources), ",")
	if p := result.Page; p != nil {
		key = "ingest " + p.ID + "@" + p.Revision
	}
	if err := logEvent(dir, key); err != nil {
		return result, err
	}
	for id := range raw {
		if err := immutable(filepath.Join(sourceDir, id+".reviewed"), []byte("reviewed as conversation; not a truth label\n")); err != nil {
			return result, err
		}
		result.Reviewed = append(result.Reviewed, id)
	}
	sort.Strings(result.Reviewed)
	return result, nil
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	sort.Strings(out)
	return out
}

func cyclic(pages map[string]Page) bool {
	state := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, r := range pages[id].Replaces {
			if visit(r) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range pages {
		if visit(id) {
			return true
		}
	}
	return false
}

func index(pages []Page) []byte {
	var out strings.Builder
	out.WriteString("# 会話から育てる wiki\n\nAI が原資料からまとめた教訓の索引です。各教訓は仮説で、実行許可や正解ではありません。\n\n")
	replaced := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			replaced[id] = true
		}
	}
	for _, p := range pages {
		status := "仮説"
		if replaced[p.ID] {
			status = "後の教訓で更新"
		}
		title, _ := json.Marshal(p.Title)
		fmt.Fprintf(&out, "- [%s](%s.md) — %s / 出典 %d 件 / %s\n", p.ID, p.ID, string(title), len(p.Sources), status)
	}
	out.WriteString("\n変更履歴: [log.md](log.md)\n")
	return []byte(out.String())
}

func logEvent(dir, key string) error {
	path := filepath.Join(dir, "log.md")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_, _ = f.Seek(max(int64(0), info.Size()-(1<<20)), io.SeekStart)
	tail, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	if !bytes.Contains(tail, []byte(" | "+key+"\n")) {
		_, err = fmt.Fprintf(f, "- %s | %s\n", time.Now().UTC().Format(time.RFC3339), key)
	}
	return err
}

func (b Book) Lint() (Report, error) {
	report := Report{Issues: []Issue{}, Repaired: []string{}}
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return report, err
	}
	defer unlock()
	pages, issues, err := b.pages()
	if err != nil {
		return report, err
	}
	report.Issues = append(report.Issues, issues...)
	known := map[string]Page{}
	history, historyErr := os.Lstat(filepath.Join(dir, "revisions"))
	validHistory := historyErr == nil && history.IsDir() && history.Mode()&os.ModeSymlink == 0
	for _, p := range pages {
		known[p.ID] = p
		report.Issues = append(report.Issues, b.sourceIssues(p)...)
		if !validHistory {
			report.Issues = append(report.Issues, Issue{Kind: "invalid-revisions", Page: p.ID})
			continue
		}
		data, err := readFile(filepath.Join(dir, "revisions", p.ID+"-"+p.Revision+".md"))
		if err != nil {
			report.Issues = append(report.Issues, Issue{Kind: "missing-revision", Page: p.ID})
		} else if original, err := parsePage(data); err != nil || original.Revision != p.Revision || revision(original) != p.Revision {
			report.Issues = append(report.Issues, Issue{Kind: "changed-revision", Page: p.ID})
		} else if expected, _ := b.pageBytes(original); !bytes.Equal(data, expected) {
			report.Issues = append(report.Issues, Issue{Kind: "changed-revision", Page: p.ID})
		}
		if p.Previous != "" {
			previous, err := readFile(filepath.Join(dir, "revisions", p.ID+"-"+p.Previous+".md"))
			if err != nil {
				report.Issues = append(report.Issues, Issue{Kind: "missing-previous-revision", Page: p.ID})
			} else if before, err := parsePage(previous); err != nil || before.ID != p.ID || before.Revision != p.Previous || revision(before) != p.Previous {
				report.Issues = append(report.Issues, Issue{Kind: "changed-previous-revision", Page: p.ID})
			} else if expected, _ := b.pageBytes(before); !bytes.Equal(previous, expected) {
				report.Issues = append(report.Issues, Issue{Kind: "changed-previous-revision", Page: p.ID})
			}
		}
	}
	for _, p := range pages {
		for _, id := range p.Replaces {
			if _, ok := known[id]; !ok {
				report.Issues = append(report.Issues, Issue{Kind: "missing-replaced-page", Page: p.ID})
			}
		}
		for _, id := range p.Related {
			if _, ok := known[id]; !ok {
				report.Issues = append(report.Issues, Issue{Kind: "missing-related-page", Page: p.ID})
			}
		}
	}
	if cyclic(known) {
		report.Issues = append(report.Issues, Issue{Kind: "cyclic-replacements"})
	}
	expected := index(pages)
	actual, err := readRegistry(filepath.Join(dir, "index.md"))
	if err != nil || !bytes.Equal(actual, expected) {
		if err := writeFile(filepath.Join(dir, "index.md"), expected); err != nil {
			return report, err
		}
		report.Repaired = append(report.Repaired, "index")
		if err := logEvent(dir, "lint repaired-index "+digest(expected)[:16]); err != nil {
			return report, err
		}
	}
	return report, nil
}

// ReadSources は引用を確かめるための原文。識別子は同じ仕事の観察だけを指す。
func (b Book) ReadSources(ids []string) (map[string]any, error) {
	if len(ids) == 0 {
		return nil, errors.New("wiki source lookup needs source ids")
	}
	recent, err := (store.Store{Dir: b.Root}).RecentFeedback(b.Scope)
	if err != nil {
		return nil, err
	}
	known := map[string]store.Feedback{}
	for _, f := range recent {
		known[f.ID] = f
	}
	pages, issues, err := b.pages()
	if err != nil || len(issues) > 0 {
		return nil, errors.New("wiki pages need inspection before their sources can be read")
	}
	expected := map[string]string{}
	expectedText := map[string]string{}
	for _, p := range pages {
		for _, source := range p.Sources {
			expected[source.ID] = source.Hash
			if source.TextHash != "" {
				expectedText[source.ID] = source.TextHash
			}
		}
	}
	dir, err := b.directory(false, "sources")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := []store.Feedback{}
	observations := []Observation{}
	originals := map[string][]Original{}
	for _, id := range unique(ids) {
		if !idPattern.MatchString(id) {
			return nil, errors.New("invalid wiki source id")
		}
		var data []byte
		var readErr error
		if dir != "" {
			data, readErr = readFile(filepath.Join(dir, id+".json"))
		}
		if dir == "" || os.IsNotExist(readErr) {
			f, ok := known[id]
			if !ok {
				return nil, errors.New("wiki source does not belong to this project")
			}
			data, readErr = json.Marshal(f)
		}
		if readErr != nil {
			return nil, readErr
		}
		if hash := expected[id]; hash != "" && hash != digest(data) {
			return nil, errors.New("a cited wiki source has changed")
		}
		var f store.Feedback
		if !validSource(data, id) {
			return nil, errors.New("invalid archived wiki source")
		}
		_ = json.Unmarshal(data, &f)
		if f.Request != "" {
			out = append(out, f)
			var text []byte
			textErr := os.ErrNotExist
			if dir != "" {
				text, textErr = readOriginals(filepath.Join(dir, id+".text.json"))
			}
			if hash := expectedText[id]; hash != "" && (textErr != nil || digest(text) != hash) {
				return nil, errors.New("原資料の長文のハッシュが一致しません")
			}
			if textErr == nil {
				var saved []Original
				if json.Unmarshal(text, &saved) != nil {
					return nil, errors.New("原資料の長文を読み取れません")
				}
				originals[id] = saved
			} else if os.IsNotExist(textErr) || dir == "" {
				if value := b.originals(f); len(value) > 0 {
					originals[id] = value
				}
			} else {
				return nil, textErr
			}
		} else {
			o, err := b.readObservation(dir, id)
			if err != nil {
				return nil, err
			}
			observations = append(observations, o)
		}
	}
	return map[string]any{"source_material": out, "observations": observations, "originals": originals}, nil
}

// MaybeLint は一日一回の軽い点検。会話ごとの取り込みと、遅い周期の整備を分ける。
// 原資料や教訓の意味は書き換えず、生成した索引のずれだけを直す。
func (b Book) MaybeLint() error {
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".last-lint"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return nil
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 0 && time.Since(info.ModTime()) < 24*time.Hour {
		return nil
	}
	report, err := b.Lint()
	if err != nil {
		return err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, ".lint-report.json"), data); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err = f.WriteAt([]byte(time.Now().UTC().Format(time.RFC3339)), 0)
	return err
}
