package store

import (
	"strings"
)

// HumanSource reports whether truths from that source were decided by a person (or the outcome). Machine-decided truths are the upper step's answer (context), the judge (judge: another model's answer)
// and an agreement (agreed, 0.11: the decision model and the agent gave the same answer, so nobody was asked; calibrating on agreements alone would overstate precision).
// Re-asking (replay) and files (file) only had the machine fetch the probabilities again; the truth itself was given by a person.
func HumanSource(source string) bool {
	return !machineSources[source]
}

// machineSources are the sources of truths no person or outcome decided.
var machineSources = map[string]bool{"context": true, "judge": true, SourceAgreed: true}

// SourceAgreed is the source of a truth written when the decision model and the agent agreed (0.11).
const SourceAgreed = "agreed"

// Partial reports whether it is a partial label in "!<id>" form.
func Partial(truth string) bool { return strings.HasPrefix(truth, "!") && len(truth) > 1 }

// Correct reports whether the model's top answer top matches the truth. A partial label "!x" means "right unless it is x".
func Correct(top, truth string) bool {
	if Partial(truth) {
		return top != truth[1:]
	}
	return top == truth
}
