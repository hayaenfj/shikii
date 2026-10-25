package main

// Wiki insertion points and contradictions. When the agent writes a new lesson, it passes it to reflect's place before saving (submit).
// shikii compares it with each live page one by one, asks the local model "is this the same topic?" (wiki-place), and for the top placeConflictTop also asks "do they disagree?" (wiki-conflict).
// The truth comes automatically from the result of submit (which page the agent added to or replaced). People do not answer.
// Until a line is drawn it is only a reference, and the agent reads the bodies and decides (a disagreement is never replaced silently, even once a line is drawn).

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// machineOnlySpecs are decisions people do not answer (the AI answers; the truth comes from the result of reflection). The page does not count them as waiting for review; it only shows the cards (0.8.0).
var machineOnlySpecs = map[string]bool{"wiki-place": true, "wiki-conflict": true}

// settledDrafts are drafts (lesson texts) whose save in reflection is done: drafts whose insertion-point decision (wiki-place) got a truth from the result.
// A contradiction decision (wiki-conflict) gets no truth for pages the draft did not touch (whether they disagree is unknown), so
// once the draft is saved, it leaves the list as "undecided (final)" rather than "waiting for results" (0.8.0: 61 items looked like they were waiting forever).
func settledDrafts(st store.Store) map[string]bool {
	out := map[string]bool{}
	recs, err := st.Records("wiki-place", 0)
	if err != nil {
		return out
	}
	labeled, err := st.LabeledTraces("wiki-place")
	if err != nil {
		return out
	}
	for _, rec := range recs {
		if _, ok := labeled[rec.TraceID]; ok {
			out[strings.TrimSpace(rec.Inputs["new_lesson"])] = true
		}
	}
	return out
}

// undecidedConflict reports whether a contradiction decision about a saved draft became final without a truth.
func undecidedConflict(specID string, rec store.Record, labeled map[string]store.Label, settled map[string]bool) bool {
	if specID != "wiki-conflict" {
		return false
	}
	if _, ok := labeled[rec.TraceID]; ok {
		return false
	}
	return settled[strings.TrimSpace(rec.Inputs["new_lesson"])]
}

// Fixed rules (there are no options).
const (
	placeCandidatesMax = 24  // max pages compared (when there are more, the ones matching the search words best)
	placeConflictTop   = 3   // number of top candidates asked about contradiction
	placeParallel      = 4   // number of questions sent to the local model at once
	placeChosenWeight  = 1.0 // pages added to or replaced (a clear result)
	placeOtherWeight   = 0.5 // pages not chosen (could be an oversight, so a partial weight)
)

// wikiSpecs are the 2 questions asked when sorting the wiki (ids of the embedded specs). Lines are drawn automatically, the same as for the hook.
var wikiSpecs = []string{"wiki-place", "wiki-conflict"}

type placeJudgement struct {
	P          float64 `json:"p"`
	Band       string  `json:"band"`
	Calibrated bool    `json:"calibrated"`
	TraceID    string  `json:"trace_id"`
}

type placeCandidate struct {
	PageID    string          `json:"page_id"`
	Revision  string          `json:"revision"`
	Title     string          `json:"title"`
	AppliesTo string          `json:"applies_to"`
	Kind      string          `json:"kind,omitempty"` // the candidate page's kind and scene (the values carried over when merging)
	Scene     string          `json:"scene,omitempty"`
	SameTopic placeJudgement  `json:"same_topic"`
	Conflict  *placeJudgement `json:"conflict,omitempty"`
}

type placement struct {
	Draft      int              `json:"draft"`
	Title      string           `json:"title"`
	Candidates []placeCandidate `json:"candidates"`
	Note       string           `json:"note"`
}

type placeResult struct {
	Placements []placement  `json:"placements"`
	Scenes     []sceneCount `json:"scenes"` // existing scenes (most used first, up to placeScenesMax). A new draft's scene uses the same wording when one is close (0.9.0)
}

// sceneCount is a scene and its number of pages.
type sceneCount struct {
	Scene string `json:"scene"`
	Count int    `json:"count"`
}

// placeScenesMax is the max number of scenes in the insertion-point reply.
const placeScenesMax = 30

