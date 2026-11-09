package notebook

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/hayaenfj/shikii/internal/store"
)

// Decoded sources belong to one sequential read snapshot. File bytes and
// source identity are still checked by the caller; no entry crosses requests.
type sourceDecodeView struct {
	entries map[string]decodedSource
	bytes   int
}

type decodedSource struct {
	id                   string
	raw                  []byte
	feedback             store.Feedback
	observation          Observation
	observationDecoded   bool
	valid                bool
	canonicalObservation []byte
	cost                 int
}

func decodeSource(data []byte, id string) decodedSource {
	v := decodedSource{id: id, raw: data}
	feedbackErr := json.Unmarshal(data, &v.feedback)
	f := v.feedback
	v.valid = feedbackErr == nil && f.ID == id && f.Request != "" && f.Summary != "" && f.Reply != ""
	if !v.valid {
		v.decodeObservation(id)
		v.valid = v.observation.ID == id && v.observationDecoded && v.observation.valid()
	}
	return v
}

func (v *decodedSource) decodeObservation(id string) {
	if v.observationDecoded {
		return
	}
	v.observationDecoded = true
	if json.Unmarshal(v.raw, &v.observation) != nil || v.observation.ID != id || !v.observation.valid() {
		v.observation = Observation{}
		return
	}
	o := v.observation
	o.ID = ""
	v.canonicalObservation, _ = json.Marshal(o)
}

func (b Book) decodedSource(path, id string, data []byte) decodedSource {
	var cache *sourceDecodeView
	if b.snapshot != nil {
		if b.snapshot.sourceDecodes == nil {
			b.snapshot.sourceDecodes = &sourceDecodeView{entries: map[string]decodedSource{}}
		}
		cache = b.snapshot.sourceDecodes
		if old, ok := cache.entries[path]; ok && old.id == id && bytes.Equal(old.raw, data) {
			old.feedback.Targets = slices.Clone(old.feedback.Targets)
			return old
		}
	}
	value := decodeSource(data, id)
	if cache != nil && value.valid {
		// Include both decoded text and canonical bytes conservatively. Large
		// sources remain usable without retaining their parsed representation.
		value.cost = 2*len(data) + len(value.canonicalObservation) + len(value.feedback.Targets)*64 + len(path) + 512
		if value.cost <= readViewLimit {
			previous := cache.entries[path]
			if cache.bytes-previous.cost+value.cost > readViewLimit {
				cache.entries = map[string]decodedSource{}
				cache.bytes = 0
				previous.cost = 0
			}
			cache.entries[path] = value
			cache.bytes += value.cost - previous.cost
		}
	}
	value.feedback.Targets = slices.Clone(value.feedback.Targets)
	return value
}
