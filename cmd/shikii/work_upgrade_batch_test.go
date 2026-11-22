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
	{"../../skills/shikii/references/tidy.md", func(n int) string { return fmt.Sprintf("(at most %d articles)", n) }},
	{"../../docs/guide.md", func(n int) string { return fmt.Sprintf("review at most one batch of %d articles", n) }},
	{"../../docs/guide.ja.md", func(n int) string { return fmt.Sprintf("\u6700\u5927%d\u8a18\u4e8b", n) }},
	{"../../docs/memory-and-learning.md", func(n int) string { return fmt.Sprintf("raised to %d articles", n) }},
	{"../../docs/memory-and-learning.ja.md", func(n int) string { return fmt.Sprintf("\u6700\u5927%d\u8a18\u4e8b", n) }},
}

func upgradeBatchDocProblems(read func(file string) string, n int) []string {
	var problems []string
	for _, d := range upgradeBatchDocs {
		if !strings.Contains(strings.Join(strings.Fields(read(d.file)), " "), d.phrase(n)) { // a sentence wrapped over two lines is the same sentence
			problems = append(problems, d.file+" does not say "+strconv.Quote(d.phrase(n)))
		}
	}
	return problems
}

// upgradeBatchProseDocs are the documents whose prose speaks of the memory upgrade in the present tense, and so may state no number of
// articles for it but the size of the batch. The history in memory-and-learning (a table of versions and the retrospective of each, which
// keep the old size of the versions that had it) is not here: the list above checks what it says of the batch now.
var upgradeBatchProseDocs = []string{
	"../../skills/shikii/references/recording.md",
	"../../skills/shikii/references/tidy.md",
	"../../skills/shikii/SKILL.md",
	"../../docs/guide.md",
	"../../docs/guide.ja.md",
}

// upgradeBatchTerms are the words by which a sentence speaks of the memory upgrade (the query, the field, the progress message, the
// optimization that includes it, and its names in Japanese). A number of articles in a block that has one is a number of the batch.
var upgradeBatchTerms = regexp.MustCompile(`(?i)memory[-_ ]upgrade|upgrading memory|memory[- ]policy|memory[- ]optimi[sz]|\x{8a18}\x{61b6}\x{306e}(?:\x{79fb}\x{3057}\x{66ff}\x{3048}|\x{66f4}\x{65b0}|\x{6700}\x{9069}\x{5316})`)

// upgradeBatchCounts finds a number of articles or pages: "at most 10 full pinned articles", "a two-article batch", and in Japanese a
// number (digits or one of the kanji numbers up to ten) before the counters for articles, items and pages.
var upgradeBatchCounts = regexp.MustCompile(`(?i)\b(\d+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)[- ](?:(?:full|pinned)[- ])*(?:articles?|pages?)\b` +
	`|([0-9\x{ff10}-\x{ff19}]+|[\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}])\s*(?:\x{8a18}\x{4e8b}|\x{4ef6}|\x{30da}\x{30fc}\x{30b8})`)

var upgradeBatchNumberWords = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
	"\u4e00": 1, "\u4e8c": 2, "\u4e09": 3, "\u56db": 4, "\u4e94": 5, "\u516d": 6, "\u4e03": 7, "\u516b": 8, "\u4e5d": 9, "\u5341": 10}

