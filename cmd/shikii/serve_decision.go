package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
)

func (s *server) handleDecide(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec    string            `json:"spec"`
		Inputs  map[string]string `json:"inputs"`
		Tainted bool              `json:"tainted"`
		Record  *bool             `json:"record"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, 413, fmt.Errorf("body larger than %d bytes", maxBody))
			return
		}
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	sp, err := s.spec(req.Spec)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, req.Spec)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, req.Inputs, decide.Session{Tainted: req.Tainted || s.o.ef.tainted}, s.o.ef.endpoints())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	attachReceipt(sp, s.st, d)
	if (req.Record == nil || *req.Record) && d.Band != decide.Abstain {
		if err := recordDecision(s.st, sp, d, req.Inputs); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	// with the band, what this project already knows about such a request (0.11): the person's answers on similar requests with the two
	// answers they judged between, and the closest lessons. An agent consulting the decision model mid-task reads these as the project's
	// own judgment, not only a probability
	if request := strings.TrimSpace(req.Inputs["request"]); request != "" && s.o.projectScope != "" {
		similar, lessons, kerr := decideKnowledge(s.st.Dir, s.o.projectScope, request)
		if kerr != nil {
			log.Printf("shikii serve: decide: what the project knows could not be read, the decision is returned alone: %v", kerr) // audit 0.11
		}
		if kerr == nil && (len(similar) > 0 || len(lessons) > 0) {
			var reply map[string]any
			raw, _ := json.Marshal(d)
			if json.Unmarshal(raw, &reply) == nil {
				if len(similar) > 0 {
					reply["similar_decisions"] = similar
				}
				if len(lessons) > 0 {
					reply["lessons"] = lessons
				}
				writeJSON(w, 200, reply)
				return
			}
		}
	}
	writeJSON(w, 200, d)
}
