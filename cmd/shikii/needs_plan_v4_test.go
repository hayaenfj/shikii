package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
)

// The needs-plan question has three answers. The third one is the Japanese screen text for not_applicable, written with \u escapes below.
func TestNeedsPlanIsAThreeAnswerChoice(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	sp, err := spec.Load(filepath.Join(dir, "needs-plan.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Type != spec.Choice {
		t.Fatalf("needs-plan type = %v, want choice", sp.Type)
	}
	var ids []string
	for _, o := range sp.Options {
		ids = append(ids, o.ID)
	}
	if want := []string{"yes", "no", "not_applicable"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("options = %v, want %v", ids, want)
	}
	for _, lang := range []string{"ja", "en"} {
		for _, id := range []string{"yes", "no", "not_applicable"} {
			if sp.Display[lang].Options[id] == "" {
				t.Fatalf("display %s lacks the answer %s", lang, id)
			}
		}
	}
	if got := localize(sp, &decide.Decision{Top: "not_applicable"}, "ja").Top; got != "\u554f\u3044\u306b\u5f53\u305f\u3089\u306a\u3044" {
		t.Fatalf("the third answer on the screen = %q, want the Japanese text for not_applicable", got)
	}
}
