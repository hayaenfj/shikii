package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func forgetBook(t *testing.T) Book {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Book{Root: root, Scope: "/work/project"}
}

func forgetCapture(t *testing.T, b Book, label string) string {
	t.Helper()
	at := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	o, err := b.CaptureObserved(Observation{Kind: "test", Title: label, Content: "content of " + label, Reference: "forget test", Version: "1", ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// Of the sources a session made, only those nothing names go: an article that cites one, an element of the search journal that stays,
// and a record outside the notebook that the caller says holds one, each keep it. The queue of unreviewed sources loses the ones that
// went, and the wiki's check finds no source missing after.
func TestForgetKeepsASourceSomethingStillNames(t *testing.T) {
	b := forgetBook(t)
	free, cited, journal, held, other := forgetCapture(t, b, "free"), forgetCapture(t, b, "cited"), forgetCapture(t, b, "journal"), forgetCapture(t, b, "held"), forgetCapture(t, b, "other")
	if _, err := b.Ingest(Draft{Sources: []string{cited}, Title: "Forget test lesson", Lesson: "Keep the article.", AppliesTo: "forget tests", Kind: "rule", Scene: "when testing forget"}); err != nil {
		t.Fatal(err)
	}
	searches := filepath.Join(b.Root, "records", "wiki-searches", b.storeName(), ".searches.json")
	if err := os.MkdirAll(filepath.Dir(searches), 0o700); err != nil {
		t.Fatal(err)
	}
	misses := `{"misses":[{"query":"a","session":"0000000000000001","result":"` + free + `"},{"query":"b","session":"0000000000000002","result":"` + journal + `"}]}`
	if err := os.WriteFile(searches, []byte(misses), 0o600); err != nil {
		t.Fatal(err)
	}
	req := ForgetRequest{Owned: []string{free, cited, journal, held}, HeldBy: map[string]string{held: "named by the work state of another session"}, Misses: []string{"0000000000000001"}}
	f := func() *store.Forgotten {
		g, err := (store.Store{Dir: b.Root}).NewForgotten(time.Now(), "ses_x")
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	unlock, err := b.LockForForget()
	if err != nil {
		t.Fatal(err)
	}

	listed, err := b.Forget(req, f(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(listed.Removed, []string{free}) || listed.Misses != 1 || len(listed.Kept) != 3 {
		t.Fatalf("the list is wrong: %+v", listed)
	}
	for id, want := range map[string]string{cited: "cited by the article", journal: "named by the wiki's search records", held: "named by the work state"} {
		if !strings.Contains(listed.Kept[id], want) {
			t.Errorf("source %s is kept for %q, want %q", id, listed.Kept[id], want)
		}
	}
	if _, err := os.Stat(filepath.Join(b.Root, "records", "wiki-sources", b.storeName(), free+".json")); err != nil {
		t.Fatalf("a list removed a source: %v", err)
	}

	res, err := b.Forget(req, f(), true)
	if err != nil || !reflect.DeepEqual(res, listed) {
		t.Fatalf("the application differs from the list: %+v %v", res, err)
	}
	unlock() // the wiki's own readers take the locks forget held
	dir := filepath.Join(b.Root, "records", "wiki-sources", b.storeName())
	for id, stays := range map[string]bool{free: false, cited: true, journal: true, held: true, other: true} {
		_, err := os.Stat(filepath.Join(dir, id+".json"))
		if stays != (err == nil) {
			t.Errorf("source %s: stays=%v but the file is %v", id, stays, err)
		}
	}
	if _, err := b.PendingObservations(); err != nil {
		t.Fatalf("the queue of unreviewed sources cannot be read: %v", err)
	}
	ids, err := capturedIDs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	want := []string{cited, journal, held, other} // the queue keeps the ids of the sources of everyone else
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("the queue holds %v, want %v", ids, want)
	}
	report, err := b.Lint()
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range report.Issues {
		if issue.Kind == "missing-source" {
			t.Errorf("a source is missing after forget: %+v", issue)
		}
	}
	var left struct {
		Misses []struct{ Query string }
	}
	data, _ := os.ReadFile(searches)
	if err := json.Unmarshal(data, &left); err != nil || len(left.Misses) != 1 || left.Misses[0].Query != "b" {
		t.Errorf("the search journal keeps the wrong misses: %s", data)
	}
}
