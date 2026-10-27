package shikii

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every module go.mod requires directly ships in the binaries, so its license notice must be in THIRD-PARTY-NOTICES (go-toml, added in
// 0.9.7, was missing until 0.9.9). The check reads go.mod's require block and looks for each module's path in a notice heading.
func TestThirdPartyNoticesNameEveryDirectDependency(t *testing.T) {
	notices, err := os.ReadFile("THIRD-PARTY-NOTICES")
	if err != nil {
		t.Fatal(err)
	}
	headings := strings.Join(regexp.MustCompile(`(?m)^={20,}\n(.+)\n={20,}$`).FindAllString(string(notices), -1), "\n")
	missing := thirdPartyMissing(t, headings)
	if len(missing) > 0 {
		t.Fatalf("add the license notices of these direct dependencies to THIRD-PARTY-NOTICES: %v", missing)
	}
	// negative example: a heading list without go-toml is caught
	if m := thirdPartyMissing(t, strings.ReplaceAll(headings, "github.com/pelletier/go-toml/v2", "")); len(m) != 1 {
		t.Fatalf("the check catches a missing notice: %v", m)
	}
}

func thirdPartyMissing(t *testing.T, headings string) []string {
	t.Helper()
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)require \((.*?)\)`).FindSubmatch(mod)
	if block == nil {
		t.Fatal("go.mod has no require block")
	}
	var missing []string
	for _, line := range strings.Split(string(block[1]), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.Contains(line, "// indirect") {
			continue
		}
		if !strings.Contains(headings, "("+fields[0]+")") {
			missing = append(missing, fields[0])
		}
	}
	return missing
}
