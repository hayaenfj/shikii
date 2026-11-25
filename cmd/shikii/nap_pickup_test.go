package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// `shikii nap` counts whether it caught what it showed uncited: of the conversations a run printed that the agent read (the window moved
// over them), the ones no live article cited when printed (no `lessons`), and how many an article cites by the next run. The count is read from the printed JSON, .shikii/nap-log and the status line
// only, so these tests do not depend on the names inside the command.

type napLastNapOut struct {
	LastNap map[string]int `json:"last_nap"`
}

// napLastNapOf is the last_nap of a printed run (nil when the key is not there).
func napLastNapOf(t *testing.T, out string) map[string]int {
	t.Helper()
	var v napLastNapOut
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	return v.LastNap
}

// napLogRows is the rows of .shikii/nap-log, decoded (none when the file is not there).
func napLogRows(t *testing.T, st store.Store) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(st.Dir, "nap-log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("one JSON object per line: %v\n%q", err, line)
		}
		rows = append(rows, row)
	}
	return rows
}

// napStatusLine is the line of `shikii status` that tells how many uncited conversations an article picked up later ("" when absent).
func napStatusLine(t *testing.T, st store.Store) string {
	t.Helper()
	out, err := captureErr(func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "nap:") {
			return l
		}
	}
	return ""
}

func requireLastNap(t *testing.T, out string, shown, uncited, pickedUp int) {
	t.Helper()
	want := map[string]int{"shown": shown, "uncited": uncited, "picked_up": pickedUp}
	if got := napLastNapOf(t, out); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("last_nap:\n got %v\nwant %v\n%s", got, want, out)
	}
}

func requireNapLogRow(t *testing.T, row map[string]any, shown, uncited, pickedUp int, pickedUpIDs ...string) {
	t.Helper()
	got := map[string]any{"shown": row["shown"], "uncited": row["uncited"], "picked_up": row["picked_up"]}
	want := map[string]any{"shown": float64(shown), "uncited": float64(uncited), "picked_up": float64(pickedUp)}
	ids, _ := row["picked_up_ids"].([]any)
	var gotIDs []string
	for _, id := range ids {
		gotIDs = append(gotIDs, fmt.Sprint(id))
	}
	if fmt.Sprint(got) != fmt.Sprint(want) || fmt.Sprint(gotIDs) != fmt.Sprint(pickedUpIDs) {
		t.Fatalf("nap-log row:\n got %v %v\nwant %v %v", got, gotIDs, want, pickedUpIDs)
	}
	if at, ok := row["at"].(string); !ok {
		t.Fatalf("the row carries the time: %v", row)
	} else if _, err := time.Parse(time.RFC3339, at); err != nil {
		t.Fatalf("the time is RFC 3339: %q %v", at, err)
	}
}

