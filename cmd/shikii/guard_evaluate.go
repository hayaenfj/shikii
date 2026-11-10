package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func (g guard) validate() error {
	if !guardIDRe.MatchString(g.ID) {
		return errors.New("a guard id is lowercase letters, digits and hyphens, up to 40 characters")
	}
	if strings.TrimSpace(g.Title) == "" || len(g.Title) > 200 {
		return errors.New("a guard needs a title (up to 200 bytes)")
	}
	switch g.Kind {
	case guardKindPairedFiles:
		if len(g.Pairs) == 0 {
			return errors.New("paired-files needs pairs ([[a, b], ...])")
		}
		for _, p := range g.Pairs {
			if len(p) != 2 || strings.TrimSpace(p[0]) == "" || strings.TrimSpace(p[1]) == "" || p[0] == p[1] {
				return errors.New("each pair in pairs is two different paths (glob allowed)")
			}
		}
	case guardKindForbiddenWords:
		if len(g.Words) == 0 {
			return errors.New("forbidden-words needs words")
		}
		for _, w := range g.Words {
			if strings.TrimSpace(w) == "" {
				return errors.New("words cannot contain an empty word")
			}
		}
		for _, a := range g.Allow {
			if err := validateAllowedWord(a, g.Words); err != nil {
				return err
			}
		}
	case guardKindCommitBody:
		if len(g.Required) == 0 {
			return errors.New("commit-body needs required (words the body must contain)")
		}
		for _, w := range g.Required {
			if strings.TrimSpace(w) == "" {
				return errors.New("required cannot contain an empty word")
			}
		}
	case guardKindForbiddenCommands:
		if len(g.Patterns) == 0 {
			return errors.New("forbidden-commands needs patterns (regular expressions no command may match)")
		}
		for _, pat := range g.Patterns {
			re, err := regexp.Compile(pat)
			if err != nil {
				return fmt.Errorf("forbidden-commands: the pattern %q does not compile: %v", pat, err)
			}
			if re.MatchString(guardPositiveCommand) {
				return fmt.Errorf("forbidden-commands: the pattern %q matches the harmless command %q, so it would stop everything", pat, guardPositiveCommand)
			}
		}
		if strings.TrimSpace(g.Example) == "" {
			return errors.New("forbidden-commands needs example (a command the check must stop)")
		}
		if r := evalGuard(g, guardInput{Commands: []string{g.Example}, CommandsKnown: true}); r.Pass {
			return errors.New("forbidden-commands: no pattern matches example, so the check would not stop it")
		}
	default:
		return fmt.Errorf("a guard's kind is one of %s", guardKinds)
	}
	return nil
}

// guardPositiveCommand is the harmless command the positive example of forbidden-commands runs: no pattern may match it.
const guardPositiveCommand = "true"

// matchPath is a glob or exact match, against the whole path or just the name. A trailing /** matches everything below.
func matchPath(pattern, p string) bool {
	if pattern == p {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		return strings.HasPrefix(p, strings.TrimSuffix(pattern, "**"))
	}
	if ok, _ := filepath.Match(pattern, p); ok {
		return true
	}
	ok, _ := filepath.Match(pattern, filepath.Base(p))
	return ok
}

func anyMatch(pattern string, paths []string) bool {
	for _, p := range paths {
		if matchPath(pattern, p) {
			return true
		}
	}
	return false
}

// validateAllowedWord checks one allow word: only ordinary words that contain a forbidden word (根拠 for 根). The forbidden word itself, or the forbidden word
// with only particles, spaces or punctuation added (根を, 「根」), would be a loophole, so they are refused (audit R3). What remains after removing the forbidden word must contain a non-kana letter (kanji, katakana, Latin).
func validateAllowedWord(a string, words []string) error {
	if strings.TrimSpace(a) != a || a == "" {
		return errors.New("an allow word cannot contain spaces: " + a)
	}
	for _, r := range a {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return errors.New("an allow word cannot contain symbols or punctuation: " + a)
		}
	}
	for _, w := range words {
		if a == w {
			return errors.New("allow cannot contain the forbidden word itself: " + a)
		}
	}
	ok := false
	for _, w := range words {
		if !strings.Contains(a, w) {
			continue
		}
		rest := strings.ReplaceAll(a, w, "")
		for _, r := range rest {
			if unicode.IsLetter(r) && !unicode.In(r, unicode.Hiragana) {
				ok = true
			}
		}
	}
	if !ok {
		return errors.New("allow takes only ordinary words that contain a forbidden word (e.g. 根拠 for 根; a word with only a particle added is not allowed): " + a)
	}
	return nil
}

