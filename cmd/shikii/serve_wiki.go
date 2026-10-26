package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The endpoint that grows the wiki from the page. It lists drafts (drafted by the machine from tune runs) and claims (placed by people),
// and a person "generalizes a draft into a claim", "changes its state (confirmed / rejected)" or "deletes a draft".
// Only people change states. The machine only says confirmable (2 or more supporting models, no counterevidence).

// wikiPages are the pages in wiki/patterns and wiki/patterns/drafts (pages without front matter are listed too, so it is clear they are not claims).
func wikiPages(wikiRoot string) ([]string, error) {
	a, _ := filepath.Glob(filepath.Join(wikiRoot, "patterns", "*.md"))
	b, _ := filepath.Glob(filepath.Join(wikiRoot, "patterns", "drafts", "*.md"))
	all := append(a, b...)
	sort.Strings(all)
	return all, nil
}

func claimSummary(path string) (map[string]any, error) {
	pg, _, err := readClaimPage(path)
	if err != nil {
		return nil, err
	}
	models := map[string]bool{}
	contra := 0
	for _, e := range pg.Evidence {
		switch e.Verdict {
		case "supports":
			models[baseModel(e.Model)] = true // layout variants on the same base are one model (audit round 4, item 11)
		case "contradicts":
			contra++
		}
	}
	ev := []claimEvidence{}
	if pg.Evidence != nil {
		ev = pg.Evidence
	}
	out := map[string]any{
		"path": filepath.ToSlash(path), "claim": pg.Claim, "status": pg.Status, "applies_to": pg.AppliesTo, "evidence": ev,
		"supporting_models": len(models), "contradicting": contra, "is_claim": pg.Retest != nil || pg.Claim != "",
		"confirmable": len(models) >= 2 && contra == 0 && pg.Status != "confirmed" && pg.Status != "rejected",
	}
	if pg.Retest != nil {
		out["retest"] = pg.Retest
	}
	return out, nil
}

// baseModel is the base name of a response model name with the layout suffix ("+state-last" etc.) removed.
func baseModel(m string) string {
	if i := strings.Index(m, "+"); i > 0 {
		return m[:i]
	}
	return m
}

func (s *server) handleWikiList(w http.ResponseWriter, r *http.Request) {
	paths, err := wikiPages(s.o.wikiDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	claims := []map[string]any{}
	for _, p := range paths {
		c, err := claimSummary(p)
		if err != nil {
			c = map[string]any{"error": err.Error(), "is_claim": false}
		}
		c["path"] = wikiRel(s.o.wikiDir, p)
		claims = append(claims, c)
	}
	result := map[string]any{"claims": claims, "lessons": []notebook.Page{}, "notebook_issues": []notebook.Issue{}, "pending_sources_count": 0, "notebook_project": filepath.Base(s.o.projectScope)}
	if list, err := (handoff.Book{Store: s.o.storeDir}).List(); err == nil { // work records (0.8.1): read in the page's knowledge section
		result["handoff"] = list
	} else {
		result["handoff_error"] = true
	}
	if stats, err := (store.Store{Dir: s.o.storeDir}).KnowledgeReport(); err == nil {
		result["knowledge_usage"] = stats
	}
	report, rerr := (store.Store{Dir: s.o.storeDir}).MemoryReport(s.o.projectScope)
	if rerr == nil {
		result["memory_effect"] = report
	} else {
		result["memory_effect_error"] = true
	}
	if r.URL.Query().Get("cursor") == "" { // the knowledge page (0.9.0): each page's last update and how often it helped, and an inbox of only the exceptions a person should look at
		result["lesson_meta"], result["inbox"] = knowledgeMeta(s.o.storeDir, s.o.projectScope, report.All)
	}
	{
		if view, err := (notebook.Book{Root: s.o.storeDir, Scope: s.o.projectScope}).QueryPage("", r.URL.Query().Get("cursor")); err == nil {
			result["lessons"], result["notebook_issues"], result["pending_sources_count"] = view.Lessons, view.Issues, len(view.Pending)
			result["maintenance"] = view.Maintenance
			result["unclassified_count"] = view.UnclassifiedCount
			if view.NextCursor != "" {
				result["next_cursor"] = view.NextCursor
			}
		} else {
			if errors.Is(err, notebook.ErrStaleCursor) { // the page reads the list again from the start
				writeErr(w, http.StatusConflict, err)
				return
			}
			if r.URL.Query().Get("cursor") != "" {
				writeErr(w, 400, err)
				return
			}
			result["notebook_issues"] = []notebook.Issue{{Kind: "unreadable-notebook"}}
		}
	}
	writeJSON(w, 200, result)
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,80}$`)

// insidePatterns reports whether path is a .md inside wiki/patterns (or drafts). Nothing outside is touched.
// The endpoint's path is relative to the wiki root ("patterns/x.md", "patterns/drafts/x.md"). Even if the store (<store>/wiki) moves, the page and endpoint stay the same.
func insidePatterns(wikiRoot, p string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(p))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || filepath.Ext(clean) != ".md" {
		return "", false
	}
	dir := filepath.Dir(clean) // the parent is exactly patterns or patterns/drafts (.. or other places are not equal)
	if dir != "patterns" && dir != filepath.Join("patterns", "drafts") {
		return "", false
	}
	return filepath.Join(wikiRoot, clean), true
}

// wikiRel is the path relative to the wiki root (the form shown in the endpoint and page).
func wikiRel(wikiRoot, p string) string {
	if rel, err := filepath.Rel(wikiRoot, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

func (s *server) handleWikiClaim(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Path      string            `json:"path"`
		Action    string            `json:"action"` // promote | status | delete
		Slug      string            `json:"slug"`
		Claim     string            `json:"claim"`
		AppliesTo map[string]string `json:"applies_to"`
		Status    string            `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	path, ok := insidePatterns(s.o.wikiDir, req.Path)
	if !ok {
		writeErr(w, 400, errors.New("path must be a .md under wiki/patterns or wiki/patterns/drafts"))
		return
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		writeErr(w, 400, errors.New("symbolic links under wiki/patterns are not followed"))
		return
	}
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	pg, body, err := readClaimPage(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, 404, err)
		} else {
			writeErr(w, 500, err)
		}
		return
	}
	isDraft := filepath.Dir(path) == filepath.Join(s.o.wikiDir, "patterns", "drafts")
	switch req.Action {
	case "promote":
		// Rewrite the draft as a generalized claim and move it to patterns/. Evidence and re-check steps carry over. The state is tentative (a person still has to check it)
		if !isDraft {
			writeErr(w, 409, errors.New("only a draft can be promoted"))
			return
		}
		if !slugRe.MatchString(req.Slug) || strings.TrimSpace(req.Claim) == "" {
			writeErr(w, 400, errors.New("promote needs a slug (lowercase, digits, dashes) and the generalized claim text"))
			return
		}
		dst := filepath.Join(s.o.wikiDir, "patterns", req.Slug+".md")
		if _, err := os.Stat(dst); err == nil {
			writeErr(w, 409, fmt.Errorf("%s already exists", dst))
			return
		}
		pg.Claim = strings.TrimSpace(req.Claim)
		pg.Status = "tentative"
		if len(req.AppliesTo) > 0 {
			pg.AppliesTo = req.AppliesTo
		}
		if err := writeClaimPage(dst, pg, body); err != nil {
			writeErr(w, 500, err)
			return
		}
		if err := os.Remove(path); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"path": wikiRel(s.o.wikiDir, dst), "status": pg.Status})
	case "status":
		if pg.Claim == "" && pg.Retest == nil {
			writeErr(w, 409, errors.New("this page has no front matter; it is prose, not a claim"))
			return
		}
		if isDraft {
			writeErr(w, 409, errors.New("a draft has no status to change; promote it first (generalize the claim)"))
			return
		}
		if req.Status != "confirmed" && req.Status != "rejected" && req.Status != "tentative" {
			writeErr(w, 400, errors.New("status must be confirmed, rejected or tentative"))
			return
		}
		pg.Status = req.Status
		if err := writeClaimPage(path, pg, body); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"path": wikiRel(s.o.wikiDir, path), "status": pg.Status})
	case "delete":
		if !isDraft {
			writeErr(w, 409, errors.New("only drafts are deleted; reject a claim instead so the record stays"))
			return
		}
		if err := os.Remove(path); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"path": wikiRel(s.o.wikiDir, path), "deleted": true})
	default:
		writeErr(w, 400, errors.New("action must be promote, status or delete"))
	}
}

