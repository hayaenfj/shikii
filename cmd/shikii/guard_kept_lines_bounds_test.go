package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The table of the uncommitted lines (guard_lines.go): its cost at Stop, the shape it is written in, the files it keeps and
// leaves out, and the lines a turn that was stopped and never fixed cannot slip into it.

func keptFilesOf(k *keptLines) map[string]bool {
	out := map[string]bool{}
	if k != nil {
		for p := range k.files {
			out[p] = true
		}
	}
	return out
}

type keptEntry struct {
	path string
	file keptFile
}

// encodeKeptLines writes a table of these files as the request writes it, in this order, up to max bytes.
func encodeKeptLines(header string, key []byte, entries []keptEntry, max int) []byte {
	table := newKeptTable(header, key, max)
	for _, e := range entries {
		if !table.add(e.path, e.file) {
			break
		}
	}
	return table.bytes()
}

// tableAt builds the table for the working tree of repo as the request does, writes it and reads it back.
func tableAt(t *testing.T, root, repo string) (*keptLines, string) {
	t.Helper()
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: the working tree is listed")
	}
	header := listingHeader("req")
	p := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(p, uncommittedLines(root, repo, header, listing)); err != nil {
		t.Fatal(err)
	}
	return readKeptLines(p, header), listing
}

// Many small files Git does not track are uncommitted at the request; the turn grows each to about 1 MB of short lines. The
// Stop's comparison with their kept tables stays close to the comparison with the request's HEAD (the way a file without a
// table is compared): both run here, on the same machine, so a slower machine slows both.
func TestStopWithKeptTablesStaysNearTheComparisonWithHead(t *testing.T) {
	const files = 100
	repo, root, _ := uncommittedLinesRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < files; i++ {
		writeFile(t, filepath.Join(repo, fmt.Sprintf("data/f%03d.txt", i)), "x\n")
	}
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: listed")
	}
	header := listingHeader("req")
	start := time.Now()
	table := uncommittedLines(root, repo, header, listing)
	t.Logf("request: table %d bytes in %v", len(table), time.Since(start))
	p := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(p, table); err != nil {
		t.Fatal(err)
	}
	kept := readKeptLines(p, header)
	if kept == nil || len(kept.files) != files {
		t.Fatalf("premise: every file kept, got %v", kept)
	}
	big := strings.Repeat("x\ny\n", (guardFileBytesMax-16)/4)
	for i := 0; i < files; i++ {
		writeFile(t, filepath.Join(repo, fmt.Sprintf("data/f%03d.txt", i)), big)
	}
	_, now, _, _, _ := treeStateWithScope(repo)
	before := header + listing
	changed := changedFiles(before, now)
	start = time.Now()
	in := guardTurnInputKept(repo, before, kept, changed, true, "", time.Time{})
	withTable := time.Since(start)
	start = time.Now()
	plain := guardTurnInputKept(repo, before, nil, changed, true, "", time.Time{})
	withHead := time.Since(start)
	t.Logf("Stop over %d changed files: with the kept tables %v, compared with HEAD %v (ratio %.2f)", len(changed), withTable, withHead, float64(withTable)/float64(withHead))
	if len(in.Added) != files || len(plain.Added) != files {
		t.Fatalf("premise: every file has added lines: %d with the tables, %d with HEAD", len(in.Added), len(plain.Added))
	}
	if withTable > 3*withHead {
		t.Fatalf("the Stop's comparison with the kept tables takes %v, more than 3 times the comparison with HEAD (%v)", withTable, withHead)
	}
}