// (1) A run prints three conversations, one already cited by an article. Of the other two, an article that cites one is saved, and the
// agent reads the first two. The next run counts only those two (the window moved over them): shown 2, 1 of them uncited, 1 picked up
// since; the third was not read, so it is not counted and prints again. .shikii/nap-log gains one row (with the id of the one picked up)
// and `shikii status` shows the line. The third is counted once it is read, in a later run, and the line sums every row of the log.
func TestNapCountsTheUncitedConversationsAnArticleCitedAfterTheRun(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-3*time.Hour), "second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-2*time.Hour), "third")

	first, out := decodeNapOutput(t)
	if got := lessonsOf(first); fmt.Sprint(got) != fmt.Sprint(map[string][]string{"0000000000000001": {draft.ID}}) || fmt.Sprint(conversationIDs(first)) != hexIDs(1, 3) {
		t.Fatalf("premise: three conversations are printed and only the first is cited: %v %v", conversationIDs(first), got)
	}
	if napLastNapOf(t, out) != nil || len(napLogRows(t, st)) != 0 || napStatusLine(t, st) != "" {
		t.Fatalf("nothing was shown before the first run: no last_nap, no log, no status line\n%s", out)
	}

	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...) // the third is not read
	second, out := decodeNapOutput(t)
	if fmt.Sprint(conversationIDs(second)) != fmt.Sprint([]string{"0000000000000003"}) {
		t.Fatalf("premise: the third was not read, so the second run prints it again: %v", conversationIDs(second))
	}
	requireLastNap(t, out, 2, 1, 1)
	rows := napLogRows(t, st)
	if len(rows) != 1 {
		t.Fatalf("one row per run that had read conversations shown before: %v", rows)
	}
	requireNapLogRow(t, rows[0], 2, 1, 1, "0000000000000002")
	if got, want := napStatusLine(t, st), "nap: conversations shown while no article cited them: 1, of which an article cited later: 1 (runs: 1)"; got != want {
		t.Fatalf("status line:\n got %q\nwant %q", got, want)
	}

	// Nothing of the third was read before this run, so it is counted by no run yet: no last_nap, no row. It is counted with the fourth
	// once both are read, and the line sums both rows.
	saveConversation(t, st, scope, "0000000000000004", now.Add(-time.Hour), "fourth")
	third, out := decodeNapOutput(t)
	if fmt.Sprint(conversationIDs(third)) != fmt.Sprint([]string{"0000000000000003", "0000000000000004"}) || napLastNapOf(t, out) != nil || len(napLogRows(t, st)) != 1 {
		t.Fatalf("a run after nothing was read has no last_nap and adds no row: %v\n%s", conversationIDs(third), out)
	}
	saveNapArticle(t, book, napDraft(draft, "Fourth article", "0000000000000004"))
	napSendSources(t, st, scope, "0000000000000003", "0000000000000004")
	_, out = decodeNapOutput(t)
	requireLastNap(t, out, 2, 2, 1)
	rows = napLogRows(t, st)
	if len(rows) != 2 {
		t.Fatalf("a second row: %v", rows)
	}
	requireNapLogRow(t, rows[1], 2, 2, 1, "0000000000000004")
	if got, want := napStatusLine(t, st), "nap: conversations shown while no article cited them: 3, of which an article cited later: 2 (runs: 2)"; got != want {
		t.Fatalf("status line sums the rows:\n got %q\nwant %q", got, want)
	}
}

// (2) Sending only sources (the notice that the agent read the conversations) is not an article that cites them: the uncited ones are
// counted as shown and uncited, and none as picked up, in the printed JSON, the log and the status line.
func TestNapDoesNotCountAReadNoticeAsAnArticleCitingTheConversation(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-3*time.Hour), "second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-2*time.Hour), "third")
	first := decodeNap(t)
	if fmt.Sprint(conversationIDs(first)) != hexIDs(1, 3) {
		t.Fatalf("premise: three conversations are printed: %v", conversationIDs(first))
	}
	napSendSources(t, st, scope, hexIDList(1, 3)...)
	live, err := book.Live()
	if err != nil {
		t.Fatal(err)
	}
	if cited := napCitations(live); len(cited["0000000000000002"]) != 0 || len(cited["0000000000000003"]) != 0 {
		t.Fatalf("premise: a read notice is not a citation by an article: %v", cited)
	}
	second, out := decodeNapOutput(t)
	if len(second.Conversations) != 0 {
		t.Fatalf("premise: the three were read, so the second run prints none: %v", conversationIDs(second))
	}
	requireLastNap(t, out, 3, 2, 0)
	rows := napLogRows(t, st)
	if len(rows) != 1 {
		t.Fatalf("one row: %v", rows)
	}
	requireNapLogRow(t, rows[0], 3, 2, 0)
	if got, want := napStatusLine(t, st), "nap: conversations shown while no article cited them: 2, of which an article cited later: 0 (runs: 1)"; got != want {
		t.Fatalf("status line:\n got %q\nwant %q", got, want)
	}
}

// hexIDList is hexIDs as a list.
func hexIDList(from, to int) []string {
	ids := []string{}
	for i := from; i <= to; i++ {
		ids = append(ids, fmt.Sprintf("%016x", i))
	}
	return ids
}
