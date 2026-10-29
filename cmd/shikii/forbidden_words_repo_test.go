package main

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The person's rule, made absolute on 2026-10-29: the character U+5370 (a Japanese word for a mark or sign) is never written anywhere
// in this repository, in any compound either; what a file or value shows is said in words instead (a label, a registration, a
// separator line, the file's name). The forbidden-words guard no-shirushi stops a turn that adds it; this test fails when any tracked
// file keeps it, written as the character or as the Go escape of its code point.
func TestTheForbiddenCharacterAppearsNowhereInTheRepository(t *testing.T) {
	root := strings.TrimSpace(run(t, "git", "rev-parse", "--show-toplevel"))
	out := run(t, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z") // tracked and new files, as they are on disk
	written := spellsTheForbiddenCharacter
	var hits []string
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		if written(name) {
			hits = append(hits, name+" (its name)")
		}
		if info, err := os.Lstat(filepath.Join(root, name)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(filepath.Join(root, name)); err == nil && written(target) {
				hits = append(hits, name+" (the link's target)")
			}
			continue // what a link points at is checked under its own name when it is tracked
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue // deleted in the working tree, or a folder (a submodule)
		}
		if !written(string(data)) {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if written(line) {
				hits = append(hits, name+":"+itoa(i+1))
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("the forbidden character (U+5370) is written in %d tracked lines; say what the thing shows instead:\n%s", len(hits), strings.Join(hits, "\n"))
	}
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// forbiddenRune is the character the person forbade in this project (U+5370). Its spellings below are built from it, never written.
const forbiddenRune = rune(0x5370)

// spellsTheForbiddenCharacter reports whether text writes the character in any spelling: the character itself, a Go escape of its code
// point (\u with 4 hex digits, \U with 8), an HTML numeric reference (hex or decimal, leading zeros, with or without the semicolon), the
// escaped UTF-8 bytes or the URL encoding. Spellings are decoded and compared, not listed (confirmation audit 1 of 0.12).
func spellsTheForbiddenCharacter(text string) bool {
	ch := string(forbiddenRune)
	if strings.Contains(text, ch) {
		return true
	}
	lower := strings.ToLower(text)
	bytesEscaped := ""
	for _, b := range []byte(ch) {
		bytesEscaped += "\\x" + strconv.FormatInt(int64(b), 16)
	}
	if strings.Contains(lower, strings.ToLower(url.QueryEscape(ch))) || strings.Contains(lower, bytesEscaped) {
		return true
	}
	hexValue := func(digits string, exact int) (int64, bool) {
		n := 0
		for n < len(digits) && (digits[n] >= '0' && digits[n] <= '9' || digits[n] >= 'a' && digits[n] <= 'f') {
			n++
		}
		if n == 0 || exact > 0 && n < exact {
			return 0, false
		}
		if exact > 0 {
			n = exact
		}
		v, err := strconv.ParseInt(digits[:n], 16, 64)
		return v, err == nil
	}
	for i := 0; i < len(lower); i++ {
		var value int64
		var ok bool
		switch {
		case strings.HasPrefix(lower[i:], "\\u"):
			value, ok = hexValue(lower[i+2:], 4)
		case strings.HasPrefix(lower[i:], "&#x"):
			value, ok = hexValue(lower[i+3:], 0)
		case strings.HasPrefix(lower[i:], "&#"):
			n := 0
			for n < len(lower[i+2:]) && lower[i+2+n] >= '0' && lower[i+2+n] <= '9' {
				n++
			}
			if n > 0 {
				v, err := strconv.ParseInt(lower[i+2:i+2+n], 10, 64)
				value, ok = v, err == nil
			}
		default:
			continue
		}
		if ok && value == int64(forbiddenRune) {
			return true
		}
	}
	// \U takes 8 hex digits and is case-sensitive in Go, so it is looked for in the original text
	for i := 0; i+10 <= len(text); i++ {
		if text[i] == '\\' && text[i+1] == 'U' {
			if v, err := strconv.ParseInt(text[i+2:i+10], 16, 64); err == nil && v == int64(forbiddenRune) {
				return true
			}
		}
	}
	return false
}
