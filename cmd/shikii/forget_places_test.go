package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// forgetPlaceClasses say how a place in the project's .shikii/ (or the agents' common folder) tells which session a record is of:
//
//	field       a field of each record holds the session id (or its hash)
//	via         no field: the records are reached through an id a session's own records name (a turn, a delivery, a decision, a source)
//	file        the file name is the session id
//	undecided   nothing in a record says it: forget lists it as a candidate at most and never removes it
//	outside     not a session's (the wiki's articles, the store's names, the specs, the place forget keeps what it removed)
//
// The table is the allowlist forget works from: a place that is in none of its rows is a place a session may write and forget does not
// know, and the tests below fail until a person decides which class it is (and, for the first three, which step removes it).
var forgetPlaceStep = map[string]bool{"field": true, "via": true, "file": true}

type forgetPlace struct {
	pattern string
	class   string
	step    string // the step of forget that removes it (empty for undecided and outside)
}

var forgetPlaces = []forgetPlace{
	{`^store/sessions/[^/]+\.(json|files|lines|json\.lock|json\.work\.lock|json\.planning\.lock)$`, "file", "sessions"},
	{`^store/records/guards\.jsonl(\.lock)?$`, "field", "guards"},
	{`^store/records/feedback/[0-9a-f]{64}\.jsonl(\.lock)?$`, "field", "feedback"},
	{`^store/records/memories/[0-9a-f]{64}(\.jsonl\.lock|\.jsonl|\.lock)$`, "field", "memories"},
	{`^store/records/memory-reviews/[0-9a-f]{64}/[0-9a-f]{64}\.json$`, "via", "assessments"},
	{`^store/records/coverage/[0-9a-f]{64}\.jsonl(\.lock)?$`, "field", "coverage"},
	{`^store/records/work-reasoning-[0-9a-f]{64}\.json$`, "field", "reasoning"},
	{`^store/records/wiki-searches/[0-9a-f]{64}/\.searches\.json(\.lock)?$`, "field", "wiki"},
	{`^store/records/wiki-deliveries/[0-9a-f]{64}/(\.deliveries\.json|\.lock)$`, "via", "wiki"},
	{`^store/records/wiki-sources/[0-9a-f]{64}/(\.captured\.json|\.lock)$`, "via", "wiki"},
	{`^store/records/wiki-sources/[0-9a-f]{64}/[^/]+\.(json|reviewed|text\.json)$`, "via", "wiki"},
	{`^store/records/reviews/[^/]+\.(jsonl|lock|jsonl\.lock)$`, "field", "reviews"},
	{`^store/records/text/[0-9a-f]{16}\.json$`, "via", "decisions"},
	{`^store/(records|labels)/[a-z-]+\.jsonl(\.lock)?$`, "via", "decisions"},
	{`^home/agent/latest/([^/]+\.json|\.lock)$`, "file", "agent"},
	{`^home/agent/told\.json(\.lock)?$`, "via", "agent"},
	// not a session's, or nothing in a record tells
	{`^store/records/assignments/`, "undecided", ""},
	{`^store/records/(stopped-lines|coverage-aside|answer-restores)/`, "undecided", ""},
	{`^store/records/(store-name|store-id)$`, "outside", ""},
	{`^store/records/forgotten/`, "outside", ""},
	{`^store/(project\.json|hook\.log|hook\.log\.[0-9]+)$`, "undecided", ""},
	{`^store/(wiki|decisions|handoff|guards)/`, "outside", ""},
	{`^home/agent/`, "outside", ""},
}

var forgetPlaceRows = func() []*regexp.Regexp {
	rows := make([]*regexp.Regexp, len(forgetPlaces))
	for i, p := range forgetPlaces {
		rows[i] = regexp.MustCompile(p.pattern)
	}
	return rows
}()

// forgetPlaceOf is the first row of the table that a path falls under.
func forgetPlaceOf(rel string) (forgetPlace, bool) {
	for i, re := range forgetPlaceRows {
		if re.MatchString(rel) {
			return forgetPlaces[i], true
		}
	}
	return forgetPlace{}, false
}

// forgetUnclassified lists the paths no row of the table takes.
func forgetUnclassified(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, ok := forgetPlaceOf(p); !ok {
			out = append(out, p)
		}
	}
	return out
}

