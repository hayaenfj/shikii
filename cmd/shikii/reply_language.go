package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Reply language (0.11): when Claude Code's `language` is one written in its own script, the final reply the person reads is checked at
// Stop. A reply written mostly in Latin letters is stopped once (the built-in ritual check reply-language), and the agent is told to
// rewrite it in the person's language. Languages written in Latin letters (and no language) are never checked: the script cannot tell
// them from English. There is no setting: it is on exactly when such a language is set.
//
// The fixed rule:
//  1. Remove what stays in English whatever the language: fenced code blocks, inline code spans, URLs, and words that look like
//     paths or identifiers (containing / \ _ = @ # < > { } [ ] or a digit, a dot inside the word, a leading dash, or camelCase).
//  2. Count the remaining letters of the person's script (for Japanese: kanji, hiragana, katakana) and the remaining Latin letters.
//  3. With fewer than replyLangMinLetters of them together the reply is short and not checked (a one-line "Done" passes).
//  4. Otherwise the reply passes when the person's script makes at least replyLangMinShare percent of them. One kanji or kana
//     carries about as much as a few Latin letters, so a Japanese reply that keeps English terms stays far above 20%; a reply
//     written in English is near 0%.

const (
	replyLangMinLetters = 60 // letters (the person's script and Latin together) below which a reply is too short to check
	replyLangMinShare   = 20 // percent of the counted letters that must be in the person's script
)

// replyLangScripts maps the language codes of hookLang to the scripts that write them. Codes not listed are written in Latin letters.
var replyLangScripts = map[string][]*unicode.RangeTable{
	"ja":    {unicode.Han, unicode.Hiragana, unicode.Katakana},
	"zh":    {unicode.Han},
	"zh-TW": {unicode.Han},
	"ko":    {unicode.Hangul, unicode.Han},
	"ru":    {unicode.Cyrillic},
	"uk":    {unicode.Cyrillic},
}

// replyLangNames is the language's name in English, for the reason the agent reads.
var replyLangNames = map[string]string{"ja": "Japanese", "zh": "Chinese", "zh-TW": "Chinese (Traditional)", "ko": "Korean", "ru": "Russian", "uk": "Ukrainian"}

var (
	replyFence      = regexp.MustCompile("(?m)^[ \t]*(```|~~~)")
	replyInlineCode = regexp.MustCompile("`[^`\n]*`")
	replyURL        = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://\S+`)
	replyCamel      = regexp.MustCompile(`\p{Ll}\p{Lu}`)
	replyInnerDot   = regexp.MustCompile(`\.[\p{L}\p{N}]`)
)

// replyLanguageCheck applies the rule to the final reply. checked is false when the language is not one with its own script or the reply
// is short; share is the percent of counted letters in the person's script.
func replyLanguageCheck(lang, reply string) (checked, pass bool, share int) {
	scripts := replyLangScripts[lang]
	if len(scripts) == 0 {
		return false, true, 0
	}
	text := replyProse(reply)
	own, latin := 0, 0
	for _, r := range text {
		switch {
		case unicode.In(r, scripts...):
			own++
		case unicode.IsLetter(r) && unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	total := own + latin
	if total < replyLangMinLetters {
		return false, true, 0
	}
	share = own * 100 / total
	return true, share >= replyLangMinShare, share
}

// replyProse is the reply without code, URLs, paths and identifiers (step 1 of the rule).
func replyProse(reply string) string {
	var kept []string
	inFence := false
	for _, line := range strings.Split(reply, "\n") {
		if replyFence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		kept = append(kept, line)
	}
	text := strings.Join(kept, "\n")
	text = replyInlineCode.ReplaceAllString(text, " ")
	text = replyURL.ReplaceAllString(text, " ")
	// Japanese and Chinese are not written with spaces, so a word may hold prose and a path together ("guard.go" followed by kana):
	// split at whitespace and at the CJK letters and punctuation, keep those, and drop only the parts that look like code
	var b strings.Builder
	part := []rune{}
	flush := func() {
		if len(part) > 0 && !replyCodeLike(string(part)) {
			b.WriteString(string(part))
		}
		b.WriteByte(' ')
		part = part[:0]
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flush()
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef:
			flush()
			b.WriteRune(r)
		default:
			part = append(part, r)
		}
	}
	flush()
	return b.String()
}

// replyCodeLike reports whether a word (without CJK letters) looks like a path or an identifier.
func replyCodeLike(s string) bool {
	if strings.ContainsAny(s, `/\_=@#<>{}[]0123456789`) || strings.HasPrefix(s, "-") {
		return true
	}
	return replyCamel.MatchString(s) || replyInnerDot.MatchString(s)
}

// replyLanguageReason is the line of the Stop block that asks the agent to rewrite the reply in the person's language.
func replyLanguageReason(lang string, share int) string {
	name := replyLangNames[lang]
	return fmt.Sprintf("The person's language setting is %s, but your final reply is written mostly in Latin letters (%d%% of its letters, outside code, URLs, paths and identifiers, are %s; at least %d%% is expected). Write the final reply again in %s, with the same content; code, commands, paths and identifiers may stay as they are", name, share, name, replyLangMinShare, name)
}
