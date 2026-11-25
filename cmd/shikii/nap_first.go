package main

// What a conversation was when `shikii nap` first printed it: whether a live article cited it. `last_nap` counts a conversation as caught
// when it was printed with no article citing it and one cites it by the time the agent has read it, so the state of the first print has to
// last until the run that counts the conversation. It is kept in .shikii/nap-first, a file of its own: one line per conversation,
// "id state" (state is 1 when an article cited it, else 0). A line is added the first time a run prints the conversation (a conversation
// printed again, or left out of a run in between, keeps the line it has), and taken out when the window moves over the conversation and it
// is counted. No run in between touches a line, so the state cannot be lost the way it was when the next record of what was shown carried
// it (.shikii/nap-shown holds the places the window moves over, and is replaced by every run). A conversation that leaves the record
// without the window moving over it leaves its line behind: the file keeps the newest napFirstKeep lines, and `shikii forget` takes out
// the lines of the conversations it forgets; a conversation whose line went is not counted (never counted twice).
//
// Two runs of `shikii nap` at the same time must not lose or repeat a count, so every change of the file is made under a lock that is
// waited for (napFirstLockWait at most) and the file is replaced whole:
//
//   - the lock is .shikii/nap-first.lock, a file of its own: the file itself is replaced by a rename, and a lock on a file that is
//     replaced locks nothing. It is taken the way the other nap files are opened: an ordinary file with one name, never through a link.
//   - a run that adds lines, and a run that counts, read the file under the lock. The counting run reads, counts and takes the counted
//     lines out under the one lock: what it reads is what it changes.
//   - the new content is written to .shikii/nap-first.tmp and swapped in by a rename, so a reader that takes no lock never sees a file
//     that is half written or empty. The counting run writes the temp file before it moves the window and swaps it in after: a failure
//     to write (no room, no permission) is found while nothing has moved, the window is then left where it is, and the conversations
//     are counted by a later run; a swap after the window moved cannot fail for those reasons.
//   - a run that cannot get the lock in time does not go on without it. The counting run leaves the window where it is (the conversations
//     print again and are counted once, by a later run that moves the window over them) and says so in its error; the run that adds
//     lines leaves the record of what was shown as it was (the conversations print again and get their lines then) and says so.
//
// The destination is checked before a rename (a symbolic link or a hard link is a file of the person's: a rename over it would replace
// the link itself, so it is refused, as the other nap files are not written through).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	napFirstName     = "nap-first"
	napFirstLockName = "nap-first.lock"
	napFirstTmpName  = "nap-first.tmp"
	napFirstMax      = 64 << 10 // bytes read of .shikii/nap-first
	napFirstKeep     = 1024     // lines kept, the newest: one is about 20 bytes, so the file is about 20,000 bytes at most
)

// napFirstLockWait is how long a run waits for the lock of .shikii/nap-first when another run holds it. The lock is held for a read and
// the write of a small file, so a moment is enough; a lock still held after this is a run that has stopped. Tests set it to an hour with
// beforeNapFirstLockTry that lets the other run finish at the try they choose, or to 0 for a run that gives up after one try.
var napFirstLockWait = 2 * time.Second

// beforeNapFirstLockTry runs right before each try to take the lock of .shikii/nap-first; a test plays the other run there.
var beforeNapFirstLockTry = func(attempt int) {}

// beforeNapFirstSwap runs after the new content of .shikii/nap-first is written to its temp file and before it is swapped in, with the
// lock held; a test looks at what a reader that takes no lock would see.
var beforeNapFirstSwap = func() {}

// errNapFirstBusy is what a run gets when the lock of .shikii/nap-first was still held after napFirstLockWait.
var errNapFirstBusy = errors.New("another `shikii nap` held .shikii/nap-first.lock for more than the time to wait")

func napFirstPath(root string) string { return filepath.Join(root, napFirstName) }

// napFirstEntry is a conversation printed for the first time and whether an article cited it then.
type napFirstEntry struct {
	id    string
	state napCited
}

// napFirst is the content of .shikii/nap-first: the lines that hold an id and a state, in the order they are in the file, the first line
// of an id standing. A file that cannot be used is the same as no file.
type napFirst struct {
	raw   string
	state map[string]napCited
	ids   []string
}

func readNapFirst(root string) napFirst {
	f := napFirst{raw: readNapStateAtLimit(napFirstPath(root), napFirstMax), state: map[string]napCited{}}
	for _, line := range strings.Split(f.raw, "\n") {
		id, word, ok := strings.Cut(line, " ")
		if !ok || id == "" || strings.Contains(word, " ") {
			continue
		}
		state := napCitedUnknown
		switch word {
		case "0":
			state = napWasUncited
		case "1":
			state = napWasCited
		}
		if _, dup := f.state[id]; state == napCitedUnknown || dup {
			continue
		}
		f.state[id] = state
		f.ids = append(f.ids, id)
	}
	return f
}