// The record of stopped lines holds one line in each of many files (a turn was stopped across them and nobody fixed them yet).
// A later Stop whose turn changed one other file (its comparison and its write of the record) stays close to the comparison
// with the request's HEAD of the same file, in the same run, and leaves the entries of the files it did not compare as they are.
func TestStopWithAManyFileRecordStaysNearTheComparisonWithHead(t *testing.T) {
	const files = 200
	repo, root, readme := uncommittedLinesRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < files; i++ {
		writeFile(t, filepath.Join(repo, fmt.Sprintf("data/f%03d.txt", i)), "x\n")
	}
	gitCommitAll(t, repo, "data")
	g := guard{ID: "no-word", Kind: guardKindForbiddenWords, Words: []string{"FORBIDDENWORD"}}
	added := map[string][]string{}
	for i := 0; i < files; i++ {
		p := fmt.Sprintf("data/f%03d.txt", i)
		writeFile(t, filepath.Join(repo, p), "x\n"+turnForbiddenLine+"\n")
		added[p] = []string{turnForbiddenLine}
	}
	top := feedbackScope(repo)
	head := gitHead(repo)
	keepStoppedLines(root, []guard{g}, guardInput{repo: top, Added: added}, head)
	record := stoppedRecordOf(root, repo)
	if st := readStoppedTable(record, top); st == nil || len(st.files) != files {
		t.Fatalf("premise: the record holds %d files", files)
	}
	kept := mustRead(t, record)
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: listed")
	}
	before := listingHeader("req") + listing
	writeFile(t, readme, "# project\na clean line\n")
	_, now, _, _, _ := treeStateWithScope(repo)
	changed := changedFiles(before, now)
	if len(changed) != 1 {
		t.Fatalf("premise: one changed file, got %v", changed)
	}
	start := time.Now()
	in := guardTurnInputStopped(root, repo, before, nil, changed, true, head, time.Time{})
	keepStoppedLines(root, nil, in, head)
	withRecord := time.Since(start)
	start = time.Now()
	guardTurnInputKept(repo, before, nil, changed, true, head, time.Time{})
	withHead := time.Since(start)
	t.Logf("Stop over %d changed file, record of %d files: with the record %v, compared with HEAD %v (ratio %.2f)", len(changed), files, withRecord, withHead, float64(withRecord)/float64(withHead))
	if withRecord > 3*withHead {
		t.Fatalf("the Stop with the record takes %v, more than 3 times the comparison with HEAD (%v)", withRecord, withHead)
	}
	if got := mustRead(t, record); string(got) != string(kept) {
		t.Fatal("the Stop changed the entries of files it did not compare")
	}
}

// A path that is not valid UTF-8 (allowed on Linux file systems; Git lists it quoted and gitPath reads back the bytes on disk)
// is found again under its own bytes once the table is written and read back.
func TestKeptTableFindsAPathThatIsNotUTF8(t *testing.T) {
	p := gitPath(`"caf\351.txt"`) // as `git ls-files --stage` prints it
	if p != "caf\xe9.txt" {
		t.Fatalf("premise: gitPath reads the bytes on disk: %q", p)
	}
	blob := strings.Repeat("a", 40)
	key := make([]byte, 32)
	header := listingHeader("req")
	table := encodeKeptLines(header, key, []keptEntry{
		{path: p, file: keptFile{Blob: blob, Lines: map[string]int{lineHash(key, "draft"): 1}}},
		{path: "caf�.txt", file: keptFile{Blob: strings.Repeat("b", 40), Lines: map[string]int{}}},
	}, keptLinesMax)
	path := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(path, table); err != nil {
		t.Fatal(err)
	}
	k := readKeptLines(path, header)
	if _, ok := k.file(p, blob); !ok {
		t.Fatalf("the table kept for %q is not found under its own path; the table holds %v", p, keptFilesOf(k))
	}
	if f, ok := k.file("caf�.txt", strings.Repeat("b", 40)); !ok || len(f.Lines) != 0 {
		t.Fatalf("a path with U+FFFD itself is a different path and keeps its own table: %v %v", f, ok)
	}
}

// A table written before the paths were stored in hex (the path itself as the key) is read without failing, and its files are
// not found: they are compared with HEAD, as without a table. A table of the present shape whose body is broken is read without
// failing too, and a key that is not the hex of a path names no file.
func TestKeptTableInTheOldShapeIsReadWithoutFailing(t *testing.T) {
	header := listingHeader("req")
	blob := strings.Repeat("a", 40)
	bodies := []string{
		`{"README.md":{"blob":"` + blob + `","lines":{"00":1}}}`,
		`{"abcd":{"blob":"` + blob + `","lines":{"00":1}}}`, // a path that is itself hex digits
		`{"zz":{"blob":1}}`,
		`[]`,
		`{"`,
	}
	for _, marker := range []string{"#key ", "#key2 "} {
		for _, body := range bodies {
			path := filepath.Join(t.TempDir(), "s.lines")
			if err := os.WriteFile(path, []byte(header+marker+strings.Repeat("00", 32)+"\n"+body), 0o600); err != nil {
				t.Fatal(err)
			}
			k := readKeptLines(path, header)
			lookups := []string{"README.md", "abcd"}
			if marker == "#key " {
				lookups = append(lookups, "\xab\xcd")
			}
			for _, p := range lookups {
				if _, ok := k.file(p, blob); ok {
					t.Errorf("a table %q%s is used for %q", marker, body, p)
				}
			}
		}
	}
}

