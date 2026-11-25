package main

// What a conversation was when `shikii nap` first printed it: whether a live article cited it. `last_nap` counts a conversation as caught
// when it was printed with no article citing it and one cites it by the time the agent has read it, so the state of the first print has to
// last until the run that counts the conversation. It is kept in .shikii/nap-first, a file of its own: one line per conversation,
// "id state" (state is 1 when an article cited it, else 0). A line is added the first time a run prints the conversation (a conversation
// printed again, or left out of a run in between, keeps the line it has), and taken out when the window moves over the conversation and it
// is counted. No run in between touches a line, so the state cannot be lost the way it was when the next record of what was shown carried
// it (.shikii/nap-shown holds the places the window moves over, and is replaced by every run). A conversation that leaves the record
// without the window moving over it (`shikii forget`) leaves its line behind: the file keeps the newest napFirstKeep lines, and a
// conversation whose line went is not counted (never counted twice).
//
// The file is read and rewritten like the other nap files: only when it is an ordinary file with one name, under its lock, and only while
// it still holds what the run read. A run that finds it changed reads it again; a run that finds it locked leaves it, and the state it had
// to add is added by the next run that prints the conversation (a conversation with no line is not counted: it is never counted twice).

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	napFirstName = "nap-first"
	napFirstMax  = 64 << 10 // bytes read of .shikii/nap-first
	napFirstKeep = 1024     // lines kept, the newest: one is about 20 bytes, so the file is about 20,000 bytes at most
)

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
// newest napFirstKeep lines at most.
func (f napFirst) text(drop map[string]bool, add []napFirstEntry) (text string, changed bool) {
	var lines []string
	line := func(id string, state napCited) {
		word := "0"
		if state == napWasCited {
			word = "1"
		}
		lines = append(lines, id+" "+word+"\n")
	}
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
	return strings.Join(lines, ""), changed
}

// updateNapFirst takes the ids of drop out of .shikii/nap-first and adds the entries, in one rewrite under the file's lock that is made
// only while the file holds what was read. When another run changed it in between, it reads it again (up to three times); when its lock
// is held, it leaves the file as it is. The error is that of a file that cannot be used, and says to remove it. Nothing is written when
// nothing changes, and the file is not made for nothing.
func updateNapFirst(root string, drop map[string]bool, add []napFirstEntry) error {
	path := napFirstPath(root)
	for try := 0; try < 3; try++ {
		f := readNapFirst(root)
		text, changed := f.text(drop, add)
		if !changed {
			return nil
		}
		wrote, err := rewriteNapFileLimit(path, f.raw, text, napFirstMax)
		if err != nil {
			return napFirstError(err, path)
		}
		if wrote {
			return nil
		}
	}
	return nil
}

func napFirstError(err error, path string) error {
	return fmt.Errorf("%w; remove %s (it must be an ordinary file with one name that can be written): until it is removed, no conversation is counted in last_nap", err, path)
}

// countNapRead counts the conversations the window was just moved over (moved, in the order printed): how many of them have a recorded
// state (shown), how many no article cited when first printed (uncited) and how many of those an article cites now (picked_up, with the
// ids in the order printed). The state of a conversation is its line of .shikii/nap-first, else the one an older record of what was shown
// carried. It is nil when none has a state. The lines of the moved conversations are then taken out of .shikii/nap-first, and the count is
// added to .shikii/nap-log: this is where the conversations are counted, once, in the run that wrote the window over them, before anything
// else of the run can fail. The error is that of .shikii/nap-first when it cannot be used.
func countNapRead(root string, moved []napShownConv, cited map[string][]string, at time.Time) (*napLastNap, error) {
	if len(moved) == 0 {
		return nil, nil
	}
	first := readNapFirst(root)
	p := napPickupOf(moved, first.state, cited)
	if p != nil {
		appendNapLog(root, *p, at) // never fails the run
	}
	drop := map[string]bool{}
	for _, c := range moved {
		drop[c.id] = true
	}
	return p, updateNapFirst(root, drop, nil)
}
