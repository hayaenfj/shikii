package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
	forbidden := string(rune(0x5370))
	escape := "\\u" + "5370"
	var hits []string
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue // deleted in the working tree, or a folder (a submodule)
		}
		if !bytes.Contains(data, []byte(forbidden)) && !bytes.Contains(data, []byte(escape)) {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, forbidden) || strings.Contains(line, escape) {
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