// text is what the file holds once the ids of drop are out and the entries are in (an entry of an id that has a line is not added), the
// newest napFirstKeep lines at most; changed says whether that differs from what the file holds.
func (f napFirst) text(drop map[string]bool, add []napFirstEntry) (text string, changed bool) {
	var lines []string
	line := func(id string, state napCited) { lines = append(lines, napFirstLine(id, state)+"\n") }
	for _, id := range f.ids {
		if drop[id] {
			changed = true
			continue
		}
		line(id, f.state[id])
	}
	added := map[string]bool{}
	for _, e := range add {
		if _, has := f.state[e.id]; (has && !drop[e.id]) || added[e.id] {
			continue
		}
		if e.id == "" || strings.ContainsAny(e.id, " \n") || e.state == napCitedUnknown {
			continue
		}
		added[e.id] = true
		line(e.id, e.state)
		changed = true
	}
	if len(lines) > napFirstKeep {
		lines, changed = lines[len(lines)-napFirstKeep:], true
	}
	text = strings.Join(lines, "")
	return text, changed || text != f.raw
}

// napFirstLine is a line of the file without its newline.
func napFirstLine(id string, state napCited) string {
	if state == napWasCited {
		return id + " 1"
	}
	return id + " 0"
}

func napFirstError(err error, path string) error {
	return fmt.Errorf("%w; remove %s (it must be an ordinary file with one name that can be written): until it is removed, no conversation is counted in last_nap", err, path)
}

// napFirstHold is the lock of .shikii/nap-first, held until release.
type napFirstHold struct {
	root string
	lock *os.File
}

