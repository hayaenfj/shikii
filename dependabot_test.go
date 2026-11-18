package shikii

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every place this repository pins a dependency must be listed in .github/dependabot.yml. The check is an allow list
// in the other direction: it walks the tracked files, derives the (ecosystem, directory) pairs that need an entry, and
// fails for any pair the config does not name. A new go.mod, Dockerfile, package.json with dependencies or workflow
// directory therefore cannot be forgotten.
func TestDependabotListsEveryPlaceThatPinsDependencies(t *testing.T) {
	entries := dependabotEntries(t)
	need := dependabotNeeded(t)
	if len(need) == 0 {
		t.Fatal("found nothing that pins a dependency; the walk is broken")
	}
	if missing := dependabotMissing(entries, need); len(missing) > 0 {
		t.Fatalf("add these to .github/dependabot.yml (ecosystem directory): %v", missing)
	}
	// negative examples: dropping any one entry is caught, so a green run is not just "the config parsed"
	for drop := range entries {
		rest := map[string]bool{}
		for k := range entries {
			if k != drop {
				rest[k] = true
			}
		}
		if m := dependabotMissing(rest, need); len(m) == 0 {
			t.Fatalf("the check did not notice that %q was removed from the config", drop)
		}
	}
}

func dependabotMissing(entries, need map[string]bool) []string {
	var missing []string
	for k := range need {
		if !entries[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

// dependabotEntries returns "ecosystem directory" for every update entry in the config.
func dependabotEntries(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version int `yaml:"version"`
		Updates []struct {
			Ecosystem   string   `yaml:"package-ecosystem"`
			Directory   string   `yaml:"directory"`
			Directories []string `yaml:"directories"`
			Schedule    struct {
				Interval string `yaml:"interval"`
			} `yaml:"schedule"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 2 {
		t.Fatalf("dependabot.yml must say version: 2, got %d", cfg.Version)
	}
	out := map[string]bool{}
	for _, u := range cfg.Updates {
		if u.Schedule.Interval == "" {
			t.Fatalf("the %s entry has no schedule.interval (Dependabot rejects it)", u.Ecosystem)
		}
		for _, d := range append([]string{u.Directory}, u.Directories...) {
			if d != "" {
				out[u.Ecosystem+" "+d] = true
			}
		}
	}
	return out
}

// dependabotNeeded returns "ecosystem directory" for every tracked file that pins something.
func dependabotNeeded(t *testing.T) map[string]bool {
	t.Helper()
	listed, err := exec.Command("git", "ls-files", "-co", "--exclude-standard").Output()
	if err != nil {
		t.Fatal(err)
	}
	need := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSpace(string(listed)), "\n") {
		if f == "" || strings.Contains(f, "testdata/") {
			continue
		}
		dir := "/" + path.Dir(f)
		if dir == "/." {
			dir = "/"
		}
		switch base := path.Base(f); {
		case base == "go.mod":
			need["gomod "+dir] = true
		case strings.Contains(strings.ToLower(base), "dockerfile"): // Dependabot reads Dockerfile, Dockerfile.dev, web.Dockerfile
			need["docker "+dir] = true
		case base == "package.json" && packageJSONPinsDependencies(t, f):
			need["npm "+dir] = true
		case strings.HasPrefix(f, ".github/workflows/") && (strings.HasSuffix(f, ".yml") || strings.HasSuffix(f, ".yaml")):
			need["github-actions /"] = true // "/" covers .github/workflows
		case base == "action.yml" || base == "action.yaml":
			need["github-actions "+dir] = true
		}
	}
	return need
}

func packageJSONPinsDependencies(t *testing.T, file string) bool {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var pkg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	for _, k := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		var deps map[string]string
		if v, ok := pkg[k]; ok && json.Unmarshal(v, &deps) == nil && len(deps) > 0 {
			return true
		}
	}
	return false
}
