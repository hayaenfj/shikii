package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// The regular agent organizes the context. The hook only keeps the original text and does not guess agreements or open questions.
type planningFrame struct {
	TurnID  string            `json:"turn_id"`
	Sources map[string]string `json:"source_material"`
	At      time.Time         `json:"at"`
	Digest  string            `json:"digest,omitempty"`
	Result  *planningReceipt  `json:"result,omitempty"`
	Recent  []planningTurn    `json:"recent,omitempty"`
}

type planningQuote struct {
	Source string `json:"source"`
	Quote  string `json:"quote"`
}

type planningFact struct {
	Unknown  bool            `json:"unknown,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Evidence []planningQuote `json:"evidence,omitempty"`
}

type planningRequest struct {
	SessionID string        `json:"session_id"`
	Query     string        `json:"query,omitempty"`
	TurnID    string        `json:"turn_id"`
	Agreed    *planningFact `json:"agreed,omitempty"`
	Open      *planningFact `json:"open,omitempty"`
	Pages     []string      `json:"pages,omitempty"`
}

type planningReceipt struct {
	Recorded bool   `json:"recorded"`
	TraceID  string `json:"trace_id,omitempty"`
	Advice   string `json:"advice"`
	Shadow   bool   `json:"shadow"`
	Reason   string `json:"reason,omitempty"`
}

func needsPlanningContext(sp *spec.Spec) bool {
	have := map[string]bool{}
	for _, key := range sp.State.Required {
		have[key] = true
	}
	return sp.ID == "needs-plan" && have["request"] && have["agreed"] && have["open"]
}

func legacyPlanningRecord(sp *spec.Spec, rec store.Record) bool {
	return needsPlanningContext(sp) && (strings.TrimSpace(rec.Inputs["agreed"]) == "" || strings.TrimSpace(rec.Inputs["open"]) == "")
}

func newPlanningFrame(previous *hookSession, in *hookInput, scope, turnID string) *planningFrame {
	f := &planningFrame{TurnID: turnID, At: time.Now(), Sources: map[string]string{"current_user": cut(strings.TrimSpace(in.Prompt), 4000)}}
	if previous.Scope == scope {
		if previous.RawPrompt != "" {
			f.Sources["previous_user"] = cut(previous.RawPrompt, 4000)
		}
		if previous.Completed != nil && previous.Completed.TurnID == previous.TurnID {
			f.Sources["previous_assistant"] = cut(previous.Completed.Summary, 4000)
		}
	}
	addPlanningContext(scope, previous, f)
	return f
}

func planningNotice(sessionID, turnID string) string {
	ids, _ := json.Marshal(map[string]string{"session_id": sessionID, "turn_id": turnID})
	command := "shikii"
	if exe, err := os.Executable(); err == nil {
		command = "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	}
	return fmt.Sprintf("shikii: organize the conversation for the pre-start judgment. Do not ask the person for input. Pass the JSON below on standard input (for example %[1]s hook plan query <<'JSON') to %[1]s hook plan query to read this turn and the recent conversation; look for related rules and existing decisions by putting topic words in query (not a copy of the request text), and when you use a returned lesson as grounds, set the evidence source to lesson:<id>. Pass the same IDs, search words, agreed and open to %s hook plan submit. Each item is summary and evidence:[{source,quote}] (quoted from the original text), or unknown:true if unknown. Grounds for an agreement need the person's words. The organizing is a hypothesis, never permission to act or the person's truth. When you create or change a rule, test or procedure, write it to the wiki before you finish even if nobody asked (add to or edit the closest page). Details are in the skill's references/planning.md. "+planningShape+"\n", command, command) + string(ids)
}

func cmdHookPlan(args []string) error {
	if len(args) != 1 || (args[0] != "query" && args[0] != "submit") {
		return errors.New("usage: shikii hook plan query | submit (JSON on standard input)")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return errors.New("the pre-start context must be JSON of 16KB or less")
	}
	var req planningRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return fmt.Errorf("cannot read the pre-start context: %v", jsonShapeError(err))
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || req.SessionID == "" || len(req.SessionID) > 200 || len(req.TurnID) != 16 {
		return errors.New("pass one JSON object with the current session_id and turn_id")
	}
	if len(req.Query) > requestMax {
		return errors.New("the wiki search words are too long")
	}
	if _, err := hex.DecodeString(req.TurnID); err != nil {
		return errors.New("turn_id is invalid")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	active, err := projectActive(root, scope)
	if err != nil {
		return err
	}
	if !active {
		return errProjectInactive
	}
	sessions, err := planningSessionRoot(root)
	if err != nil {
		return err
	}
	defer sessions.Close()
	name := filepath.Base(sessionPath(root, req.SessionID))
	s, err := readPlanningSession(sessions, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	if args[0] == "query" {
		notice, knowledge, err := preparePlanningKnowledge(root, scope, req, s.Planning.Sources)
		if err != nil {
			return err
		}
		if req.Agreed != nil || req.Open != nil {
			return errors.New("query does not accept organized content")
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"session_id": req.SessionID, "turn_id": req.TurnID, "source_material": s.Planning.Sources, "context_notice": notice, "wiki_context": knowledge})
	}
	// concurrent submits do not wait; they are asked to retry. A new request from the person and Stop are not blocked.
	busy, err := sessions.OpenFile(name+".planning.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer busy.Close()
	if info, err := busy.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("cannot check the context lock")
	}
	if err := syscall.Flock(int(busy.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("the context for the same request is being saved; retry")
	}
	defer syscall.Flock(int(busy.Fd()), syscall.LOCK_UN)
	// check whether another submit or a new request finished before the lock was taken.
	s, err = readPlanningSession(sessions, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	_, knowledge, err := preparePlanningKnowledge(root, scope, req, s.Planning.Sources)
	if err != nil {
		return err
	}
	agreed, err := renderPlanningFact(req.Agreed, s.Planning.Sources, true)
	if err != nil {
		return err
	}
	open, err := renderPlanningFact(req.Open, s.Planning.Sources, false)
	if err != nil {
		return err
	}
	planningCitations(req, knowledge)
	canonical, _ := json.Marshal(req)
	digest := sha256.Sum256(canonical)
	key := hex.EncodeToString(digest[:])
	if s.Planning.Result != nil && s.Planning.Digest == key {
		return json.NewEncoder(stdout).Encode(s.Planning.Result)
	}
	if s.Planning.Result != nil && s.Planning.Result.Recorded {
		return errors.New("the context for this request has already been judged")
	}
	result := &planningReceipt{Shadow: true}
	var d *decide.Decision
	sp, err := spec.Load(hookSpecPath(root, "needs-plan"))
	if err != nil {
		return err
	}
	if !needsPlanningContext(sp) {
		return errors.New("the current question does not use agreed and open inputs")
	}
	inputs := map[string]string{"request": cut(s.Planning.Sources["current_user"], requestMax), "agreed": agreed, "open": open}
	if req.Agreed.Unknown && req.Open.Unknown {
		result.Reason = "agreed and open are both unknown, so the decision was skipped. It does not count as the person's answer."
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), hookDecideTimeout)
		defer cancel()
		d, err = decide.Decide(ctx, sp, inputs, decide.Session{}, hookEndpoints())
		if err != nil {
			// even if the decision model is busy or down, the organizing is saved (the ritual check does not get stuck). The decision is skipped and does not count as the person's answer (0.8.1)
			hookLog("plan submit: " + err.Error())
			d = nil
			result.Reason = "the decision model could not be reached, so the organizing was saved and the decision skipped. It does not count as the person's answer."
		} else if d.Band == decide.Abstain {
			return errors.New("the decision inputs are incomplete")
		}
	}
	if d != nil {
		trace := sha256.Sum256([]byte("planning\x00" + scope + "\x00" + req.TurnID + "\x00" + sp.Hash() + "\x00" + key))
		d.TraceID = hex.EncodeToString(trace[:8])
		result.Recorded, result.TraceID, result.Shadow = true, d.TraceID, !d.IsCalibrated
		attachReceipt(sp, store.Store{Dir: root}, d)
		out := hookOutcome("needs-plan", d, localize(sp, d, hookLang()))
		result.Advice = joinLines(out.Context, out.System)
		result.Advice = strings.ReplaceAll(result.Advice, hookPageURL+"/", pageRef(filepath.Dir(root)))
	}
	// if the request moved on while waiting for the model, the old context is discarded without a record or advice.
	err = savePlanningResult(sessions, name, root, scope, req, sp, d, inputs, result, key, knowledge)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// planningPreface comes before the agent's organizing in the needs-plan input. web/src/components/ItemCard.svelte removes the same text
// (and the Japanese one of records made before 0.9.2); a test checks the two match.
const planningPreface = "AI's organizing (not permission to act, not the person's truth): "

func renderPlanningFact(f *planningFact, sources map[string]string, agreement bool) (string, error) {
	if f == nil {
		return "", errors.New("pass agreed and open. If unknown, set unknown:true")
	}
	if f.Unknown {
		if f.Summary != "" || len(f.Evidence) != 0 {
			return "", errors.New("do not fill in an agreement or grounds for an unknown item")
		}
		return "unknown (cannot be confirmed from the current conversation)", nil
	}
	if strings.TrimSpace(f.Summary) == "" || len(f.Summary) > 600 || len(f.Evidence) == 0 || len(f.Evidence) > 3 {
		return "", errors.New("keep the organizing within 600 bytes, with 1 to 3 quoted grounds")
	}
	parts := []string{planningPreface + f.Summary}
	human := false
	for i, e := range f.Evidence {
		text, ok := sources[e.Source]
		switch {
		case !ok:
			keys := make([]string, 0, len(sources))
			for k := range sources {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return "", fmt.Errorf("the source %q of evidence[%d] is not in this request's original text. Usable sources: %s", e.Source, i, strings.Join(keys, ", "))
		case strings.TrimSpace(e.Quote) == "" || len(e.Quote) > 240:
			return "", fmt.Errorf("the quote of evidence[%d] must be 1 to 240 bytes (%d bytes)", i, len(e.Quote))
		case !strings.Contains(text, e.Quote):
			return "", fmt.Errorf("the quote of evidence[%d] is not found verbatim in the original text of source %s. Copy it from the original text query returned, including formatting and symbols", i, e.Source)
		}
		human = human || e.Source == "current_user" || e.Source == "previous_user"
		role := map[string]string{"current_user": "the person's current message", "previous_user": "the person's previous message", "previous_assistant": "the AI's previous reply"}[e.Source]
		if strings.HasPrefix(e.Source, "history:") {
			role = "a past exchange in the same session (for reference, not the current agreement) [" + e.Source + "]"
		}
		if strings.HasPrefix(e.Source, "lesson:") || strings.HasPrefix(e.Source, "article:") {
			role = "a wiki lesson (a hypothesis with sources, not the current agreement) [" + e.Source + "]"
		}
		parts = append(parts, role+": "+e.Quote)
	}
	if agreement && !human {
		return "", errors.New("the AI's proposal alone cannot be treated as an agreement. Grounds from the person's words are needed")
	}
	return strings.Join(parts, "\n"), nil
}

func planningSessionRoot(root string) (*os.Root, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	info, err := dir.Lstat("sessions")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cannot check where the current session is saved")
	}
	return dir.OpenRoot("sessions")
}

func readPlanningSession(dir *os.Root, name string) (*hookSession, error) {
	f, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("there is no record of that session (check the session_id)")
		}
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("cannot check the session record")
	}
	var s hookSession
	decoder := json.NewDecoder(io.LimitReader(f, (1<<20)+1))
	if err := decoder.Decode(&s); err != nil {
		return nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("the session must be saved as one JSON object")
	}
	return &s, nil
}

func currentPlanningFrame(s *hookSession, turnID, scope string) error {
	// after Judged (the end-of-turn decision is done) it is not accepted, but right after the ritual check stopped (no organizing submit yet) it is (0.8.0)
	if s.Scope != scope || s.TurnID != turnID || (s.Judged && s.Planning != nil && s.Planning.Result != nil) || s.Planning == nil || s.Planning.TurnID != turnID || time.Since(s.Planning.At) > hookSessionTTL {
		return errors.New("only the context of the ongoing request in the same project can be handled")
	}
	return nil
}

func savePlanningResult(dir *os.Root, name, root, scope string, req planningRequest, sp *spec.Spec, d *decide.Decision, inputs map[string]string, result *planningReceipt, digest string, knowledge *store.KnowledgeContext) error {
	lock, err := dir.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("cannot check the session lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("the request record is being updated; retry")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	fresh, err := spec.Load(sp.Path())
	if err != nil {
		return err
	}
	if fresh.Hash() != sp.Hash() {
		return errors.New("the question changed during the decision; read the context again")
	}
	if err := validatePlanningKnowledge(root, scope, knowledge); err != nil {
		return err
	}
	if d != nil {
		st := store.Store{Dir: root}
		records, err := st.Records("needs-plan", 0)
		if err != nil {
			return err
		}
		recorded := false
		for _, prior := range records {
			if prior.TraceID != d.TraceID {
				continue
			}
			if prior.SpecHash != d.SpecHash || prior.InputsHash != d.InputsHash {
				return errors.New("cannot match the record of the same request")
			}
			recorded = true
			// a resend where only saving the completion mark failed after the record was appended. Keep the first prediction and give no new advice.
			result.Advice, result.Shadow = "", true
			result.Reason = "kept the first decision that was already saved."
			break
		}
		if !recorded {
			rec := recordOf(sp, d, inputs)
			rec.KnowledgeContext = knowledge
			rec.Project = filepath.Base(scope)
			l := localize(sp, d, hookLang())
			if l.Lang != "" {
				rec.Display = &store.RecordDisplay{Lang: l.Lang, Question: l.Question, Top: l.Top}
			}
			if err := recordDecisionRec(st, sp, d, rec); err != nil {
				return err
			}
		}
		if err := st.SaveRecordText(recordOf(sp, d, inputs), map[string]string{"request": s.Planning.Sources["current_user"]}); err != nil {
			return err
		}
		alreadyAsked := false
		for _, item := range s.Asked {
			alreadyAsked = alreadyAsked || item.Trace == d.TraceID
		}
		if !alreadyAsked && waitsForPersonFor("needs-plan", d) {
			s.Asked = append(s.Asked, askedItem{Spec: "needs-plan", Trace: d.TraceID})
		}
	}
	s.Planning.Digest, s.Planning.Result = digest, result
	s.At = time.Now()
	b, _ := json.MarshalIndent(s, "", "  ")
	temp := ".planning-" + store.NewTraceID() + ".tmp"
	f, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := dir.Rename(temp, name); err != nil {
		return fmt.Errorf("saving the context: %w", err)
	}
	return nil
}
