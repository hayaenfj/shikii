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

// 画面から wiki を育てる口。下書き(機械が tune の run から起草)と主張(人が置いたもの)を一覧し、
// 人が「下書きを一般化して主張にする」「状態を変える(confirmed / rejected)」「下書きを消す」をする。
// 状態を変えるのは人だけ。機械は confirmable(支持するモデルが 2 つ以上、反証無し)と言うだけ。

// wikiPages は wiki/patterns と wiki/patterns/drafts のページ(前書きの無いものも、主張ではないと分かるように載せる)。
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
			models[baseModel(e.Model)] = true // 同じ土台の配置違いは 1 つのモデル(監査 4 回目の 11)
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

// baseModel は応答 model 名から配置の接尾辞("+state-last" 等)を外した土台の名前。
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
	if list, err := (handoff.Book{Store: s.o.storeDir}).List(); err == nil { // 作業記録(0.8.1): 画面の知識の欄で読む
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
	if r.URL.Query().Get("cursor") == "" { // 知識の画面(0.9.0): 記事ごとの最終更新と効いた回数、人に見てほしい例外だけの受け箱
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

// insidePatterns は path が wiki/patterns(と drafts)の中の .md か。外は触らない。
// 口の path は wiki ルートからの相対("patterns/x.md"、"patterns/drafts/x.md")。置き場(<store>/wiki)が変わっても画面と口は変わらない。
func insidePatterns(wikiRoot, p string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(p))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || filepath.Ext(clean) != ".md" {
		return "", false
	}
	dir := filepath.Dir(clean) // 親が patterns か patterns/drafts そのもの(.. や別の場所は等しくならない)
	if dir != "patterns" && dir != filepath.Join("patterns", "drafts") {
		return "", false
	}
	return filepath.Join(wikiRoot, clean), true
}

// wikiRel は wiki ルートからの相対の path(口と画面に出す形)。
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
		// 下書きを一般化した主張に書き直して patterns/ に移す。証拠と再検証の手順は引き継ぐ。状態は tentative(人が確かめるのはこれから)
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

// knowledgeMeta は、記事ごとの最終更新と、届いた教訓の評価から数えた役立った・害があった回数(lesson_meta)と、
// 人に見てほしい例外だけの受け箱(inbox)を返す。受け箱は、害があった教訓・繰り返された訂正・有効にする前の止める検査の 3 つだけ。
// 人が全部の記事を確かめる作りにはしない(機械が決められない物だけを出す)。
func knowledgeMeta(storeDir, scope string, observed []store.MemoryObservation) (map[string]map[string]any, []map[string]any) {
	meta := map[string]map[string]any{}
	if times, err := (notebook.Book{Root: storeDir, Scope: scope}).PageTimes(); err == nil {
		for id, at := range times {
			meta[id] = map[string]any{"updated": at.Format(time.RFC3339), "helpful": 0, "harmful": 0}
		}
	}
	// 受け箱は生きている記事だけ(置き換え済みの記事は出さない)、記事と種類ごとに 1 件へまとめて回数を付ける(監査 0.9.0 の E1)。
	// observed は新しい順なので、最初の 1 件の依頼と時刻が最新
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
