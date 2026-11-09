package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/spec"
)

// loadSpecs makes the folder's *.yaml lookupable by id. Read every time (the files are small).
func loadSpecs(dir string) (map[string]*spec.Spec, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no spec (*.yaml) in %s", dir)
	}
	out := map[string]*spec.Spec{}
	seen := map[string]map[int]string{} // id → version → path
	for _, f := range files {
		sp, err := spec.Load(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		// For the same id, the larger version wins (the practice of keeping wording v1 → v2 under the same id). Two with the same id and the same version cannot be decided, so that is an error
		if prev, ok := seen[sp.ID][sp.Version]; ok {
			return nil, fmt.Errorf("duplicate spec id %q with the same version %d: %s and %s", sp.ID, sp.Version, prev, f)
		}
		if seen[sp.ID] == nil {
			seen[sp.ID] = map[int]string{}
		}
		seen[sp.ID][sp.Version] = f
		if cur, ok := out[sp.ID]; !ok || sp.Version > cur.Version {
			out[sp.ID] = sp
		}
	}
	return out, nil
}

func (s *server) spec(id string) (*spec.Spec, error) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		return nil, err
	}
	sp, ok := specs[id]
	if !ok {
		return nil, errNotFound
	}
	return sp, nil
}

var errNotFound = errors.New("not found")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *server) handleSpecs(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := []map[string]any{}
	for _, sp := range specs {
		out = append(out, specSummary(sp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	writeJSON(w, 200, map[string]any{"specs": out})
}

func specSummary(sp *spec.Spec) map[string]any {
	titles, questions := map[string]string{}, map[string]string{}
	for lang, d := range sp.Display {
		if d.Title != "" {
			titles[lang] = d.Title
		}
		if d.Question != "" {
			questions[lang] = d.Question
		}
	}
	return map[string]any{"id": sp.ID, "type": string(sp.Type), "path": sp.Path(), "hash": sp.Hash(), "question": sp.Question,
		"titles": titles, "questions": questions,
		"min_n": sp.Eval.MinN, "act": sp.Thresholds.Act, "ask": sp.Thresholds.Ask, "choices": truthChoices(sp)}
}

// truthChoices are the truth candidates that become buttons on the page. For choice, choice ids; for noul, yes/no; for score, "0".."n-1".
func truthChoices(sp *spec.Spec) []map[string]any {
	// labels are display names per language (from display). For a missing language the page falls back to label (English/id)
	labelsFor := func(id string, level int) map[string]string {
		out := map[string]string{}
		for lang, d := range sp.Display {
			if level >= 0 {
				if level < len(d.Levels) {
					out[lang] = fmt.Sprintf("%d: %s", level, d.Levels[level])
				}
			} else if v, ok := d.Options[id]; ok {
				out[lang] = v
			}
		}
		return out
	}
	var out []map[string]any
	switch sp.Type {
	case spec.Noul:
		out = append(out, map[string]any{"truth": "yes", "label": "yes", "labels": labelsFor("yes", -1)},
			map[string]any{"truth": "no", "label": "no", "labels": labelsFor("no", -1)})
	case spec.Choice:
		for _, o := range sp.Options {
			out = append(out, map[string]any{"truth": o.ID, "label": o.ID, "description": o.Description, "labels": labelsFor(o.ID, -1)})
		}
	case spec.Score:
		for i, lv := range sp.Levels {
			out = append(out, map[string]any{"truth": fmt.Sprint(i), "label": fmt.Sprintf("%d: %s", i, lv), "labels": labelsFor("", i)})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

// maxBody is the limit for POST bodies. Smaller than the store's per-line limit (4 MiB).
const maxBody = 1 << 20

// acceptJSONPost rejects writes from other sites in a browser (audit S3).
//   - If there is an Origin, only this endpoint's own (127.0.0.1 / localhost) passes
//   - Content-Type must be application/json (a text/plain "simple request" arrives without a preflight)
//   - Body up to maxBody (413 beyond that)
func acceptJSONPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeErr(w, 405, errors.New("POST only"))
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "[::1]" && u.Hostname() != "::1") {
			writeErr(w, 403, fmt.Errorf("cross-origin request from %q is not allowed", origin))
			return false
		}
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeErr(w, 415, errors.New("Content-Type must be application/json"))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	return true
}