// liveScenes returns the scenes of live pages, most used first (ties by name).
func liveScenes(live []notebook.Page) []sceneCount {
	n := map[string]int{}
	for _, p := range live {
		if p.Scene != "" {
			n[p.Scene]++
		}
	}
	out := []sceneCount{}
	for sc, c := range n {
		out = append(out, sceneCount{Scene: sc, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Scene < out[j].Scene
	})
	if len(out) > placeScenesMax {
		out = out[:placeScenesMax]
	}
	return out
}

const placeNote = "Until a line is drawn this is only a reference. Read the body of pages judged to be the same topic; to add to one, put its id in submit, and to replace one, put it in replaces. For a page suspected to disagree, check which one rests on newer sources before deciding. If the topic is not among the candidates, make a new page. Saving needs a kind (rule / howto / criteria / fact / history) and a scene. If a scene in scenes is close, use the same wording (when merging, leave it out to carry it over)."

// placeDrafts returns, for each draft, the same-topic candidates (highest p first) and the top suspected contradictions. The decisions are recorded and get their truth from the result of submit.
func placeDrafts(root, scope string, drafts []notebook.Draft) (placeResult, error) {
	res := placeResult{Placements: []placement{}}
	if len(drafts) == 0 || len(drafts) > 2 {
		return res, errors.New("place takes 1 or 2 wiki drafts")
	}
	book := notebook.Book{Root: root, Scope: scope}
	live, err := book.Live()
	if err != nil {
		return res, err
	}
	res.Scenes = liveScenes(live)
	project := filepath.Base(scope)
	for i, d := range drafts {
		lesson, applies := strings.TrimSpace(d.Lesson), strings.TrimSpace(d.AppliesTo)
		if lesson == "" || applies == "" {
			return res, errors.New("a draft asking for an insertion point needs lesson and applies_to")
		}
		pl := placement{Draft: i, Title: strings.TrimSpace(d.Title), Candidates: []placeCandidate{}, Note: placeNote}
		pages, err := placeCandidatePages(book, live, d)
		if err != nil {
			return res, err
		}
		if len(pages) == 0 {
			pl.Note = "There are no existing pages, so this becomes a new page."
			res.Placements = append(res.Placements, pl)
			continue
		}
		judged := make([]*decide.Decision, len(pages))
		errs := make([]error, len(pages))
		var wg sync.WaitGroup
		slots := make(chan struct{}, placeParallel)
		for j, p := range pages {
			wg.Add(1)
			go func(j int, p notebook.Page) {
				defer wg.Done()
				slots <- struct{}{}
				defer func() { <-slots }()
				inputs := map[string]string{"title": p.Title, "lesson": p.Lesson, "applies_to": p.AppliesTo, "new_lesson": lesson, "new_applies": applies}
				judged[j], _, errs[j] = hookDecide(root, "wiki-place", inputs, project)
			}(j, p)
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				return res, e
			}
		}
		for j, p := range pages {
			if judged[j] == nil || judged[j].Band == decide.Abstain {
				continue
			}
			pl.Candidates = append(pl.Candidates, placeCandidate{PageID: p.ID, Revision: p.Revision, Title: p.Title, AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: p.Scene, SameTopic: judgement(judged[j])})
		}
		if len(pl.Candidates) == 0 {
			return res, errors.New("the local model does not answer about insertion points (check that it is running, then call place again)")
		}
		sort.SliceStable(pl.Candidates, func(a, b int) bool { return pl.Candidates[a].SameTopic.P > pl.Candidates[b].SameTopic.P })
		for j := 0; j < len(pl.Candidates) && j < placeConflictTop; j++ {
			c := &pl.Candidates[j]
			existing := ""
			for _, p := range pages {
				if p.ID == c.PageID {
					existing = p.Lesson
				}
			}
			d, _, err := hookDecide(root, "wiki-conflict", map[string]string{"lesson": existing, "new_lesson": lesson}, project)
			if err != nil {
				return res, err
			}
			if d != nil && d.Band != decide.Abstain {
				j := judgement(d)
				c.Conflict = &j
			}
		}
		res.Placements = append(res.Placements, pl)
	}
	return res, nil
}

func judgement(d *decide.Decision) placeJudgement {
	return placeJudgement{P: d.P, Band: string(d.Band), Calibrated: d.IsCalibrated, TraceID: d.TraceID}
}

// placeCandidatePages are the pages to compare. Over the limit, take them in order of how well they match the draft's words.
func placeCandidatePages(book notebook.Book, live []notebook.Page, d notebook.Draft) ([]notebook.Page, error) {
	if len(live) <= placeCandidatesMax {
		return live, nil
	}
	v, err := book.Query(strings.TrimSpace(d.Title + " " + d.Lesson + " " + d.AppliesTo))
	if err != nil {
		return nil, err
	}
	byID := map[string]notebook.Page{}
	for _, p := range live {
		byID[p.ID] = p
	}
	picked := []notebook.Page{}
	seen := map[string]bool{}
	if v.Search != nil {
		for _, c := range v.Search.Candidates {
			if p, ok := byID[c.ID]; ok && !seen[c.ID] && len(picked) < placeCandidatesMax {
				picked, seen[c.ID] = append(picked, p), true
			}
		}
	}
	for _, p := range live {
		if !seen[p.ID] && len(picked) < placeCandidatesMax {
			picked, seen[p.ID] = append(picked, p), true
		}
	}
	return picked, nil
}

