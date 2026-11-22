package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// upgradeBatchFixture is a project with n pages waiting for the memory upgrade.
func upgradeBatchFixture(t *testing.T, n int) (repo, root string, b notebook.Book) {
	t.Helper()
	repo, root, _ = planningFixture(t)
	b = notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		loopPage(t, b, s.ID, map[string]any{"title": fmt.Sprintf("Upgrade rule %02d", i)})
	}
	return repo, root, b
}

// The batch of ten pages comes through prepare as ten pages, and the page after them waits for the next prepare. One record takes the
// ten reviews of the full batch, and refuses one more.
func TestWorkUpgradeBatchIsTenPagesAndTheEleventhComesNext(t *testing.T) {
	repo, root, b := upgradeBatchFixture(t, 11)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := v["memory_upgrade"].(map[string]any)
	pages, _ := u["pages"].([]any)
	if len(pages) != 10 || u["total"] != float64(11) || u["reviewed"] != float64(0) {
		t.Fatalf("eleven pages wait: the batch is ten pages, got %d (%v of %v reviewed)", len(pages), u["reviewed"], u["total"])
	}
	if next, _ := u["next_action"].(string); !strings.Contains(next, "at most 10 articles") {
		t.Errorf("the next action names the batch of ten: %q", next)
	}
	maintenance := func(from, to int) []any {
		items := []any{}
		for _, raw := range pages[from:to] {
			p := raw.(map[string]any)
			items = append(items, map[string]any{"ref": p["maintenance_ref"], "reason": "Checked original evidence; the rule remains applicable."})
		}
		return items
	}
	tooMany := append(maintenance(0, 10), map[string]any{"ref": "upgrade-extra", "reason": "One more than a batch."})
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": tooMany}); err == nil || !strings.Contains(err.Error(), "maintenance: 11 sent, at most 10") {
		t.Fatalf("one record takes at most a batch of maintenance reviews, and says so: %v", err)
	}
	if progress, err := b.MemoryUpgrade(); err != nil || progress.Reviewed != 0 {
		t.Fatal("a refused record saves no review", progress, err)
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": maintenance(0, 10)}); err != nil {
		t.Fatalf("the ten reviews of a full batch go in one record: %v", err)
	}
	if progress, err := b.MemoryUpgrade(); err != nil || progress.Reviewed != 10 || len(progress.Tasks) != 1 {
		t.Fatalf("ten reviewed, one page left: reviewed %d, %d tasks, %v", progress.Reviewed, len(progress.Tasks), err)
	}
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	left := next["memory_upgrade"].(map[string]any)["pages"].([]any)
	if len(left) != 1 || left[0].(map[string]any)["article"] == nil {
		t.Fatalf("the next prepare holds the one page left: %d pages", len(left))
	}
}

var upgradeBatchWords = regexp.MustCompile(`at most (\d+) articles`)

// upgradeBatchNumber is the number a guidance text gives for the batch, or -1 when the text names none.
func upgradeBatchNumber(text string) int {
	m := upgradeBatchWords.FindStringSubmatch(text)
	if m == nil {
		return -1
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// The English guidance (the next action of the batch, the one of a summary, and the sentence of the hook) names the batch size. Each
// number comes from the constant that the notebook uses, and the literal ten is pinned beside it.
func TestUpgradeGuidanceNamesTheBatchSizeTheNotebookUses(t *testing.T) {
	repo, root, _ := upgradeBatchFixture(t, 3)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "guidance", "cwd": repo, "prompt": "Check the routing implementation."})
	hook := feedbackContext(t, out)
	if !strings.Contains(hook, "Upgrading memory") {
		t.Fatalf("the hook delivers the upgrade sentence: %q (%s)", hook, root)
	}
	texts := map[string]string{
		"memoryUpgradeNext":        memoryUpgradeNext,
		"memoryUpgradeSummaryNext": memoryUpgradeSummaryNext(notebook.MemoryUpgrade{Status: "upgrading"}),
		"the hook sentence":        hook,
	}
	for name, text := range texts {
		got := upgradeBatchNumber(text)
		if got != notebook.MemoryUpgradeBatch {
			t.Errorf("%s names a batch of %d articles, the notebook returns %d: %q", name, got, notebook.MemoryUpgradeBatch, text)
		}
		if got != 10 {
			t.Errorf("%s names a batch of %d articles, not ten: %q", name, got, text)
		}
	}
}

