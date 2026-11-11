package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A write or an append of a text with nothing to read is refused, and nothing is written: neither a record that exists nor a new
// one (audit of 0.31.0: a write with its text left out replaced a record with 0 bytes, and one with a byte order mark alone
// replaced it with the mark). The rule names what a text is, a character that shows, so the blanks are whatever is left.
func TestWriteRefusesATextWithNothingToRead(t *testing.T) {
	b, dir := book(t)
	const report = "# Audit\n"
	if _, err := b.Write("audit/report.md", report, false); err != nil {
		t.Fatal(err)
	}
	mark := string([]byte{0xef, 0xbb, 0xbf}) // a byte order mark
	for name, blank := range map[string]string{
		"nothing":                      "",
		"line breaks":                  "\r\n\n",
		"spaces and a tab":             " \t ",
		"a byte order mark":            mark,
		"a byte order mark and spaces": mark + " \n",
		"a zero-width space":           string([]byte{0xe2, 0x80, 0x8b}),
		"a full-width space":           string([]byte{0xe3, 0x80, 0x80}),
		"a no-break space":             string([]byte{0xc2, 0xa0}),
		"control characters":           "\x00\x1b\x7f",
	} {
		if HasText(blank) {
			t.Errorf("%s counts as a text with something to read", name)
		}
		for _, appendMode := range []bool{false, true} {
			if _, err := b.Write("audit/report.md", blank, appendMode); !errors.Is(err, ErrNoText) {
				t.Errorf("a write of %s (append: %v) is refused with ErrNoText: %v", name, appendMode, err)
			}
			if _, err := b.Write("audit/new.md", blank, appendMode); !errors.Is(err, ErrNoText) {
				t.Errorf("a write of %s to a new record (append: %v) is refused with ErrNoText: %v", name, appendMode, err)
			}
		}
		if f, err := b.Read("audit/report.md"); err != nil || f.Content != report {
			t.Fatalf("after a write of %s the record reads %q (%v)", name, f.Content, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "audit", "new.md")); !os.IsNotExist(err) {
		t.Errorf("a refused write leaves no record behind: %v", err)
	}
	// what counts as text: one character that shows, alone or among the blanks
	for _, text := range []string{"x", ".", mark + "x", " \n-\n", string([]byte{0xe3, 0x81, 0x82})} {
		if !HasText(text) {
			t.Errorf("%q counts as a text with nothing to read", text)
		}
		if _, err := b.Write("audit/report.md", text, false); err != nil {
			t.Errorf("a write of %q is accepted: %v", text, err)
		}
		if f, _ := b.Read("audit/report.md"); f.Content != text {
			t.Errorf("a write of %q is read back as %q", text, f.Content)
		}
	}
}