// placementOutcome is the truth from the result for the insertion-point and contradiction decisions asked about this draft (built before saving, written after the save succeeds).
// Pages added to (id) or replaced (replaces) are "yes" for same topic; others are "no" (partial). Contradiction is "yes" if replaced and "no" (partial) if added to without replacing.
// Which draft a decision belongs to is looked up by the draft text at placing time (new_lesson). Merging often rewrites the lesson text, so even when not exactly the same,
// a draft counts as the same if most (placeMatchMin) of the text at placing time is in the draft being saved (title, lesson, body) (0.8.3. With exact matches only,
// truths were attached only when a new page was made, and every insertion-point truth became "no").
func placementOutcome(root, scope string, d notebook.Draft) ([]store.Label, error) {
	lesson := strings.TrimSpace(d.Lesson)
	if lesson == "" {
		return nil, nil
	}
	live, err := (notebook.Book{Root: root, Scope: scope}).Live()
	if err != nil {
		return nil, err
	}
	pageOf := map[string]string{} // page lesson text → id (to find the page from the decision's input)
	for _, p := range live {
		pageOf[p.Lesson] = p.ID
	}
	chosen := map[string]bool{}
	if d.ID != "" {
		chosen[d.ID] = true
	}
	replaced := map[string]bool{}
	for _, id := range d.Replaces {
		chosen[id], replaced[id] = true, true
	}
	st := store.Store{Dir: root}
	recsBy := map[string][]store.Record{}
	labeledBy := map[string]map[string]store.Label{}
	for _, specID := range wikiSpecs {
		recs, err := st.Records(specID, 0)
		if err != nil {
			return nil, err
		}
		labeled, err := st.LabeledTraces(specID)
		if err != nil {
			return nil, err
		}
		recsBy[specID], labeledBy[specID] = recs, labeled
	}
	placed := matchPlacedDraft(d, recsBy["wiki-place"], labeledBy["wiki-place"], pageOf, time.Now())
	if placed == "" {
		return nil, nil
	}
	var out []store.Label
	for _, specID := range wikiSpecs {
		for i := range recsBy[specID] {
			rec := &recsBy[specID][i]
			if _, done := labeledBy[specID][rec.TraceID]; done || strings.TrimSpace(rec.Inputs["new_lesson"]) != placed {
				continue
			}
			page, ok := pageOf[rec.Inputs["lesson"]]
			if !ok {
				continue
			}
			truth, weight := "no", placeOtherWeight
			switch {
			case specID == "wiki-place" && chosen[page]:
				truth, weight = "yes", placeChosenWeight
			case specID == "wiki-conflict" && replaced[page]:
				truth, weight = "yes", placeChosenWeight
			case specID == "wiki-conflict" && page != d.ID:
				continue // for pages neither added to nor replaced, whether they disagree is unknown
			}
			labels, err := store.LabelsFor(rec, truth, weight, "outcome")
			if err != nil {
				return nil, err
			}
			out = append(out, labels...)
		}
	}
	return out, nil
}

// placeMatchMin is the minimum share of adjacent word pairs (bigrams) of the draft text at placing time that appear in the draft being saved (title, lesson, body).
// placeMatchWindow is the max age of a placed draft for fuzzy matching (saving usually follows placing right away).
const (
	placeMatchMin    = 0.6
	placeMatchWindow = 24 * time.Hour
)

