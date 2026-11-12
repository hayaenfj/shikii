package main

import (
	"strings"
	"testing"
)

// Audit H of 0.31.0 (fourth round): the checks of the shipped tests themselves.

// H-7 (U, test quality): planFlowProblem and olderEntryProblem take a text that has "older" anywhere as one that says the
// command is the older entry. "folder" has "older" in it: the help of hook plan with its older-entry note taken out still passes,
// because its first line says "run it in the project's folder". The mutation of agent_usage.go that put the note of 0.30.8 back
// survived TestThePlanCheckNamesTheFlowAnAgentRuns.
func TestAuditHOlderMatchesFolder(t *testing.T) {
	mutated := "usage: shikii hook plan query | submit (one JSON object on standard input; run it in the project's folder)\n" +
		"  submit takes the same ids and query plus agreed and open, as how_to_submit in the reply of query describes.\n" +
		"  work prepare and work record do the same in one call each, with the knowledge and the record of the result.\n"
	if problem := planFlowProblem(mutated); problem == "" {
		t.Errorf("planFlowProblem passes a help of hook plan that does not say it is the older entry (\"folder\" counts as \"older\"): %q", mutated)
	}
	line := "Run `shikii hook plan query` in the project's folder before work."
	if problem := olderEntryProblem(line); problem == "" {
		t.Errorf("olderEntryProblem passes a line that names hook plan query as the call to run (\"folder\" counts as \"older\"): %q", line)
	}
	// negative: the line as the documents write it now passes, and the line without "folder" is reported
	if olderEntryProblem("Run `shikii hook plan query` before work.") == "" {
		t.Errorf("negative: a line without the word is reported")
	}
}

// H-8 (U, test quality): TestEveryToolAnswersWhatItCannotReadInTheReadersWords finds a function that reads a JSON body on its
// own only in the form json.NewDecoder(<x>.Body). A handler that reads the body with io.ReadAll and json.Unmarshal, or wraps it
// (json.NewDecoder(io.LimitReader(r.Body, n))), is not found, and answers in encoding/json's words again.
func TestAuditHBodyReadersSeeOneFormOnly(t *testing.T) {
	for name, source := range map[string]string{
		"io.ReadAll and json.Unmarshal": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\tdata, _ := io.ReadAll(r.Body)\n\t_ = json.Unmarshal(data, &req)\n}\n",
		"a limited body":                "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)\n}\n",
		"the body in a variable":        "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\tbody := r.Body\n\t_ = json.NewDecoder(body).Decode(&req)\n}\n",
	} {
		if found := bodyReaders(t, map[string]string{"new_handler.go": source}); len(found) == 0 {
			t.Errorf("%s: a new handler that reads its JSON body on its own is not found", name)
		}
	}
	// negative: the one form it knows is found
	if found := bodyReaders(t, map[string]string{"new_handler.go": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n"}); len(found) != 1 {
		t.Errorf("negative: the form json.NewDecoder(r.Body) is found: %v", found)
	}
	_ = strings.Contains
}