// lockNapFirst takes the lock of .shikii/nap-first, waiting up to napFirstLockWait for a run that holds it. It returns errNapFirstBusy
// when the lock is still held then, and an error that says to remove the lock file when it is not an ordinary file with one name.
func lockNapFirst(root string) (*napFirstHold, error) {
	path := filepath.Join(root, napFirstLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, napFirstError(err, path)
	}
	if !napStateUsable(f) {
		f.Close()
		return nil, napFirstError(errors.New("the nap state must be a regular file with one name"), path)
	}
	deadline := time.Now().Add(napFirstLockWait)
	for attempt := 0; ; attempt++ {
		beforeNapFirstLockTry(attempt)
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			return &napFirstHold{root, f}, nil
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, errNapFirstBusy
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *napFirstHold) release() {
	syscall.Flock(int(h.lock.Fd()), syscall.LOCK_UN)
	h.lock.Close()
}

// napFirstEdit is new content of .shikii/nap-first written to its temp file and not yet swapped in.
type napFirstEdit struct {
	root string
}

// checkNapFirstDest is nil when .shikii/nap-first is not there or is an ordinary file with one name, and else the error that says why a
// rename over it is refused.
func checkNapFirstDest(root string) error {
	path := napFirstPath(root)
	if err := napFileProblem(path); err != nil {
		return napFirstError(err, path)
	}
	return nil
}

// stage writes text to the temp file (an ordinary file with one name, made empty first) and returns what swaps it in. Nothing of
// .shikii/nap-first changes: a failure here leaves it as it was.
func (h *napFirstHold) stage(text string) (*napFirstEdit, error) {
	if err := checkNapFirstDest(h.root); err != nil {
		return nil, err
	}
	path := filepath.Join(h.root, napFirstTmpName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, napFirstError(err, path)
	}
	defer f.Close()
	if !napStateUsable(f) {
		return nil, napFirstError(errors.New("the nap state must be a regular file with one name"), path)
	}
	if err := f.Truncate(0); err != nil {
		return nil, napFirstError(err, path)
	}
	if _, err := f.WriteAt([]byte(text), 0); err != nil {
		os.Remove(path)
		return nil, napFirstError(err, path)
	}
	return &napFirstEdit{h.root}, nil
}

// swap makes the staged content .shikii/nap-first by one rename; the destination is checked again first.
func (e *napFirstEdit) swap() error {
	beforeNapFirstSwap()
	if err := checkNapFirstDest(e.root); err != nil {
		e.discard()
		return err
	}
	if err := os.Rename(filepath.Join(e.root, napFirstTmpName), napFirstPath(e.root)); err != nil {
		e.discard()
		return napFirstError(err, napFirstPath(e.root))
	}
	return nil
}

// discard removes the temp file: the staged content is not used.
func (e *napFirstEdit) discard() { os.Remove(filepath.Join(e.root, napFirstTmpName)) }

// addNapFirst adds the lines of the entries (the conversations a run printed for the first time) under the lock, reading the file under
// it. It returns errNapFirstBusy when the lock could not be had in time, and nothing is then added.
func addNapFirst(root string, add []napFirstEntry) error {
	if len(add) == 0 {
		return nil
	}
	h, err := lockNapFirst(root)
	if err != nil {
		return err
	}
	defer h.release()
	text, changed := readNapFirst(root).text(nil, add)
	if !changed {
		return nil
	}
	edit, err := h.stage(text)
	if err != nil {
		return err
	}
	return edit.swap()
}

// settleAndCountNapRead moves .shikii/nap-window-end over the first n conversations of shown (the ones the agent read) and counts them,
// all under the one lock of .shikii/nap-first: it reads the file, works out what is counted and what the file holds once the counted
// lines are out, writes that to the temp file, moves the window, and only when it is this run that wrote the window adds the line to
// .shikii/nap-log and swaps the file in. Only the run that wrote the window counts, and it counts there and then, before anything else
// of the run can fail.
//
// It returns the ids of the conversations that are printed again (those after the first n when the window moved; all of them when it did
// not), what was counted (nil when nothing has a state) and the error to return once the report is printed. When the lock cannot be had
// in time, or the temp file cannot be written, the window is not moved: the error says so, and a later run does both.
func settleAndCountNapRead(root string, shown napShown, n int, cited map[string][]string, at time.Time) (unread []string, last *napLastNap, err error) {
	all := func() (ids []string) {
		for _, c := range shown.convs {
			ids = append(ids, c.id)
		}
		return ids
	}
	notMoved := func(cause error) ([]string, *napLastNap, error) {
		if errors.Is(cause, errNapFirstBusy) {
			cause = fmt.Errorf("%w, so the conversations you read were not moved past and not counted yet: they are printed again: send their ids again once you have read them, and a later `shikii nap` moves the window over them and counts them", cause)
		} else {
			cause = fmt.Errorf("the conversations you read could not be moved past or counted: %w", cause)
		}
		return all(), nil, cause
	}
	h, err := lockNapFirst(root)
	if err != nil {
		return notMoved(err)
	}
	defer h.release()
	moved := shown.convs[:n]
	first := readNapFirst(root)
	drop := map[string]bool{}
	for _, c := range moved {
		drop[c.id] = true
	}
	var edit *napFirstEdit
	if text, changed := first.text(drop, nil); changed {
		if edit, err = h.stage(text); err != nil {
			return notMoved(err)
		}
	}
	wrote, err := moveNapWindow(root, shown, n)
	if err != nil || !wrote {
		if edit != nil {
			edit.discard()
		}
		if err != nil {
			return all()[n:], nil, err
		}
		return all()[n:], nil, nil // another run moved the window first, and counted them
	}
	last = napPickupOf(moved, first.state, cited)
	if last != nil {
		appendNapLog(root, *last, at) // never fails the run
	}
	if edit != nil {
		if err := edit.swap(); err != nil {
			return all()[n:], last, err
		}
	}
	return all()[n:], last, nil
}

// forgetNapFirst takes the lines of the conversations in ids out of .shikii/nap-first, for `shikii forget`: it returns how many there
// are (only counting, with apply false). With apply it holds the lock, hands the removed lines to keep before it changes anything, and
// swaps the rest in. A file that is not there, or has none of them, is left as it is.
func forgetNapFirst(root string, ids map[string]bool, apply bool, keep func(removed [][]byte) error) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if !apply {
		return napFirstLinesOf(readNapFirst(root), ids), nil
	}
	h, err := lockNapFirst(root)
	if err != nil {
		return 0, err
	}
	defer h.release()
	f := readNapFirst(root)
	n := napFirstLinesOf(f, ids)
	if n == 0 {
		return 0, nil
	}
	var removed [][]byte
	for _, id := range f.ids {
		if ids[id] {
			removed = append(removed, []byte(napFirstLine(id, f.state[id])))
		}
	}
	if err := keep(removed); err != nil {
		return 0, err
	}
	text, _ := f.text(ids, nil)
	edit, err := h.stage(text)
	if err != nil {
		return 0, err
	}
	return n, edit.swap()
}

func napFirstLinesOf(f napFirst, ids map[string]bool) int {
	n := 0
	for _, id := range f.ids {
		if ids[id] {
			n++
		}
	}
	return n
}
