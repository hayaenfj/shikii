// Package notebook is a wiki grown from source records. The agent in use writes the text;
// shikii manages sources, the index and the change history. A lesson is never a truth label or permission to act.
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
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
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
	Kind         string            `json:"kind,omitempty" yaml:"kind,omitempty"`   // kind (one of Kinds; 0.9.0)
	Scene        string            `json:"scene,omitempty" yaml:"scene,omitempty"` // scene (a "when ..." phrase in the project's own words, up to sceneMax characters; 0.9.0)
}

// Kinds are the kinds of page. Fixed to shapes every kind of work has (do not add more): a rule decided in this project, a how-to, a criterion for judging, a premise or fact, and history.
var Kinds = []string{"rule", "howto", "criteria", "fact", "history"}

// SceneMax is the maximum number of characters in a scene (48 from 0.9.2: 24 was sized for Japanese, and natural English scenes
// such as "when checking invoice totals" were refused).
const SceneMax = 48

// errKindScene is the reply when kind or scene is missing or invalid (it says how to pick the values, so the agent can fix it as is).
var errKindScene = errors.New("a lesson needs kind (one of rule|howto|criteria|fact|history: rule = a rule decided in this project, howto = how to do something, criteria = criteria for judging, fact = premises and facts, history = how things came to be) and scene (the situation in the project's own words, as a \"when ...\" phrase such as \"when committing\", one line, up to 48 characters); when updating an existing page they are inherited from that page if omitted")

// validKindScene reports whether kind is one of Kinds and scene is one line, has no control characters and is at most SceneMax characters.
// kindSceneError is errKindScene, or a reply naming the length when only the scene is too long.
func kindSceneError(kind, scene string) error {
	if n := utf8.RuneCountInString(scene); slices.Contains(Kinds, kind) && n > SceneMax {
		return fmt.Errorf("the scene is too long (%d characters; keep it to %d, a short \"when ...\" phrase)", n, SceneMax)
	}
	return errKindScene
}

