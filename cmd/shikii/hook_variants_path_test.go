package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// hookSpecPath returns decisions/<id>.yaml unless an adopted wording (<id>-v<N>.yaml) of the same question has a higher version. It
// does not read the original when there is no such file, so every answer below has to come from the files alone: the original
// missing or broken, a variant broken, of another question, of the same or a lower version, and the highest of several chosen by the
// version inside, not by the name.
func TestHookSpecPathChoosesTheHighestReadableVersionOfTheSameQuestion(t *testing.T) {
	const id = "changes-in-scope"
	type variant struct {
		name    string // file name in decisions/
		version int    // added to the original's version; ignored when broken
		id      string // the id written in it ("" = the question's own)
		broken  bool
	}
	const original = "original"
	cases := []struct {
		name         string
		originalFile string // "ok", "broken" or "missing"
		variants     []variant
		want         string // the file name expected, or original
	}{
		{"none", "ok", nil, original},
		{"none, original missing", "missing", nil, original},
		{"none, original broken", "broken", nil, original},
		{"a higher version", "ok", []variant{{name: id + "-v2.yaml", version: 1}}, id + "-v2.yaml"},
		{"a higher version, original broken", "broken", []variant{{name: id + "-v2.yaml", version: 1}}, id + "-v2.yaml"},
		{"a higher version, original missing", "missing", []variant{{name: id + "-v2.yaml", version: 1}}, id + "-v2.yaml"},
		{"the same version", "ok", []variant{{name: id + "-v2.yaml", version: 0}}, original},
		{"a lower version", "ok", []variant{{name: id + "-v2.yaml", version: -1}}, original},
		{"the highest of several by the version inside, not by the name", "ok", []variant{{name: id + "-v10.yaml", version: 2}, {name: id + "-v9.yaml", version: 3}, {name: id + "-v3.yaml", version: 1}}, id + "-v9.yaml"},
		{"another question inside", "ok", []variant{{name: id + "-v2.yaml", version: 5, id: "done-as-asked"}}, original},
		{"a broken variant", "ok", []variant{{name: id + "-v2.yaml", broken: true}}, original},
		{"a broken variant beside a readable one", "ok", []variant{{name: id + "-v5.yaml", broken: true}, {name: id + "-v2.yaml", version: 1}}, id + "-v2.yaml"},
		{"an undone variant", "ok", []variant{{name: id + "-v2.yaml.reverted", version: 1}}, original},
		{"a variant of another question", "ok", []variant{{name: "done-as-asked-v2.yaml", version: 1}}, original},
	}
	versionLine := regexp.MustCompile(`(?m)^version: \d+$`)
	idLine := regexp.MustCompile(`(?m)^id: .*$`)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := t.TempDir()
			dir := filepath.Join(agent, "decisions")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := seedAgentSpecs(dir); err != nil {
				t.Fatal(err)
			}
			base := filepath.Join(dir, id+".yaml")
			text, err := os.ReadFile(base)
			if err != nil {
				t.Fatal(err)
			}
			baseVersion := mustLoad(t, base).Version
			switch c.originalFile {
			case "broken":
				if err := os.WriteFile(base, []byte("id: [unclosed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(base); err != nil {
					t.Fatal(err)
				}
			}
			for _, v := range c.variants {
				body := []byte("id: [unclosed\n")
				if !v.broken {
					s := versionLine.ReplaceAllString(string(text), fmt.Sprintf("version: %d", baseVersion+v.version))
					if v.id != "" {
						s = idLine.ReplaceAllString(s, "id: "+v.id)
					}
					if (v.version != 0 || v.id != "") && s == string(text) {
						t.Fatal("the variant is the same as the original: nothing was replaced")
					}
					body = []byte(s)
				}
				if err := os.WriteFile(filepath.Join(dir, v.name), body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want := base
			if c.want != original {
				want = filepath.Join(dir, c.want)
			}
			if got := hookSpecPath(agent, id); got != want {
				t.Fatalf("hookSpecPath = %s, want %s", got, want)
			}
		})
	}
}
