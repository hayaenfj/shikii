package main

// What `shikii nap` caught of what it showed uncited. A conversation printed while no live article cited it (no `lessons`) is a
// conversation the wiki may hold nothing of. When an article cites it by the time the agent has read it, the agent's reading of it reached
// the wiki. The count needs no new operation from the agent: .shikii/nap-first keeps, for every conversation a run printed for the first
// time, whether an article cited it then (nap_first.go), and the run that moves the window over it looks at the live articles again (the
// same read it makes for `lessons`). Only an article counts: the sources the agent sends to say it read a conversation are not saved as an
// article's source, so a read notice alone never raises the count.
//
// Only the conversations the agent has read are counted: the ones the window has just moved over (the first ones of .shikii/nap-shown, in a
// run, that are all in .shikii/nap-read). A conversation that was not read prints again and is counted by the run that moves the window
// over it, once, whatever happened to it in between (a run that left it out, or printed it again).
//
// A run that wrote the window over conversations with a recorded state counts them there and then (countNapRead): it adds one JSON line to
// .shikii/nap-log (the time, the three counts and the ids picked up) and prints the counts as last_nap. That is before the report is built,
// so nothing that fails after the window moved can lose the count, and only the run that wrote the window counts, so two runs at the same
// time count the conversations once. `shikii status` sums every line of the log. The log is an ordinary file with one name or nothing: a
// log that cannot be written never fails a run (one line a day does not need a size limit).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// napLogName is the file under the store's root that holds one JSON line for each run that had a record of the run before it.
const napLogName = "nap-log"

// napLastNap is what the run before this one printed, looked at again now, for the conversations the agent read since (the window moved
// over them): how many there are (shown) whose state was recorded, how many of them no article cited when printed (uncited), and how
// many of those an article cites now (picked_up, ids in the order they were printed).
type napLastNap struct {
	Shown    int `json:"shown"`
	Uncited  int `json:"uncited"`
	PickedUp int `json:"picked_up"`

	ids []string // not printed in the report; the log keeps them
}

// napCited is whether an article cited a conversation when a run first printed it, as .shikii/nap-first records it (and, in a record of
// what was shown from before that file, as the fourth field of a line of .shikii/nap-shown did).
type napCited byte

const (
	napCitedUnknown napCited = iota // not recorded: the conversation is not counted
	napWasUncited
	napWasCited
)

// napPickupOf counts the conversations the window has just moved over (moved): the state of each is first[id], else the one the record of
// what was shown carried; cited is the live articles' citations by conversation id as they are now. It is nil when none has a state.
func napPickupOf(moved []napShownConv, first map[string]napCited, cited map[string][]string) *napLastNap {
	var p napLastNap
	for _, c := range moved {
		state, ok := first[c.id]
		if !ok {
			state = c.cited
		}
		if state == napCitedUnknown {
			continue
		}
		p.Shown++
		if state == napWasCited {
			continue
		}
		p.Uncited++
		if len(cited[c.id]) > 0 {
			p.PickedUp++
			p.ids = append(p.ids, c.id)
		}
	}
	if p.Shown == 0 {
		return nil
	}
	return &p
}

type napLogRow struct {
	At          string   `json:"at"`
	Shown       int      `json:"shown"`
	Uncited     int      `json:"uncited"`
	PickedUp    int      `json:"picked_up"`
	PickedUpIDs []string `json:"picked_up_ids"`
}

// appendNapLog adds the line for a run to .shikii/nap-log. It writes only to an ordinary file with one name (never through a symbolic link
// or into a hard-linked file) and never fails the run: a line that cannot be written is not counted. When the last line of the file is not
// ended by a newline (a run stopped in the middle of writing it), the new line starts on a line of its own, so the two are not joined and
// lost together.
func appendNapLog(root string, p napLastNap, at time.Time) {
	row := napLogRow{At: at.UTC().Format(napTimeLayout), Shown: p.Shown, Uncited: p.Uncited, PickedUp: p.PickedUp, PickedUpIDs: p.ids}
	if row.PickedUpIDs == nil {
		row.PickedUpIDs = []string{} // printed as [], not null
	}
	line, err := json.Marshal(row)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(root, napLogName), os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if !napStateUsable(f) {
		return
	}
	line = append(line, '\n')
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], info.Size()-1); err == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	_, _ = f.Write(line) // one write: with O_APPEND the line is not split by another writer's
}

// readNapLog sums the lines of .shikii/nap-log: the uncited conversations, those picked up later and the number of lines. A line that is
// not a row is skipped, and the lines around it count. The file counts as not there (runs 0) when it is not an ordinary file with one name.
func readNapLog(root string) (uncited, pickedUp, runs int) {
	f, err := os.OpenFile(filepath.Join(root, napLogName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()
	if !napStateUsable(f) {
		return 0, 0, 0
	}
	data, _ := io.ReadAll(f)
	for _, line := range strings.Split(string(data), "\n") {
		var row napLogRow
		if json.Unmarshal([]byte(line), &row) != nil || row.Shown <= 0 {
			continue
		}
		uncited += row.Uncited
		pickedUp += row.PickedUp
		runs++
	}
	return uncited, pickedUp, runs
}

// printNapPickup is the line of `shikii status` that sums .shikii/nap-log; nothing is printed when the log has no line.
func printNapPickup(out io.Writer, root string) {
	if uncited, pickedUp, runs := readNapLog(root); runs > 0 {
		fmt.Fprintf(out, "nap: conversations read while no article cited them: %d, of which an article cites now: %d (runs: %d)\n", uncited, pickedUp, runs)
	}
}
