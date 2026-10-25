package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

// 0.9.2 redesign: the page asks a person each ready-made question once, in plain words, in Japanese and in English.
// Every shipped spec has a display for ja and en with a title, a question that is one sentence (the question mark comes before any end of a sentence,
// so there is no preface such as "from your answer the AI learns ..." in front of it), and a name for every answer.
// Without an English display the English page fell back to the prompt for the decision model (audit E1).
func TestShippedSpecsAskPeopleInOneSentenceInBothLanguages(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if len(files) != len(hookSpecs)+len(wikiSpecs) {
		t.Fatalf("expected every shipped spec, got %v", files)
	}
	marks := map[string][2]string{"ja": {"？", "。"}, "en": {"?", ". "}}
	for _, f := range files {
		s, err := spec.Load(f)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{"yes", "no"}
		if s.Type != spec.Noul {
			ids = nil
			for _, o := range s.Options {
				ids = append(ids, o.ID)
			}
		}
		for lang, m := range marks {
			d, ok := s.Display[lang]
			if !ok || d.Title == "" || d.Question == "" {
				t.Errorf("%s: display.%s needs a title and a question", s.ID, lang)
				continue
			}
			q := strings.TrimSpace(d.Question)
			i := strings.Index(q, m[0])
			if i < 0 || strings.Contains(q[:i], m[1]) {
				t.Errorf("%s: display.%s.question must be one question sentence with nothing before it: %q", s.ID, lang, q)
			}
			for _, id := range ids {
				if d.Options[id] == "" {
					t.Errorf("%s: display.%s.options has no name for %q", s.ID, lang, id)
				}
			}
		}
	}
}
