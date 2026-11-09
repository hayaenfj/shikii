package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// saveTuneResult keeps the result in a json next to the run page (read by the page and MCP). Does nothing without a wiki.
func saveTuneResult(res *tuneResult) {
	if res.RunPage == "" {
		return
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(strings.TrimSuffix(res.RunPage, ".md")+".json", b, 0o644)
}

// appendRunNote adds one line to the run's md.
func appendRunNote(runPage, line string) {
	if runPage == "" {
		return
	}
	if f, err := os.OpenFile(runPage, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString("\n" + line + "\n")
		f.Close()
	}
}

// loadTuneResult reads the json for a run name.
func loadTuneResult(wikiDir, run string) (*tuneResult, error) {
	run = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(run), ".json"), ".md")
	b, err := os.ReadFile(filepath.Join(wikiDir, "runs", run+".json"))
	if err != nil {
		return nil, err
	}
	var res tuneResult
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}
	res.rebase(wikiDir, run)
	return &res, nil
}

// rebase rebuilds the absolute paths kept in the json from the wiki it was read from (audit 0.9.2). After the project folder was moved, the recorded
// paths point at a place that is gone; in a copied project they point at the original project, which still exists. Either way every reader and writer
// must stay inside this project: run_page is the md next to the json, and a spec or adopted version that was inside the store the run was written in
// (the store of the recorded run_page, normally <store>/decisions/<file>) is the same file in this store. An undone version keeps its recorded name
// (only the file on disk gets ".reverted"), so it is found the same way. A spec outside that store (serve --specs, or tune on a file elsewhere) keeps its path.
func (res *tuneResult) rebase(wikiDir, run string) {
	if abs, err := filepath.Abs(wikiDir); err == nil {
		wikiDir = abs
	}
	recorded := res.RunPage
	res.RunPage = filepath.Join(wikiDir, "runs", run+".md")
	if recorded == "" {
		return
	}
	oldStore, store := filepath.Dir(filepath.Dir(filepath.Dir(recorded))), filepath.Dir(wikiDir)
	for _, p := range []*string{&res.SpecPath, &res.AdoptedPath} {
		if *p == "" {
			continue
		}
		rel, err := filepath.Rel(oldStore, *p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		*p = filepath.Join(store, rel)
	}
}

// lockTuneRun takes the lock of one run (a file next to its json) while it is adopted or undone. The agent's adoption (hook variants check, a separate
// process) and the page's (serve) share it, so the same run is not adopted twice at the same time (audit 0.9.2, low 2). It does not wait: when the run
// is held, it returns errTuneRunBusy and the caller refuses.
func lockTuneRun(wikiDir, run string) (func(), error) {
	run = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(run), ".json"), ".md")
	f, err := os.OpenFile(filepath.Join(wikiDir, "runs", run+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errTuneRunBusy
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

var errTuneRunBusy = errors.New("this run is being adopted or undone right now (by the agent or on the page); try again after it finishes")

// latestTuneResult is the json of the spec's newest run.
func latestTuneResult(wikiDir, specID string) (*tuneResult, error) {
	files, _ := filepath.Glob(filepath.Join(wikiDir, "runs", "*.json"))
	var best *tuneResult
	for _, f := range files {
		res, err := loadTuneResult(wikiDir, filepath.Base(f))
		if err != nil || res.SpecID != specID {
			continue
		}
		if best == nil || parsedAt(res.At).After(parsedAt(best.At)) {
			best = res
		}
	}
	if best == nil {
		return nil, errNotFound
	}
	return best, nil
}

func parsedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// recordMeaningCheck keeps the person's "same meaning / different" in the run's md and json. A rejection also stays on the spec's page, and the next proposer reads it.
func recordMeaningCheck(res *tuneResult, same bool, note, by string) {
	recordCheck(res, meaningCheck{Same: same, Note: note, By: by})
}

// recordCheck keeps the meaning check (by a person, or from 0.9.2 by an agent) in the run's md and json. When the agent could not decide (unsure), it is not a rejection.
func recordCheck(res *tuneResult, mc meaningCheck) {
	mc.At = time.Now().Format("2006-01-02 15:04")
	res.MeaningChecks = append(res.MeaningChecks, mc)
	same, note, by := mc.Same, mc.Note, mc.By
	if mc.Difference != "" {
		note = strings.TrimSpace(note + " " + mc.Difference)
	}
	verdict := "meaning confirmed as the same"
	switch {
	case mc.Verdict == "unsure":
		verdict = "the agent could not decide whether the meaning is the same: waiting for a person"
	case !same:
		verdict = "meaning judged to have CHANGED: not adopted"
	}
	line := fmt.Sprintf("\n**Meaning check (%s, %s):** %s", mc.At, by, verdict)
	if note != "" {
		line += " — " + note
	}
	if res.RunPage != "" {
		if f, err := os.OpenFile(res.RunPage, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(line + "\n")
			f.Close()
		}
		if !same && mc.Verdict != "unsure" {
			q := ""
			if res.WinnerWording != nil {
				q = trim(res.WinnerWording.Question, 80)
			}
			specPage := filepath.Join(filepath.Dir(filepath.Dir(res.RunPage)), "specs", res.SpecID+".md")
			who := "a person"
			if by == "agent" {
				who = "the agent"
			}
			appendRunNote(specPage, fmt.Sprintf("- %s: %s rejected %s (%q): the meaning changed. %s", mc.At, who, res.Winner, q, note))
		}
	}
	saveTuneResult(res)
}
