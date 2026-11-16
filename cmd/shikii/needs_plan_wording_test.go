package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

// The question the person answers about a request asks what the person wanted ("did you want to be asked first"),
// not whether the AI should have judged differently: the answer is the person's own feeling, so the question must say so.
func TestNeedsPlanAsksWhatThePersonWantedAboutTheRequest(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	s, err := spec.Load(filepath.Join(dir, "needs-plan.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ja": "\u78ba\u8a8d\u3092\u53d7\u3051\u305f\u304b\u3063\u305f", "en": "did you want the AI to check with you"}
	for lang, phrase := range want {
		if q := s.Display[lang].Question; !strings.Contains(q, phrase) {
			t.Errorf("display.%s.question = %q, want it to ask what the person wanted (%q)", lang, q, phrase)
		}
	}
}
