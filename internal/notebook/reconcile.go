package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

type ReconcileConflict struct {
	Field    string `json:"field"`
	Base     any    `json:"base"`
	Current  any    `json:"current"`
	Proposed any    `json:"proposed"`
}

type Reconciliation struct {
	Status     string              `json:"status"`
	Base       ArticleReference    `json:"base"`
	Current    ArticleReference    `json:"current"`
	Patch      *Patch              `json:"patch,omitempty"`
	Result     *Page               `json:"result,omitempty"`
	Conflicts  []ReconcileConflict `json:"conflicts"`
	Reason     string              `json:"reason,omitempty"`
	NextAction string              `json:"next_action"`
	Notice     string              `json:"notice"`
}

// Reconcile never publishes a page. The ordinary preview validates the merged
// metadata under its lock, and the returned patch retains that exact revision.
func (b Book) Reconcile(intent Patch) (Reconciliation, error) {
	out := Reconciliation{Status: "unavailable", Base: ArticleReference{ID: intent.ID, Revision: intent.Revision}, Conflicts: []ReconcileConflict{},
		NextAction: "Inspect both writers' conditions and sources. Apply only a ready patch with wiki patch, keeping its revision and requires. On a new conflict, read the current page and replan; a preview is not a save.",
		Notice:     "An unsaved conservative merge proposal, never semantic agreement, permission or a human truth. Overlapping text and competing metadata remain explicit conflicts."}
	if !idPattern.MatchString(intent.ID) || !idPattern.MatchString(intent.Revision) || strings.TrimSpace(intent.ChangeReason) == "" || len(intent.Sources) == 0 || len(intent.Edits) > 16 || len(intent.Edits) == 0 && intent.Passages == nil && intent.Learning == nil && intent.Related == nil {
		return out, errors.New("reconcile needs a pinned patch with sources, a change_reason and bounded edits or metadata")
	}
	for _, id := range intent.Sources {
		if !idPattern.MatchString(id) {
			return out, errors.New("invalid reconciliation source")
		}
	}
	old, err := b.readRevision("lesson:" + intent.ID + "@" + intent.Revision)
	if err != nil {
		out.Reason = "The base revision or its sources cannot be checked."
		return out, nil
	}
	view, err := b.Query("lesson:" + intent.ID)
	// The exact query also lists source problems of the articles it scanned before
	// this one, in id order. Only a problem of this article, or an unreadable page
	// (which blocks every save), makes it unavailable.
	blocked := false
	for _, issue := range view.Issues {
		blocked = blocked || issue.Page == intent.ID || issue.Kind == "unreadable-page" || issue.Kind == "changed-page"
	}
	if err != nil || len(view.Lessons) != 1 || view.Lessons[0].Status == "deleted" || blocked {
		out.Reason = "The current article is unavailable, replaced, deleted or has invalid sources."
		return out, nil
	}
	current := view.Lessons[0]
	out.Current = ArticleReference{ID: current.ID, Revision: current.Revision}
	proposed, err := applyReconcileEdits(old, intent.Edits)
	if err != nil {
		return out, err
	}
	ready := Patch{ID: current.ID, Revision: current.Revision, Sources: append([]string(nil), intent.Sources...), ChangeReason: intent.ChangeReason, Requires: append([]ArticleReference(nil), intent.Requires...)}
	oldFields, currentFields, proposedFields := reconcileFields(old), reconcileFields(current), reconcileFields(proposed)
	for _, field := range []string{"title", "lesson", "applies_to", "body", "rationale", "kind", "scene"} {
		base, now, mine := oldFields[field], currentFields[field], proposedFields[field]
		if mine == base || mine == now {
			continue
		}
		merged := mine
		if now != base {
			if field == "body" {
				var ok bool
				merged, ok = mergeArticleText(base, now, mine)
				if !ok {
					out.Conflicts = append(out.Conflicts, ReconcileConflict{field, base, now, mine})
					continue
				}
			} else {
				out.Conflicts = append(out.Conflicts, ReconcileConflict{field, base, now, mine})
				continue
			}
		}
		if merged != now {
			ready.Edits = append(ready.Edits, TextEdit{Field: field, Before: now, After: merged})
		}
	}
	check := func(field string, base, now, mine any) bool {
		if !sameReconcileMetadata(now, base) && !sameReconcileMetadata(now, mine) {
			out.Conflicts = append(out.Conflicts, ReconcileConflict{field, base, now, mine})
			return false
		}
		return true
	}
	if intent.Passages != nil && !sameReconcileMetadata(old.Passages, *intent.Passages) && !sameReconcileMetadata(current.Passages, *intent.Passages) && check("passages", old.Passages, current.Passages, *intent.Passages) {
		ready.Passages = intent.Passages
	}
	if intent.Learning != nil && !sameReconcileMetadata(old.Learning, intent.Learning) && !sameReconcileMetadata(current.Learning, intent.Learning) && check("learning", old.Learning, current.Learning, intent.Learning) {
		ready.Learning = intent.Learning
	}
	if intent.Related != nil && !sameReconcileMetadata(old.Related, *intent.Related) && !sameReconcileMetadata(current.Related, *intent.Related) && check("related", old.Related, current.Related, *intent.Related) {
		ready.Related = intent.Related
	}
	if len(out.Conflicts) > 0 {
		out.Status = "conflict"
		return boundedReconciliation(out)
	}
	if len(ready.Edits) == 0 && ready.Passages == nil && ready.Learning == nil && ready.Related == nil {
		out.Status, out.Reason = "unchanged", "The proposed changes are already present; no patch is needed."
		return out, nil
	}
	preview, err := b.PreviewPatch(ready)
	if err != nil {
		out.Status, out.Reason = "conflict", "The combined page failed current revision, citation or metadata validation: "+err.Error()
		return boundedReconciliation(out)
	}
	out.Status, out.Patch, out.Result = "ready", &ready, preview.Page
	return boundedReconciliation(out)
}

