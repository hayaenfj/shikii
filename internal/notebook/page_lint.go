package notebook

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

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
	b = b.scopedRead()
	if len(ids) == 0 {
		return nil, errors.New("wiki source lookup needs source ids")
	}
	recent, err := b.RecentFeedback()
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
			data, readErr = b.sharedSourceRecord(filepath.Join(dir, id+".json"))
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
		decoded := b.decodedSource(filepath.Join(dir, id+".json"), id, data)
		if !decoded.valid {
			return nil, errors.New("invalid archived wiki source")
		}
		f := decoded.feedback
		if f.Request != "" {
			out = append(out, f)
			var text []byte
			textErr := os.ErrNotExist
			if dir != "" {
				text, textErr = b.sharedOriginals(filepath.Join(dir, id+".text.json"))
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
			o, err := b.checkedObservation(dir, id, expected[id], data, decoded)
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

// classifyRead runs after Classify has read the page and before it saves; a test makes another agent edit the lesson there.
var classifyRead = func() {}

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
		classifyRead()
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
