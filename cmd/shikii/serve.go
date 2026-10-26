package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// shikii serve: in one process, serves the HTTP endpoints for programs and the page for people.
//
//	POST /v1/decide  {"spec":"id","inputs":{...},"tainted":false}   → Decision (same shape as decide --json). Recorded
//	POST /v1/label   {"trace_id":"…","truth":"…","weight":1,"source":"ui"} → the label given. Once min_n is reached it calibrates automatically and writes back
//	GET  /v1/queue?spec=id&limit=50                                 → records without a truth (ask band first). The text is attached if it is still kept
//	GET  /v1/status                                                 → per-spec state of records, truths and (model, hash)
//	GET  /v1/specs                                                  → the specs loaded
//	GET  /                                                          → the page (the Svelte page with web/dist embedded; no external loads)
//
// Specs are reread from the folder every time, so editing a file takes effect at once. It does no inference itself (that is left to --local / --hosted).

type serveOpts struct {
	projectScope  string // the work that started the process. Do not mix wiki sources with other work
	port          int
	specsDir      string
	storeDir      string
	ef            endpointFlags
	autoCalibrate bool
	wikiDir       string // the wording wiki. Empty means ./wiki. Holds tune's results, read by the page and MCP
	proposer      string // URL of the proposer (a chat LLM) that drafts candidates. Empty means only the spec's variants
	proposerModel string
}

// bindHost is where to listen. On a host, only 127.0.0.1 (never exposed to the LAN). Inside a container (Docker, where /.dockerenv exists) it listens on every interface,
// and whether it is exposed is decided by the host's port publishing (compose limits it to 127.0.0.1:8095). There is no setting to choose.
func bindHost() string { return systemone.BindHost() }

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var o serveOpts
	o.ef.bind(fs)
	fs.IntVar(&o.port, "port", 8095, "port to listen on (127.0.0.1 only; 0 takes a free port and writes it to ~/.shikii/agent/pages/; the hook starts it this way)")
	fs.StringVar(&o.specsDir, "specs", "", "folder of specs (*.yaml) (default: <store>/decisions)")
	fs.StringVar(&o.storeDir, "store", ".shikii", "where records and labels are kept")
	fs.BoolVar(&o.autoCalibrate, "auto-calibrate", true, "calibrate automatically and write back to the spec once truths reach eval.min_n")
	fs.StringVar(&o.proposer, "proposer", "", "URL of the proposer (a chat LLM) that drafts wording candidates. Empty measures only the spec's variants")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "model name of the proposer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := resolveStoreFlag(fs); err != nil {
		return err
	}
	o.ef.useForHookDecisions()
	o.resolve()
	common, _ := agentHome()
	absStore, _ := filepath.Abs(o.storeDir)
	hub := absStore == common
	if !hub {
		if _, err := loadSpecs(o.specsDir); err != nil {
			return err
		}
	}
	scope := filepath.Dir(absStore)
	auto := !hub && o.port == 0 // the project's page started by the hook (0.8.0): free port, mark, browser; the local model is waited for after the port opens
	if auto {
		if url := livePage(scope); url != "" { // one page per project (another agent's hook started it first)
			log.Printf("shikii serve: this project's page is already up at %s/ ; not starting another", url)
			return nil
		}
	} else if err := waitForLocalModel(o.ef.local, usesHosted(), systemone.WaitReady); err != nil {
		return err // a serve started by hand (Docker etc.) keeps the old behavior: the port does not open until the model answers
	}
	ln, err := listenPage(common, scope, hub, o.port)
	if err != nil {
		return err
	}
	log.Printf("shikii serve: specs=%s store=%s local=%q hosted=%q auto-calibrate=%v  →  http://%s/", o.specsDir, o.storeDir, systemone.RedactURL(o.ef.local), systemone.RedactURL(o.ef.hosted), o.autoCalibrate, ln.Addr())
	var handler http.Handler = newServer(o)
	if hub {
		handler = newProjectHub(common, o)
	}
	handler = localHostOnly(handler) // so another site cannot read records or work records through DNS rebinding (audit 0.8.1-1)
	httpServer := newHTTPServer(handler)
	if !auto {
		return httpServer.Serve(ln)
	}
	errc := make(chan error, 1)
	go func() { errc <- httpServer.Serve(ln) }()
	// Wait for the local model after opening the port (status, queue and wiki can answer without a model; the hook does not wait)
	if err := waitForLocalModel(o.ef.local, usesHosted(), systemone.WaitReady); err != nil {
		ln.Close()
		return err
	}
	return <-errc
}

