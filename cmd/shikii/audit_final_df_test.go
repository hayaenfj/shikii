package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// 0.9.7 final audit (D1 to D2, F1 to F3). The checks are written independently in the tests; the functions are only what is tested.

// F2: the full organizing notice says where the id for restates comes from (the <id> in history:<id>:user); the short form did not.
// An agent with only the short form that put the key "history:<id>:user" query returned into restates was not refused, and nothing
// happened (restateDone only matches t.ID exactly).
func TestAuditFinalF2ShortPlanningNoticeSaysWhereTheRestatesIDComesFrom(t *testing.T) {
	full, short := planningNotice("s", "0123456789abcdef"), planningNoticeShort("s", "0123456789abcdef")
	const where = "history:<id>:user"
	if !strings.Contains(full, where) {
		t.Fatalf("precondition: the full text says where the restates id comes from %q: %q", where, full)
	}
	if !strings.Contains(short, where) {
		t.Fatalf("F2: the short form lacks the restates condition (the <id> of %q)\nshort: %q", where, short)
	}
}

// F2: the field names and conditions the full reflection request gives (wiki id and replaces; memory results tied to a check result) were
// missing from the short form. The earlier TestAuditF4ReflectionShortKeepsSubmitFields only looked at the names inside [{...}].
func TestAuditFinalF2ShortReflectionNamesEveryFieldOfTheFullText(t *testing.T) {
	full, short := reflectAsk("X reflect"), reflectAskShort("X reflect", []string{"a"})
	var missing []string
	for name, re := range map[string]*regexp.Regexp{
		"wiki id":                regexp.MustCompile(`\bid\b`),
		"wiki replaces":          regexp.MustCompile(`\breplaces\b`),
		"result tied to a check": regexp.MustCompile(`check result`),
	} {
		if !re.MatchString(full) {
			t.Fatalf("precondition: the full text gives %s", name)
		}
		if !re.MatchString(short) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("F2: the short reflection request lacks fields and conditions the full text gives: %v\nshort: %q", missing, short)
	}
}

// F3 (decided 2026-10-27): the Stop check repeats the organizing steps when the turn changed files without the organizing it was asked
// for. A turn that only read or answered is not stopped (stopping every answer-only turn would make the person wait for nothing), and no
// miss is tracked across turns.
func TestAuditFinalF3SkippedOrganizingIsRestatedAtStop(t *testing.T) {
	repo, root, _ := planningFixture(t)
	first := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest}))
	if !strings.Contains(first, "hook plan query") {
		t.Fatalf("precondition: organizing asked: %q", first)
	}
	if p := loadSession(root, "planning").Planning; p == nil || p.Result != nil {
		t.Fatalf("precondition: not submitted yet")
	}
	if err := os.WriteFile(filepath.Join(repo, "filters.go"), []byte("package filters\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Changed the filters."})
	if !strings.Contains(stop, "hook plan") {
		t.Fatalf("F3: a turn that changed files without the organizing is told the steps again: %q", stop)
	}
}

// D1: a serve pointed at the same project with another case spelling (--port 0, store named .shikii) took another lock and kept running
// as a second page while the page the hook started held the lock (case-insensitive disk).
func TestAuditFinalD1OnePageAcrossCaseSpellings(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	initAt(t, repo)
	other := strings.ToUpper(repo)
	a, errA := os.Stat(repo)
	b, errB := os.Stat(other)
	if other == repo || errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Skip("on a case-sensitive disk the two spellings are not the same folder")
	}
	takePageLock(t, common, repo) // the page the hook started (the recorded spelling) is running
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(other, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		rec, ok := readPageRecord(common, other)
		t.Fatalf("D1: a second page of the same project (%s and %s are the same folder) keeps running (record under the other spelling %v: %+v)", repo, other, ok, rec)
	}
}

// F2 control: an organizing sent from the short form alone is accepted (query, agreed, open and continues_previous added to its JSON).
func TestAuditFinalF2ShortPlanningNoticeAloneSubmits(t *testing.T) {
	repo, root, _ := planningFixture(t)
	turn := func(prompt string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	}
	turn(planningTestRequest)
	if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
		t.Fatal(err)
	}
	short := turn("Now add a tax rate to the agreed CSV columns and keep the existing filters.")
	if !strings.Contains(short, "as earlier in this session") {
		t.Fatalf("precondition: the second notice is the short form: %q", short)
	}
	var ids map[string]any
	if err := json.Unmarshal([]byte(regexp.MustCompile(`\{"session_id":"[^"]*","turn_id":"[0-9a-f]{16}"\}`).FindString(short)), &ids); err != nil {
		t.Fatalf("the JSON at the end of the short form: %v", err)
	}
	ids["query"] = "CSV columns filters"
	q, err := planningCall(t, "query", ids)
	if err != nil {
		t.Fatal(err)
	}
	src := q["source_material"].(map[string]any)["current_user"].(string)
	ids["agreed"] = map[string]any{"summary": "\u7a0e\u7387\u3092 CSV \u306e\u5217\u306b\u8db3\u3059\u3002", "evidence": []map[string]string{{"source": "current_user", "quote": src[:20]}}}
	ids["open"] = map[string]any{"unknown": true}
	ids["continues_previous"] = false
	if _, err := planningCall(t, "submit", ids); err != nil {
		t.Fatalf("F2: an organizing sent from the short form alone was refused: %v", err)
	}
}

// F2 control: answering what query returned from the short reflection request alone, everything is saved and left is empty.
func TestAuditFinalF2ShortReflectionAloneSubmits(t *testing.T) {
	_, scope, h, _, _ := memoryEffectFixture(t)
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	stop := func() {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		stop()
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	stop()
	short := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if !strings.Contains(short, "as earlier in this session") || !strings.Contains(short, "wrap up the reflection") {
		t.Fatalf("precondition: the second ask is the short form: %q", short)
	}
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	submit := map[string]any{"operation": "submit"}
	var reviews, memory []map[string]string
	var wiki []map[string]any
	for _, raw := range anySlice(q["sources"]) {
		s := raw.(map[string]any)
		id := s["source_material"].(map[string]any)["id"].(string)
		for _, tr := range anySlice(s["targets"]) {
			reviews = append(reviews, map[string]string{"source_id": id, "trace_id": tr.(map[string]any)["trace_id"].(string), "truth": "unknown", "reason": "\u4f1a\u8a71\u304b\u3089\u306f\u6c7a\u3081\u3089\u308c\u306a\u3044"})
		}
		if s["wiki_pending"] == true {
			wiki = append(wiki, map[string]any{"sources": []string{id}})
		}
		for _, m := range anySlice(s["memories"]) {
			item := m.(map[string]any)
			memory = append(memory, map[string]string{"source_id": item["source_id"].(string), "key": item["quote"].(map[string]any)["key"].(string), "result": "unknown", "decision": "", "evidence": "\u78ba\u304b\u3081\u305f\u7d50\u679c\u304c\u7121\u3044"})
		}
	}
	for _, raw := range anySlice(q["observations"]) {
		wiki = append(wiki, map[string]any{"sources": []string{raw.(map[string]any)["id"].(string)}})
	}
	submit["reviews"], submit["wiki"], submit["memory"] = reviews, wiki, memory
	r := reflectCall(t, h, submit)
	t.Logf("sent: reviews=%d wiki=%d memory=%d reply=%v", len(reviews), len(wiki), len(memory), r)
	if r["complete"] != true || r["left"] != nil {
		t.Fatalf("F2: a reflection sent from the short form alone was not saved: %v", r)
	}
}

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}