// On Linux a file name may be bytes that are not UTF-8: the person's draft in such a file is still not an added line.
func TestUncommittedLinesOfAFileNamedInBytesThatAreNotUTF8(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	p := filepath.Join(repo, "caf\xe9.md")
	if err := os.WriteFile(p, []byte(draftLine+"\n"), 0o644); err != nil {
		t.Skipf("this file system does not take a name that is not UTF-8: %v", err)
	}
	if entries, _ := os.ReadDir(repo); !hasName(entries, "caf\xe9.md") {
		t.Skip("this file system stores the name in another form")
	}
	organizedRequest(t, repo, root, "u")
	writeFile(t, p, draftLine+"\na clean line\n")
	out := runHook(t, "stop", map[string]any{"session_id": "u", "cwd": repo, "last_assistant_message": "added"})
	if stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("a line the person wrote before the request in a file named in bytes that are not UTF-8 stopped the turn: %q", out)
	}
}

func hasName(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

// A large file Git does not track (listed, so not ignored) is not read whole to learn that it is past the 1 MB limit.
func TestLargeUntrackedFileIsNotReadWhole(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	f, err := os.Create(filepath.Join(repo, "big.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(256 << 20); err != nil { // sparse: 256 MB of zero bytes
		t.Fatal(err)
	}
	f.Close()
	listing := "100644 " + strings.Repeat("b", 40) + " 0\tbig.log"
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	uncommittedLines(root, repo, listingHeader("req"), listing)
	runtime.ReadMemStats(&after)
	if read := after.TotalAlloc - before.TotalAlloc; read > 16<<20 {
		t.Fatalf("a file past the 1 MB limit was read whole: %d MB allocated", read>>20)
	}
}

// The limit of one table is the bytes it is written in: the JSON around each file and the paths as they are written count, so
// a table never passes the limit, and the files that do not fit are left out (compared with HEAD) instead of failing.
func TestKeptTableSizeCountsWhatIsWritten(t *testing.T) {
	key := make([]byte, 32)
	header := listingHeader("req")
	var entries []keptEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, keptEntry{path: fmt.Sprintf("%s%03d.md", strings.Repeat("<", 60), i), file: keptFile{Blob: strings.Repeat("a", 40), Lines: map[string]int{lineHash(key, fmt.Sprint(i)): 1}}})
	}
	const max = 2000
	table := encodeKeptLines(header, key, entries, max)
	if len(table) == 0 || len(table) > max {
		t.Fatalf("the table is %d bytes for a limit of %d", len(table), max)
	}
	path := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(path, table); err != nil {
		t.Fatal(err)
	}
	k := readKeptLines(path, header)
	if k == nil || len(k.files) == 0 || len(k.files) >= len(entries) {
		t.Fatalf("the files that fit are kept and the rest left out: %d of %d", len(keptFilesOf(k)), len(entries))
	}
	for i := 0; i < len(k.files); i++ {
		if _, ok := k.file(entries[i].path, entries[i].file.Blob); !ok {
			t.Fatalf("the files are kept in the listing's order: %q is missing", entries[i].path)
		}
	}
}

// Past 16 MB of tables (four files of about 1 MB of distinct short lines), the files that fit are kept and the table stays
// readable; the rest are compared with HEAD.
func TestKeptTablesPast16MBKeepTheFilesThatFit(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	var b strings.Builder
	for i := 0; b.Len() < guardFileBytesMax-16; i++ {
		fmt.Fprintf(&b, "%07d\n", i)
	}
	for i := 0; i < 4; i++ {
		writeFile(t, filepath.Join(repo, fmt.Sprintf("n%d.txt", i)), b.String())
	}
	_, listing, _, _, _ := treeStateWithScope(repo)
	table := uncommittedLines(root, repo, listingHeader("req"), listing)
	if len(table) > keptLinesMax {
		t.Fatalf("the table is %d bytes, past the %d limit", len(table), keptLinesMax)
	}
	k, _ := tableAt(t, root, repo)
	if n := len(keptFilesOf(k)); n == 0 || n >= 4 {
		t.Fatalf("the files that fit in 16 MB are kept and the rest left out: %d of 4 kept", n)
	}
}