func boundedReconciliation(v Reconciliation) (Reconciliation, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 128<<10 {
		return Reconciliation{}, errors.New("the reconciliation preview exceeds 128 KiB; narrow the proposed changes")
	}
	return v, nil
}

func reconcileFields(p Page) map[string]string {
	return map[string]string{"title": p.Title, "lesson": p.Lesson, "applies_to": p.AppliesTo, "body": p.Body, "rationale": p.Rationale, "kind": p.Kind, "scene": p.Scene}
}

func applyReconcileEdits(p Page, edits []TextEdit) (Page, error) {
	fields := map[string]*string{"title": &p.Title, "lesson": &p.Lesson, "applies_to": &p.AppliesTo, "body": &p.Body, "rationale": &p.Rationale, "kind": &p.Kind, "scene": &p.Scene}
	for i, e := range edits {
		field, ok := fields[e.Field]
		if !ok || !utf8.ValidString(e.Before) || !utf8.ValidString(e.After) || len(e.Before) > 16<<10 || len(e.After) > 16<<10 || e.Before == e.After {
			return p, fmt.Errorf("invalid reconciliation edit %d", i+1)
		}
		if e.Before == "" {
			if *field != "" {
				return p, errors.New("an empty before can only fill an empty base field")
			}
		} else if !uniqueLearningQuote(*field, e.Before) {
			return p, errors.New("a reconciliation edit must match exactly once in its base field")
		}
		*field = strings.Replace(*field, e.Before, e.After, 1)
	}
	return p, nil
}

type articleRange struct {
	start, end int
	text       string
}

// Line LCS is bounded before allocation. Repeated changed lines are ambiguous;
// reject those instead of moving an edit onto another occurrence.
func articleRanges(base, changed string) ([]articleRange, bool) {
	a, z := strings.SplitAfter(base, "\n"), strings.SplitAfter(changed, "\n")
	if len(a) > 512 || len(z) > 512 {
		return nil, false
	}
	dp := make([][]uint16, len(a)+1)
	for i := range dp {
		dp[i] = make([]uint16, len(z)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(z) - 1; j >= 0; j-- {
			if a[i] == z[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var out []articleRange
	for i, j := 0, 0; i < len(a) || j < len(z); {
		if i < len(a) && j < len(z) && a[i] == z[j] {
			i, j = i+1, j+1
			continue
		}
		start := i
		var replacement strings.Builder
		for i < len(a) || j < len(z) {
			if i < len(a) && j < len(z) && a[i] == z[j] {
				break
			}
			if j < len(z) && (i == len(a) || dp[i][j+1] >= dp[i+1][j]) {
				replacement.WriteString(z[j])
				j++
			} else {
				if strings.TrimSpace(a[i]) != "" {
					count := 0
					for _, line := range a {
						if line == a[i] {
							count++
						}
					}
					if count > 1 {
						return nil, false
					}
				}
				i++
			}
		}
		out = append(out, articleRange{start, i, replacement.String()})
	}
	return out, true
}

func mergeArticleText(base, current, proposed string) (string, bool) {
	a, ok := articleRanges(base, current)
	if !ok {
		return "", false
	}
	z, ok := articleRanges(base, proposed)
	if !ok {
		return "", false
	}
	all := append([]articleRange(nil), a...)
	for _, mine := range z {
		same := false
		for _, now := range a {
			if mine == now {
				same = true
				break
			}
			overlap := mine.start < now.end && now.start < mine.end
			if mine.start == mine.end {
				overlap = mine.start >= now.start && mine.start <= now.end
			}
			if now.start == now.end {
				overlap = now.start >= mine.start && now.start <= mine.end
			}
			if overlap {
				return "", false
			}
		}
		if !same {
			all = append(all, mine)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].start < all[j].start })
	lines, position := strings.SplitAfter(base, "\n"), 0
	var merged strings.Builder
	for _, edit := range all {
		if edit.start < position {
			return "", false
		}
		merged.WriteString(strings.Join(lines[position:edit.start], ""))
		merged.WriteString(edit.text)
		position = edit.end
	}
	merged.WriteString(strings.Join(lines[position:], ""))
	return merged.String(), true
}

func sameReconcileMetadata(a, z any) bool {
	if reflect.DeepEqual(a, z) {
		return true
	}
	switch v := a.(type) {
	case []KnowledgePassage:
		w, ok := z.([]KnowledgePassage)
		return ok && len(v) == 0 && len(w) == 0
	case []string:
		w, ok := z.([]string)
		return ok && len(v) == 0 && len(w) == 0
	case *Learning:
		w, ok := z.(*Learning)
		return ok && learningEmpty(v) && learningEmpty(w)
	}
	return false
}