// forgetSourceAreas are the folders of records/ the program's code names: recordPath("<area>", ...) and "records", "<name>".
func forgetSourceAreas(t *testing.T, here string) []string {
	t.Helper()
	pattern := regexp.MustCompile(`(?:"records",\s*"([A-Za-z0-9._-]+)"|recordPath\("([a-z-]+)")`)
	found := map[string]bool{}
	for _, dir := range []string{here, filepath.Join(here, "..", "..", "internal")} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range pattern.FindAllStringSubmatch(string(b), -1) {
				found[m[1]+m[2]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var out []string
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Every file a full flow writes, and every folder of records/ the code names, falls under a row of the table; each row that says a step
// removes it names a step forget has.
func TestForgetKnowsEveryPlaceASessionWrites(t *testing.T) {
	_, file, _, _ := runtime.Caller(0) // the world changes the working folder
	here := filepath.Dir(file)
	w := newForgetWorld(t)
	var paths []string
	for path := range w.snapshot() {
		paths = append(paths, path)
	}
	if len(paths) < 100 {
		t.Fatalf("the flow wrote too little to check the places: %d files", len(paths))
	}
	if left := forgetUnclassified(paths); len(left) > 0 {
		t.Errorf("a file of a full flow is in no place of the table (decide its class in forget_places_test.go, and teach forget a step for it if a session writes it): %v", left)
	}
	// a folder of records/ is known when a file of the shapes the code writes there falls under a row (a folder is never taken by the
	// row of a spec's file records/<spec>.jsonl: that one needs the file name itself)
	h64, h16 := strings.Repeat("0", 64), strings.Repeat("0", 16)
	var unknown []string
	for _, name := range forgetSourceAreas(t, here) {
		shapes := []string{name + "/" + h64 + ".jsonl", name + "/" + h64 + "/" + h64 + ".json", name + "/" + h64 + "/.lock", name + "/" + h64 + "/.searches.json", name + "/" + h16 + ".json", name + "/x", "../" + name + "/x.jsonl"}
		if strings.Contains(name, ".") || name == "store-name" || name == "store-id" {
			shapes = []string{name}
		}
		known := false
		for _, shape := range shapes {
			if _, ok := forgetPlaceOf(filepath.ToSlash(filepath.Clean("store/records/" + shape))); ok {
				known = true
			}
		}
		if !known {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		t.Errorf("the code writes to records/ places the table does not know: %v", unknown)
	}
	steps := map[string]bool{}
	for _, s := range (&forgetRun{}).steps() {
		steps[s.name] = true
	}
	for _, p := range forgetPlaces {
		switch {
		case forgetPlaceStep[p.class] && !steps[p.step]:
			t.Errorf("the place %s is removed by the step %q, which forget does not have", p.pattern, p.step)
		case !forgetPlaceStep[p.class] && p.step != "":
			t.Errorf("the place %s is %s but names a step", p.pattern, p.class)
		}
	}
}

// A place the table does not hold is seen: a file under a new folder of records/, and a row taken out of the table.
func TestForgetPlaceTableSeesAPlaceItDoesNotKnow(t *testing.T) {
	if left := forgetUnclassified([]string{"store/records/a-new-place/" + strings.Repeat("0", 64) + ".jsonl", "store/records/feedback/" + strings.Repeat("0", 64) + ".jsonl"}); len(left) != 1 || !strings.Contains(left[0], "a-new-place") {
		t.Errorf("a file of a new place is not seen: %v", left)
	}
	saved, savedRows := forgetPlaces, forgetPlaceRows
	defer func() { forgetPlaces, forgetPlaceRows = saved, savedRows }()
	for i, p := range saved {
		if p.class == "field" && p.step == "feedback" {
			forgetPlaces = append(append([]forgetPlace{}, saved[:i]...), saved[i+1:]...)
			forgetPlaceRows = append(append([]*regexp.Regexp{}, savedRows[:i]...), savedRows[i+1:]...)
		}
	}
	if left := forgetUnclassified([]string{"store/records/feedback/" + strings.Repeat("0", 64) + ".jsonl"}); len(left) != 1 {
		t.Errorf("a place taken out of the table is still taken: %v", left)
	}
}