// The hashes are keyed with the table's own random key: the plain SHA-256 of a line is not in it, and two requests over the
// same content keep different hashes.
func TestKeptLineHashesAreKeyed(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	k1, _ := tableAt(t, root, repo)
	k2, _ := tableAt(t, root, repo)
	f1, f2 := k1.files["README.md"], k2.files["README.md"]
	plain := sha256.Sum256([]byte(draftLine))
	if len(f1.Lines) != 2 || f1.Lines[hex.EncodeToString(plain[:16])] != 0 {
		t.Fatalf("the table holds the plain hash of a line: %v", f1.Lines)
	}
	if f1.Lines[lineHash(k1.key, draftLine)] != 1 {
		t.Fatalf("premise: the keyed hash of the draft is kept: %v", f1.Lines)
	}
	for h := range f1.Lines {
		if f2.Lines[h] != 0 {
			t.Fatalf("two tables share a hash %s: their keys are not their own", h)
		}
	}
}

// Without a forbidden-words guard nothing reads added lines, so neither a request nor a slash command keeps a table.
func TestNoKeptLinesWithoutAForbiddenWordsGuard(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	if err := os.Remove(guardPath(root, "no-word")); err != nil {
		t.Fatal(err)
	}
	writeGuard(t, root, guard{ID: "pair", Kind: guardKindPairedFiles, Title: "change both", Pairs: [][]string{{"README.md", "README.ja.md"}}})
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	organizedRequest(t, repo, root, "w")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": repo, "prompt": "/commit"})
	if _, err := os.Stat(sessionGuardFilesPath(root, "w")); err != nil {
		t.Fatalf("premise: the slash command opened a stretch: %v", err)
	}
	for _, suffix := range []string{".lines", ".guard.lines"} {
		if _, err := os.Stat(keptLinesPath(root, "w", suffix)); !os.IsNotExist(err) {
			t.Fatalf("a table %s is kept without a forbidden-words guard: %v", suffix, err)
		}
	}
}

// A file whose content on disk is no longer the blob the listing names (it changed after the listing was taken) is not kept;
// the files that still match are.
func TestFileChangedSinceTheListingIsNotKept(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	writeFile(t, filepath.Join(repo, "same.md"), draftLine+"\n")
	writeFile(t, filepath.Join(repo, "moved.md"), draftLine+"\n")
	_, listing, _, _, _ := treeStateWithScope(repo)
	writeFile(t, filepath.Join(repo, "moved.md"), draftLine+"\nwritten after the listing\n")
	header := listingHeader("req")
	p := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(p, uncommittedLines(root, repo, header, listing)); err != nil {
		t.Fatal(err)
	}
	if got := keptFilesOf(readKeptLines(p, header)); !got["same.md"] || got["moved.md"] {
		t.Fatalf("only the file still matching its listed blob is kept: %v", got)
	}
}

// A kept file is used only for the blob it was kept for.
func TestKeptTableIsUsedOnlyForItsBlob(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	writeFile(t, filepath.Join(repo, "draft.md"), draftLine+"\n")
	k, listing := tableAt(t, root, repo)
	blob := strings.Fields(parseListing(listing)["draft.md"])[1]
	if _, ok := k.file("draft.md", blob); !ok {
		t.Fatalf("premise: the file is kept for its blob")
	}
	if _, ok := k.file("draft.md", strings.Repeat("0", len(blob))); ok {
		t.Fatal("the table of a file is used for another blob of it")
	}
}

