package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// upgradeTrimReply is the memory_upgrade part of a prepare reply with n pages, as prepareMemoryUpgrade builds it (each page whole,
// with its orientation), and the texts the session would keep as delivered, one per page.
func upgradeTrimReply(n int) (map[string]any, map[string]string) {
	pages := []any{}
	quoted := map[string]string{}
	for i := 1; i <= n; i++ {
		p := notebook.Page{ID: fmt.Sprintf("page%02d", i), Revision: "rev0001", Title: fmt.Sprintf("Upgrade rule %02d", i), Kind: "rule",
			Lesson: "Keep the evidence of the rule.", Body: strings.Repeat("The long background of the rule. ", 80)}
		ref := fmt.Sprintf("knowledge%d", i)
		quoted[ref] = p.Body
		pages = append(pages, map[string]any{"ref": ref, "article": p, "orientation": notebook.Orient(p, notebook.MemoryUpgradePolicy, 1200),
			"task": map[string]any{"page_id": p.ID, "revision": p.Revision}, "maintenance_ref": "upgrade-" + p.ID, "body_included": true})
	}
	return map[string]any{"memory_upgrade": map[string]any{"status": "upgrading", "pages": pages}}, quoted
}

func jsonSize(v any) int {
	raw, _ := json.Marshal(v)
	return len(raw)
}

// trimmedPageIDs is the ids of the pages a trimmed reply still holds, in order.
func trimmedPageIDs(reply map[string]any) []string {
	pages, _ := reply["memory_upgrade"].(map[string]any)["pages"].([]any)
	ids := []string{}
	for _, raw := range pages {
		head, _ := raw.(map[string]any)["article"].(map[string]any)
		ids = append(ids, fmt.Sprint(head["id"]))
	}
	return ids
}

// When the pages of the batch do not fit even in their smallest form, trimMemoryUpgrade takes the last pages off one at a time, and
// the first page at least stays: it never keeps a batch of no pages. When not even the first page fits it says so (false), and the
// caller leaves the whole batch out. The smallest bound is measured here, from the smallest form of one page, and the two bounds
// beside it are one byte apart, so a change to the number of pages that stay at the least (zero, two) is found on either side.
func TestTrimMemoryUpgradeKeepsTheFirstPageOrReportsThatNoneFits(t *testing.T) {
	probe, probeQuoted := upgradeTrimReply(1)
	if _, fits := trimMemoryUpgrade(probe, probeQuoted, 0, jsonSize); fits {
		t.Fatal("control: a bound of 0 bytes holds nothing")
	}
	oneSmallest := jsonSize(probe)
	pages, _ := probe["memory_upgrade"].(map[string]any)["pages"].([]any)
	if len(pages) != 1 {
		t.Fatalf("control: a batch of one page keeps its page when nothing fits: %d pages", len(pages))
	}
	if head, _ := pages[0].(map[string]any)["article"].(map[string]any); head["lesson"] != nil || pages[0].(map[string]any)["body_included"] != false {
		t.Fatalf("control: the smallest form of a page has no body and no lesson: %v", pages)
	}

	for _, n := range []int{1, 4} {
		reply, quoted := upgradeTrimReply(n)
		taken, fits := trimMemoryUpgrade(reply, quoted, oneSmallest, jsonSize)
		if ids := trimmedPageIDs(reply); !fits || len(ids) != 1 || ids[0] != "page01" {
			t.Errorf("%d pages, a bound that holds one page in its smallest form: the first page stays (fits %v): %v", n, fits, ids)
		}
		if last := taken[len(taken)-1]; n > 1 && !strings.Contains(last, "memory_upgrade.pages[] after the first 1 ") {
			t.Errorf("%d pages: what was taken names the pages that went: %v", n, taken)
		}

		reply, quoted = upgradeTrimReply(n)
		taken, fits = trimMemoryUpgrade(reply, quoted, oneSmallest-1, jsonSize)
		if fits {
			t.Errorf("%d pages, a bound one byte short of the first page: the batch is not kept (it would be of no pages): %v %v", n, taken, trimmedPageIDs(reply))
		}
	}
}

// Through prepare: a bound that does not hold even the first page of the batch (the request is long, and the bound is the smallest the
// CLI takes) leaves memory_upgrade out whole, and output names memory_upgrade, not pages of it. The batch is not given up: nothing was
// reviewed, so the next prepare with room returns every page. The control is the same project with a short request, whose batch
// stays at the same bound.
func TestUpgradeBatchIsLeftOutWholeWhenNotEvenItsFirstPageFits(t *testing.T) {
	repo, root, b := upgradeBatchFixture(t, 3)
	prepareWith := func(session, prompt string, bound int) map[string]any {
		t.Helper()
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		v, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "memory-upgrade", "max_bytes": bound})
		if err != nil {
			t.Fatalf("prepare with max_bytes %d: %v", bound, err)
		}
		return v
	}
	pagesOf := func(v map[string]any) []any {
		u, _ := v["memory_upgrade"].(map[string]any)
		pages, _ := u["pages"].([]any)
		return pages
	}

	control := prepareWith("short", "Check the routing implementation.", workMinBudget)
	if len(pagesOf(control)) == 0 {
		t.Fatalf("control: with a short request the batch stays at %d bytes: %v", workMinBudget, control["output"])
	}

	v := prepareWith("long", strings.Repeat("Check the routing implementation. ", 60), workMinBudget)
	if v["memory_upgrade"] != nil {
		t.Fatalf("not even the first page fits: memory_upgrade is left out, not kept with %d pages", len(pagesOf(v)))
	}
	omitted := []string{}
	for _, name := range v["output"].(map[string]any)["omitted"].([]any) {
		omitted = append(omitted, fmt.Sprint(name))
	}
	if !slices.Contains(omitted, "memory_upgrade") || strings.Contains(strings.Join(omitted, "\n"), "memory_upgrade.pages") {
		t.Errorf("output names the whole batch, not pages of it: %v", omitted)
	}
	if size := jsonSize(v); size > workMinBudget {
		t.Errorf("the reply stays within the bound: %d bytes", size)
	}
	again, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "memory-upgrade", "max_bytes": 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(pagesOf(again)); got != 3 || again["memory_upgrade"].(map[string]any)["reviewed"] != float64(0) {
		t.Errorf("nothing was reviewed, so the next prepare with room returns the whole batch: %d pages", got)
	}
}
