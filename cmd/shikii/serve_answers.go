package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// handleAnswersRestore is the person's undo, from the knowledge section, of what the AI decided about quoting their past answers
// (memory_answers.go): {"trace_id": ...} undoes the rejection of one answer, under every decision that carries it, and {"stop":
// true} undoes the stop of quoting past answers in this project. The assessments up to now stop counting for it; nothing else
// changes (the answer's truth was never touched). Only what is in place can be undone, so an answer that is not rejected and a stop
// that is not in place are refused, and a refusal writes nothing. It is the person's operation, so MCP has no counterpart.
func (s *server) handleAnswersRestore(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		TraceID string `json:"trace_id"`
		Stop    bool   `json:"stop"`
	}
	if err := decodeObject(r.Body, &req); err != nil { // the reader's words for what it cannot read
		writeBodyErr(w, err)
		return
	}
	req.TraceID = strings.TrimSpace(req.TraceID)
	if (req.TraceID != "") == req.Stop {
		writeErr(w, 400, errors.New("give either trace_id (one past answer) or stop (quoting past answers in this project)"))
		return
	}
	if s.o.projectScope == "" {
		writeErr(w, 409, errors.New("the page is not open for one project, so there is nothing to undo"))
		return
	}
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	rejection := readAnswerRejection(s.o.storeDir, s.o.projectScope)
	if req.Stop {
		if !rejection.kindStopped() {
			writeErr(w, 409, errors.New("quoting past answers is not stopped in this project"))
			return
		}
	} else {
		answers, err := s.st.PersonAnswers(answerSpecs)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		rejection = rejection.withAnswers(answers)
		if !rejection.rejected(req.TraceID) {
			known := len(rejection.effects.ByTrace[req.TraceID]) > 0
			for _, a := range answers {
				known = known || a.TraceID == req.TraceID
			}
			if !known {
				writeErr(w, 404, fmt.Errorf("no past answer %q in this project", req.TraceID))
			} else {
				writeErr(w, 409, errors.New("this answer is not rejected"))
			}
			return
		}
	}
	undo := store.AnswerRestore{TraceID: req.TraceID, Stop: req.Stop, At: time.Now().UTC()}
	if err := s.st.AppendAnswerRestore(s.o.projectScope, undo); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"restored": true, "trace_id": undo.TraceID, "stop": undo.Stop, "at": undo.At})
}