// newHTTPServer bounds every phase of an HTTP connection. The longest handler operation is a
// model decision with a 60-second context deadline, so the write deadline leaves another minute
// for the response while slow request headers and bodies are cut off much earlier.
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// allowLongResponse lets one handler that starts long work (the automatic calibration after an answer, a synchronous tune) answer after
// newHTTPServer's WriteTimeout; the header and read limits still cut slow clients. A recorder (MCP calls handlers in process) has no deadline.
func allowLongResponse(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(longResponseLimit))
}

// longRoutes take or wait for the calibration lock (a background tune holds it up to 30 minutes) or calibrate themselves, so their replies may
// come after newHTTPServer's WriteTimeout. They are registered through longWork (0.9.6 audit of the HTTP limits).
var longRoutes = []string{"/v1/label", "/v1/tune", "/v1/tune/run", "/v1/tune/sync", "/v1/tune/adopt", "/v1/tune/review", "/v1/tune/variant"}

// longWork extends the write deadline before the handler runs; reading the request is still bounded by ReadTimeout.
func longWork(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowLongResponse(w)
		h(w, r)
	}
}

// longResponseLimit bounds a long handler's reply: a calibration that asks the model again for many earlier truths, or a tune over every fold.
const longResponseLimit = 30 * time.Minute

// listenPage starts listening. For the project's page started by the hook (port 0), it takes a free port, writes a mark with the port and pid
// to ~/.shikii/agent/pages/, and opens it once in the browser. A serve started by hand (fixed port) writes no mark (so it never overwrites the hook page's mark; audit R5).
func listenPage(common, scope string, hub bool, port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost(), port))
	if err != nil {
		return nil, err
	}
	if !hub && port == 0 {
		p := ln.Addr().(*net.TCPAddr).Port
		if err := writePageRecord(common, scope, p, os.Getpid()); err != nil {
			ln.Close()
			return nil, err
		}
		openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", p))
	}
	return ln, nil
}

// waitForLocalModel waits by itself for the local model's endpoint to come up (when Docker starts the model first; it does not rely on curl / wget).
// Without --local (hosted only) it does not wait: hosted is an outside service, not something to wait to start.
// It also does not wait when there is a key and Jev is used (hosted = usesHosted()): the rule is not to start a local model (0.7.1), so waiting would keep the page from ever opening
// (the hook always starts serve with --local; observed on a real machine on 2026-10-23). At most 15 minutes. When the URL is wrong, the first line makes it clear.
func waitForLocalModel(local string, hosted bool, wait func(context.Context, string, time.Duration, func(string, ...any)) error) error {
	if local == "" {
		return nil
	}
	if hosted {
		log.Printf("key present: decisions go to TypeSafe Jev; not waiting for the local model at %s (SHIKII_MODEL=local would wait)", systemone.RedactURL(local))
		return nil
	}
	log.Printf("waiting for the local model at %s (if the URL is wrong, stop now and fix --local; serve does not open its port until the model answers)", systemone.RedactURL(local))
	wctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return wait(wctx, local, 3*time.Second, log.Printf)
}

type calibrationResult struct {
	Reason      string `json:"reason"`
	HumanTruths int    `json:"human_truths"`
	MinN        int    `json:"min_n"`
	Hash        string `json:"hash"`
	Model       string `json:"model"`
	At          string `json:"at"`
	// TriesWordings: when no line fits, the agent is asked for other wordings (the hook's questions only: variantsContext, hook variants submit).
	// The page says "the AI will try other wordings" only then (0.9.4 audit)
	TriesWordings bool `json:"tries_wordings"`
}

type calibrationReport struct {
	log    string
	result calibrationResult
}

type server struct {
	wikiMu  sync.Mutex // serializes wiki operations from the page (tune's updateClaims is separate when in another process; writes are atomic)
	o       serveOpts
	st      store.Store
	calibMu sync.Mutex                   // serializes calibration and candidate saving for specs of the same project
	lastCal map[string]calibrationReport // spec id → report of the last automatic calibration
	mu      sync.Mutex
	tuning  map[string]bool   // spec id → tune is running in the background
	tuneErr map[string]string // spec id → the last tune failure
}