// evalGuard runs one check against what was observed in the turn. Unknown observations (the file list or commits could not be read) pass: it stops only when certain.
// commit-body with a rewritten start (CommitsUnreadable) also stops nothing, but comes back Unverified with the reason, so it is not recorded as passed.
func evalGuard(g guard, in guardInput) guardResult {
	r := guardResult{ID: g.ID, Pass: true}
	switch g.Kind {
	case guardKindPairedFiles:
		if !in.Known {
			return r
		}
		var missing, missingJA []string
		for _, p := range g.Pairs {
			a, b := anyMatch(p[0], in.Changed), anyMatch(p[1], in.Changed)
			switch {
			case a && !b:
				missing = append(missing, p[0]+" changed but "+p[1]+" did not")
				missingJA = append(missingJA, p[0]+" が変わったのに "+p[1]+" が変わっていない")
			case b && !a:
				missing = append(missing, p[1]+" changed but "+p[0]+" did not")
				missingJA = append(missingJA, p[1]+" が変わったのに "+p[0]+" が変わっていない")
			}
		}
		if len(missing) > 0 {
			r.Pass, r.Reason, r.ReasonJA = false, strings.Join(missing, "; "), strings.Join(missingJA, "; ")
		}
	case guardKindForbiddenWords:
		var hits, hitsJA []string
		paths := make([]string, 0, len(in.Added))
		for p := range in.Added {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			skip := false
			for _, e := range g.Except {
				if matchPath(e, p) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			for _, line := range in.Added[p] {
				for _, a := range g.Allow { // remove the allowed words (such as 根拠) first
					line = strings.ReplaceAll(line, a, "")
				}
				for _, w := range g.Words {
					if strings.Contains(line, w) {
						hits = append(hits, p+": \""+w+"\"")
						hitsJA = append(hitsJA, p+": 「"+w+"」")
					}
				}
			}
		}
		if len(hits) > 0 {
			if len(hits) > 5 {
				hits = append(hits[:5], fmt.Sprintf("... and %d more", len(hits)-5))
				hitsJA = append(hitsJA[:5], fmt.Sprintf("… ほか %d 件", len(hitsJA)-5))
			}
			r.Pass, r.Reason, r.ReasonJA = false, "added lines contain a word not to use: "+strings.Join(hits, ", "), "足した行に使わない語がある: "+strings.Join(hitsJA, ", ")
		}
	case guardKindCommitBody:
		if !in.CommitsKnown {
			if in.CommitsUnreadable { // not a pass: the record says the check could not be verified (it still stops nothing)
				r.Unverified = true
				r.Reason = "this turn's commits could not be read, so their bodies were not checked: the HEAD kept at the request is no longer an ancestor of the current HEAD (a commit was amended or rebased, or another branch was checked out)"
				r.ReasonJA = "\u3053\u306e\u30bf\u30fc\u30f3\u306e\u30b3\u30df\u30c3\u30c8\u3092\u8aad\u3081\u305a\u3001\u672c\u6587\u3092\u78ba\u304b\u3081\u3089\u308c\u306a\u304b\u3063\u305f: \u4f9d\u983c\u306e\u6642\u306e HEAD \u304c\u4eca\u306e HEAD \u306e\u7956\u5148\u3067\u306a\u3044(\u30b3\u30df\u30c3\u30c8\u306e amend\u30fbrebase \u304b\u3001\u5225\u306e\u30d6\u30e9\u30f3\u30c1\u3078\u306e\u5207\u308a\u66ff\u3048)"
			}
			return r
		}
		var bad, badJA []string
		for _, m := range in.Commits {
			subject, body, _ := strings.Cut(strings.TrimSpace(m), "\n")
			subjectJA := subject
			if strings.TrimSpace(m) == "" {
				subject, subjectJA = "(no subject and no body)", "(件名も本文も無い)"
			}
			var lack, lackJA []string
			for _, w := range g.Required {
				if !strings.Contains(body, w) {
					lack = append(lack, "\""+w+"\"")
					lackJA = append(lackJA, "「"+w+"」")
				}
			}
			if len(lack) > 0 {
				bad = append(bad, "the body of commit \""+cut(subject, 60)+"\" lacks "+strings.Join(lack, ", "))
				badJA = append(badJA, "コミット「"+cut(subjectJA, 60)+"」の本文に "+strings.Join(lackJA, "・")+" が無い")
			}
		}
		if len(bad) > 0 {
			r.Pass, r.Reason, r.ReasonJA = false, strings.Join(bad, "; "), strings.Join(badJA, "; ")
		}
	case guardKindForbiddenCommands:
		if !in.CommandsKnown {
			return r
		}
		var hits, hitsJA []string
		for _, c := range in.Commands {
			for _, pat := range g.Patterns {
				re, err := regexp.Compile(pat)
				if err != nil {
					continue // validate refuses a pattern that does not compile; a file edited by hand is not a reason to stop everything
				}
				if m := re.FindString(c); m != "" {
					hits = append(hits, "\""+cut(m, 80)+"\" matches "+pat)
					hitsJA = append(hitsJA, "\u300c"+cut(m, 80)+"\u300d\u304c "+pat+" \u306b\u5f53\u305f\u308b")
				}
			}
		}
		if len(hits) > 0 {
			r.Pass, r.Reason, r.ReasonJA = false, "the command about to run matches a pattern not to run: "+strings.Join(hits, ", "), "\u6d41\u305d\u3046\u3068\u3057\u3066\u3044\u308b\u30b3\u30de\u30f3\u30c9\u304c\u3001\u6d41\u3055\u306a\u3044\u5f62\u306b\u5f53\u305f\u308b: "+strings.Join(hitsJA, ", ")
		}
	}
	return r
}

// guardNegative returns the negative example the check must stop (used for enable's RED) and its description. guardPositive is the positive example that must pass.
func guardNegative(g guard) (guardInput, string) {
	switch g.Kind {
	case guardKindPairedFiles:
		return guardInput{Changed: []string{g.Pairs[0][0]}, Known: true}, "a turn where only " + g.Pairs[0][0] + " changed"
	case guardKindForbiddenWords:
		path := negativePathFor(g)
		return guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {"this line contains " + g.Words[0] + " here"}}}, "a turn that added a line containing \"" + g.Words[0] + "\""
	case guardKindCommitBody:
		return guardInput{Commits: []string{"a commit with only a subject"}, CommitsKnown: true}, "a turn that made a commit with no body"
	case guardKindForbiddenCommands:
		return guardInput{Commands: []string{g.Example}, CommandsKnown: true}, "about to run \"" + g.Example + "\""
	}
	return guardInput{}, ""
}