// knowledgeMeta returns each page's last update, the counts of helpful and harmful from the evaluations of delivered lessons (lesson_meta),
// and an inbox of only the exceptions a person should look at. The inbox holds only three kinds: harmful lessons, repeated corrections, and guards not yet enabled.
// It is not built so that a person checks every page (it shows only what the machine cannot decide).
func knowledgeMeta(storeDir, scope string, observed []store.MemoryObservation) (map[string]map[string]any, []map[string]any) {
	meta := map[string]map[string]any{}
	if times, err := (notebook.Book{Root: storeDir, Scope: scope}).PageTimes(); err == nil {
		for id, at := range times {
			meta[id] = map[string]any{"updated": at.Format(time.RFC3339), "helpful": 0, "harmful": 0}
		}
	}
	// The inbox has only live pages (replaced pages are not shown), grouped into one item per page and kind with a count (audit 0.9.0 E1).
	// observed is newest first, so the first item's request and time are the latest
	alive := map[string]bool{}
	if live, err := (notebook.Book{Root: storeDir, Scope: scope}).Live(); err == nil {
		for _, p := range live {
			alive[p.ID] = true
		}
	}
	inbox := []map[string]any{}
	seen := map[string]map[string]any{}
	for _, o := range observed {
		if o.Assessment == nil {
			continue
		}
		if m := meta[o.Quote.ID]; m != nil {
			switch o.Assessment.Result {
			case "helpful":
				m["helpful"] = m["helpful"].(int) + 1
			case "harmful":
				m["harmful"] = m["harmful"].(int) + 1
			}
		}
		kind := ""
		switch {
		case o.Assessment.Result == "harmful":
			kind = "harmful"
		case o.Assessment.CorrectionResult == "repeated":
			kind = "repeated"
		}
		if kind == "" || !alive[o.Quote.ID] {
			continue
		}
		if it := seen[kind+" "+o.Quote.ID]; it != nil {
			it["count"] = it["count"].(int) + 1
			continue
		}
		item := map[string]any{"kind": kind, "lesson_id": o.Quote.ID, "lesson": o.Quote.Lesson, "request": cut(o.Request, 300), "at": o.At.Format(time.RFC3339), "count": 1}
		seen[kind+" "+o.Quote.ID] = item
		inbox = append(inbox, item)
	}
	if guards, err := loadGuards(storeDir); err == nil {
		for _, g := range guards {
			if !g.Enabled && !isBuiltinGuard(g.ID) {
				inbox = append(inbox, map[string]any{"kind": "guard_draft", "guard_id": g.ID, "title": g.Title})
			}
		}
	}
	return meta, inbox
}
