package main

import (
	"fmt"
	"maps"
	"slices"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Delivery references belong to one receipt. They record only the final payload
// that passed the transport budget, never use, quality, agreement or permission.
type workDeliveryState struct {
	Sequence         uint64                     `json:"sequence"`
	Metadata         map[string]workMetadataRef `json:"metadata,omitempty"`
	CollectiveNotice *workNoticeRef             `json:"collective_notice,omitempty"`
}

type workNoticeRef struct {
	Text     string `json:"text"`
	Delivery uint64 `json:"delivery"`
}

type workCollectiveDelivery struct {
	notebook.CollectiveContext
	NoticeFrom uint64 `json:"notice_from"`
}

func reusedWorkNotice(reply map[string]any) bool {
	_, ok := reply["collective"].(workCollectiveDelivery)
	return ok
}

type workMetadataRef struct {
	Signature string `json:"signature"`
	Delivery  uint64 `json:"delivery"`
	// Held names, for each text the orientation of that delivery did not write (id, revision, summary, conditions), the place of the
	// candidate that held it there (orientationPlaces). A record without it, as older versions saved, means the orientation wrote them all.
	Held map[string]string `json:"held,omitempty"`
}

// reusedMetadataFields names the fields of the earlier delivery that the entry now refers to: where each text of the orientation stood
// there (its place in the candidate when the orientation did not write it, orientation.<name> otherwise), then the orientation's
// interpretations. A name is written once, and a place the record does not know is taken as an orientation field.
func reusedMetadataFields(old workMetadataRef) []string {
	fields := []string{}
	add := func(name string) {
		if !slices.Contains(fields, name) {
			fields = append(fields, name)
		}
	}
	for _, key := range []string{"id", "revision", "summary", "conditions"} {
		places := orientationPlaces[key]
		if at := old.Held[key]; at != "" && (at == places[0] || at == places[1] || at == orientationTextPlace && orientationTextLines[key] != "") {
			add(at)
		} else {
			add("orientation." + key)
		}
	}
	add("orientation.current")
	add("orientation.history")
	return fields
}

func workMetadataSignature(e map[string]any) (string, bool) {
	// The whole orientation is hashed, not the form written out, so the value does not depend on which texts the candidate holds.
	o, ok := orientationOf(e["orientation"])
	if !ok || e["ref"] == nil || e["id"] == nil || e["revision"] == nil {
		return "", false
	}
	return workHash([]any{e["id"], e["revision"], o, e["title"], e["summary"], e["applies_to"], e["rationale"], e["learning_context"], e["recheck"]}), true
}

func reusedWorkMetadata(reply map[string]any) bool {
	entries, _ := reply["knowledge"].([]any)
	for _, raw := range entries {
		if e, ok := raw.(map[string]any); ok && e["metadata_from"] != nil {
			return true
		}
	}
	return false
}

func reuseWorkMetadata(w *workState, reply map[string]any, size func(any) int) {
	if w.Nonce == "" {
		return
	}
	if w.Delivery == nil {
		w.Delivery = &workDeliveryState{Metadata: map[string]workMetadataRef{}}
	}
	d := w.Delivery
	d.Sequence++
	reply["delivery"] = d.Sequence
	entries, _ := reply["knowledge"].([]any)
	for _, raw := range entries {
		e := raw.(map[string]any)
		signature, ok := workMetadataSignature(e)
		ref, _ := e["ref"].(string)
		old := d.Metadata[ref]
		// Body/excerpt reuse already excludes explicit recovery, changed depth,
		// changed premises and other receipts. Metadata has its own exact match.
		if !ok || e["reused"] != true || loopPremiseChanged(e) || old.Signature != signature || old.Delivery == 0 || old.Delivery >= d.Sequence {
			continue
		}
		original := maps.Clone(e)
		before := size(reply)
		o, _ := orientationOf(e["orientation"])
		// Leave unresolved text, counterexamples, missing material and warnings
		// visible on every delivery. Only unchanged descriptive fields move.
		e["orientation"] = map[string]any{"recheck": o.Recheck, "counterexamples": o.Counterexamples, "unclassified": o.Unclassified, "needs_interpretation": o.NeedsInterpretation, "premise_changed": o.PremiseChanged, "omitted": o.Omitted}
		fields := reusedMetadataFields(old)
		for _, key := range []string{"title", "summary", "applies_to", "rationale"} {
			if _, exists := e[key]; exists {
				delete(e, key)
				if !slices.Contains(fields, key) {
					fields = append(fields, key)
				}
			}
		}
		e["metadata_from"] = map[string]any{"delivery": old.Delivery, "fields": fields}
		if size(reply)+128 >= before {
			clear(e)
			maps.Copy(e, original)
			// Restore reading accounting too when the candidate is rejected.
			size(reply)
		}
	}
	reuseWorkCollectiveNotice(w, reply, size)
}

func reuseWorkCollectiveNotice(w *workState, reply map[string]any, size func(any) int) {
	old := w.Delivery.CollectiveNotice
	context, ok := reply["collective"].(notebook.CollectiveContext)
	if !ok || old == nil || old.Delivery == 0 || old.Delivery >= w.Delivery.Sequence || context.Notice != old.Text {
		return
	}
	entries, _ := reply["knowledge"].([]any)
	if len(entries) == 0 {
		return
	}
	// Reuse only when the existing article path already proved this is a broad
	// repeated read. Explicit source/edition/depth recovery always stays full.
	for _, raw := range entries {
		e, ok := raw.(map[string]any)
		if !ok || e["reused"] != true || loopPremiseChanged(e) {
			return
		}
	}
	before := size(reply)
	view := context
	view.Notice = fmt.Sprintf("Reuse collective.notice from delivery %d of this receipt. If unavailable, repeat prepare with reasoning:{depth:focused} for the full notice.", old.Delivery)
	reply["collective"] = workCollectiveDelivery{CollectiveContext: view, NoticeFrom: old.Delivery}
	if size(reply) >= before {
		reply["collective"] = context
		size(reply)
	}
}

// Call only after final sizing, immediately before the atomic context save.
// Entries omitted or replaced by reference cannot establish a new delivery.
func rememberWorkMetadata(w *workState, reply map[string]any) {
	if w.Delivery == nil {
		return
	}
	d := w.Delivery
	if d.Metadata == nil {
		d.Metadata = map[string]workMetadataRef{}
	}
	entries, _ := reply["knowledge"].([]any)
	for _, raw := range entries {
		e := raw.(map[string]any)
		if e["metadata_from"] != nil {
			continue
		}
		if signature, ok := workMetadataSignature(e); ok {
			ref := e["ref"].(string)
			if len(d.Metadata) < 64 || d.Metadata[ref].Signature != "" {
				var held map[string]string
				if o, bound := e["orientation"].(*workOrientation); bound {
					if at := o.held(); len(at) > 0 {
						held = at
					}
				}
				d.Metadata[ref] = workMetadataRef{Signature: signature, Delivery: d.Sequence, Held: held}
			}
		}
	}
	if context, ok := reply["collective"].(notebook.CollectiveContext); ok && len(context.Notice) <= 4096 && context.Notice != "" {
		d.CollectiveNotice = &workNoticeRef{Text: context.Notice, Delivery: d.Sequence}
	}
}