// matchPlacedDraft returns which placed draft without a truth (new_lesson) the draft being saved refers to ("" if none).
// Only placed drafts with records that can get a truth (the candidate page still exists) are considered (so old records whose candidate pages are gone do not match first; audit 0.8.3 M1).
//   - If the lesson text is the same as at placing time, that draft (the newer one).
//   - Otherwise only merges and replacements: among drafts that have that page as a candidate (a new page has no target page, so it never matches fuzzily; audit 0.8.3 H1), were placed within placeMatchWindow, and have placeMatchMin or more of the text at placing time in the draft being saved,
//     the one that is contained best (ties go to the most recently placed. Placing 2 at once is the normal procedure, so choosing by recency alone
//     attaches to the other one; re-audit new-1). Old drafts placed the day before drop out by placeMatchWindow (audit 0.8.3 M2).
func matchPlacedDraft(d notebook.Draft, recs []store.Record, labeled map[string]store.Label, pageOf map[string]string, now time.Time) string {
	lesson := strings.TrimSpace(d.Lesson)
	targets := map[string]bool{}
	if d.ID != "" {
		targets[d.ID] = true
	}
	for _, id := range d.Replaces {
		targets[id] = true
	}
	type group struct {
		at        time.Time
		labelable bool
		hasTarget bool
	}
	groups := map[string]*group{}
	for _, rec := range recs {
		if _, done := labeled[rec.TraceID]; done {
			continue
		}
		placed := strings.TrimSpace(rec.Inputs["new_lesson"])
		if placed == "" {
			continue
		}
		g := groups[placed]
		if g == nil {
			g = &group{}
			groups[placed] = g
		}
		if rec.At.After(g.at) {
			g.at = rec.At
		}
		if page, ok := pageOf[rec.Inputs["lesson"]]; ok {
			g.labelable = true
			if targets[page] {
				g.hasTarget = true
			}
		}
	}
	if g := groups[lesson]; g != nil && g.labelable && !targets[pageOf[lesson]] {
		return lesson // if the lesson is the same as the merge target's current lesson (a merge that only adds to the body), do not use it, so it is not confused with the old record from when that page was made (re-audit remaining-2)
	}
	text := d.Title + "\n" + d.Lesson
	if d.Body != nil {
		text += "\n" + *d.Body
	}
	have := bigrams(text)
	known := map[string]bool{} // word pairs already in the merge target's current lesson. A draft that only rephrases is not ranked above a draft that really added something (re-re-audit new-B)
	for l, id := range pageOf {
		if targets[id] {
			for b := range bigrams(l) {
				known[b] = true
			}
		}
	}
	// Tier: a draft with word pairs not in the merge target's lesson (added content) is 1; a draft whose pairs are all already there (only rephrased) is 0. Rephrasing drafts
	// still get a score from their overall coverage and stay as candidates (so a truth is attached when a person states the same rule again; audit 0.9.0 C1), ranked below drafts that added content
	best, bestTier, bestScore, bestAt := "", -1, 0.0, time.Time{}
	for placed, g := range groups {
		if !g.hasTarget || now.Sub(g.at) > placeMatchWindow {
			continue
		}
		want := bigrams(placed)
		if len(want) == 0 {
			continue
		}
		added := map[string]bool{}
		for b := range want {
			if !known[b] {
				added[b] = true
			}
		}
		tier := 1
		if len(added) == 0 {
			tier, added = 0, want
		}
		hit := 0
		for b := range added {
			if have[b] {
				hit++
			}
		}
		score := float64(hit) / float64(len(added))
		if score < placeMatchMin {
			continue
		}
		if tier > bestTier || tier == bestTier && (score > bestScore || score == bestScore && g.at.After(bestAt)) {
			best, bestTier, bestScore, bestAt = placed, tier, score, g.at
		}
	}
	return best
}

// bigrams is the set of adjacent word pairs in a text. A run of letters and digits is one word (lowercased); any other letter (kanji, kana) is one word per character;
// spaces and symbols separate words. Comparing English 2 characters at a time made even long unrelated texts overlap mostly (re-audit remaining-1).
func bigrams(s string) map[string]bool {
	var words []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			words = append(words, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	for _, r := range s {
		switch {
		case r < 0x3000 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			word = append(word, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flush()
			words = append(words, string(r))
		default:
			flush()
		}
	}
	flush()
	out := map[string]bool{}
	for i := 0; i+1 < len(words); i++ {
		out[words[i]+" "+words[i+1]] = true
	}
	return out
}

// ingestWithOutcome saves the page and, if the save succeeds, attaches truths from the result to the insertion-point and contradiction decisions asked about that draft.
// Both the reflection save (reflect submit) and wiki ingest (CLI, MCP, HTTP) go through here.
func ingestWithOutcome(root, scope string, d notebook.Draft) (notebook.Result, error) {
	outcome, oerr := placementOutcome(root, scope, d)
	res, err := (notebook.Book{Root: root, Scope: scope}).Ingest(d)
	if err != nil {
		return res, err
	}
	if oerr != nil {
		hookLog("wiki place: " + oerr.Error())
		return res, nil
	}
	bySpec := map[string][]store.Label{} // appended per spec
	for _, l := range outcome {
		bySpec[l.SpecID] = append(bySpec[l.SpecID], l)
	}
	for _, labels := range bySpec {
		if err := (store.Store{Dir: root}).AppendLabels(labels); err != nil {
			hookLog("wiki place: " + err.Error())
		}
	}
	return res, nil
}