// negativePathFor is a made-up path used for the negative and positive examples. It picks a name that matches none of except (so enable works even when except is *.txt or **).
func negativePathFor(g guard) string {
	for _, p := range []string{guardNegativePath, "shikii-guard-negative/example.go", "shikii-guard-negative/a/b/c.md", "shikii-guard-negative"} {
		skip := false
		for _, e := range g.Except {
			if matchPath(e, p) {
				skip = true
			}
		}
		if !skip {
			return p
		}
	}
	return guardNegativePath
}

func guardPositive(g guard) guardInput {
	switch g.Kind {
	case guardKindPairedFiles:
		return guardInput{Changed: []string{g.Pairs[0][0], g.Pairs[0][1]}, Known: true}
	case guardKindForbiddenWords:
		path := negativePathFor(g)
		return guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {withoutWords("this line is clean", g.Words)}}}
	case guardKindCommitBody:
		return guardInput{Commits: []string{"subject\n\n" + strings.Join(g.Required, "\n") + "\n"}, CommitsKnown: true}
	case guardKindForbiddenCommands:
		return guardInput{Commands: []string{guardPositiveCommand}, CommandsKnown: true}
	}
	return guardInput{}
}

// withoutWords removes every forbidden word from the positive example (a word such as "his" or "line" would otherwise stop it and enable would refuse).
// It repeats until nothing is left (each pass makes the line shorter), because removing one word can join the text around it into another.
func withoutWords(line string, words []string) string {
	for changed := true; changed; {
		changed = false
		for _, w := range words {
			if w != "" && strings.Contains(line, w) {
				line, changed = strings.ReplaceAll(line, w, ""), true
			}
		}
	}
	return line
}