// resolve fills in the store defaults: specs in <store>/decisions, the wiki in <store>/wiki (the basic layout; there are no other flags).
func (o *serveOpts) resolve() {
	if o.projectScope == "" {
		o.projectScope = currentProjectScope()
	}
	if o.specsDir == "" {
		o.specsDir = specsDirOf(o.storeDir)
	}
	if o.wikiDir == "" {
		o.wikiDir = wikiDirOf(o.storeDir)
	}
}

func newServer(o serveOpts) http.Handler {
	o.resolve()
	s := &server{o: o, st: store.Store{Dir: o.storeDir}, lastCal: map[string]calibrationReport{}, tuning: map[string]bool{}, tuneErr: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleUI)
	mux.HandleFunc("/v1/specs", s.handleSpecs)
	mux.HandleFunc("/v1/decide", s.handleDecide)
	mux.HandleFunc("/v1/label", longWork(s.handleLabel))
	mux.HandleFunc("/v1/queue", s.handleQueue)
	mux.HandleFunc("/v1/answered", s.handleAnswered)
	mux.HandleFunc("/v1/redo", s.handleRedo)
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/tune", longWork(s.handleTuneGet))
	mux.HandleFunc("/v1/tune/run", longWork(s.handleTuneRun))
	mux.HandleFunc("/v1/tune/sync", longWork(s.handleTuneSync))
	mux.HandleFunc("/v1/tune/adopt", longWork(s.handleTuneAdopt))
	mux.HandleFunc("/v1/tune/review", longWork(s.handleTuneReview))
	mux.HandleFunc("/v1/tune/variant", longWork(s.handleTuneVariant))
	mux.HandleFunc("/v1/wiki", s.handleWikiList)
	mux.HandleFunc("/v1/wiki/claim", s.handleWikiClaim)
	mux.HandleFunc("/v1/wiki/notebook", s.handleNotebook)
	mux.HandleFunc("/v1/handoff/file", s.handleHandoffFile)
	mux.HandleFunc("/v1/review", s.handleReview)
	mux.HandleFunc("/v1/reflect", s.handleReflect)
	return mux
}

// loadSpecs makes the folder's *.yaml lookupable by id. Read every time (the files are small).
func loadSpecs(dir string) (map[string]*spec.Spec, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no spec (*.yaml) in %s", dir)
	}
	out := map[string]*spec.Spec{}
	seen := map[string]map[int]string{} // id → version → path
	for _, f := range files {
		sp, err := spec.Load(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		// For the same id, the larger version wins (the practice of keeping wording v1 → v2 under the same id). Two with the same id and the same version cannot be decided, so that is an error
		if prev, ok := seen[sp.ID][sp.Version]; ok {
			return nil, fmt.Errorf("duplicate spec id %q with the same version %d: %s and %s", sp.ID, sp.Version, prev, f)
		}
		if seen[sp.ID] == nil {
			seen[sp.ID] = map[int]string{}
		}
		seen[sp.ID][sp.Version] = f
		if cur, ok := out[sp.ID]; !ok || sp.Version > cur.Version {
			out[sp.ID] = sp
		}
	}
	return out, nil
}

func (s *server) spec(id string) (*spec.Spec, error) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		return nil, err
	}
	sp, ok := specs[id]
	if !ok {
		return nil, errNotFound
	}
	return sp, nil
}