// upgradeBatchCountValue is the number of one match of upgradeBatchCounts (-1 when it cannot be read).
func upgradeBatchCountValue(m []string) int {
	word := strings.ToLower(m[1] + m[2])
	if n, ok := upgradeBatchNumberWords[word]; ok {
		return n
	}
	n := 0
	for _, r := range word {
		if r >= '\uff10' && r <= '\uff19' {
			r = '0' + (r - '\uff10')
		}
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// proseBlocks splits a text into its blocks: a paragraph (up to a blank line) or a list item (a line that starts with a bullet or a
// number, with the lines that continue it), each with its lines joined.
func proseBlocks(text string) []string {
	var blocks []string
	var block []string
	flush := func() {
		if len(block) > 0 {
			blocks = append(blocks, strings.Join(block, " "))
			block = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case listItemLine.MatchString(line):
			flush()
			block = append(block, strings.TrimSpace(line))
		default:
			block = append(block, strings.TrimSpace(line))
		}
	}
	flush()
	return blocks
}

var listItemLine = regexp.MustCompile(`^\s*(?:[-*]|\d+\.)\s`)

// upgradeBatchCountProblems is the allow-list of the numbers: in a block (a paragraph or a list item) that speaks of the memory upgrade,
// each number of articles must be n, the size of the batch. A block that says another number is named with it, whether the number is the
// old two or any other, and wherever in the block it stands (a stale sentence after the right one is found too). A count in a block that
// is not about the upgrade (the ordinary review of two articles in `wiki query`) is not looked at.
func upgradeBatchCountProblems(text string, n int) []string {
	var problems []string
	for _, block := range proseBlocks(text) {
		if !upgradeBatchTerms.MatchString(block) {
			continue
		}
		for _, at := range upgradeBatchCounts.FindAllStringSubmatchIndex(block, -1) {
			m := []string{block[at[0]:at[1]], "", ""}
			for i := 1; i <= 2; i++ {
				if at[2*i] >= 0 {
					m[i] = block[at[2*i]:at[2*i+1]]
				}
			}
			if got := upgradeBatchCountValue(m); got != n {
				around := []rune(block)[max(0, len([]rune(block[:at[0]]))-50):min(len([]rune(block)), len([]rune(block[:at[1]]))+30)]
				problems = append(problems, fmt.Sprintf("%s (%d, not %d) in a block that speaks of the memory upgrade: ...%s...", strconv.Quote(m[0]), got, n, string(around)))
			}
		}
	}
	return problems
}

func upgradeBatchProseProblems(read func(file string) string, n int) []string {
	var problems []string
	for _, file := range upgradeBatchProseDocs {
		for _, p := range upgradeBatchCountProblems(read(file), n) {
			problems = append(problems, file+": "+p)
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
		if problems := upgradeBatchProseProblems(read, n); len(problems) > 0 {
			t.Errorf("a paragraph or list item about the memory upgrade says a number of articles other than %d:\n%s", n, strings.Join(problems, "\n"))
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

// A negative case for the allow-list of the numbers: a paragraph or list item about the memory upgrade that says any number of articles
// but the size of the batch is named, the old two or another; a text that says only the size, and counts in blocks that are not about the
// upgrade, pass. Each text is judged alone, so the check does not pass by finding the right sentence somewhere else in the file.
func TestUpgradeBatchCountCheckNamesAnyOtherNumberAboutTheUpgrade(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       int
	}{
		{"the right number alone passes", "At a break in work, prepare with the same receipt and query memory-upgrade for at most 10 full pinned articles.", 0},
		{"the right number in Japanese passes", "\u901a\u5e38\u306f1\u4f9d\u983c\u306b\u3064\u304d\u3001\u8a18\u61b6\u306e\u66f4\u65b0\u3092\u6700\u592710\u8a18\u4e8b\u306e1\u7d44\u3060\u3051\u78ba\u8a8d\u3057\u307e\u3059\u3002", 0},
		{"the old number in a sentence of its own, beside the right one", "At a break in work, query memory-upgrade for at most 10 full pinned articles. Ordinary tasks review at most two full pinned articles of the memory upgrade.", 1},
		{"a number that was never the size", "Prepare with query memory-upgrade for at most 9 full pinned articles.", 1},
		{"the sentence after the one that names the upgrade", "A shipped memory policy upgrade shows Upgrading memory and reviews knowledge in batches. Ordinary tasks review at most one two-article batch.", 1},
		{"a sentence wrapped over two lines", "Prepare with query memory-upgrade for at most\ntwo full pinned articles.", 1},
		{"a number in words that is not the size", "Prepare with query memory-upgrade for at most nine articles.", 1},
		{"the old number in Japanese, in digits", "\u8a18\u61b6\u306e\u66f4\u65b0\u306f1\u4f9d\u983c\u3067\u6700\u59272\u8a18\u4e8b\u306e1\u7d44\u305a\u3064\u9032\u3081\u307e\u3059\u3002", 1},
		{"the old number in Japanese, with the counter for items", "\u8a18\u61b6\u306e\u79fb\u3057\u66ff\u3048\u306f2 \u4ef6\u305a\u3064\u78ba\u8a8d\u3057\u307e\u3059\u3002", 1},
		{"the old number in Japanese, in kanji", "\u8a18\u61b6\u306e\u6700\u9069\u5316\u3067\u306f\u4e8c\u8a18\u4e8b\u305a\u3064\u6574\u3048\u307e\u3059\u3002", 1},
		{"a stale sentence after the right one, in the same paragraph", "A shipped memory policy upgrade shows Upgrading memory. Ordinary tasks review at most one batch of 10 articles, then resume. Ordinary tasks review at most one two-article batch. An explicit request continues.", 1},
		{"a list item about something else is not near", "- The memory upgrade takes a batch of at most 10 articles.\n- A tidy request takes small batches of up to two articles.\n  The second line of that item.", 0},
		{"a number about something else is not looked at", "The wiki query reviews at most two articles of the ordinary maintenance.", 0},
		{"another paragraph is not near", "The memory upgrade runs in batches.\n\nThe ordinary review takes at most two articles.", 0},
	} {
		if got := upgradeBatchCountProblems(c.text, 10); len(got) != c.want {
			t.Errorf("%s: %d problems, want %d: %v", c.name, len(got), c.want, got)
		}
	}
}

// The documents say what becomes of the pages of the batch that do not fit max_bytes: what goes first, that the pages left out come back
// first in the next prepare, and where output says so. The label is the one trimMemoryUpgrade writes to output.omitted
// (TestTheUpgradeBatchOfTenStaysWhenItsPagesDoNotFit reads it from a real reply).
func TestUpgradeBatchDocsSayWhatBecomesOfPagesThatDoNotFit(t *testing.T) {
	for _, d := range []struct {
		file  string
		words []string
	}{
		{"../../skills/shikii/references/recording.md", []string{"the CLI's default bound often cannot hold 10 full pages", "`output.omitted` names each step", "memory_upgrade.pages[] after the first N", "the next prepare returns them first", "`memory_upgrade` is left out whole", "the exact `query` that reads it whole"}},
		{"../../docs/guide.md", []string{"the CLI's default bound often cannot hold 10 full articles", "the next prepare returns them first", "`output.omitted` says what was left out"}},
		{"../../docs/guide.ja.md", []string{"CLI\u306e\u65e2\u5b9a\u306e\u4e0a\u9650\u306b\u306f\u5165\u308a\u304d\u3089\u306a\u3044\u3053\u3068\u304c\u591a\u3044", "\u6b21\u306eprepare\u304c\u6700\u521d\u306b\u8fd4\u3057", "`output.omitted`\u306b\u51fa\u307e\u3059"}},
	} {
		text := strings.Join(strings.Fields(string(mustRead(t, d.file))), " ")
		for _, words := range d.words {
			if !strings.Contains(text, words) {
				t.Errorf("%s does not say %q", d.file, words)
			}
		}
	}
}
