package main

// wiki の差し込み先と矛盾。エージェントが新しい教訓を書いたら、保存(submit)の前に reflect の place へ渡す。
// shikii は生きている記事と一本ずつ突き合わせて「同じ話題か」(wiki-place)を手元のモデルに聞き、上位 placeConflictTop 件には「食い違うか」(wiki-conflict)も聞く。
// 正解は submit の結果から自動で付く(エージェントがどの記事へ足したか・置き換えたか)。人は答えない。
// 線が無い間は参考で、エージェントが本文を読んで決める(食い違いは、線が引けても黙って置き換えない)。

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// machineOnlySpecs は、人が答えない判定(AI が答え、正解は振り返りの結果から付く)。画面は確認待ちに数えず、カードを読むだけにする(0.7.6)。
var machineOnlySpecs = map[string]bool{"wiki-place": true, "wiki-conflict": true}

// 固定の規則(オプションは無い)。
const (
	placeCandidatesMax = 24  // 突き合わせる記事の上限(多ければ検索語の一致が高い順)
	placeConflictTop   = 3   // 矛盾を聞く上位の数
	placeParallel      = 4   // 手元のモデルへ同時に聞く数
	placeChosenWeight  = 1.0 // 足した・置き換えた記事(はっきりした結果)
	placeOtherWeight   = 0.5 // 選ばなかった記事(見落としもあり得るので部分の重み)
)

// wikiSpecs は wiki の整理で聞く 2 つ(埋め込みの spec の id)。hook と同じく自動で線を引く。
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
	Placements []placement `json:"placements"`
}

const placeNote = "線が引けるまでは参考です。同じ話題と見た記事は本文を読み、足すなら id を、置き換えるなら replaces を submit に書きます。食い違いの疑いがある記事は、どちらが新しい出典に基づくかを確かめてから決めます。候補に無い話題なら新しい記事にします。"

// placeDrafts は草稿ごとに、同じ話題の候補(p の高い順)と、上位の矛盾の疑いを返す。判定は記録に残り、submit の結果で正解が付く。
func placeDrafts(root, scope string, drafts []notebook.Draft) (placeResult, error) {
	res := placeResult{Placements: []placement{}}
	if len(drafts) == 0 || len(drafts) > 2 {
		return res, errors.New("place は 1〜2 件の wiki の草稿を受け取ります")
	}
	book := notebook.Book{Root: root, Scope: scope}
	live, err := book.Live()
	if err != nil {
		return res, err
	}
	project := filepath.Base(scope)
	for i, d := range drafts {
		lesson, applies := strings.TrimSpace(d.Lesson), strings.TrimSpace(d.AppliesTo)
		if lesson == "" || applies == "" {
			return res, errors.New("差し込み先を聞く草稿には lesson と applies_to が要ります")
		}
		pl := placement{Draft: i, Title: strings.TrimSpace(d.Title), Candidates: []placeCandidate{}, Note: placeNote}
		pages, err := placeCandidatePages(book, live, d)
		if err != nil {
			return res, err
		}
		if len(pages) == 0 {
			pl.Note = "既存の記事が無いので、新しい記事になります。"
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
			pl.Candidates = append(pl.Candidates, placeCandidate{PageID: p.ID, Revision: p.Revision, Title: p.Title, AppliesTo: p.AppliesTo, SameTopic: judgement(judged[j])})
		}
		if len(pl.Candidates) == 0 {
			return res, errors.New("手元のモデルが差し込み先に答えません(起動を確かめてから、もう一度 place を呼びます)")
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

// placeCandidatePages は突き合わせる記事。上限を超えるなら、草稿の語との一致が高い順に取る。
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

// placementOutcome は、この草稿について聞いた差し込み先と矛盾の判定に付ける結果の正解(submit の前に組み、保存が通ってから書く)。
// 足した(id)・置き換えた(replaces)記事は同じ話題「はい」、他は「いいえ」(部分)。矛盾は置き換えたら「はい」、置き換えずに足したら「いいえ」(部分)。
func placementOutcome(root, scope string, d notebook.Draft) ([]store.Label, error) {
	lesson := strings.TrimSpace(d.Lesson)
	if lesson == "" {
		return nil, nil
	}
	live, err := (notebook.Book{Root: root, Scope: scope}).Live()
	if err != nil {
		return nil, err
	}
	pageOf := map[string]string{} // 記事の教訓の文 → id(判定の入力から記事を引く)
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
	var out []store.Label
	for _, specID := range wikiSpecs {
		recs, err := st.Records(specID, 0)
		if err != nil {
			return nil, err
		}
		labeled, err := st.LabeledTraces(specID)
		if err != nil {
			return nil, err
		}
		for i := range recs {
			rec := &recs[i]
			if _, done := labeled[rec.TraceID]; done || strings.TrimSpace(rec.Inputs["new_lesson"]) != lesson {
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
				continue // 足しも置き換えもしなかった記事とは、食い違うかどうか分からない
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