func validKindScene(kind, scene string) bool {
	if !slices.Contains(Kinds, kind) || scene == "" || utf8.RuneCountInString(scene) > SceneMax {
		return false
	}
	for _, r := range scene {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
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
	Kind         string            `json:"kind,omitempty"`  // kind (one of Kinds). Required for a draft with a lesson; inherited if omitted when updating
	Scene        string            `json:"scene,omitempty"` // scene (a "when ..." phrase). Same as above
	// baseRevision is the page revision classify started from. Inside the lock, refuse if it differs from the current revision (do not undo a concurrent edit with an old lesson; audit 0.9.0 B1)
	baseRevision string
}

type Issue struct {
	Kind   string `json:"kind"`
	Page   string `json:"page,omitempty"`
	Source string `json:"source,omitempty"`
	Store  string `json:"store,omitempty"` // the store for other-path-store (relative path from Root)
}

type View struct {
	// Pages without kind and scene (made before the upgrade; 0.9.0). Only the first reply carries their count, up to unclassifiedBatch of them and how to fill them in.
	// Placed at the start of the reply (at the end, agents without a hook skimmed past it)
	Remaining
	Lessons      []Page            `json:"lessons"`
	Pending      []store.Feedback  `json:"pending_sources"`
	Issues       []Issue           `json:"issues"`
	Matches      []Match           `json:"matches,omitempty"`
	Observations []ObservationHead `json:"observations,omitempty"`
	Maintenance  []MaintenanceTask `json:"maintenance,omitempty"`
	Search       *SearchResult     `json:"search,omitempty"`
	NextCursor   string            `json:"next_cursor,omitempty"`
}

type Result struct {
	Remaining
	Page     *Page    `json:"page"`
	Reviewed []string `json:"reviewed"`
}

// Remaining is how to fill in the remaining pages without kind and scene, their count and the next unclassifiedBatch of them (0.9.0). It goes into
// query and save replies, and shows nothing when there are none.
type Remaining struct {
	UnclassifiedHow   string             `json:"unclassified_how,omitempty"`
	UnclassifiedCount int                `json:"unclassified_count,omitempty"`
	Unclassified      []UnclassifiedPage `json:"unclassified,omitempty"`
}

// RemainingUnclassified is what remains now. When it cannot be read, nothing is added (it does not block the body of the reply).
func (b Book) RemainingUnclassified() Remaining {
	un, err := b.Unclassified()
	if err != nil || len(un) == 0 {
		return Remaining{}
	}
	r := Remaining{UnclassifiedHow: unclassifiedHow, UnclassifiedCount: len(un)}
	for i, p := range un {
		if i == unclassifiedBatch {
			break
		}
		r.Unclassified = append(r.Unclassified, UnclassifiedPage{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo})
	}
	return r
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

// directory uses only fixed subdirectories. It does not follow symlinks in the generated area.
func (b Book) directory(create bool, area string) (string, error) {
	if b.Scope == "" || b.Root == "" {
		return "", errors.New("wiki needs a project and a store")
	}
	parts := []string{"wiki", "lessons", storeDirName}
	if area == "sources" {
		parts = []string{"records", "wiki-sources", storeDirName}
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
	if err := MigrateLayout(path, b.Scope); err != nil {
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
	// Publish a complete temporary file by linking it. An existing source record is not replaced.
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
		old, rerr := readSourceRecord(path) // a source record can be larger than a page
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

// pageLabels are the fixed words around a lesson's fields in its Markdown file. New pages are written with englishLabels (0.9.2);
// pages written before 0.9.2 used legacyLabels, and they are still accepted as unchanged (matchesPage), so updating does not
// turn every existing page into a changed page.
type pageLabels struct{ correction, action, reason, related, applies, sources, note string }

var englishLabels = pageLabels{"The person's correction: ", "Next action (hypothesis): ", "Reason for the change: ", "## Related", "Applies when: ", "## Sources",
	"This lesson is a hypothesis with sources. The current request and the rules come first."}

// legacyLabels are the Japanese words pages were written with before 0.9.2 (kept only to recognize those pages)
var legacyLabels = pageLabels{"人の訂正の引用: ", "次に変える行動（仮説）: ", "変更理由: ", "## 関連する知識", "適用する場面: ", "## 出典",
	"この教訓は出典付きの仮説です。今回の依頼と規則を優先します。"}

func (b Book) pageBytes(p Page) ([]byte, error) { return b.renderPage(p, englishLabels) }

func (b Book) renderPage(p Page, l pageLabels) ([]byte, error) {
	front, err := yaml.Marshal(p)
	if err != nil {
		return nil, err
	}
	var links strings.Builder
	for _, source := range p.Sources {
		fmt.Fprintf(&links, "- [%s](../../../records/wiki-sources/%s/%s.json)\n", source.ID, storeDirName, source.ID)
	}
	correction := ""
	if p.Correction != nil {
		correction = "\n\n" + l.correction + p.Correction.Quote + "\n\n" + l.action + p.Correction.Action
	}
	article := ""
	if p.ChangeReason != "" {
		article = "\n\n" + l.reason + p.ChangeReason
	}
	if p.Body != "" {
		article += "\n\n" + p.Body
	}
	if len(p.Related) > 0 {
		article += "\n\n" + l.related + "\n\n"
		for _, id := range p.Related {
			article += fmt.Sprintf("- [%s](%s.md)\n", id, id)
		}
	}
	return []byte("---\n" + string(front) + "---\n\n# " + p.Title + "\n\n" + p.Lesson + "\n\n" + l.applies + p.AppliesTo + article + correction + "\n\n" + l.sources + "\n\n" + links.String() + "\n" + l.note + "\n"), nil
}

// matchesPage says whether data is exactly what shikii writes for p (with the current labels, or with the labels of before 0.9.2).
func (b Book) matchesPage(data []byte, p Page) bool {
	data = legacySourceLink.ReplaceAll(data, []byte("](../../../records/wiki-sources/"+storeDirName+"/"))
	for _, l := range []pageLabels{englishLabels, legacyLabels} {
		if expected, err := b.renderPage(p, l); err == nil && bytes.Equal(data, expected) {
			return true
		}
	}
	return false
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
	// Cover every generated page; splitting the reply is done by QueryPage.
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
		if !b.matchesPage(data, p) {
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
		var hash string
		err := dirErr
		if err == nil {
			hash, err = sourceRecordDigest(filepath.Join(dir, s.ID+".json"))
		}
		if err != nil {
			issues = append(issues, Issue{Kind: "missing-source", Page: p.ID, Source: s.ID})
		} else if hash != s.Hash {
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

// Recall is for lessons delivered automatically. It weighs the title and applies-to condition over chance matches in the body.
// Query, for manual search, still searches the body as before.
// Live returns the pages usable in search (sources intact and not replaced), without ranking by search terms. Candidates for choosing where to insert.
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

// RecallCandidates is the cap on automatic-delivery candidates. 2 are delivered after the caller removes revisions it already delivered (audit 2026-10-23: cutting before removing lets them take the seats).
const RecallCandidates = 6

// AlwaysScenes are the ways to write the scene of a "rule that applies to every request" (the Japanese and English the guide fixed).
var AlwaysScenes = []string{"いつも", "always"}

// IsAlways reports whether scene is one of AlwaysScenes (case-insensitive for English).
func IsAlways(scene string) bool {
	for _, a := range AlwaysScenes {
		if strings.EqualFold(strings.TrimSpace(scene), a) {
			return true
		}
	}
	return false
}

// AlwaysRules returns the rules decided in this project (rule) whose scene is "always" (in the order the pages were read). Unreadable pages, pages missing sources and replaced pages are not returned.
// This is what goes to subagents whose task text is unknown (0.9.1): they cannot be chosen by closeness to the request, so only rules that apply to every request
func (b Book) AlwaysRules() ([]Page, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	superseded := map[string]bool{}
	valid := []Page{}
	for _, p := range pages {
		if len(b.sourceIssues(p)) > 0 {
			continue
		}
		for _, id := range p.Replaces {
			superseded[id] = true
		}
		valid = append(valid, p)
	}
	out := []Page{}
	for _, p := range valid {
		if !superseded[p.ID] && p.Kind == "rule" && IsAlways(p.Scene) {
			out = append(out, p)
		}
	}
	return out, nil
}

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
		// A direct reference can also read pages from before replacement. Source checks are not skipped.
		if !recall && query == "lesson:"+p.ID {
			return View{Lessons: []Page{p}, Pending: v.Pending, Issues: v.Issues}, nil
		}
	}
	for _, p := range valid {
		// For automatic delivery (recall), drop history pages before ranking (dropping them after cutting the candidates lets history take the seats and rules do not arrive; audit 0.9.1 medium-3)
		if !superseded[p.ID] && !exact {
			v.Lessons = append(v.Lessons, p)
		}
	}
	if query != "" || recall {
		v.Lessons, v.Matches = rankPages(v.Lessons, query, recall)
	}
	if recall {
		// History stays in the count for rare words, and is ranked but dropped before cutting
		lessons, matches := v.Lessons[:0:0], v.Matches[:0:0]
		for i, p := range v.Lessons {
			if p.Kind != "history" {
				lessons = append(lessons, p)
				if i < len(v.Matches) {
					matches = append(matches, v.Matches[i])
				}
			}
		}
		v.Lessons, v.Matches = lessons, matches
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
		limit = RecallCandidates // the caller (memoryContext) cuts to 2 after removing revisions it already delivered
	}
	if query == "" && !recall {
		pending, err := b.PendingObservations()
		if err != nil {
			return v, err
		}
		v.Observations = Heads(pending)
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

// PendingSources returns source records from oldest. Store limits how much history is read.
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
		return result, errors.New("a correction needs the source and quote of what the person said, and a lesson stating the next behavior to change")
	}
	d.Title, d.Lesson, d.AppliesTo = strings.TrimSpace(d.Title), strings.TrimSpace(d.Lesson), strings.TrimSpace(d.AppliesTo)
	d.Kind, d.Scene = strings.TrimSpace(d.Kind), strings.TrimSpace(d.Scene)
	body := ""
	if d.Body != nil {
		body = strings.TrimSpace(*d.Body)
	}
	if len(d.Sources) < 1 {
		return result, errors.New("wiki ingest needs sources: an array of source ids (from wiki capture or reflect query)")
	}
	if d.Lesson != "" && (strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.AppliesTo) == "") {
		return result, errors.New("a lesson needs a title and the conditions where it applies")
	}
	for _, s := range []string{d.Title, d.Lesson, d.AppliesTo, body, d.ChangeReason} {
		if !utf8.ValidString(s) {
			return result, errors.New("wiki text must be UTF-8")
		}
	}
	for _, f := range []struct {
		name  string
		size  int
		limit int
	}{{"title", len(d.Title), 200}, {"lesson", len(d.Lesson), 2000}, {"applies_to", len(d.AppliesTo), 1000}, {"body", len(body), 16 << 10}, {"change_reason", len(d.ChangeReason), 1000}} {
		if f.size > f.limit {
			return result, fmt.Errorf("wiki %s is %d bytes, over the limit of %d bytes: shorten it, or move older parts to a new page and link it with related", f.name, f.size, f.limit)
		}
	}
	if d.Lesson == "" && d.ID != "" {
		return result, errors.New("updating a page by id sends its lesson, title and applies_to again (read them with wiki query lesson:<id>), with sources and change_reason")
	}
	if d.Lesson == "" && (d.Title != "" || d.AppliesTo != "" || d.ID != "" || len(d.Replaces) > 0 || d.Body != nil || d.Related != nil || d.ChangeReason != "" || d.Kind != "" || d.Scene != "") {
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
	pages, issues, err := b.pages()
	if err != nil || len(issues) > 0 {
		return result, errors.New("wiki has unreadable or changed pages; inspect wiki lint before ingesting")
	}
	cited := map[string]string{} // a source a page already cites is checked by that citation's hash (it may have been captured under the previous path)
	for _, p := range pages {
		for _, ref := range p.Sources {
			cited[ref.ID] = ref.Hash
		}
	}
	refs := []Source{}
	raw := map[string][]byte{}
	texts := map[string][]byte{}
	for _, id := range d.Sources {
		data, err := readSourceRecord(filepath.Join(sourceDir, id+".json"))
		if os.IsNotExist(err) {
			f, ok := available[id]
			if !ok {
				return result, fmt.Errorf("wiki source %s does not belong to this project's observations or recent conversations (send the other ids again without it)", id)
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
			if _, err := b.readObservation(sourceDir, id, cited[id]); err != nil {
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
		// A correction quotes the person's own next message, which only the hook records with a conversation. A captured source
		// (even kind "human") is written by an agent, so it cannot carry a correction: the refusal says so and what to do instead.
		id := d.Correction.SourceID
		var observed store.Feedback
		var captured Observation
		data, ok := raw[id]
		switch {
		case !ok:
			return result, fmt.Errorf("correction.source_id %s must also be one of sources (cite the conversation the quote comes from)", id)
		case json.Unmarshal(data, &captured) == nil && captured.Kind != "":
			return result, fmt.Errorf("correction.source_id %s is a captured source, not a conversation recorded by the hook; a correction must quote the person's message from a recorded conversation. Without hooks there is none: save the lesson without correction and cite the source in sources", id)
		case json.Unmarshal(data, &observed) != nil || observed.Reply == "":
			return result, fmt.Errorf("conversation %s has no recorded reply from the person yet; a correction must quote the person's message. Save the lesson without correction, or add the correction after the person replies", id)
		case !strings.Contains(observed.Reply, d.Correction.Quote):
			return result, fmt.Errorf("the correction quote must be copied verbatim from the person's reply (the reply field) in conversation %s", id)
		}
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
		p := Page{ID: id, Title: d.Title, Lesson: d.Lesson, AppliesTo: d.AppliesTo, Status: "hypothesis", Sources: refs, Replaces: d.Replaces, Correction: d.Correction, Body: body, Related: d.Related, Kind: d.Kind, Scene: d.Scene}
		if d.baseRevision != "" && byID[id].Revision != d.baseRevision {
			return result, errors.New("the page changed while classifying; read it again and classify it again")
		}
		if old, ok := byID[id]; ok { // inherited if omitted when updating (pages from the previous version have none, so they cannot be saved without them)
			if p.Kind == "" {
				p.Kind = old.Kind
			}
			if p.Scene == "" {
				p.Scene = old.Scene
			}
		}
		if !validKindScene(p.Kind, p.Scene) {
			return result, kindSceneError(p.Kind, p.Scene)
		}
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
				if !found && !b.recaptured(sourceDir, ref, raw) {
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
			// Even an update that restores old content becomes a new revision with a link to the previous one.
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
		// The same content is the same revision. A revision file written before 0.9.2 holds that page with the old fixed words; it is the same
		// page (matchesPage), so it is kept as it is instead of being reported as changed (audit 0.9.2, low 3). Any other difference is refused.
		revisionFile := filepath.Join(history, p.ID+"-"+p.Revision+".md")
		if old, err := readFile(revisionFile); err != nil || !b.matchesPage(old, *p) {
			if err := immutable(revisionFile, data); err != nil {
				return result, err
			}
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
	out.WriteString("# Wiki grown from conversations\n\nAn index of lessons the AI summarized from source records. Each lesson is a hypothesis, not permission to act or a correct answer.\n\n")
	replaced := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			replaced[id] = true
		}
	}
	for _, p := range pages {
		status := "hypothesis"
		if replaced[p.ID] {
			status = "updated by a later lesson"
		}
		title, _ := json.Marshal(p.Title)
		fmt.Fprintf(&out, "- [%s](%s.md) — %s / %d sources / %s\n", p.ID, p.ID, string(title), len(p.Sources), status)
	}
	out.WriteString("\nChange history: [log.md](log.md)\n")
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
	// Stores left under a different path's name (after moving or renaming the folder; 0.9.2). Report them even when the new path has no wiki yet
	others, err := b.OtherPathStores()
	if err != nil {
		return report, err
	}
	for _, s := range others {
		report.Issues = append(report.Issues, Issue{Kind: "other-path-store", Store: s})
	}
	if waiting, err := b.UncheckedWaiting(); err == nil { // waiting sources captured under the previous path (cited ones are checked by the citation)
		for _, id := range waiting {
			report.Issues = append(report.Issues, Issue{Kind: "source-from-other-path", Source: id})
		}
	}
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
		} else if !b.matchesPage(data, original) {
			report.Issues = append(report.Issues, Issue{Kind: "changed-revision", Page: p.ID})
		}
		if p.Previous != "" {
			previous, err := readFile(filepath.Join(dir, "revisions", p.ID+"-"+p.Previous+".md"))
			if err != nil {
				report.Issues = append(report.Issues, Issue{Kind: "missing-previous-revision", Page: p.ID})
			} else if before, err := parsePage(previous); err != nil || before.ID != p.ID || before.Revision != p.Previous || revision(before) != p.Previous {
				report.Issues = append(report.Issues, Issue{Kind: "changed-previous-revision", Page: p.ID})
			} else if !b.matchesPage(previous, before) {
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

// ReadSources returns the original text for checking quotes. Identifiers point only to observations of the same work.
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
			data, readErr = readSourceRecord(filepath.Join(dir, id+".json"))
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
				return nil, errors.New("the hash of the long source text does not match")
			}
			if textErr == nil {
				var saved []Original
				if json.Unmarshal(text, &saved) != nil {
					return nil, errors.New("cannot read the long source text")
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
			o, err := b.readObservation(dir, id, expected[id])
			if err != nil {
				return nil, err
			}
			observations = append(observations, o)
		}
	}
	return map[string]any{"source_material": out, "observations": observations, "originals": originals}, nil
}

// MaybeLint is a light check once a day. It separates per-conversation ingestion from slower-cycle maintenance.
// It does not rewrite the meaning of source records or lessons; it only fixes drift in the generated index.
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

// Unclassified lists live pages missing kind or scene (pages made before the upgrade; 0.9.0), in id order.
func (b Book) Unclassified() ([]Page, error) {
	live, err := b.Live()
	if err != nil {
		return nil, err
	}
	out := []Page{}
	for _, p := range live {
		if p.Kind == "" || p.Scene == "" {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Classify saves a new revision of the page with only kind and scene added (lesson, title, applies-to, body, related and sources unchanged).
// It is a light operation to fill in pages made before the upgrade; use Ingest to rewrite a lesson.
func (b Book) Classify(id, kind, scene string) (Result, error) {
	kind, scene = strings.TrimSpace(kind), strings.TrimSpace(scene)
	if !validKindScene(kind, scene) {
		return Result{}, kindSceneError(kind, scene)
	}
	live, err := b.Live()
	if err != nil {
		return Result{}, err
	}
	for _, p := range live {
		if p.ID != id {
			continue
		}
		sources := make([]string, 0, len(p.Sources))
		for _, s := range p.Sources {
			sources = append(sources, s.ID)
		}
		// Leave the change reason empty to inherit the previous reason (audit 0.9.0 D1). Carry the base revision and compare it inside the lock (B1)
		return b.Ingest(Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Sources: sources, Kind: kind, Scene: scene, baseRevision: p.Revision})
	}
	return Result{}, errors.New("the page to classify is not a live lesson in this project")
}

// RewriteWithoutKindScene rewrites a page into the form without kind and scene (a page from before the upgrade). Used only to reproduce
// the 0.9.0 migration (the hook has them filled in) in tests and hands-on checks. Not called from normal operations.
func (b Book) RewriteWithoutKindScene(id string) error {
	live, err := b.Live()
	if err != nil {
		return err
	}
	for _, p := range live {
		if p.ID != id {
			continue
		}
		p.Kind, p.Scene, p.Previous = "", "", ""
		p.Revision = revision(p)
		data, err := b.pageBytes(p)
		if err != nil {
			return err
		}
		dir, err := b.directory(false, "pages")
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dir, p.ID+".md"), data)
	}
	return errors.New("no such live lesson")
}

// UnclassifiedPage is the short form of a page that needs kind and scene.
type UnclassifiedPage struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Lesson    string `json:"lesson"`
	AppliesTo string `json:"applies_to"`
}

const unclassifiedBatch = 5

const unclassifiedHow = "Give these pages a kind and a scene: shikii wiki classify <page ID> <rule|howto|criteria|fact|history> <scene> (over MCP, shikii_wiki operation classify with id, kind and scene). Kinds: rule = a rule decided in this project / howto = how to do something / criteria = criteria for judging / fact = premises and facts / history = how things came to be; the scene is a \"when ...\" phrase in the project's own words."

// PageIssues are problems with the page itself that make a save (Ingest) refuse (unreadable or hand-edited pages). Source problems are not included.
func (b Book) PageIssues() ([]Issue, error) {
	_, issues, err := b.pages()
	return issues, err
}

// PageTimes is the last update of each live page (the order on the 0.9.0 knowledge screen and "recently learned"). Decided by the time of
// the last save (ingest, including adding kind and scene) in the wiki log (log.md). Copying the wiki to another machine with cp sets every file time to that moment,
// so file times are used only for pages missing from the log (0.9.2, audit 0.9.1 test-speed low-7).
func (b Book) PageTimes() (map[string]time.Time, error) {
	out := map[string]time.Time{}
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".md")
		if !e.Type().IsRegular() || id == e.Name() || !idPattern.MatchString(id) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out[id] = info.ModTime().UTC()
		}
	}
	saved := map[string]time.Time{}
	if f, err := os.OpenFile(filepath.Join(dir, "log.md"), os.O_RDONLY|syscall.O_NOFOLLOW, 0); err == nil {
		data, _ := io.ReadAll(f)
		f.Close()
		for _, line := range strings.Split(string(data), "\n") {
			at, key, ok := strings.Cut(strings.TrimPrefix(line, "- "), " | ")
			page, ok2 := strings.CutPrefix(key, "ingest ")
			when, err := time.Parse(time.RFC3339, at)
			if !ok || !ok2 || err != nil {
				continue
			}
			if id, _, _ := strings.Cut(page, "@"); when.After(saved[id]) {
				saved[id] = when.UTC()
			}
		}
	}
	for id := range out {
		if at, ok := saved[id]; ok {
			out[id] = at
		}
	}
	return out, nil
}