// upgradeBatchBudgetFixture is a project with ten pages waiting for the memory upgrade, each with a body of about repeat*40 bytes,
// and the function that prepares the batch with a max_bytes (it fails the test when prepare is refused). Each prepare reads the same
// batch: nothing is reviewed in between.
func upgradeBatchBudgetFixture(t *testing.T, repeat int) func(bound int) (map[string]any, []any, int) {
	t.Helper()
	repo, root, b := upgradeBatchFixture(t, 0)
	s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Second original rule", Content: "Keep the evidence of every rule.", Reference: "synthetic", Version: "2", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("topic%02d", i)
		body := strings.Repeat("The long background of the "+name+" rule. ", repeat)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{s.ID}, Title: "Upgrade rule for " + name, Lesson: "Keep the evidence of " + name + ". " + strings.Repeat("A long lesson sentence. ", 30), AppliesTo: name, Kind: "rule", Scene: "when handling " + name, Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "budget", "cwd": repo, "prompt": "Check the routing implementation."})
	return func(bound int) (map[string]any, []any, int) {
		t.Helper()
		v, err := workCall(t, b, "prepare", map[string]any{"session_id": "budget", "turn_id": loadSession(root, "budget").TurnID, "query": "memory-upgrade", "max_bytes": bound})
		if err != nil {
			t.Fatalf("prepare with max_bytes %d: %v", bound, err)
		}
		raw, _ := json.Marshal(v)
		u, _ := v["memory_upgrade"].(map[string]any)
		pages, _ := u["pages"].([]any)
		return v, pages, len(raw)
	}
}

func upgradePageIDs(pages []any) []string {
	ids := []string{}
	for _, entry := range pages {
		head, _ := entry.(map[string]any)["article"].(map[string]any)
		ids = append(ids, fmt.Sprint(head["id"]))
	}
	return ids
}

// With ten pages whose bodies do not fit, the batch keeps its pages and gives up text in the order the code says: first the passages of
// each page (orientation), then the bodies, then the lessons, and only then the last pages, which the next prepare returns. A page that
// stays keeps its references, its task and the exact query that reads it, and none is claimed as read. The batch is never dropped
// whole, down to the smallest bound the CLI takes (8192 bytes: ten pages of about 700 bytes each do not fit there, so pages go).
func TestTheUpgradeBatchOfTenStaysWhenItsPagesDoNotFit(t *testing.T) {
	prepare := upgradeBatchBudgetFixture(t, 140)
	_, all, _ := prepare(1 << 20 >> 3) // 128 KiB: room for all of it, as a reference for the order of the pages
	if len(all) != 10 {
		t.Fatalf("the reference batch is ten pages: %d", len(all))
	}
	order := upgradePageIDs(all)
	for _, bound := range []int{64 << 10, 28 << 10, 16 << 10, 12 << 10, 10 << 10, workMinBudget} {
		v, pages, size := prepare(bound)
		if len(pages) == 0 || size > bound {
			t.Fatalf("max_bytes %d: the batch stays within the bound with pages in it, got %d pages and %d bytes (output %v)", bound, len(pages), size, v["output"])
		}
		for i, id := range upgradePageIDs(pages) {
			if id != order[i] {
				t.Errorf("max_bytes %d: the pages that stay are the first of the batch, in its order: %v against %v", bound, upgradePageIDs(pages), order)
				break
			}
		}
		omitted := fmt.Sprint(v["output"].(map[string]any)["omitted"])
		if len(pages) < 10 && !strings.Contains(omitted, fmt.Sprintf("memory_upgrade.pages[] after the first %d ", len(pages))) {
			t.Errorf("max_bytes %d: %d pages stay and output names how many: %s", bound, len(pages), omitted)
		}
		if len(pages) == 10 && strings.Contains(omitted, "memory_upgrade.pages[] after the first") {
			t.Errorf("max_bytes %d: all ten pages stay and output does not say that pages went: %s", bound, omitted)
		}
		for _, entry := range pages {
			page := entry.(map[string]any)
			head, _ := page["article"].(map[string]any)
			query, _ := page["query"].(map[string]any)
			if page["ref"] == "" || page["maintenance_ref"] == "" || page["task"] == nil || head["id"] == "" || head["title"] == "" || page["body_included"] != false ||
				query["query"] != fmt.Sprintf("lesson:%v@%v", head["id"], head["revision"]) {
				t.Errorf("max_bytes %d: a page keeps its references, its head, its task and the query that reads it, without its body: %v", bound, page)
			}
		}
		t.Logf("max_bytes %d: %d pages, %d bytes", bound, len(pages), size)
	}
}

