package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The release archive (scripts/build-release.py) bundles both READMEs, and the English install text points at the English README's
// Requirements section, which exists (audit 0.9.2 translation, low 4: it pointed at README_ja.md and bundled only that).
func TestReleaseBundlesBothReadmesAndPointsAtTheEnglishOne(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-release.py"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	bundled := regexp.MustCompile(`(?s)BUNDLED = \[(.*?)\]\n`).FindStringSubmatch(script)
	if bundled == nil {
		t.Fatal("build-release.py has no BUNDLED list")
	}
	pairs := regexp.MustCompile(`\("([^"]+)", "([^"]+)"\)`).FindAllStringSubmatch(bundled[1], -1)
	got := map[string]string{}
	for _, p := range pairs {
		got[p[1]] = p[2]
	}
	if got["README.md"] != "README.md" || got["docs/readme/README_ja.md"] != "README_ja.md" {
		t.Errorf("both READMEs are bundled: %v", got)
	}
	// 2 binaries and INSTALL.txt besides the bundled files: the count the script checks must follow the list
	if want := len(pairs) + 3; !strings.Contains(script, "len(members) == "+strconv.Itoa(want)) || !strings.Contains(script, "are not "+strconv.Itoa(want)+" regular files") {
		t.Errorf("the archive check expects %d files (%d bundled + 2 binaries + INSTALL.txt)", want, len(pairs))
	}
	install := regexp.MustCompile(`(?s)install = f"""(.*?)"""`).FindStringSubmatch(script)
	if install == nil || !strings.Contains(install[1], `See "Requirements" in README.md`) {
		t.Fatalf("the English install text points at README.md's Requirements: %v", install)
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^## Requirements$`).Match(readme) {
		t.Error("README.md has a Requirements section")
	}
}
