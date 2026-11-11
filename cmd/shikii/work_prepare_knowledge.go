package main

import (
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

func initialPlanningKnowledge(s *hookSession, w *workState) []any {
	knowledge := []any{}
	keys := []string{}
	for key := range s.Planning.Sources {
		if strings.HasPrefix(key, "article:") || strings.HasPrefix(key, "lesson:") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, key := range keys {
		parts := strings.Split(key, ":")
		if len(parts) != 3 || seen[parts[1]] {
			continue
		}
		seen[parts[1]] = true
		ref := preparedArticleRef(w, parts[1], parts[2])
		w.Keys[ref] = key
		w.Articles[ref] = store.KnowledgeReference{ID: parts[1], Revision: parts[2]}
		knowledge = append(knowledge, map[string]any{"ref": ref, "id": parts[1], "revision": parts[2], "text": s.Planning.Sources[key], "body_included": strings.HasPrefix(key, "article:"), "query": map[string]any{"operation": "query", "query": "lesson:" + parts[1] + "@" + parts[2]}})
	}
	return knowledge
}