// Binary files, files past 1 MB and files under .shikii/ are not kept; a small text file next to them is.
func TestBinaryLargeAndShikiiFilesAreNotKept(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	writeFile(t, filepath.Join(repo, "small.md"), draftLine+"\n")
	writeFile(t, filepath.Join(repo, "bin.dat"), "a\x00b\n")
	writeFile(t, filepath.Join(repo, "large.txt"), strings.Repeat("a line\n", guardFileBytesMax/7+1))
	k, listing := tableAt(t, root, repo)
	got := keptFilesOf(k)
	if !got["small.md"] || got["bin.dat"] || got["large.txt"] {
		t.Fatalf("only the small text file is kept: %v", got)
	}
	// .shikii/ is ignored in the project, so a listing that names a file under it is made here, with that file's real blob
	inner := filepath.Join(root, "note.md")
	writeFile(t, inner, draftLine+"\n")
	listing += "\n100644 " + gitBlobID([]byte(draftLine+"\n"), 40) + " 0\t.shikii/note.md"
	header := listingHeader("req")
	p := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(p, uncommittedLines(root, repo, header, listing)); err != nil {
		t.Fatal(err)
	}
	if got := keptFilesOf(readKeptLines(p, header)); got[".shikii/note.md"] || !got["small.md"] {
		t.Fatalf("a file under .shikii/ is kept: %v", got)
	}
}

// A link standing where the listing names a regular file is not followed, even to a file of the same content in the project.
func TestALinkInPlaceOfAListedFileIsNotFollowed(t *testing.T) {
	repo, root, _ := uncommittedLinesRepo(t)
	writeFile(t, filepath.Join(repo, "draft.md"), draftLine+"\n")
	if err := os.Symlink("draft.md", filepath.Join(repo, "link.md")); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
	listing := "100644 " + gitBlobID([]byte(draftLine+"\n"), 40) + " 0\tdraft.md\n100644 " + gitBlobID([]byte(draftLine+"\n"), 40) + " 0\tlink.md"
	header := listingHeader("req")
	p := filepath.Join(t.TempDir(), "s.lines")
	if err := keepLines(p, uncommittedLines(root, repo, header, listing)); err != nil {
		t.Fatal(err)
	}
	if got := keptFilesOf(readKeptLines(p, header)); !got["draft.md"] || got["link.md"] {
		t.Fatalf("a link in place of a listed file was followed: %v", got)
	}
}

// The slash command's table goes with its stretch: the person's next request removes it.
func TestSlashCommandTableGoesWithItsStretch(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "g")
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": repo, "prompt": "/commit"})
	if _, err := os.Stat(keptLinesPath(root, "g", ".guard.lines")); err != nil {
		t.Fatalf("premise: the slash command keeps its table: %v", err)
	}
	organizedRequest(t, repo, root, "g")
	if _, err := os.Stat(keptLinesPath(root, "g", ".guard.lines")); !os.IsNotExist(err) {
		t.Fatalf("the slash command's table is left after its stretch ended: %v", err)
	}
}

// When an organizing regroups the request while the Stop of the same turn is asking the model, the Stop's grouping wins, and
// the kept table follows the listing back to the request the Stop judged.
func TestStopRegroupingMovesTheKeptTable(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "main \u3092\u53d6\u308a\u8fbc\u3093\u3067\u3001\u885d\u7a81\u304c\u3042\u308c\u3070\u76f4\u3057\u3001\u30c6\u30b9\u30c8\u304c\u5168\u90e8\u901a\u308b\u3053\u3068\u3092\u78ba\u304b\u3081\u3066\u304b\u3089\u77e5\u3089\u305b\u3066\u304f\u3060\u3055\u3044\u3002")
	snapshot := loadSession(root, "planning")
	if err := writeAtomic(sessionLinesPath(root, "planning"), []byte(listingHeader(snapshot.Request)+"#key 00\n{}")); err != nil {
		t.Fatal(err)
	}
	in := &hookInput{SessionID: "planning", CWD: repo, LastAssistantMessage: "Merged main."}
	supplyReasoningFixture(root, "planning")
	snapshot = loadSession(root, "planning")
	_, muts, err := hookStop(root, in, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	continuesSubmit(t, root, true) // the organizing lands before the Stop takes the lock
	if b := mustRead(t, sessionLinesPath(root, "planning")); strings.HasPrefix(string(b), listingHeader(snapshot.Request)) {
		t.Fatal("premise: the organizing moved the table to the regrouped request")
	}
	if err := updateSession(root, "planning", func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	s := loadSession(root, "planning")
	if b := mustRead(t, sessionLinesPath(root, "planning")); !strings.HasPrefix(string(b), listingHeader(s.Request)) || s.Request != snapshot.Request {
		t.Fatalf("the kept table must follow the request the Stop judged: request %q, table %q", s.Request, b)
	}
}