// With room the ten pages come whole under their references, and with a bound that holds the heads but not the bodies they come as
// ten pages with the exact query of each.
func TestTheUpgradeBatchOfTenComesWholeWithRoom(t *testing.T) {
	prepare := upgradeBatchBudgetFixture(t, 6)
	v, pages, size := prepare(64 << 10)
	if len(pages) != 10 || size > 64<<10 {
		t.Fatalf("ten short pages fit in 64 KiB: %d pages, %d bytes (output %v)", len(pages), size, v["output"])
	}
	for _, entry := range pages {
		page := entry.(map[string]any)
		body, _ := page["article"].(map[string]any)["body"].(string)
		if page["body_included"] != true || !strings.Contains(body, "The long background of the") {
			t.Errorf("with room a page comes whole: %v", page["ref"])
		}
	}
	if v["output"] != nil {
		t.Errorf("nothing was left out: %v", v["output"])
	}
}

// upgradeBatchDocs is the list of the places that state the size of the memory upgrade's batch, each with the sentence it holds for
// a given size. A place not on the list is not checked (the batch of ordinary maintenance, two articles in `wiki query`, is another
// number in other files). A document that still says the old size, or no size, is named.
var upgradeBatchDocs = []struct {
	file   string
	phrase func(n int) string
}{
	{"../../skills/shikii/references/recording.md", func(n int) string { return fmt.Sprintf("for at most %d full pinned articles", n) }},
	{"../../docs/guide.md", func(n int) string { return fmt.Sprintf("review at most one batch of %d articles", n) }},
	{"../../docs/guide.ja.md", func(n int) string { return fmt.Sprintf("\u6700\u5927%d\u8a18\u4e8b", n) }},
	{"../../docs/memory-and-learning.md", func(n int) string { return fmt.Sprintf("raised to %d articles", n) }},
	{"../../docs/memory-and-learning.ja.md", func(n int) string { return fmt.Sprintf("\u6700\u5927%d\u8a18\u4e8b", n) }},
}

func upgradeBatchDocProblems(read func(file string) string, n int) []string {
	var problems []string
	for _, d := range upgradeBatchDocs {
		if !strings.Contains(read(d.file), d.phrase(n)) {
			problems = append(problems, d.file+" does not say "+strconv.Quote(d.phrase(n)))
		}
	}
	return problems
}

func TestUpgradeBatchDocsStateTheSizeTheNotebookUses(t *testing.T) {
	read := func(file string) string { return string(mustRead(t, file)) }
	for _, n := range []int{notebook.MemoryUpgradeBatch, 10} {
		if problems := upgradeBatchDocProblems(read, n); len(problems) > 0 {
			t.Errorf("the documents state a batch other than %d articles: %s", n, strings.Join(problems, "; "))
		}
	}
}

// A negative case for the list above: documents that carry the old size are named, one per file, so the check does not pass by
// finding a sentence that merely exists.
func TestUpgradeBatchDocCheckNamesADocumentWithTheOldSize(t *testing.T) {
	stale := func(file string) string {
		for _, d := range upgradeBatchDocs {
			if d.file == file {
				return "text before " + d.phrase(2) + " text after"
			}
		}
		return ""
	}
	if problems := upgradeBatchDocProblems(stale, 10); len(problems) != len(upgradeBatchDocs) {
		t.Fatalf("every document with the old size is named: %v", problems)
	}
	if problems := upgradeBatchDocProblems(stale, 2); len(problems) != 0 {
		t.Fatalf("a document that says the size asked for passes: %v", problems)
	}
}