var errNotFound = errors.New("not found")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *server) handleSpecs(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := []map[string]any{}
	for _, sp := range specs {
		out = append(out, specSummary(sp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	writeJSON(w, 200, map[string]any{"specs": out})
}

func specSummary(sp *spec.Spec) map[string]any {
	titles, questions := map[string]string{}, map[string]string{}
	for lang, d := range sp.Display {
		if d.Title != "" {
			titles[lang] = d.Title
		}
		if d.Question != "" {
			questions[lang] = d.Question
		}
	}
	return map[string]any{"id": sp.ID, "type": string(sp.Type), "path": sp.Path(), "hash": sp.Hash(), "question": sp.Question,
		"titles": titles, "questions": questions,
		"min_n": sp.Eval.MinN, "act": sp.Thresholds.Act, "ask": sp.Thresholds.Ask, "choices": truthChoices(sp)}
}

// truthChoices are the truth candidates that become buttons on the page. For choice, choice ids; for noul, yes/no; for score, "0".."n-1".
func truthChoices(sp *spec.Spec) []map[string]any {
	// labels are display names per language (from display). For a missing language the page falls back to label (English/id)
	labelsFor := func(id string, level int) map[string]string {
		out := map[string]string{}
		for lang, d := range sp.Display {
			if level >= 0 {
				if level < len(d.Levels) {
					out[lang] = fmt.Sprintf("%d: %s", level, d.Levels[level])
				}
			} else if v, ok := d.Options[id]; ok {
				out[lang] = v
			}
		}
		return out
	}
	var out []map[string]any
	switch sp.Type {
	case spec.Noul:
		out = append(out, map[string]any{"truth": "yes", "label": "yes", "labels": labelsFor("yes", -1)},
			map[string]any{"truth": "no", "label": "no", "labels": labelsFor("no", -1)})
	case spec.Choice:
		for _, o := range sp.Options {
			out = append(out, map[string]any{"truth": o.ID, "label": o.ID, "description": o.Description, "labels": labelsFor(o.ID, -1)})
		}
	case spec.Score:
		for i, lv := range sp.Levels {
			out = append(out, map[string]any{"truth": fmt.Sprint(i), "label": fmt.Sprintf("%d: %s", i, lv), "labels": labelsFor("", i)})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

// maxBody is the limit for POST bodies. Smaller than the store's per-line limit (4 MiB).
const maxBody = 1 << 20

// acceptJSONPost rejects writes from other sites in a browser (audit S3).
//   - If there is an Origin, only this endpoint's own (127.0.0.1 / localhost) passes
//   - Content-Type must be application/json (a text/plain "simple request" arrives without a preflight)
//   - Body up to maxBody (413 beyond that)
func acceptJSONPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeErr(w, 405, errors.New("POST only"))
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "[::1]" && u.Hostname() != "::1") {
			writeErr(w, 403, fmt.Errorf("cross-origin request from %q is not allowed", origin))
			return false
		}
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeErr(w, 415, errors.New("Content-Type must be application/json"))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	return true
}

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
	writeJSON(w, 200, d)
}

// handleAnswered lists decisions a person answered (newest first). It is the material for correcting an answer (another /v1/label on the same trace: the new answer wins) and for asking the AI again (/v1/redo).
func (s *server) handleAnswered(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit < 1 || limit > 1000 {
		limit = 50
	}
	sp, err := s.spec(id)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, id)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	recs, err := s.st.Records(id, 0)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	labeled, err := s.st.LabeledTraces(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	superseded := map[string]bool{} // do not show a redone original record as answered either (the answer was copied to the new record; prevents inflated counts and a path where a correction to the original never reaches the new record)
	for _, rec := range recs {
		if rec.RedoOf != "" {
			superseded[rec.RedoOf] = true
		}
	}
	items := []map[string]any{}
	for _, rec := range recs {
		l, ok := labeled[rec.TraceID]
		if !ok || !store.HumanSource(l.Source) || superseded[rec.TraceID] {
			continue
		}
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "stale": !sp.LiveHash(rec.SpecHash), "rung": rec.RungOf(), "redo_of": rec.RedoOf,
			"answerable": len(rec.Inputs) > 0, "choices": truthChoices(sp), "project": rec.Project,
			"truth": l.Truth, "truth_at": l.At, "source": l.Source, "correct": store.Correct(rec.Top, l.Truth),
		})
		if legacyPlanningRecord(sp, rec) {
			items[len(items)-1]["legacy_planning"] = true
			if rec.Display != nil {
				items[len(items)-1]["record_question"] = rec.Display.Question
			}
			items[len(items)-1]["choices"] = []map[string]any{{"truth": "yes", "label": "yes", "labels": map[string]string{"ja": "はい"}}, {"truth": "no", "label": "no", "labels": map[string]string{"ja": "いいえ"}}}
		}
		items[len(items)-1]["original_inputs"], items[len(items)-1]["text_status"] = s.st.RecordText(rec)
		if len(items) >= limit {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items})
}

// handleRedo decides a past decision's text again with the current wording (spec) and line. It keeps a new record (redo_of = the original trace), and the original record leaves the queue.
// If a person already answered, that answer is the truth for the same text, so it is copied to the new record too (no need to answer again). Returns before and now side by side.
func (s *server) handleRedo(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec    string `json:"spec"`
		TraceID string `json:"trace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	rec, err := s.st.FindRecord(req.Spec, req.TraceID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if len(rec.Inputs) == 0 {
		writeErr(w, 409, errors.New("this decision kept no text, so it cannot be asked again"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, rec.Inputs, decide.Session{Tainted: rec.Tainted || s.o.ef.tainted}, s.o.ef.endpoints())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	attachReceipt(sp, s.st, d)
	resp := map[string]any{"before": map[string]any{"trace_id": rec.TraceID, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model, "stale": !sp.LiveHash(rec.SpecHash)}, "after": d, "recorded": false}
	if d.Band == decide.Abstain {
		writeJSON(w, 200, resp)
		return
	}
	newRec := recordOf(sp, d, rec.Inputs)
	newRec.RedoOf = rec.TraceID
	if err := recordDecisionRec(s.st, sp, d, newRec); err != nil { // same manners as /v1/decide (if act on a context rung, the machine truth for the minimal rung too)
		writeErr(w, 500, err)
		return
	}
	resp["recorded"] = true
	labeled, err := s.st.LabeledTraces(req.Spec)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if l, ok := labeled[rec.TraceID]; ok && store.HumanSource(l.Source) {
		copied, err := store.LabelsFor(&newRec, l.Truth, l.Weight, l.Source)
		if err == nil {
			err = s.st.AppendLabels(copied)
		}
		if err != nil {
			writeErr(w, 500, fmt.Errorf("carry the person's answer to the new decision: %w", err))
			return
		}
		resp["truth"] = l.Truth
		resp["correct_before"] = store.Correct(rec.Top, l.Truth)
		resp["correct_after"] = store.Correct(d.Top, l.Truth)
	}
	writeJSON(w, 200, resp)
}

func (s *server) handleLabel(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		TraceID string  `json:"trace_id"`
		Spec    string  `json:"spec"`
		Truth   string  `json:"truth"`
		Weight  float64 `json:"weight"`
		Source  string  `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	if req.TraceID == "" || req.Truth == "" {
		writeErr(w, 400, errors.New("trace_id and truth are required"))
		return
	}
	rec, err := s.st.FindRecord(req.Spec, req.TraceID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if req.Source == "" {
		req.Source = "ui"
	}
	labels, err := store.LabelsFor(rec, req.Truth, req.Weight, req.Source)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.st.AppendLabels(labels); err != nil { // write both rungs in one go (so only one never remains)
		writeErr(w, 500, err)
		return
	}
	l := labels[0]
	// An answer to a decision settled after adding context also goes to the minimal question (rungs = 2). Lines are drawn per rung
	resp := map[string]any{"trace_id": l.TraceID, "spec": l.SpecID, "model": l.Model, "truth": l.Truth, "weight": l.Weight, "source": l.Source, "rungs": len(labels), "calibrated": false}
	if s.o.autoCalibrate {
		if report, did := s.maybeCalibrate(rec.SpecID, rec.SpecHash, rec.Model); did {
			resp["calibrated"] = true
			resp["calibration"] = report
		}
	}
	writeJSON(w, 200, resp)
}

// maybeCalibrate calibrates and writes back only when the (hash, model) truths reach min_n and there is no line yet (or it is old).
// Even if it fails, the label handling stays successful (it is only noted in the report).
func (s *server) maybeCalibrate(specID, hash, model string) (string, bool) {
	sp, err := s.spec(specID)
	if err != nil || !sp.LiveHash(hash) || model == "" {
		return "", false
	}
	rs, rung := sp, "minimal" // this record's rung. A line is drawn only for this rung (other rungs' lines are not touched)
	if hash == sp.ContextHash() {
		rs, rung = sp.ContextSpec(), "context"
	}
	if c := rs.CalibrationFor(model); c != nil && c.SpecHash == hash {
		return "", false // a line already exists. Redraw it when drift says it got worse (not touched here)
	}
	labels, err := s.st.Labels(specID, hash, model)
	if err != nil || len(labels) < sp.Eval.MinN {
		return "", false
	}
	s.calibMu.Lock()
	defer s.calibMu.Unlock()
	var buf bytes.Buffer
	ef := s.o.ef
	ef.model = model
	err = func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("calibrate panicked: %v", r)
			}
		}()
		return runCalibrate(sp.Path(), calibrateOpts{output: &buf, ef: ef, storeDir: s.o.storeDir, labeled: "none", bins: 10, write: true, rung: rung})
	}()
	report := strings.TrimSpace(buf.String())
	result := calibrationResult{Reason: "written", MinN: rs.Eval.MinN, Hash: hash, Model: model, At: time.Now().Format("2006-01-02 15:04:05"), TriesWordings: containsString(hookSpecs, specID)}
	if err != nil {
		report += "\nERROR: " + err.Error()
		result.Reason = "error"
		var refused *calibrationRefusal
		if errors.As(err, &refused) {
			result.Reason, result.HumanTruths, result.MinN = refused.reason(), refused.humanTruths, refused.minN
			// every truth on one side is told as one kind of answer (as the per-model marker one_answer), not as "this wording cannot tell them apart"
			if result.Reason == "quality" && lineStateOf(rs, rs.Eval.MinN, model, hash, labels, false).Kind == lineOneAnswer {
				result.Reason = "one_answer"
			}
		}
	} else if after, loadErr := s.spec(specID); loadErr == nil {
		if rung == "context" {
			after = after.ContextSpec()
		}
		if after != nil {
			if c := after.CalibrationFor(model); c != nil {
				result.HumanTruths = c.NHuman
			}
		}
	}
	s.mu.Lock()
	s.lastCal[specID] = calibrationReport{log: result.At + "\n" + report, result: result}
	s.mu.Unlock()
	log.Printf("auto-calibrate %s (%s): %s", specID, model, lastLine(report))
	return report, err == nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// handleQueue lists records without a truth. ask band first, newest first within it.
