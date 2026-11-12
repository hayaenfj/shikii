package main

import (
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A refused call is the moment an agent needs the shape. Counted on 2026-11-11 in this machine's Claude Code and Codex sessions:
// project (the MCP argument) sent to the CLI, a draft's title at the top of a reflection, max_bytes beside work instead of inside it,
// old/new inside an edit. Each refusal was `json: unknown field "x"`: the key and nothing else, so the next call was another guess.
// The refusal now says which object held the key, which fields that object takes, and where a field of that name does belong.
func TestUnknownFieldRefusalNamesThePlaceAndTheFieldsThere(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	refused := func(input string, run func() error) string {
		t.Helper()
		var err error
		withStdin(t, input, func() { _, err = captureErr(run) })
		if err == nil {
			t.Fatalf("%s must be refused", input)
		}
		return err.Error()
	}
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	mcp := func(tool, arguments string) string {
		t.Helper()
		code, body, err := callTool(h, tool, []byte(arguments))
		if err != nil || code != 400 {
			t.Fatalf("%s must be refused with 400: %d %s %v", arguments, code, body, err)
		}
		return string(body)
	}
	for _, c := range []struct {
		name, got string
		want      []string
	}{
		{"the MCP project argument sent to the CLI",
			refused(`{"query":"csv","project":"/tmp/p"}`, func() error { return cmdWork([]string{"prepare"}) }),
			[]string{`unknown field "project" at the top level`, "the fields there: ", "query", "receipt", "session_id", "turn_id", "project is an argument of the MCP tools", "run the command in the project's folder (for a worktree, the original project)"}},
		{"a key inside a list inside a list",
			refused(`{"receipt":"r","notes":[{"target":"knowledge1","edits":[{"field":"body","old":"a","new":"b"}]}]}`, func() error { return cmdWork([]string{"record"}) }),
			[]string{`unknown field "old" in notes[0].edits[0]`, "the fields there: field, before, after"}},
		{"a draft's key at the top of a reflection",
			refused(`{"title":"t","lesson":"l"}`, func() error { return cmdReflect([]string{"place"}) }),
			[]string{`unknown field "title" at the top level`, "reviews, wiki, memory", `"title" is a field of wiki[]`}},
		{"a lesson's key in a source",
			refused(`{"kind":"human","title":"t","content":"c","scene":"when"}`, func() error { return cmdNotebook([]string{"capture"}) }),
			[]string{`unknown field "scene" at the top level`, "the fields there: id, kind, title, content, reference, version, observed_at"}},
		{"the pre-start organizing",
			refused(`{"session_id":"s","turn_id":"0123456789abcdef","topic":"csv"}`, func() error { return cmdHook([]string{"plan", "query"}) }),
			[]string{`unknown field "topic" at the top level`, "session_id, query, turn_id"}},
		{"max_bytes beside work through MCP",
			mcp("shikii_wiki", `{"operation":"query","query":"csv","max_bytes":20000}`),
			[]string{`unknown field \"max_bytes\" at the top level`, `\"max_bytes\" is a field of work`}},
		{"a key inside work through MCP",
			mcp("shikii_wiki", `{"operation":"prepare","work":{"query":"csv","request":"check csv","topic":"x"}}`),
			[]string{`unknown field \"topic\" in work`, "the fields there: ", "max_bytes"}},
		{"a key inside an object whose own key is in another case (encoding/json reads Work as work)",
			mcp("shikii_wiki", `{"operation":"prepare","Work":{"query":"csv","request":"check csv","topic":"x"}}`),
			[]string{`unknown field \"topic\" in Work`, "the fields there: "}},
		{"a draft's key at the top of a reflection through MCP",
			mcp("shikii_reflect", `{"operation":"place","title":"t"}`),
			[]string{`unknown field \"title\" at the top level`, `\"title\" is a field of wiki[]`}},
	} {
		for _, w := range c.want {
			if !strings.Contains(c.got, w) {
				t.Errorf("%s: the refusal lacks %q: %s", c.name, w, c.got)
			}
		}
	}
	// A key the object takes is never reported, whatever its case (encoding/json matches names case-insensitively), and a refusal that
	// is not about an unknown key keeps its own text.
	if got := refused(`{"Query":"csv","REQUEST":"check csv","max_bytes":1}`, func() error { return cmdWork([]string{"prepare"}) }); strings.Contains(got, "unknown field") || !strings.Contains(got, "max_bytes must be between") {
		t.Errorf("known keys in another case are not unknown: %s", got)
	}
	if got := refused(`{"query":7}`, func() error { return cmdWork([]string{"prepare"}) }); strings.Contains(got, "unknown field") {
		t.Errorf("a wrong type is not an unknown field: %s", got)
	}
}

// One reader, so the next entry cannot answer with the bare key again: refusing unknown keys (DisallowUnknownFields) is written
// only in the shared reader and in the two readers that are not an agent's input (the page's own article form and project.json).
// An allowlist, with a negative example: the same call written anywhere else is found.
func TestOnlyTheSharedReaderRefusesUnknownFields(t *testing.T) {
	allowed := map[string]bool{"json_input.go": true, "serve_article.go": true, "project.go": true}
	if got := strictReaders(t, "."); !sameNames(got, allowed) {
		t.Errorf("DisallowUnknownFields is written in %v; only %v may (read an agent's JSON with decodeStrictObject)", got, names(allowed))
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "elsewhere.go"), []byte("package main\nfunc f() { d := json.NewDecoder(nil); d.DisallowUnknownFields() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "elsewhere_test.go"), []byte("package main\nfunc g() { d.DisallowUnknownFields() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := strictReaders(t, dir); len(got) != 1 || got[0] != "elsewhere.go" {
		t.Errorf("the check must find the call in a file outside the list (and skip tests): %v", got)
	}
}

func strictReaders(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			sources[filepath.Base(f)] = string(mustRead(t, f))
		}
	}
	// the method DisallowUnknownFields of encoding/json's Decoder, however it is reached (read by its type)
	uses := typedUses(t, sources, dir == ".", func(obj types.Object) bool {
		fn, ok := obj.(*types.Func)
		return ok && fn.Name() == "DisallowUnknownFields" && fn.Pkg() != nil && fn.Pkg().Path() == "encoding/json"
	})
	seen := map[string]bool{}
	var found []string
	for _, use := range uses {
		file := strings.SplitN(use, ":", 2)[0]
		if !seen[file] {
			seen[file] = true
			found = append(found, file)
		}
	}
	sort.Strings(found)
	return found
}

func names(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sameNames(got []string, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for _, name := range got {
		if !want[name] {
			return false
		}
	}
	return true
}
