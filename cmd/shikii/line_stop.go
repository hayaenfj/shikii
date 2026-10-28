package main

// Stopping a question no line can be drawn for (0.10). When calibrate, with enough truths, draws no line for one of the hook's
// questions and the probabilities do not rank the answers acceptably (the upper end of the AUC's 95% interval is below 0.7, the lower
// end of "acceptable discrimination" in Hosmer and Lemeshow's guide), asking the model on every request only adds time and records.
// calibrate keeps the reason in stopped-<spec>-<wording hash>; the hook then asks that question on 1 request in 20 (the spot-check
// rate) so a new model is noticed and truths still come in, and status and the page say why. A stop applies only while the question
// has no line at all (for any model, on either rung): a line is never silenced by a stop. Each wording has its own stop, so trying
// another wording leaves the current one's alone; a line drawn later removes it.

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
)

// Fixed rules (no options).
const (
	stopBelowAUC = 0.7 // the upper end of the AUC's 95% interval below this stops the question
	stoppedEvery = 20  // a stopped question is still asked on 1 request in this many
)

// stoppedLine is the record of a stop: the wording and model it was measured on, and how well that pair told the answers apart.
type stoppedLine struct {
	Model    string    `json:"model"`
	SpecHash string    `json:"spec_hash"`
	AUC      float64   `json:"auc"`
	Low      float64   `json:"low"`
	High     float64   `json:"high"`
	Pos      int       `json:"pos"` // truths on the "yes" side (for a choice: decisions whose top answer was right)
	Neg      int       `json:"neg"`
	At       time.Time `json:"at"`
}

func stoppedPath(storeDir, specID, hash string) string {
	return filepath.Join(storeDir, "stopped-"+specID+"-"+hash)
}

// stoppable reports whether the question is one the hook asks (only the hook asks a stopped question on 1 request in 20).
func stoppable(specID string) bool { return slices.Contains(hookSpecs, specID) }

// hasLine reports whether the question has a line for its current wording on either rung, for any model.
func hasLine(sp *spec.Spec) bool {
	for _, c := range sp.Calibration {
		if sp.LiveHash(c.SpecHash) {
			return true
		}
	}
	return false
}

// noteStopped keeps the stop when a refused calibration of a hook question without any line had enough truths and the probabilities
// did not tell the answers apart acceptably, whatever else refused it (the precision at act included). Too few truths, or one side
// only (no AUC), is not a stop.
func noteStopped(storeDir string, sp *spec.Spec, cal spec.Calibration) error {
	d := cal.Discrimination
	if storeDir == "" || !stoppable(sp.ID) || hasLine(sp) || d == nil || cal.NHuman < sp.Eval.MinN || d.High >= stopBelowAUC {
		return nil
	}
	b, err := json.Marshal(stoppedLine{Model: cal.Model, SpecHash: cal.SpecHash, AUC: d.AUC, Low: d.Low, High: d.High, Pos: d.Pos, Neg: d.Neg, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	return writeAtomic(stoppedPath(storeDir, sp.ID, cal.SpecHash), append(b, '\n'))
}

func clearStopped(storeDir, specID, hash string) {
	if storeDir != "" {
		_ = os.Remove(stoppedPath(storeDir, specID, hash))
	}
}

// readStopped returns the stop in force for the spec's current wording: a hook question without any line whose wording was stopped.
func readStopped(storeDir string, sp *spec.Spec) (stoppedLine, bool) {
	var s stoppedLine
	if !stoppable(sp.ID) || hasLine(sp) {
		return stoppedLine{}, false
	}
	b, err := os.ReadFile(stoppedPath(storeDir, sp.ID, sp.Hash()))
	if err != nil || json.Unmarshal(b, &s) != nil || s.SpecHash != sp.Hash() {
		return stoppedLine{}, false
	}
	return s, true
}

// stoppedAsk reports whether this request still asks a stopped question: 1 in stoppedEvery, decided by the inputs so the same request
// gets the same answer.
func stoppedAsk(specID string, inputs map[string]string) bool {
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := fnv.New32a()
	fmt.Fprint(h, specID)
	for _, k := range keys {
		fmt.Fprintf(h, "\x00%s\x00%s", k, inputs[k])
	}
	return h.Sum32()%stoppedEvery == 0
}

// hookSkipsStopped reports whether the hook leaves the question unasked on this request.
func hookSkipsStopped(agent, specID string, inputs map[string]string) bool {
	sp, err := spec.Load(hookSpecPath(agent, specID))
	if err != nil {
		return false
	}
	_, stopped := readStopped(agent, sp)
	return stopped && !stoppedAsk(specID, inputs)
}

// resumeOnNewModel removes the stop when a question asked anyway was answered by another model than the one it was measured on.
func resumeOnNewModel(agent, specID, model string) {
	sp, err := spec.Load(hookSpecPath(agent, specID))
	if err != nil || model == "" {
		return
	}
	if s, ok := readStopped(agent, sp); ok && s.Model != model {
		clearStopped(agent, specID, sp.Hash())
	}
}

// stopNote is the status line for a stopped question.
func (s stoppedLine) stopNote() string {
	return fmt.Sprintf("  stopped: the probabilities do not tell the answers apart (AUC %.2f, 95%% %.2f-%.2f, on %d yes or right / %d no or wrong); asked on 1 request in %d, where a new model is noticed. Change the wording or the model to try again", s.AUC, s.Low, s.High, s.Pos, s.Neg, stoppedEvery)
}
