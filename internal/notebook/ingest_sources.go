package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

type loadedIngestSources struct {
	pages      []Page
	refs       []Source
	raw, texts map[string][]byte
}

func (b Book) loadIngestSources(sourceDir string, d Draft) (loadedIngestSources, error) {
	recent, err := (store.Store{Dir: b.Root}).RecentFeedback(b.Scope)
	if err != nil {
		return loadedIngestSources{}, err
	}
	available := map[string]store.Feedback{}
	for _, f := range recent {
		available[f.ID] = f
	}
	pages, issues, err := b.pages()
	if err != nil || len(issues) > 0 {
		return loadedIngestSources{}, errors.New("wiki has unreadable or changed pages; inspect wiki lint before ingesting")
	}
	cited := map[string]string{} // a source a page already cites is checked by that citation's hash (it may have been captured under the previous path)
	for _, p := range pages {
		for _, ref := range p.Sources {
			cited[ref.ID] = ref.Hash
		}
	}
	refs := []Source{}
	raw := map[string][]byte{}
	texts := map[string][]byte{}
	for _, id := range d.Sources {
		data, err := readSourceRecord(filepath.Join(sourceDir, id+".json"))
		if os.IsNotExist(err) {
			f, ok := available[id]
			if !ok {
				return loadedIngestSources{}, fmt.Errorf("wiki source %s does not belong to this project's observations or recent conversations (send the other ids again without it)", id)
			}
			data, err = json.Marshal(f)
		}
		if err != nil {
			return loadedIngestSources{}, err
		}
		if !validSource(data, id) {
			return loadedIngestSources{}, errors.New("invalid archived wiki source")
		}
		var observation Observation
		if json.Unmarshal(data, &observation) == nil && observation.Kind != "" {
			if _, err := b.readObservation(sourceDir, id, cited[id]); err != nil {
				return loadedIngestSources{}, err
			}
		}
		raw[id] = data
		ref := Source{ID: id, Hash: digest(data)}
		text, err := readOriginals(filepath.Join(sourceDir, id+".text.json"))
		if os.IsNotExist(err) {
			var f store.Feedback
			_ = json.Unmarshal(data, &f)
			if originals := b.originals(f); len(originals) > 0 {
				text, _ = json.Marshal(originals)
			}
		} else if err != nil {
			return loadedIngestSources{}, err
		}
		if len(text) > 0 {
			texts[id] = text
			ref.TextHash = digest(text)
		}
		refs = append(refs, ref)
	}
	if d.Correction != nil {
		// A correction quotes the person's own next message, which only the hook records with a conversation. A captured source
		// (even kind "human") is written by an agent, so it cannot carry a correction: the refusal says so and what to do instead.
		id := d.Correction.SourceID
		var observed store.Feedback
		var captured Observation
		data, ok := raw[id]
		switch {
		case !ok:
			return loadedIngestSources{}, fmt.Errorf("correction.source_id %s must also be one of sources (cite the conversation the quote comes from)", id)
		case json.Unmarshal(data, &captured) == nil && captured.Kind != "":
			return loadedIngestSources{}, fmt.Errorf("correction.source_id %s is a captured source, not a conversation recorded by the hook; a correction must quote the person's message from a recorded conversation. Without hooks there is none: save the lesson without correction and cite the source in sources", id)
		case json.Unmarshal(data, &observed) != nil || observed.Reply == "":
			return loadedIngestSources{}, fmt.Errorf("conversation %s has no recorded reply from the person yet; a correction must quote the person's message. Save the lesson without correction, or add the correction after the person replies", id)
		case !strings.Contains(observed.Reply, d.Correction.Quote):
			return loadedIngestSources{}, fmt.Errorf("the correction quote must be copied verbatim from the person's reply (the reply field) in conversation %s", id)
		}
	}
	return loadedIngestSources{pages: pages, refs: refs, raw: raw, texts: texts}, nil
}
