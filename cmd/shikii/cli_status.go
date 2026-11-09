package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// cmdStatus matches the store against the specs and shows on one screen: records and labels per decision, label sources and skew,
// whether there is a calibration and whether its key matches. The first screen to look at in operation. Calls neither the model nor anything external.
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	storeDir := bindStore(fs)
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 { // without spec paths, read the store's decisions/*.yaml (base layout), so it counts the same way as the page (audit R3)
		paths = storeSpecPaths(*storeDir)
	}
	st := store.Store{Dir: *storeDir}
	specs := map[string]*spec.Spec{}
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			return err
		}
		specs[sp.ID] = sp
	}
	ids, err := st.SpecIDs()
	if err != nil {
		return err
	}
	for id := range specs {
		if !containsString(ids, id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "no records, no labels, no specs")
		if scope := storeScope(*storeDir); scope != "" {
			printMemoryEffect(stdout, st, scope, time.Now())
		}
		printGuardStats(stdout, *storeDir)
		return nil
	}
	var total personWork
	for _, id := range ids {
		recs, labs, err := st.Counts(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: records=%d labels=%d\n", id, recs, len(labs))
		byKey := map[string]int{}
		bySource := map[string]int{}
		byTruth := map[string]int{}
		var last time.Time
		for _, l := range labs {
			byKey[l.Model+" @ "+l.SpecHash]++
			bySource[l.Source]++
			byTruth[l.Truth]++
			if l.At.After(last) {
				last = l.At
			}
		}
		if len(labs) > 0 {
			fmt.Fprintf(stdout, "  last label: %s\n", last.Format("2006-01-02 15:04"))
			fmt.Fprintf(stdout, "  by source: %s\n", fmtCounts(bySource))
			fmt.Fprintf(stdout, "  by truth:  %s\n", fmtCounts(byTruth))
			if sp := specs[id]; bySource["outcome"] > 0 && outcomeCheckSpecs[id] && sp != nil {
				fmt.Fprintln(stdout, outcomeCheckLine(store.OutcomeAgreement(labs), sp.Thresholds.Act))
			}
		}
		// Counts per work (the mark on hook records. One store collects decisions from several projects, so show how many where. There is one line overall)
		if all, err := st.Records(id, 0); err == nil {
			byProject := map[string]int{}
			for _, r := range all {
				if r.Project != "" {
					byProject[r.Project]++
				}
			}
			if len(byProject) > 0 {
				fmt.Fprintf(stdout, "  by project: %s\n", fmtCounts(byProject))
			}
		}
		sp := specs[id]
		if f, err := st.Funnel(id, sp != nil && decide.DiscardUnsure(sp)); err == nil && f.Records > 0 {
			// Counts per rung. Progress is human judgments per 1000 decisions (those that stayed ask and went to a person)
			fmt.Fprintf(stdout, "  rungs: settled by the minimal question %d, by added context %d, left to a person %d (%.0f per 1000 decisions)\n", f.Minimal, f.Context, f.Human, f.HumanPerThousand())
		}
		if all, err := st.Records(id, 0); err == nil && len(all) > 0 {
			reviews, _ := st.Reviews(id)
			w := countPersonWork(id, sp != nil && decide.DiscardUnsure(sp), all, labs, reviews, time.Now())
			w.print(stdout, "  ")
			total.add(w)
		}
		if len(labs) > 0 {
			audited := map[string]bool{} // count by decision (trace). A decision that went through context has its answer on 2 rungs, but a person checks it once
			for _, l := range labs {
				if l.Source == "audit" {
					audited[l.TraceID] = true
				}
			}
			// A different number from the receipt's "human-checked act decisions": this is the number of decisions a person answered in the sampling. The receipt counts all human truths that this line would put in act
			fmt.Fprintf(stdout, "  audit: 1 in 20 act decisions is shown to a person; audit answers so far: %d\n", len(audited))
		}
		for key, n := range byKey {
			note := ""
			if sp != nil {
				parts := strings.SplitN(key, " @ ", 2)
				rs := sp // the rung for this hash
				if parts[1] == sp.ContextHash() {
					rs = sp.ContextSpec()
					note = "  (context rung)"
				}
				// the count is the page's (keys[].n) and calibration's: the latest truth per input, from people and results only (audit 0.9.3).
				// Without a spec it stays the number of labels for the key
				n = lineStateOf(rs, sp.Eval.MinN, parts[0], parts[1], labs, false).N
				switch {
				case !sp.LiveHash(parts[1]):
					note = "  (stale hash: spec wording changed since; not used)"
				case rs.CalibrationFor(parts[0]) != nil:
					note += "  calibrated: " + rs.CalibrationFor(parts[0]).Method
				default:
					// Do not say "a line can be drawn" from counts alone (0.8.3): separate only one kind of answer, tried but could not separate, and on the way to drawing automatically
					tried := (parts[1] == sp.Hash() || parts[1] == sp.ContextHash()) && triedWithoutLine(*storeDir, sp, parts[0]) // the context rung is tried at the same time
					if stop, ok := readStopped(*storeDir, sp); ok && parts[1] == sp.Hash() {                                      // the wording's stop, on every model's line
						note += stop.stopNote()
						break
					}
					note += lineStateOf(rs, sp.Eval.MinN, parts[0], parts[1], labs, tried).statusNote(rs, sp.Eval.MinN)
				}
			}
			fmt.Fprintf(stdout, "  %-50s %4d%s\n", key, n, note)
		}
		if sp == nil {
			fmt.Fprintln(stdout, "  (spec not given; pass its path to see calibration state)")
		}
	}
	if len(ids) > 1 && total.Records > 0 {
		total.print(stdout, "all specs: ")
	}
	if scope := storeScope(*storeDir); scope != "" { // effect of delivered lessons (the store's parent is the project)
		printMemoryEffect(stdout, st, scope, time.Now())
	}
	printGuardStats(stdout, *storeDir)               // blocking checks (0.7.3): how many are on, how many blocked, how many fixed
	if scope := storeScope(*storeDir); scope != "" { // the page (0.8.0): one per project, on a free port
		if url := livePage(scope); url != "" {
			fmt.Fprintf(stdout, "page: %s/ (up)\n", url)
		} else {
			fmt.Fprintln(stdout, "page: not running (the first request's hook starts it on a free port and opens the browser)")
		}
	}
	return nil
}
