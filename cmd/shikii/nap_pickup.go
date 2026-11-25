package main

// What `shikii nap` caught of what it showed uncited. A conversation printed while no live article cited it (no `lessons`) is a
// conversation the wiki may hold nothing of. When an article cites it by the next run, the agent's reading of it reached the wiki. The
// count needs no new operation from the agent: .shikii/nap-shown keeps, for every conversation a run printed, whether an article cited it
// then, and the next run looks at the live articles again (the same read it makes for `lessons`). Only an article counts: the sources the
// agent sends to say it read a conversation are not saved as an article's source, so a read notice alone never raises the count.
//
// Only the conversations the agent has read are counted: the ones the window has just moved over (the first ones of .shikii/nap-shown,
// in a run, that are all in .shikii/nap-read). A conversation that was not read prints again and is counted by the run that moves the
// window over it, once, whatever happened to it in between.
//
// Each run that moved the window over conversations with a recorded state prints the counts as last_nap
// ({"shown":N,"uncited":U,"picked_up":K}) and, once it has printed and replaced .shikii/nap-shown, adds one JSON line to
// .shikii/nap-log (the time, the three counts and the ids picked up). `shikii status` sums every line of the log. Both files are ordinary files with one name or nothing: a log that cannot be written never
// fails a run (one line a day does not need a size limit).

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

// napCited is whether an article cited a conversation when a run printed it, as .shikii/nap-shown records it.
type napCited byte

const (
	napCitedUnknown napCited = iota // a record from before this was kept: the conversation is not counted
	napWasUncited
	napWasCited
)

// napPickupOf compares the first moved conversations of a record of .shikii/nap-shown (raw is its text; moved is how many of them the
// window has been moved over: the ones read) with the live articles' citations (cited, by conversation id) as they are now. It is nil
// when the record is not there or cannot be read, and when none of those conversations has a recorded state.
func napPickupOf(raw string, cited map[string][]string, moved int) *napLastNap {
	shown, ok := parseNapShown(raw)
	if !ok {
		return nil
	}
	var p napLastNap
	for _, c := range shown.convs[:min(moved, len(shown.convs))] {
		if c.cited == napCitedUnknown {
			continue
		}
		p.Shown++
		if c.cited == napWasCited {
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
// or into a hard-linked file) and never fails the run: a line that cannot be written is not counted.
func appendNapLog(root string, p napLastNap, at time.Time) {
	row := napLogRow{At: at.UTC().Format(napTimeLayout), Shown: p.Shown, Uncited: p.Uncited, PickedUp: p.PickedUp, PickedUpIDs: p.ids}
	if row.PickedUpIDs == nil {
		row.PickedUpIDs = []string{} // printed as [], not null
	}
	line, err := json.Marshal(row)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(root, napLogName), os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if !napStateUsable(f) {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}

// readNapLog sums the lines of .shikii/nap-log: the uncited conversations, those picked up later and the number of lines. A line that is
// not a row is skipped. The file counts as not there (runs 0) when it is not an ordinary file with one name.
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
		fmt.Fprintf(out, "nap: conversations shown while no article cited them: %d, of which an article cited later: %d (runs: %d)\n", uncited, pickedUp, runs)
	}
}
