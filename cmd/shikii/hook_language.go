package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
)

// hookLang turns ~/.claude/settings.json's language into a language code. English, missing or unknown: "".
func hookLang() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Language string `json:"language"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return langCodes[strings.ToLower(strings.TrimSpace(cfg.Language))]
}

// hookLoc is the wording in the configured language: the language code, and the question and answer from the spec's display.<lang> (empty if none). Lang "" means English only.
// Name / EnName are the question's title on the page (questionTitle) and EnTop the answer in English, for the notices the person reads (never the spec id).
type hookLoc struct {
	Lang, Question, Top string
	Name, EnName, EnTop string
}

func localize(sp *spec.Spec, d *decide.Decision, lang string) hookLoc {
	l := hookLoc{Lang: lang}
	if sp == nil || d == nil {
		return l
	}
	id := d.Top
	if sp.Type == spec.Noul && id == "yes" && d.P < 0.5 { // a noul decision always has Top yes with P = p(yes): in the person's words p<0.5 is the "no" side
		id = "no"
	}
	l.Name, l.EnName = questionTitle(sp, "ja"), questionTitle(sp, "en")
	if l.EnTop = strings.TrimSpace(sp.Display["en"].Options[id]); l.EnTop == "" {
		l.EnTop = id
	}
	if lang == "" {
		return l
	}
	if disp, ok := sp.Display[lang]; ok {
		l.Question = strings.TrimSpace(disp.Question)
		l.Top = disp.Options[id]
	}
	if l.Top == "" && lang == "ja" {
		l.Top = map[string]string{"yes": "はい", "no": "いいえ"}[id]
	}
	return l
}

// topName is the answer in the person's words (the id if none).
func (l hookLoc) topName(id string) string {
	if l.Top != "" {
		return l.Top
	}
	return id
}

// bi shows text in the configured language alongside the English text (only ja has text templates; other languages stay English).
func bi(en, ja string, l hookLoc) string {
	if l.Lang != "ja" || ja == "" {
		return en
	}
	return en + "\n" + ja
}

func answerURL(specID, trace string) string {
	return fmt.Sprintf("%s/?spec=%s&trace=%s", hookPageURL, specID, trace)
}

// hookOutcome is a pure function mapping the band to behaviour. Without a line (shadow) it outputs nothing. No permission decision is output (no tool is stopped).
// The question right after the request (needs-plan) becomes context for the agent; the end-of-turn questions (changes-in-scope / done-as-asked) become text for the person in the terminal
// (returning context at Stop keeps the conversation going. Whether to go on is the person's call).
func hookOutcome(specID string, d *decide.Decision, l hookLoc) hookOut {
	if d == nil || !d.IsCalibrated || d.Band == decide.Abstain || !agentReviewOnly(specID) {
		return hookOut{}
	}
	if d.Band == decide.Ask || d.Band == decide.Discard && specID != "needs-plan" || specID == "needs-plan" && d.Band == decide.Act {
		return hookOut{Context: fmt.Sprintf("shikii: internal review %s is %s (p=%.2f). Inspect the concrete task and evidence; correct any actual omission yourself. Ask the person only a specific task choice that requires their input. Do not request a score for this review.", specID, d.Band, d.P)}
	}
	return hookOut{}
}
