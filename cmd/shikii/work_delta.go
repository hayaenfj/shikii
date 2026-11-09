package main

import (
	"fmt"
	"maps"

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
}

func workMetadataSignature(e map[string]any) (string, bool) {
	_, ok := e["orientation"].(notebook.KnowledgeOrientation)
	if !ok || e["ref"] == nil || e["id"] == nil || e["revision"] == nil {
		return "", false
	}
	return workHash([]any{e["id"], e["revision"], e["orientation"], e["title"], e["summary"], e["applies_to"], e["rationale"], e["learning_context"], e["recheck"]}), true
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
		o := e["orientation"].(notebook.KnowledgeOrientation)
		// Leave unresolved text, counterexamples, missing material and warnings
		// visible on every delivery. Only unchanged descriptive fields move.
		e["orientation"] = map[string]any{"recheck": o.Recheck, "counterexamples": o.Counterexamples, "unclassified": o.Unclassified, "needs_interpretation": o.NeedsInterpretation, "premise_changed": o.PremiseChanged, "omitted": o.Omitted}
		fields := []string{"orientation.id", "orientation.revision", "orientation.summary", "orientation.conditions", "orientation.current", "orientation.history"}
		for _, key := range []string{"title", "summary", "applies_to", "rationale"} {
			if _, exists := e[key]; exists {
				delete(e, key)
				fields = append(fields, key)
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
				d.Metadata[ref] = workMetadataRef{Signature: signature, Delivery: d.Sequence}
			}
		}
	}
	if context, ok := reply["collective"].(notebook.CollectiveContext); ok && len(context.Notice) <= 4096 && context.Notice != "" {
		d.CollectiveNotice = &workNoticeRef{Text: context.Notice, Delivery: d.Sequence}
	}
}
