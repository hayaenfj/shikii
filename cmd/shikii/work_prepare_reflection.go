package main

import (
	"strconv"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func prepareWorkReflection(book notebook.Book, w *workState, reflection reflectView) (pending, evidence, maintenance []any) {
	pending = []any{}
	reviewN, memoryN := 0, 0
	for i, source := range reflection.Sources {
		ref := "source" + strconv.Itoa(i+1)
		if source.Head {
			pending = append(pending, map[string]any{"request": source.Source.Request, "head": true, "next_action": "Prepare again after the complete sources are settled; do not judge a shortened source."})
			continue
		}
		material := prepareWorkSource(book, source.Source.ID)
		if !material.Complete {
			pending = append(pending, map[string]any{"request": source.Source.Request, "head": true, "incomplete": true, "wiki_pending": source.WikiPending, "source": material.Material, "source_query": material.Query, "next_action": material.Notice})
			continue
		}
		w.Sources[ref] = source.Source.ID
		reviews := []any{}
		memories := []any{}
		for _, target := range source.Targets {
			reviewN++
			r := "review" + strconv.Itoa(reviewN)
			w.Reviews[r] = workReviewRef{source.Source.ID, target.TraceID}
			reviews = append(reviews, map[string]any{"ref": r, "question": target.Question, "files": target.Files, "decision_model_answer": target.ModelAnswer, "original_inputs": target.OriginalInputs, "text_status": target.TextStatus})
		}
		for _, m := range source.Memories {
			memoryN++
			r := "memory" + strconv.Itoa(memoryN)
			w.Memory[r] = workMemoryRef{m.SourceID, m.Quote.Key}
			memories = append(memories, map[string]any{"ref": r, "quote": m.Quote, "request": m.Request, "parent": m.Parent, "agent": m.Agent, "tool": m.Tool, "phrase": m.Phrase, "at": m.At})
		}
		pending = append(pending, map[string]any{"ref": ref, "request": source.Source.Request, "answer": source.Source.Summary, "reply": source.Source.Reply, "head": source.Head, "wiki_pending": source.WikiPending, "source": material.Material, "reviews": reviews, "memory": memories})
	}
	evidence = []any{}
	for i, o := range reflection.Observations {
		ref := "evidence" + strconv.Itoa(i+1)
		material := prepareWorkSource(book, o.ID)
		if !material.Complete {
			evidence = append(evidence, map[string]any{"head": o, "incomplete": true, "source": material.Material, "source_query": material.Query, "next_action": material.Notice})
			continue
		}
		w.Sources[ref] = o.ID
		if workReport(o.Kind, o.Title) { // a report of this or an earlier request that waits to be sorted: reviewed, never evidence
			markReport(w, o.ID)
		}
		evidence = append(evidence, map[string]any{"ref": ref, "source": material.Material})
	}
	maintenance = []any{}
	for _, m := range reflection.Maintenance {
		relevant := false
		for _, article := range w.Articles {
			relevant = relevant || article.ID == m.PageID
		}
		if !relevant {
			continue
		}
		ref := "maintenance" + strconv.Itoa(len(maintenance)+1)
		w.Maintenance[ref] = notebook.MaintenanceReview{PageID: m.PageID, Revision: m.Revision, Key: m.Key}
		maintenance = append(maintenance, map[string]any{"ref": ref, "task": m})
	}
	return pending, evidence, maintenance
}
