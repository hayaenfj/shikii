package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// hookEndpoints are where the hook sends. The standard is TypeSafe's Jev (when there is a key), then the local model. The first one that can send, in the spec's send_to order, is used.
// Even with a key, SHIKII_MODEL=local means local only (modelChoice).
func hookEndpoints() []decide.Endpoint {
	var eps []decide.Endpoint
	if key := hostedKey(); key != "" && modelChoice() != "local" {
		eps = append(eps, decide.Endpoint{Kind: "hosted", Client: systemone.New(hookHostedURL, key, "jev-latest")})
	}
	if modelChoice() != "hosted" {
		eps = append(eps, decide.Endpoint{Kind: "local", Client: systemone.New(hookLocalURL, "", "jev-latest")})
	}
	return eps
}

// usesHosted reports whether the hook uses Jev (a key is set and SHIKII_MODEL is not local).
func usesHosted() bool {
	return hostedKey() != "" && modelChoice() != "local"
}

// hookDecide decides with the project's spec, maps the band to behaviour and records it. Without a line the band never becomes act (= the shadow period).
// Human notices and record explanations may include the configured language; agent instructions remain English.
func hookDecide(agent, specID string, inputs map[string]string, project string, originals ...map[string]string) (*decide.Decision, hookOut, error) {
	return hookDecideWithCommit(agent, specID, inputs, project, nil, originals...)
}

func hookDecideWithCommit(agent, specID string, inputs map[string]string, project string, commit workCommitter, originals ...map[string]string) (*decide.Decision, hookOut, error) {
	if commit == nil {
		commit = func(action func() error) error { return action() }
	}
	sp, err := spec.Load(hookSpecPath(agent, specID))
	if err != nil {
		return nil, hookOut{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookDecideTimeout)
	defer cancel()
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{}, hookEndpoints())
	if err != nil {
		return nil, hookOut{}, err
	}
	st := store.Store{Dir: agent}
	attachReceipt(sp, st, d)
	l := localize(sp, d, hookLang())
	out := hookOutcome(specID, d, l)
	if d.Band == decide.Discard && sp.Type == spec.Noul && !discardTrusted(sp, d) {
		out = hookOut{} // the confident "no" was not right often enough when the line was drawn (0.10)
	}
	page := pageRef(feedbackScope(filepath.Dir(agent))) // absolute: MCP started with --store .shikii would look up and start a second page under "."
	out.Context = strings.ReplaceAll(out.Context, hookPageURL+"/", page)
	out.System = strings.ReplaceAll(out.System, hookPageURL+"/", page)
	out.RecordMessage = strings.ReplaceAll(out.RecordMessage, hookPageURL+"/", page)
	if d.Band != decide.Abstain {
		rec := recordOf(sp, d, inputs)
		rec.ReviewPending = store.ReviewSpec(specID)
		rec.Project = project                                                         // which work the decision belongs to (a marker to separate on the page and in status. Not an input, so the hash does not change)
		if l.Lang != "" && (l.Question != "" || l.Top != "" || out.message() != "") { // no display with only the language (nothing to read)
			rec.Display = &store.RecordDisplay{Lang: l.Lang, Question: l.Question, Top: l.Top, Message: out.message()}
		}
		if e := commit(func() error {
			if e := recordDecisionRec(st, sp, d, rec); e != nil {
				return e
			}
			for _, text := range originals {
				if e := st.SaveRecordText(rec, text); e != nil {
					return e
				}
			}
			return nil
		}); e != nil {
			return nil, hookOut{}, e
		}
	}
	return d, out, nil
}

// discardTrusted reports whether the line the decision used put real "no" answers below ask at least as often as act asks of the
// confident side (measured when the line was drawn; PrecisionAtDiscard).
func discardTrusted(sp *spec.Spec, d *decide.Decision) bool {
	rs := sp
	if d.Rung == "context" && sp.ContextSpec() != nil {
		rs = sp.ContextSpec()
	}
	c := rs.CalibrationFor(d.Model)
	return c != nil && c.PrecisionAtDiscard >= sp.Thresholds.Act
}

// hookDecideAll asks several specs with the same input in parallel (waiting for each in turn inside one hook gets close to the timeout).
// Returned in specIDs order. A spec not asked (skipped) is nil.
func hookDecideAll(agent string, specIDs []string, inputs map[string]string, project string, skip func(id string) bool, originals ...map[string]string) ([]*decide.Decision, []hookOut, error) {
	return hookDecideAllWithCommit(agent, specIDs, inputs, project, skip, nil, originals...)
}

func hookDecideAllWithCommit(agent string, specIDs []string, inputs map[string]string, project string, skip func(id string) bool, commit workCommitter, originals ...map[string]string) ([]*decide.Decision, []hookOut, error) {
	ds := make([]*decide.Decision, len(specIDs))
	outs := make([]hookOut, len(specIDs))
	errs := make([]error, len(specIDs))
	var wg sync.WaitGroup
	for i, id := range specIDs {
		if (skip != nil && skip(id)) || hookSkipsStopped(agent, id, inputs) {
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			ds[i], outs[i], errs[i] = hookDecideWithCommit(agent, id, inputs, project, commit, originals...)
			if ds[i] != nil {
				resumeOnNewModel(agent, id, ds[i].Model)
			}
		}(i, id)
	}
	wg.Wait()
	var firstErr error
	for _, e := range errs {
		if e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return ds, outs, firstErr
}

// langCodes maps Claude Code's `language` setting → a language code (the README's 15 languages). English and unknown are "" (nothing alongside).
var langCodes = map[string]string{
	"japanese": "ja", "日本語": "ja", "chinese": "zh", "simplified chinese": "zh", "简体中文": "zh", "traditional chinese": "zh-TW", "繁體中文": "zh-TW",
	"korean": "ko", "한국어": "ko", "german": "de", "deutsch": "de", "spanish": "es", "español": "es", "french": "fr", "français": "fr",
	"italian": "it", "italiano": "it", "portuguese": "pt-BR", "português": "pt-BR", "russian": "ru", "русский": "ru", "ukrainian": "uk", "українська": "uk",
	"polish": "pl", "polski": "pl", "turkish": "tr", "türkçe": "tr", "vietnamese": "vi", "tiếng việt": "vi", "indonesian": "id", "bahasa indonesia": "id",
}
