package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The internal records (.shikii/) are the hooks' own writing, not the person's files: they stay out of the listing and of the hash
// that say which files changed, whether git ignores them (the usual case, info/exclude), does not ignore them yet, or tracks them
// although an ignore rule names them. A change of a real file still changes both (the control).
func TestTheInternalRecordsStayOutOfTheListingAndTheHash(t *testing.T) {
	patientGit(t)
	for _, c := range []struct {
		name    string
		ignored bool // /.shikii/ in info/exclude
		tracked bool // committed before the rule
	}{
		{"not ignored", false, false},
		{"ignored", true, false},
		{"tracked and ignored", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := gitRepo(t)
			write := func(name, body string) {
				t.Helper()
				p := filepath.Join(repo, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("a.txt", "a\n")
			write(".shikiix", "next to the records, not one of them\n")
			write(".shikii/wiki/page.md", "one\n")
			write(".shikii/wiki/\u65e5\u672c\u8a9e.md", "one\n") // a name git quotes in a listing
			if c.tracked {
				gitCommitAll(t, repo, "records tracked")
			}
			if c.ignored {
				if err := os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("/.shikii/\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			hash, listing, listed, _ := treeState(repo)
			if hash == "" || !listed || !strings.Contains(listing, "\ta.txt") || !strings.Contains(listing, "\t.shikiix") {
				t.Fatalf("premise: the files of the person are listed: hash %q listed %v\n%s", hash, listed, listing)
			}
			if strings.Contains(listing, "\t.shikii/") || strings.Contains(listing, "\t\".shikii/") {
				t.Fatalf("the internal records are in the listing:\n%s", listing)
			}
			write(".shikii/wiki/page.md", "two, written by a hook\n")
			write(".shikii/records/new.json", "{}\n")
			if again, relisted, ok, _ := treeState(repo); again != hash || relisted != listing || !ok {
				t.Errorf("a change of the internal records is a change of the tree: hash %q -> %q\n%s\nwas\n%s", hash, again, relisted, listing)
			}
			write("a.txt", "changed by the person\n")
			if changed, relisted, _, _ := treeState(repo); changed == hash || relisted == listing {
				t.Errorf("a change of a real file must change the hash and the listing: %q\n%s", changed, relisted)
			}
		})
	}
}
