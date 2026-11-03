package notebook

import "errors"

// ReconcileChange previews a person's retained editor input. Its current
// citations are validated by the same patch preview as agent-authored changes.
// The ordinary Manage edit captures the actual human change when it is saved.
func (b Book) ReconcileChange(c Change) (Reconciliation, error) {
	if c.Action != "reconcile" {
		return Reconciliation{}, errors.New("a comparison needs the reconcile action")
	}
	base, err := b.readRevision("lesson:" + c.ID + "@" + c.Revision)
	if err != nil {
		return Reconciliation{Status: "unavailable", Reason: "The original article revision cannot be checked."}, nil
	}
	patch := Patch{ID: c.ID, Revision: c.Revision, ChangeReason: "Compare retained Knowledge page input", Passages: c.Passages}
	for _, s := range base.Sources {
		patch.Sources = append(patch.Sources, s.ID)
	}
	proposed := base
	proposed.Title, proposed.Lesson, proposed.AppliesTo, proposed.Body, proposed.Kind, proposed.Scene = c.Title, c.Lesson, c.AppliesTo, c.Body, c.Kind, c.Scene
	if c.Rationale != nil {
		proposed.Rationale = *c.Rationale
	}
	a, z := reconcileFields(base), reconcileFields(proposed)
	for _, field := range []string{"title", "lesson", "applies_to", "body", "rationale", "kind", "scene"} {
		if a[field] != z[field] {
			patch.Edits = append(patch.Edits, TextEdit{Field: field, Before: a[field], After: z[field]})
		}
	}
	if len(patch.Edits) == 0 && patch.Passages == nil {
		return Reconciliation{Status: "unchanged", Base: ArticleReference{ID: c.ID, Revision: c.Revision}}, nil
	}
	return b.Reconcile(patch)
}
