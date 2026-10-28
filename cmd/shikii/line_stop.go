package main

// Stopping a question no line can be drawn for (0.10). When calibrate, with enough truths, draws no line and the probabilities do not
// rank the answers acceptably (the upper end of the AUC's 95% interval is below 0.7, the lower end of "acceptable discrimination" in
// Hosmer and Lemeshow's guide), asking the model on every request only adds time and records. calibrate keeps the reason in
// stopped-<spec>, the hook then asks that question on 1 request in 20 (the spot-check rate) so a new model is noticed and truths still
// come in, and status and the page say why. Changing the wording gives a new hash, which is not stopped; a line drawn later removes it.

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
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

func stoppedPath(storeDir, specID string) string { return filepath.Join(storeDir, "stopped-"+specID) }

// noteStopped keeps the stop when a refused calibration had enough truths and the answers could not be told apart acceptably.
// A refusal for another reason (too few truths, one side only, the precision at act) is not a stop.
func noteStopped(storeDir string, sp *spec.Spec, cal spec.Calibration) error {
	d := cal.Discrimination
	if storeDir == "" || d == nil || cal.NHuman < sp.Eval.MinN || d.High >= stopBelowAUC {
		return nil
	}
	b, err := json.Marshal(stoppedLine{Model: cal.Model, SpecHash: cal.SpecHash, AUC: d.AUC, Low: d.Low, High: d.High, Pos: d.Pos, Neg: d.Neg, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	return writeAtomic(stoppedPath(storeDir, sp.ID), append(b, '\n'))
}

func clearStopped(storeDir, specID string) {
	if storeDir != "" {
		_ = os.Remove(stoppedPath(storeDir, specID))
	}
}

// readStopped returns the stop kept for the spec's current wording (a stop measured on another wording does not apply).
func readStopped(storeDir string, sp *spec.Spec) (stoppedLine, bool) {
	var s stoppedLine
	b, err := os.ReadFile(stoppedPath(storeDir, sp.ID))
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
	var s stoppedLine
	b, err := os.ReadFile(stoppedPath(agent, specID))
	if err != nil || json.Unmarshal(b, &s) != nil || model == "" || s.Model == model {
		return
	}
	_ = os.Remove(stoppedPath(agent, specID))
}

// stopNote is the status line for a stopped question.
func (s stoppedLine) stopNote() string {
	return fmt.Sprintf("  stopped: the probabilities do not tell the answers apart (AUC %.2f, 95%% %.2f-%.2f, on %d and %d); asked on 1 request in %d to notice a new model. Change the wording or the model to try again", s.AUC, s.Low, s.High, s.Pos, s.Neg, stoppedEvery)
}
