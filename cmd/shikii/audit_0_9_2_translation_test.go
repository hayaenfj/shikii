package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit 0.9.2 (translation): what moving the texts to English broke or left untested.

// T2: the preface Go puts in front of the pre-work organizing is the one the page removes (web/src/components/ItemCard.svelte).
func TestAuditTranslationPlanningPrefaceMatchesThePage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "components", "ItemCard.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "planningPrefixes =") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("ItemCard.svelte no longer has planningPrefixes")
	}
	if !strings.Contains(line, `"`+planningPreface+`"`) && !strings.Contains(line, `'`+planningPreface+`'`) {
		t.Fatalf("the page removes a different preface than Go writes.\nGo:   %q\npage: %s", planningPreface, line)
	}
	got, err := renderPlanningFact(&planningFact{Summary: "s", Evidence: []planningQuote{{Source: "current_user", Quote: "q"}}}, map[string]string{"current_user": "q"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, planningPreface+"s") {
		t.Fatalf("renderPlanningFact starts with planningPreface: %q", got)
	}
}

// T4: the habits notice keeps handoff_write (the MCP way to write work records); the planning notice keeps lesson:<id> (how to cite a lesson).
func TestAuditTranslationNoticesKeepTheirOperations(t *testing.T) {
	if !strings.Contains(habitsNotice, "handoff_write in shikii_wiki") || !strings.Contains(habitsNotice, "shikii handoff write <path>") {
		t.Errorf("habitsNotice lost how to write a work record: %q", habitsNotice)
	}
	if n := planningNotice("s", "t"); !strings.Contains(n, "evidence source to lesson:<id>") {
		t.Errorf("planningNotice lost how to cite a lesson: %q", n)
	}
}

// T6: Claude Code's language setting written in Japanese maps to ja.
func TestAuditTranslationLanguageNameInJapanese(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for language, want := range map[string]string{"\u65e5\u672c\u8a9e": "ja", "Japanese": "ja", " japanese ": "ja", "English": "", "": ""} {
		setHookLanguage(t, language)
		if got := hookLang(); got != want {
			t.Errorf("language %q: hookLang() = %q, want %q", language, got, want)
		}
	}
}
