package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The fidelity shapes make the same preparations of a request of a given size through the entrances a person's request takes (the
// hook, the CLI without max_bytes, and an explicit read of an article under a named bound) and write down what each delivered, with
// what changes from run to run (ids, the receipt, times, paths) made the same. testdata/work_hook_fidelity_golden.json holds what
// the code before the last fields of the bound (befc20f4) delivered, taken from a copy of that code run on the same inputs; a
// request that was delivered whole or as its head before is delivered the same after, field by field.
//
// This file uses nothing but what befc20f4 has, so the same file ran in that copy.

// fidelityPrompt is a request of exactly n bytes: one sentence that finds the three articles of the fixture, then filler.
func fidelityPrompt(n int) string {
	text := "Check the CSV export filters and totals. " + strings.Repeat("Also look at every column of the export. ", n/41+1)
	return text[:n]
}

// fidelityFixture is a hook project with three articles that the request finds (two of them reach the hook's lessons).
func fidelityFixture(t *testing.T) (repo, root string, book notebook.Book, pages []notebook.Page) {
	t.Helper()
	repo, root, _ = planningFixture(t)
	t.Chdir(repo)
	book = notebook.Book{Root: root, Scope: feedbackScope(repo)}
	for i := 1; i <= 3; i++ {
		pages = append(pages, interimAddArticle(t, book, fmt.Sprintf("CSV export filters and totals %d", i), strings.Repeat("The export filters stay on until the totals are checked.\n\n", 60)))
	}
	return repo, root, book, pages
}

var (
	fidelityHex  = regexp.MustCompile(`[0-9a-f]{16}`)
	fidelityTime = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T[0-9:.]+(Z|[+-]\d{2}:\d{2})`)
	fidelityPath = regexp.MustCompile(`/[A-Za-z0-9_.+-]+(/[A-Za-z0-9_.+-]+)+`)
)

// fidelityNormalize writes a reply with its ids (each 16-hex text, numbered by first sight in key order), receipts, times and paths
// made the same from run to run.
func fidelityNormalize(v any) string {
	seen := map[string]string{}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			x = fidelityTime.ReplaceAllString(x, "TIME")
			x = fidelityPath.ReplaceAllString(x, "PATH")
			return fidelityHex.ReplaceAllStringFunc(x, func(id string) string {
				if seen[id] == "" {
					seen[id] = fmt.Sprintf("id%d", len(seen)+1)
				}
				return seen[id]
			})
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out := map[string]any{}
			for _, k := range keys {
				if k == "receipt" {
					out[k] = "RECEIPT"
					continue
				}
				out[k] = walk(x[k])
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i := range x {
				out[i] = walk(x[i])
			}
			return out
		}
		return v
	}
	raw, _ := json.Marshal(walk(v))
	return string(raw)
}

// fidelityRecord is what the golden file keeps of one delivery: a hash of the normalized reply and a summary a failure can be read
// from (the top-level fields with their sizes, each knowledge entry's fields, and what output named).
type fidelityRecord struct {
	Hash    string   `json:"hash"`
	Summary []string `json:"summary"`
}

func fidelitySummary(text string) fidelityRecord {
	sum := sha256.Sum256([]byte(text))
	record := fidelityRecord{Hash: hex.EncodeToString(sum[:8])}
	var reply map[string]any
	if json.Unmarshal([]byte(text), &reply) != nil {
		record.Summary = []string{text}
		return record
	}
	names := make([]string, 0, len(reply))
	for k := range reply {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		raw, _ := json.Marshal(reply[k])
		record.Summary = append(record.Summary, fmt.Sprintf("%s=%d", k, len(raw)))
	}
	entries, _ := reply["knowledge"].([]any)
	for i, raw := range entries {
		e, _ := raw.(map[string]any)
		keys := make([]string, 0, len(e))
		for k := range e {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		record.Summary = append(record.Summary, fmt.Sprintf("knowledge[%d]: %s", i, strings.Join(keys, ",")))
	}
	if output, ok := reply["output"].(map[string]any); ok {
		record.Summary = append(record.Summary, fmt.Sprintf("output.omitted=%v", output["omitted"]))
	}
	return record
}

// fidelityHookReply is what the hook delivers for a request of n bytes: the prepared work as the agent reads it, normalized, or the
// words "not prepared" when the hook fell back to the manual notice.
func fidelityHookReply(t *testing.T, n int) string {
	t.Helper()
	repo, _, _, _ := fidelityFixture(t)
	out := runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": fidelityPrompt(n)})
	var envelope any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	context := fidelityContext(envelope)
	const prefix = "shikii prepared work:\n"
	i := strings.Index(context, prefix)
	if i < 0 {
		return "not prepared"
	}
	var prepared map[string]any
	if err := json.Unmarshal([]byte(context[i+len(prefix):]), &prepared); err != nil {
		t.Fatal(err)
	}
	if hookContextLength(context) > hookContextMax {
		t.Fatalf("the hook's context is %d characters", hookContextLength(context))
	}
	return fidelityNormalize(prepared)
}

// fidelityContext finds the hook's additional context in its output.
func fidelityContext(v any) string {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, "work prepare") || strings.Contains(x, "shikii prepared work:") {
			return x
		}
	case map[string]any:
		for _, c := range x {
			if s := fidelityContext(c); s != "" {
				return s
			}
		}
	}
	return ""
}

// fidelityCLIReply is what `shikii work prepare` delivers, without max_bytes, for the hook's ids of a request of n bytes.
func fidelityCLIReply(t *testing.T, n int) string {
	t.Helper()
	repo, _, _, _ := fidelityFixture(t)
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": fidelityPrompt(n)}))
	ids := auditIDs.FindString(notice)
	if ids == "" {
		t.Fatal("the notice carries the ids")
	}
	var out string
	var err error
	withStdin(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export filters and totals"}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
	if err != nil {
		return "refused: " + err.Error()
	}
	var reply map[string]any
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatal(err)
	}
	return fidelityNormalize(reply)
}

// fidelityExplicitReply is what a read of one article by its exact revision delivers under max_bytes 8192, with the receipt of the
// hook's preparation of a request of n bytes, or the words of the refusal.
func fidelityExplicitReply(t *testing.T, n int) string {
	t.Helper()
	repo, root, book, pages := fidelityFixture(t)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": fidelityPrompt(n)})
	s := loadSession(root, "planning")
	if s.Planning == nil || s.Planning.Work == nil {
		return "not prepared"
	}
	receipt := workEncode(workToken{Session: "planning", Turn: s.TurnID, Nonce: s.Planning.Work.Nonce})
	reply, err := workCall(t, book, "prepare", map[string]any{"receipt": receipt, "query": "lesson:" + pages[0].ID + "@" + pages[0].Revision, "max_bytes": 8192})
	if err != nil {
		return "refused: " + err.Error()
	}
	return fidelityNormalize(reply)
}