func (s *server) handleQueue(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit < 1 || limit > 1000 {
		limit = 50
	}
	sp, err := s.spec(id)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, id)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	recs, err := s.st.Records(id, 0)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	labeled, err := s.st.LabeledTraces(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	rank := map[string]int{"ask": 0, "audit": 1, "act": 2, "discard": 3} // unsure → spot checks (1 in 20 of act) → the rest
	reviews, err := s.st.Reviews(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	superseded := map[string]bool{} // do not show a redone original record in the queue (the new decision appears instead)
	for _, rec := range recs {
		if rec.RedoOf != "" {
			superseded[rec.RedoOf] = true
		}
	}
	type queued struct {
		record store.Record
		audit  bool
		reason string
	}
	var pending []queued
	settled := map[string]bool{}
	if sp.ID == "wiki-conflict" {
		settled = settledDrafts(s.st)
	}
	for _, rec := range recs {
		if legacyPlanningRecord(sp, rec) || undecidedConflict(sp.ID, rec, labeled, settled) {
			continue
		}
		show, audit, reason := reviewQueueState(rec, labeled, reviews)
		if show && !superseded[rec.TraceID] {
			pending = append(pending, queued{rec, audit, reason})
		}
	}
	order := func(it queued) int {
		if it.audit {
			return rank["audit"]
		}
		return rank[it.record.Band]
	}
	sort.SliceStable(pending, func(i, j int) bool { return order(pending[i]) < order(pending[j]) })
	if len(pending) > limit {
		pending = pending[:limit]
	}
	// Reading original text and building display data are limited to the records actually returned.
	items := make([]map[string]any, 0, len(pending))
	for _, item := range pending {
		rec := item.record
		// A person cannot answer an (old) record without text. Give no button material and say answerable=false (truths from a program that knows the outcome come by another path)
		answerable := len(rec.Inputs) > 0
		choices := []map[string]any{}
		if answerable {
			choices = truthChoices(sp)
		}
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "raw": rec.Raw, "stale": !sp.LiveHash(rec.SpecHash),
			"answerable": answerable, "choices": choices, "rung": rec.RungOf(), "project": rec.Project,
			"audit": item.audit, "review_reason": item.reason, "waits_for_person": waitsForPersonRecord(id, item.record, item.audit),
		})
		if grade, ok := reviews[rec.TraceID]; ok {
			items[len(items)-1]["review"] = grade
		}
		items[len(items)-1]["original_inputs"], items[len(items)-1]["text_status"] = s.st.RecordText(rec)
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items})
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	var page map[string]any // where this page is (0.8.0): so an agent can answer the URL and port over MCP too
	if common, err := agentHome(); err == nil {
		if abs, err := filepath.Abs(s.o.storeDir); err == nil {
			if rec, ok := readPageRecord(common, filepath.Dir(abs)); ok && rec.PID == os.Getpid() { // only our own mark (do not answer with the mark of another serve started by hand)
				page = map[string]any{"url": rec.url() + "/", "port": rec.Port, "pid": rec.PID}
			}
		}
	}
	out := []map[string]any{}
	var person personWork // the summary at the top of the page (0.7.4): the person's effort summed over all specs
	for _, sp := range specs {
		titles := map[string]string{}
		questions := map[string]string{} // the page's "what it asks" (in the person's words)
		for lang, d := range sp.Display {
			if d.Title != "" {
				titles[lang] = d.Title
			}
			if d.Question != "" {
				questions[lang] = d.Question
			}
		}
		nRec, labs, err := s.st.Counts(sp.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		byKey := map[string]bool{}
		for _, l := range labs {
			byKey[l.Model+"\x00"+l.SpecHash] = true
		}
		keys := []map[string]any{} // [] even if empty (null makes the page crash)
		for k := range byKey {
			parts := strings.SplitN(k, "\x00", 2)
			model, hash := parts[0], parts[1]
			state := "collecting"
			method, rung := "", "minimal"
			rs := sp // this hash's rung
			if hash == sp.ContextHash() {
				rs, rung = sp.ContextSpec(), "context"
			}
			// n is the count calibration uses (0.9.3): the latest truth per input, from people and results only (machine truths and re-labels do not raise it)
			n := lineStateOf(rs, sp.Eval.MinN, model, hash, labs, false).N
			switch {
			case !sp.LiveHash(hash):
				state = "stale"
			case rs.CalibrationFor(model) != nil && rs.CalibrationFor(model).SpecHash == hash:
				state, method = "calibrated", rs.CalibrationFor(model).Method
			default:
				// Do not say "complete" from counts alone (0.8.3): separate having answers on only one side, tried and could not separate, and partway through automatic drawing
				tried := (hash == sp.Hash() || hash == sp.ContextHash()) && triedWithoutLine(s.st.Dir, sp, model)
				switch lineStateOf(rs, sp.Eval.MinN, model, hash, labs, tried).Kind {
				case lineReady:
					state = "ready"
				case lineOneAnswer:
					state = "one_answer"
				case lineNotSeparable:
					state = "not_separable"
				}
			}
			keys = append(keys, map[string]any{"model": model, "hash": hash, "n": n, "state": state, "method": method, "rung": rung})
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i]["model"] != keys[j]["model"] {
				return keys[i]["model"].(string) < keys[j]["model"].(string)
			}
			return keys[i]["rung"].(string) > keys[j]["rung"].(string) // minimal first
		})
		labeled, _ := s.st.LabeledTraces(sp.ID)
		reviews, rerr := s.st.Reviews(sp.ID)
		if rerr != nil {
			writeErr(w, 500, rerr)
			return
		}
		records, rerr := s.st.Records(sp.ID, 0)
		if rerr != nil {
			writeErr(w, 500, rerr)
			return
		}
		todo, reviewPending, legacyPlanning, waitingPerson, undecided := 0, 0, 0, 0, 0
		settled := map[string]bool{}
		if sp.ID == "wiki-conflict" {
			settled = settledDrafts(s.st)
		}
		for _, rec := range records {
			if legacyPlanningRecord(sp, rec) {
				legacyPlanning++
				continue
			}
			if undecidedConflict(sp.ID, rec, labeled, settled) { // the draft is saved, but it did not touch this page: settled without a truth (0.8.0)
				undecided++
				continue
			}
			if show, audit, _ := reviewQueueState(rec, labeled, reviews); show {
				todo++
				if waitsForPersonRecord(sp.ID, rec, audit) { // only spot checks and unsure decisions whose truth no outcome gives wait for a person's answer (0.8.0)
					waitingPerson++
				}
			}
			if _, graded := reviews[rec.TraceID]; rec.ReviewPending && !graded {
				if _, answered := labeled[rec.TraceID]; !answered {
					reviewPending++
				}
			}
		}
		// answered count = the number of texts (inputs_hash) a person answered. Answers copied by a redo and corrections to an answer on the same record are not counted
		answered := map[string]bool{}
		for _, l := range labeled {
			if store.HumanSource(l.Source) {
				answered[l.InputsHash] = true
			}
		}
		funnel, _ := s.st.Funnel(sp.ID, decide.DiscardUnsure(sp))
		person.add(countPersonWork(sp.ID, decide.DiscardUnsure(sp), records, labs, reviews, time.Now()))
		s.mu.Lock()
		last := s.lastCal[sp.ID]
		s.mu.Unlock()
		var result *calibrationResult
		if last.log != "" {
			result = &last.result
		}
		truthSource, personAnswers := "person", !machineOnlySpecs[sp.ID] // decisions people do not answer (wiki contradictions etc.): in 0.8.0, 60 items looked like they were waiting for confirmation
		if outcomeSpecs[sp.ID] {
			truthSource = "results"
		}
		awaiting, settledByAI := todo-waitingPerson, 0 // waiting for an outcome (the next request or the review's result gives the truth)
		if !outcomeSpecs[sp.ID] && personAnswers {     // act on a question a person gives truths to (outside spot checks) gets no outcome, so it counts as "the AI decided (no check needed)" (audit R5)
			settledByAI, awaiting = awaiting, 0
		}
		out = append(out, map[string]any{"id": sp.ID, "type": string(sp.Type), "hash": sp.Hash(), "titles": titles, "questions": questions, "question": strings.TrimSpace(sp.Question), "records": nRec, "labels": len(answered), "label_rows": len(labs),
			"unlabeled": todo, "waiting_for_person": waitingPerson, "awaiting_results": awaiting, "settled_by_ai": settledByAI, "undecided": undecided, "truth_source": truthSource, "person_answers": personAnswers, "review_pending": reviewPending, "review_quality": store.ReviewQuality(reviews, labeled), "min_n": sp.Eval.MinN, "keys": keys, "last_calibration": last.log, "calibration_result": result, "planning_context": needsPlanningContext(sp), "legacy_planning_records": legacyPlanning,
			"funnel": map[string]any{"minimal": funnel.Minimal, "context": funnel.Context, "human": funnel.Human, "human_per_thousand": funnel.HumanPerThousand()}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	modelName, endpoint := "local", s.o.ef.local // the AI's state (0.8.0): which model is deciding
	if usesHosted() {
		modelName, endpoint = "TypeSafe Jev", systemone.RedactURL(s.o.ef.hosted)
	}
	writeJSON(w, 200, map[string]any{"specs": out, "project_name": filepath.Base(s.o.projectScope), "auto_calibrate": s.o.autoCalibrate, "store": s.o.storeDir, "model": map[string]any{"name": modelName, "endpoint": endpoint},
		"summary": statusSummary(s.st, s.o.storeDir, s.o.projectScope, person, time.Now()), "page": page})
}

// handleUI serves the page (web/ built with Svelte). The build output web/dist is embedded in the binary (shikii.WebFS), so there are no external loads.
// "/" is index.html and "/assets/..." its JS and CSS. Any other unknown path is 404 (/v1/... is taken by the mux first).
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r) // no folder listing (/assets/). There is no HTML path other than "/"
		return
	}
	w.Header().Set("Cache-Control", "no-cache") // dist uses the same name every version (assets/index.js), so do not hold on to an old version
	webFiles.ServeHTTP(w, r)
}

var webFiles = http.FileServer(http.FS(mustSub(shikii.WebFS, "web/dist")))

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// writeUnknownSpec is the reply when the spec id cannot be resolved. If the id is empty, it says "a spec is needed" (when a first-time user sent only a trace_id to /v1/redo,
// `unknown spec ""` did not tell them what to add).
func writeUnknownSpec(w http.ResponseWriter, id string) {
	if id == "" {
		writeErr(w, 400, errors.New(`spec is required: the decision's spec id (the file name in decisions/ without .yaml, as in /v1/queue?spec=<id>)`))
		return
	}
	writeErr(w, 404, fmt.Errorf("unknown spec %q", id))
}

// localHostOnly passes only when Host is a local name (127.0.0.1, localhost, ::1). It refuses requests that came from another site's name through DNS rebinding
// (including GETs that read records and work records). A separate guard from the Origin check on POST (acceptJSONPost).
func localHostOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		switch strings.ToLower(strings.Trim(host, "[]")) {
		case "127.0.0.1", "localhost", "::1":
			h.ServeHTTP(w, r)
		default:
			writeErr(w, 403, fmt.Errorf("host %q is not allowed; open the page as http://127.0.0.1:<port>/ or http://localhost:<port>/", host))
		}
	})
}
