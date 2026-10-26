package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
)

// pageGlossary reads the old words of the page's glossary (web/scripts/test-glossary.mjs, the first item of each row) for one language,
// so the notices the person reads in the terminal are held to the same words as the page.
func pageGlossary(t *testing.T, lang string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "scripts", "test-glossary.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "\n  "+lang+": [\n")
	if start < 0 {
		t.Fatalf("the glossary has no %s rows", lang)
	}
	end := strings.Index(src[start:], "\n  ],")
	if end < 0 {
		t.Fatalf("the %s rows of the glossary do not end", lang)
	}
	var words []string
	for _, m := range regexp.MustCompile(`(?m)^\s*\['([^']+)',`).FindAllStringSubmatch(src[start:start+end], -1) {
		words = append(words, m[1])
	}
	if len(words) < 10 {
		t.Fatalf("the %s glossary was not read: %q", lang, words)
	}
	return words
}

var noticeURL = regexp.MustCompile(`https?://\S+`)

// noticeProblems lists what a notice to the person must not hold: a spec id, a raw probability (p=, p(yes)=), and the glossary's old words
// (Japanese also: act). The page link is removed first (its query names the spec, which the person does not read).
func noticeProblems(t *testing.T, text string, glossary map[string][]string) []string {
	t.Helper()
	en, ja, _ := strings.Cut(text, "\n")
	var found []string
	for lang, line := range map[string]string{"en": en, "ja": ja} {
		line = noticeURL.ReplaceAllString(line, "")
		if line == "" {
			continue
		}
		bad := append(append([]string{"p=", "p(", "p(はい)"}, hookSpecs...), wikiSpecs...)
		bad = append(bad, glossary[lang]...)
		if lang == "ja" {
			bad = append(bad, "act")
		}
		for _, w := range bad {
			if strings.Contains(strings.ToLower(line), strings.ToLower(w)) {
				found = append(found, lang+": "+w+" in "+line)
			}
		}
	}
	return found
}

// Audit 0.9.3 low 4: the end-of-turn and unsure notices of hookOutcome and the waiting-answers line are read by the person in the terminal.
// They name the question by its title on the page and use the page's plain words; the probability is told as the AI's confidence, never as p=.
func TestHookPersonNoticesUsePlainWordsAndTitle(t *testing.T) {
	glossary := map[string][]string{"ja": pageGlossary(t, "ja"), "en": pageGlossary(t, "en")}
	load := func(id string) *spec.Spec {
		sp, err := spec.Load(filepath.Join("..", "..", "integrations", "claude-code", "specs", id+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	receipt := &decide.Receipt{N: 61, NHuman: 40, HumanCheckedAct: 12, HumanPrecision: 0.96}
	cases := []struct {
		spec string
		band decide.Band
		p    float64
	}{
		{"needs-plan", decide.Ask, 0.53},
		{"changes-in-scope", decide.Discard, 0.10},
		{"done-as-asked", decide.Discard, 0.10},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			sp := load(c.spec)
			d := &decide.Decision{TraceID: "0123456789abcdef", Band: c.band, Top: "yes", P: c.p, IsCalibrated: true, Receipt: receipt}
			o := hookOutcome(c.spec, d, localize(sp, d, "ja"))
			if o.System == "" || o.Context != "" {
				t.Fatalf("a notice for the person only: %+v", o)
			}
			if bad := noticeProblems(t, o.System, glossary); len(bad) > 0 {
				t.Fatalf("the notice holds inner words:\n%s\n%s", strings.Join(bad, "\n"), o.System)
			}
			en, ja, _ := strings.Cut(o.System, "\n")
			if !strings.Contains(en, `"`+sp.Display["en"].Title+`"`) || !strings.Contains(ja, "「"+sp.Display["ja"].Title+"」") {
				t.Fatalf("the notice names the question by its title:\n%s", o.System)
			}
			if c.band == decide.Discard && (!strings.Contains(en, "90%") || !strings.Contains(ja, "90%")) {
				t.Fatalf("the confidence is said plainly (90%% sure of the no side):\n%s", o.System)
			}
		})
	}
	t.Run("waiting answers", func(t *testing.T) {
		msg := waitingNotice(3, "http://127.0.0.1:1/", hookLoc{Lang: "ja"})
		if bad := noticeProblems(t, msg, glossary); len(bad) > 0 || !strings.Contains(msg, "3") {
			t.Fatalf("the waiting-answers line holds inner words:\n%s\n%s", strings.Join(bad, "\n"), msg)
		}
	})
	// the check itself catches the old text (the notices before 0.9.3's fix)
	old := "shikii: this turn's changes look outside what you asked for (changes-in-scope p(yes)=0.10; line on 61 truths)\nshikii: このターンで変えたものは頼まれた範囲の外に見える(changes-in-scope p(はい)=0.10。線は正解 61 件)"
	if len(noticeProblems(t, old, glossary)) < 6 {
		t.Fatalf("the check misses the old notice's words: %q", noticeProblems(t, old, glossary))
	}
}
