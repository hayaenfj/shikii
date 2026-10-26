// shikii is the CLI for running decision models.
//
//	shikii decide    <spec.yaml> -i k=v ... [--model M] [--hosted URL] [--local URL] [--tainted] [--store DIR]
//	shikii label     <trace_id> <truth> [--spec-id ID] [--weight W] [--source ask|undo|manual] [--store DIR]
//	shikii calibrate <spec.yaml> [--labeled file] [--model M] [--store DIR] [--write] [--force]
//	shikii hash      <spec.yaml>
//
// The hosted key is read from TYPESAFE_API_KEY. This process never fetches keys itself.
// Decisions are recorded in ./.shikii/ by default; label turns them into truth labels, and calibrate uses those.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

var stdout io.Writer = os.Stdout

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "version", "--version":
		fmt.Fprintf(stdout, "shikii %s\n", shikii.Version)
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "review":
		err = cmdReview(os.Args[2:])
	case "reflect":
		err = cmdReflect(os.Args[2:])
	case "decide":
		err = cmdDecide(os.Args[2:])
	case "label":
		err = cmdLabel(os.Args[2:])
	case "calibrate":
		err = cmdCalibrate(os.Args[2:])
	case "hash":
		err = cmdHash(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "drift":
		err = cmdDrift(os.Args[2:])
	case "bias":
		err = cmdBias(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "page":
		err = cmdPage(os.Args[2:])
	case "gc":
		err = cmdGC(os.Args[2:])
	case "wiki":
		err = cmdWiki(os.Args[2:])
	case "handoff":
		err = cmdHandoff(os.Args[2:])
	case "init", "--init":
		err = cmdInit(os.Args[2:])
	case "import":
		err = cmdImport(os.Args[2:])
	case "judge":
		err = cmdJudge(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "new":
		err = cmdNew(os.Args[2:])
	case "up":
		err = cmdUp(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "tune":
		err = cmdTune(os.Args[2:])
	case "hook":
		err = cmdHook(os.Args[2:])
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "guard":
		err = cmdGuard(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "shikii:", err)
		os.Exit(1)
	}
}

func usage() { fmt.Fprint(os.Stderr, usageText()) }

func usageText() string {
	return `usage (flags may come before or after the positional arguments):
  shikii init      (same as shikii --init) Turn on automatic recording for this project and set up the 4 kinds of decisions, the wiki, the work records folder, MCP, the working rules and the entry to the page. Safe to run again
  shikii new       <id> [--type choice|noul|score] [--question ...] [--option id=desc ...] [--input name ...] [--pii true|false] [--send-to local|hosted] [--from spec.yaml]
                   Answer a few questions to create .shikii/decisions/<id>.yaml
  shikii up        [--hf repo:quant | --gguf file --alias name] [--port 8080]
                   Start a local model (finds llama-server; for a plain GGUF it also puts shikii-readout in front)
  shikii serve     [--port 8095] [--specs DIR] [--store DIR] [--local URL] [--hosted URL] [--proposer URL] [--auto-calibrate=false]
                   HTTP endpoint + the page for checking answers and question wordings. Specs are in <store>/decisions, the wiki in <store>/wiki (base layout: everything shikii creates lives under .shikii/)
  shikii mcp       [--specs DIR] [--store DIR] [--local URL] [--hosted URL] [--proposer URL]
                   Serve the same features as a stdio MCP server (tools: shikii_specs / shikii_decide / shikii_label / shikii_status / shikii_tune / shikii_wiki / shikii_review / shikii_reflect. A person adopts new question wordings)
  shikii doctor    [--specs DIR] [--store DIR] [--local URL] [--hosted URL]
  shikii update    Check the latest stable release and update shikii and readout in the same place as the running shikii
  shikii hook      install [--yes] | uninstall [--yes] | user-prompt-submit | stop | subagent-start | plan query|submit | variants submit
                   As a Claude Code / Codex hook, measure how the AI itself thinks at 2 points: before thinking (will this request end up changing files, should the plan be agreed before starting) and
                   after thinking (did the changes stay within what was asked, was the request finished). The store is the project's .shikii/, the model is :8080, the page uses a free port per project (shikii page). Quotes past conversations and wiki lessons and hands the organizing to the agent. Advice from decisions starts only after a line is drawn. Never blocks tools
  shikii hook plan query | submit (JSON on stdin)
                   Your usual agent organizes the request, the agreed plan and open questions before starting, with the original text, and decides. Not treated as a person's input or as a truth
  shikii uninstall [--yes]
                   Remove what shikii put into the agents: its hooks (~/.claude/settings.json, ~/.codex/hooks.json), [mcp_servers.shikii] in ~/.codex/config.toml,
                   the shikii entry in each registered project's .mcp.json and opencode.json, the skill (~/.claude/skills/shikii, ~/.agents/skills/shikii), and stop its pages.
                   Without --yes it only lists them. The data (each project's .shikii/ and ~/.shikii/agent) stays; the binaries stay and it prints the rm command for them
  shikii decide   <spec.yaml> -i key=value ... [--model M] [--hosted URL] [--local URL] [--tainted] [--store DIR] [--no-record] [--json]
                   If the spec has state.context, ask again with the context inputs added only when unsure (ask; for choice also discard) (separate hash, separate line). An answer that reaches confidence there is kept as a truth for the minimal question
  shikii label     <trace_id> <truth> [--spec-id ID] [--weight W] [--source ask|outcome|manual] [--store DIR]
  shikii label     --last --spec-id ID <truth>
  shikii calibrate <spec.yaml> [--labeled file] [--model M] [--local URL] [--hosted URL] [--store DIR] [--bins 10] [--write] [--force]
                   Draw the line from truths. Truths missing for the current (hash, model) are asked again from the stored text. With state.context, the context rung gets its own line
                   If a --labeled row has raw (a P(yes) number for noul, {id: p} for choice / score), measure with that probability without calling the model (to measure another tool's probabilities on the same scale)
  shikii status    [spec.yaml...] [--store DIR]      Counts of records and truths, counts per rung (human judgments per 1000 decisions), state of the lines
  shikii page                                         URL, port and pid of this project's page (starts it if not running)
  shikii tune      <spec.yaml> [--labeled file] [--local URL] [--store DIR] [--folds 5] [--propose N --proposer URL [--proposer-model M]] [--write]
                   Score the spec's variants: (candidate wordings) against the saved truths. Only a candidate that beats the current one on holdout and is confirmed to mean the same (by an agent or a person) becomes the new version. Results go to <store>/wiki/runs/
  shikii import    <past.csv> --spec <spec.yaml> [--out truths.json]
                   Turn a local table of past cases (input columns + truth [+ weight]) into saved truths (JSON). Fills the 50 cases needed before a line from your business records
  shikii judge     <spec.yaml> --local <judge URL> [--model M] [--store DIR]
                   Judge: ask another (larger) model the same spec. If its agreement with human truths reaches act, add weighted truths (source judge) to records without one. If the judge is unsure too, it goes to a person
  shikii wiki verify [pattern.md...] [--local URL] [--labeled file] [--store DIR]
                   Re-test the claims in <store>/wiki/patterns (front matter: claim / applies_to / evidence / retest) with the current model and append the evidence. Only a person changes their status
  shikii wiki query [search text or source ID] | ingest (JSON on stdin) | lint
                   Turn everyday conversations into sourced lessons in the project's .shikii/. The AI writes the summaries; shikii manages the original text, index, versions and checks. Lessons are hypotheses and never count as truths or calibration
  shikii handoff   list | read <path> | write <path> | append <path> (body on stdin)
                   Work records (.shikii/handoff/): working files you rewrite as you go (audit reports, release steps, diffs, images). Lessons go in the wiki; evidence as of when it was checked goes in the wiki's sources
  shikii guard     list | add (JSON on stdin) | enable <id> | disable <id> | try
                   Turn a person's correction into a "blocking check" (paired-files / forbidden-words / commit-body). enable turns it on after confirming that it blocks a negative example
  shikii version                                  Print the version
  shikii review query | submit (JSON on stdin)       The AI gives provisional scores with reasons from conversations and outcomes. A person checks the samples and held items, and corrections feed the next scoring. Provisional scores are stored apart from truths and calibration
  shikii reflect query | submit (JSON on stdin)      Combine scoring and wiki organizing into one confirmation and one bulk save (query returns 2 at a time from the oldest conversations, submit takes any number). Pass reviews and wiki, then check complete and each item's error
  shikii check     <spec.yaml>... [--store DIR]      CI: fail lines whose wording changed, hand-written lines, and lines with too little precision
  shikii drift     <spec.yaml> --model M [--window N] [--store DIR]
  shikii bias      <spec.yaml> [--rows K] [--permutations N] [--store DIR] [--labeled file] [--local URL] [--hosted URL]
                   Measure whether the answer changes with the order of the options. Texts come from the same saved truths as calibrate (store, or --labeled)
  shikii gc        [spec.yaml...] [--older-than 30d] [--apply] [--store DIR]
  shikii hash      <spec.yaml>
`
}

// endpointFlags are the endpoint flags shared by decide / calibrate.
type endpointFlags struct {
	model, hosted, local string
	tainted              bool
}

func (f *endpointFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.model, "model", "jev-latest", "model name (sent in the request; in calibrate it is also the calibration key)")
	fs.StringVar(&f.hosted, "hosted", "https://api.typesafe.ai", "hosted URL. A URL with a path is used as is (e.g. https://openrouter.ai/api/alpha/decisions). Key from DECISIONS_API_KEY or TYPESAFE_API_KEY")
	fs.StringVar(&f.local, "local", "", "base URL of a local server (llama.cpp etc.)")
	fs.BoolVar(&f.tainted, "tainted", false, "session that read production data: never send to hosted")
}

// hostedKey is the key for hosted: DECISIONS_API_KEY (generic) or TYPESAFE_API_KEY. This process never fetches keys itself.
func hostedKey() string {
	if k := os.Getenv("DECISIONS_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("TYPESAFE_API_KEY")
}

// modelChoice is SHIKII_MODEL (local / hosted). Use local to keep the local model even when a key is set. Empty means both (hosted first if there is a key).
func modelChoice() string {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("SHIKII_MODEL"))); v {
	case "local", "hosted":
		return v
	}
	return ""
}

func (f *endpointFlags) endpoints() []decide.Endpoint {
	var eps []decide.Endpoint
	if key := hostedKey(); key != "" && f.hosted != "" && modelChoice() != "local" {
		eps = append(eps, decide.Endpoint{Kind: "hosted", Client: systemone.New(f.hosted, key, f.model)})
	}
	if f.local != "" && modelChoice() != "hosted" {
		eps = append(eps, decide.Endpoint{Kind: "local", Client: systemone.New(f.local, "", f.model)})
	}
	return eps
}

func bindStore(fs *flag.FlagSet) *string {
	return fs.String("store", ".shikii", "where records and labels are stored")
}

type kvFlag map[string]string

func (k kvFlag) String() string { return "" }
func (k kvFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("want key=value, got %q", v)
	}
	k[v[:i]] = v[i+1:]
	return nil
}

// The report destination is passed per call. HTTP handling never swaps the process-wide stdout.
func reportWriter(writers ...io.Writer) io.Writer {
	if len(writers) > 0 && writers[0] != nil {
		return writers[0]
	}
	return stdout
}

// ---------------------------------------------------------------- decide

func cmdDecide(args []string) error {
	fs := flag.NewFlagSet("decide", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	noRecord := fs.Bool("no-record", false, "do not record")
	inputs := kvFlag{}
	fs.Var(inputs, "i", "input key=value (repeatable)")
	asJSON := fs.Bool("json", false, "print the Decision as JSON")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{Tainted: ef.tainted}, ef.endpoints())
	if err != nil {
		return err
	}
	attachReceipt(sp, store.Store{Dir: *storeDir}, d)
	if !*noRecord && d.Band != decide.Abstain {
		if err := recordDecision(store.Store{Dir: *storeDir}, sp, d, inputs); err != nil {
			return err
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s endpoint=%s trace=%s\n", d.SpecID, d.SpecHash, d.Model, d.Endpoint, d.TraceID)
	fmt.Fprintf(stdout, "band=%s top=%s p=%.3f calibrated=%v", d.Band, d.Top, d.P, d.IsCalibrated)
	if sp.Type == spec.Score {
		fmt.Fprintf(stdout, " score=%.2f", d.Score)
	}
	fmt.Fprintln(stdout)
	if d.Reason != "" {
		fmt.Fprintln(stdout, "reason:", d.Reason)
	}
	if d.Minimal != nil {
		fmt.Fprintf(stdout, "rung=context (the minimal question was unsure: top=%s p=%.3f band=%s; asked again with state.context)\n", d.Minimal.Top, d.Minimal.P, d.Minimal.Band)
	}
	if r := d.Receipt; r != nil {
		fmt.Fprintf(stdout, "receipt: line %s fitted %s on %d truths (%d human); human-checked act decisions %d, precision %.3f\n", r.Method, r.FittedAt, r.N, r.NHuman, r.HumanCheckedAct, r.HumanPrecision)
	}
	if len(d.Raw) > 0 {
		fmt.Fprintln(stdout, "raw:", fmtDist(d.Raw))
		if d.IsCalibrated {
			fmt.Fprintln(stdout, "calibrated:", fmtDist(d.Calibrated))
		}
	}
	return nil
}

// recordOf turns a Decision into a record. The text is always kept.
// copyInputs copies the text (kept in records and labels).
func copyInputs(inputs map[string]string) map[string]string {
	if len(inputs) == 0 {
		return nil
	}
	out := make(map[string]string, len(inputs))
	for k, v := range inputs {
		out[k] = v
	}
	return out
}

func recordOf(sp *spec.Spec, d *decide.Decision, inputs map[string]string) store.Record {
	return store.Record{
		TraceID: d.TraceID, SpecID: d.SpecID, SpecHash: d.SpecHash, Type: d.Type,
		Model: d.Model, Endpoint: d.Endpoint,
		Inputs: copyInputs(inputs), InputsHash: d.InputsHash,
		Raw: d.Raw, Calibrated: d.Calibrated, IsCalibrated: d.IsCalibrated,
		Top: d.Top, P: d.P, Band: string(d.Band), Tainted: d.Tainted, Rung: d.Rung, Minimal: d.Minimal, At: d.At,
	}
}

// recordDecision records a decision. When the context rung reaches confidence (act), that answer is kept as a truth for the minimal question:
// the upper rung's answer becomes the lower rung's truth (weight is the calibrated probability, source is context). If a person answers later, theirs is newer and wins.
func recordDecision(st store.Store, sp *spec.Spec, d *decide.Decision, inputs map[string]string) error {
	return recordDecisionRec(st, sp, d, recordOf(sp, d, inputs))
}

// recordDecisionRec writes rec (recordOf plus marks; RedoOf for a redo). This is the one place that defines how decisions are recorded (audit 6 item 5).
func recordDecisionRec(st store.Store, sp *spec.Spec, d *decide.Decision, rec store.Record) error {
	if err := st.AppendRecord(rec); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	if d.Rung != "context" || d.Band != decide.Act || d.Minimal == nil {
		return nil
	}
	if sp.Type == spec.Score && sp.Thresholds.Level > 0 {
		return nil // P is P(level ≥ k) and Top is the highest level. They are different events, so they cannot become one truth (audit round 3 item 8)
	}
	inputsHash := d.Minimal.InputsHash
	if inputsHash == "" {
		inputsHash = rec.InputsHash
	}
	l := store.Label{
		TraceID: rec.TraceID, SpecID: rec.SpecID, SpecHash: d.Minimal.SpecHash, Type: rec.Type, Model: rec.Model,
		InputsHash: inputsHash, Inputs: rec.Inputs, Raw: d.Minimal.Raw,
		Truth: d.Top, Weight: d.P, Source: "context", Tainted: rec.Tainted, Rung: "minimal", At: rec.At,
	}
	if err := st.AppendLabel(l); err != nil {
		return fmt.Errorf("record context truth: %w", err)
	}
	return nil
}

// attachReceipt attaches a receipt to an act decision: that rung's line (method, date, number of truths), and the number and precision of act decisions a person checked (human truths in the store only).
func attachReceipt(sp *spec.Spec, st store.Store, d *decide.Decision) {
	if d.Band != decide.Act {
		return
	}
	rs := sp
	if d.Rung == "context" && sp.ContextSpec() != nil {
		rs = sp.ContextSpec()
	}
	c := rs.CalibrationFor(d.Model)
	if c == nil {
		return
	}
	r := &decide.Receipt{SpecHash: c.SpecHash, Model: c.Model, Method: c.Method, FittedAt: c.FittedAt, N: c.N, NHuman: c.NHuman, PrecisionAtFit: c.PrecisionAt}
	if labels, err := st.Labels(sp.ID, c.SpecHash, d.Model); err == nil {
		var human []store.Label
		for _, l := range store.LatestPerInput(labels) {
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		if len(human) > 0 {
			r.HumanPrecision, _, r.HumanCheckedAct = evaluateCalibration(rs, *c, human)
		}
	}
	d.Receipt = r
}

// ---------------------------------------------------------------- label

func cmdLabel(args []string) error {
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	storeDir := bindStore(fs)
	specID := fs.String("spec-id", "", "spec to search records in (all when omitted)")
	weight := fs.Float64("weight", 1, "label weight (below 1 for weak labels)")
	source := fs.String("source", "manual", "label source: ask | outcome | manual | ui | mcp etc. (calibrate sets replay and file)")
	last := fs.Bool("last", false, "instead of a trace_id, label the latest record of --spec-id")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	var rec *store.Record
	var truth string
	switch {
	case *last:
		if len(pos) != 1 || *specID == "" {
			return fmt.Errorf("usage: shikii label --last --spec-id ID <truth>")
		}
		r, err := st.LastRecord(*specID)
		if err != nil {
			return err
		}
		rec, truth = r, pos[0]
	default:
		if len(pos) != 2 {
			return fmt.Errorf("usage: shikii label <trace_id> <truth>  |  shikii label --last --spec-id ID <truth>")
		}
		r, err := st.FindRecord(*specID, pos[0])
		if err != nil {
			return err
		}
		rec, truth = r, pos[1]
	}
	labels, err := store.LabelsFor(rec, truth, *weight, *source)
	if err != nil {
		return err
	}
	if err := st.AppendLabels(labels); err != nil { // write both rungs in one go (never just one of them)
		return err
	}
	for _, l := range labels {
		fmt.Fprintf(stdout, "labeled %s (%s, %s, %s rung) truth=%s weight=%.2f source=%s\n", l.TraceID, l.SpecID, l.Model, l.Rung, l.Truth, l.Weight, l.Source)
	}
	return nil
}

// ---------------------------------------------------------------- calibrate

// labeled is one row of labeled data. Truth is true/false or "yes"/"no" for noul, and an id/number for choice/score.
// If Raw (noul's raw probability) is present, the model is not called.
type labeled struct {
	Inputs map[string]string `json:"inputs"`
	Truth  json.RawMessage   `json:"truth"`
	Weight float64           `json:"weight,omitempty"`
	// Raw brings in a probability (a row that does not call the model). For noul it is one P(yes) number; for choice / score an {id: p} object.
	// Used to measure probabilities from another tool on the same scale, like the LoRA arm of the comparison bench.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// rawFromRow turns a row's raw into a distribution, or nil if absent. A key not among the spec's options is an error (never silently dropped).
func rawFromRow(sp *spec.Spec, raw json.RawMessage) (map[string]float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var p float64
	if err := json.Unmarshal(raw, &p); err == nil {
		if sp.Type != spec.Noul {
			return nil, fmt.Errorf("raw: a single number is only for noul; give {id: probability} for %s", sp.Type)
		}
		if p < 0 || p > 1 {
			return nil, fmt.Errorf("raw: P(yes) must be in 0..1, got %g", p)
		}
		return map[string]float64{"yes": p, "no": 1 - p}, nil
	}
	var dist map[string]float64
	if err := json.Unmarshal(raw, &dist); err != nil {
		return nil, fmt.Errorf("raw: want a number (noul) or {id: probability}: %w", err)
	}
	// All keys must be present (noul needs yes; no is filled in), each value in 0..1, summing close to 1. Missing or out-of-range values are never silently set to 0 (audit round 4 item 9)
	if sp.Type == spec.Noul {
		if _, ok := dist["yes"]; !ok {
			return nil, errors.New("raw: noul needs \"yes\"")
		}
		if _, ok := dist["no"]; !ok {
			dist["no"] = 1 - dist["yes"]
		}
	}
	sum := 0.0
	for k, v := range dist {
		if !sp.HasTruthKey(k) {
			return nil, fmt.Errorf("raw: %q is not an option of this spec", k)
		}
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("raw: %q must be in 0..1, got %g", k, v)
		}
		sum += v
	}
	for _, id := range sp.TruthKeys() {
		if _, ok := dist[id]; !ok {
			return nil, fmt.Errorf("raw: missing %q (give every option's probability)", id)
		}
	}
	if sum < 0.98 || sum > 1.02 {
		return nil, fmt.Errorf("raw: probabilities sum to %.3f, want 1", sum)
	}
	return dist, nil
}

func parseTruth(t spec.Type, raw json.RawMessage) (string, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if t != spec.Noul {
			return "", fmt.Errorf("%s truth must be a string, got %s", t, raw)
		}
		if b {
			return "yes", nil
		}
		return "no", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("truth must be bool or string, got %s", raw)
	}
	// a noul truth is yes or no, as in store.LabelFor. Any other string ("true", "Yes") used to count as "no" (0.9.4 audit)
	if t == spec.Noul && s != "yes" && s != "no" {
		return "", fmt.Errorf("noul truth must be yes or no (or true / false), got %q", s)
	}
	return s, nil
}

func cmdCalibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	labeledPath := fs.String("labeled", "", "labeled data (default: the spec's eval.labeled; without it, only store labels; none skips the spec default too)")
	bins := fs.Int("bins", 10, "number of bins in the reliability diagram")
	write := fs.Bool("write", false, "write the calibration back to the spec")
	force := fs.Bool("force", false, "write back even if inverted or lacking precision")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	return runCalibrate(path, calibrateOpts{ef: ef, storeDir: *storeDir, labeled: *labeledPath, bins: *bins, write: *write, force: *force})
}

// calibrateOpts are the calibrate options. The CLI and serve (automatic calibration) use the same values.
type calibrateOpts struct {
	output   io.Writer
	ef       endpointFlags
	storeDir string
	labeled  string // "" = the spec's eval.labeled, "none" = use no file
	bins     int
	write    bool
	force    bool
	rung     string // "" = both rungs, "minimal" | "context" = draw only that rung (serve's automatic calibration only does rungs with enough truths)
}

// Splits the reasons calibration cannot be saved into too few truths and prediction quality. The page uses the same reasons.
type calibrationRefusal struct {
	humanTruths int
	minN        int
}

func (e *calibrationRefusal) reason() string {
	if e.humanTruths < e.minN {
		return "insufficient"
	}
	return "quality"
}

func (e *calibrationRefusal) Error() string {
	if e.reason() == "insufficient" {
		return fmt.Sprintf("calibration not saved: there are %d truths from people or outcomes, short of the %d required. Add truths with shikii serve, shikii label or shikii import. For a trial only, you can lower the spec's eval.min_n.", e.humanTruths, e.minN)
	}
	return fmt.Sprintf("calibration not saved: there are %d truths, but with the current question and the model's predictions the conditions for acting automatically are not met. Review the question or the model.", e.humanTruths)
}

// runCalibrate is the body of calibrate. It prints its report to stdout.
func runCalibrate(path string, o calibrateOpts) error {
	stdout := reportWriter(o.output)
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	wrote := false
	// The minimal-question rung. Failing to draw a line here is an error (as before).
	if o.rung != "context" {
		cal, ok, err := calibrateRung(sp, o)
		if err != nil {
			return err
		}
		if o.write && !ok {
			return &calibrationRefusal{humanTruths: cal.NHuman, minN: sp.Eval.MinN}
		}
		if o.write {
			sp.PutCalibration(cal)
			wrote = true
		}
	}
	// The context rung (if any). Its truths are only those a person gave to decisions settled with added context. If there are none yet, say so and move on.
	if csp := sp.ContextSpec(); csp != nil && o.rung != "minimal" {
		fmt.Fprintf(stdout, "\ncontext rung: the same question asked with state.context (hash=%s)\n", csp.Hash())
		co := o
		co.labeled = "none" // file rows belong to the minimal question
		ccal, cok, err := calibrateRung(csp, co)
		switch {
		case errors.Is(err, errNoLabels):
			fmt.Fprintln(stdout, "note: no truths on the context rung yet (they come from people answering decisions that went through it)")
		case err != nil:
			return err
		case o.write && cok:
			sp.PutCalibration(ccal)
			wrote = true
		case o.write && o.rung == "context":
			return &calibrationRefusal{humanTruths: ccal.NHuman, minN: csp.Eval.MinN}
		case o.write:
			fmt.Fprintln(stdout, "note: context rung not written (see WARNING above)")
		}
	}
	if wrote {
		if err := sp.Save(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "wrote calibration to", sp.Path())
	}
	return nil
}

var errNoLabels = errors.New("no labels")

// calibrateRung draws the line for one rung (one spec hash). It always prints the report and returns whether it may be written (ok). It does not write the line itself (the caller writes them together).
func calibrateRung(sp *spec.Spec, o calibrateOpts) (spec.Calibration, bool, error) {
	stdout := reportWriter(o.output)
	ef, storeDir, labeledPath, bins, force := o.ef, &o.storeDir, &o.labeled, &o.bins, &o.force
	var cal spec.Calibration
	if *bins <= 0 {
		*bins = 10
	}
	if *labeledPath == "none" {
		*labeledPath = "" // proceed with store labels and re-asking only
	} else if *labeledPath == "" {
		// The spec's default file may be missing (proceed with store labels only). If given explicitly with --labeled, a missing file is an error.
		if _, err := os.Stat(sp.Eval.Labeled); sp.Eval.Labeled != "" && err == nil {
			*labeledPath = sp.Eval.Labeled
		} else if sp.Eval.Labeled != "" {
			fmt.Fprintf(stdout, "note: eval.labeled %s not found; using store labels only\n", sp.Eval.Labeled)
		}
	}

	// 1. The labeled file (if any). For rows without Raw, use a store label with the same inputs if there is one; otherwise ask the model.
	st := store.Store{Dir: *storeDir}
	labels, model, err := labelsFromFile(sp, *labeledPath, ef, st, stdout)
	if err != nil {
		return cal, false, err
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if l.InputsHash != "" {
			seen[l.InputsHash] = true
		}
	}
	// 2. Re-ask: for truths given under old wording or another model, ask again with the current (hash, model) from records that still have their text.
	//    A truth is decided by a person and does not depend on wording or model. Only the probability is fetched again.
	replayed, replayModel, err := replayTruths(sp, st, ef, model, seen, stdout)
	if err != nil {
		return cal, false, err
	}
	if model == "" {
		model = replayModel
	}
	if model == "" {
		model = ef.model
	}
	// 3. Labels collected in the store: all that match (hash, model). Keep them even if the file has the same input:
	//    if a correction a person added later is newer, it wins in LatestPerInput below (audit C2).
	stored, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return cal, false, err
	}
	replayedSet, err := storedInputs(st, sp, model)
	if err != nil {
		return cal, false, err
	}
	for _, l := range replayed {
		if replayedSet[l.InputsHash] {
			continue
		}
		replayedSet[l.InputsHash] = true
		if err := st.AppendLabel(l); err != nil {
			return cal, false, fmt.Errorf("persist replayed label: %w", err)
		}
		stored = append(stored, l)
	}
	// 4. Keep in the store the labels for file rows we got by asking the model, so check / drift can verify without the model later.
	persisted := 0
	storedSet, err := storedInputs(st, sp, model)
	if err != nil {
		return cal, false, err
	}
	for _, l := range labels {
		if l.Model == "" || l.InputsHash == "" {
			continue // rows with raw (model not called) are not kept
		}
		if storedSet[l.InputsHash] {
			continue
		}
		storedSet[l.InputsHash] = true
		if err := st.AppendLabel(l); err != nil {
			return cal, false, fmt.Errorf("persist label: %w", err)
		}
		persisted++
	}
	if persisted > 0 {
		fmt.Fprintf(stdout, "persisted %d queried labels to %s\n", persisted, *storeDir)
	}
	fileN, storedN := len(labels), len(stored)
	labels = append(labels, stored...)
	// The latest truth per input (counted the same way as check / drift). A correction replaces; duplicates of one input count once
	if before := len(labels); before > 0 {
		labels = store.LatestPerInput(labels)
		if len(labels) < before {
			fmt.Fprintf(stdout, "deduplicated: %d labels on %d distinct inputs (latest truth per input wins)\n", before, len(labels))
		}
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s n=%d (file=%d store=%d replay=%d)\n", sp.ID, sp.Hash(), model, len(labels), fileN, storedN, len(replayed))
	if len(labels) == 0 {
		return cal, false, fmt.Errorf("calibrate: %w for (hash=%s, model=%s)", errNoLabels, sp.Hash(), model)
	}
	// Print the report even with too few. Only writing back is stopped. Count only truths decided by people (or outcomes): never draw a line from machine truths (upper-rung answers) alone (audit round 3 item 2)
	nHuman := 0
	for _, l := range labels {
		if store.HumanSource(l.Source) {
			nHuman++
		}
	}
	underMin := nHuman < sp.Eval.MinN
	if underMin {
		fmt.Fprintf(stdout, "WARNING: %d human truths (%d samples in all) < eval.min_n=%d; report only, will not write (collect more, or lower eval.min_n in the spec for a trial)\n", nHuman, len(labels), sp.Eval.MinN)
	}

	// Always print the report. Whether it may be written back (ok) is returned separately and only matters with --write.
	var ok bool
	switch sp.Type {
	case spec.Noul:
		cal, ok, err = calibrateNoul(sp, labels, *bins, stdout)
	default:
		cal, ok, err = verifyTop(sp, labels, *bins, stdout)
	}
	if err != nil {
		return cal, false, err
	}
	cal.Model, cal.SpecHash, cal.N, cal.NHuman = model, sp.Hash(), len(labels), nHuman
	cal.FittedOn, cal.FittedAt = *labeledPath, time.Now().Format("2006-01-02")
	// --force overrides a refusal of a fitted line, never the lack of one: calibrateNoul returns no method when no line fits (one side only, or a
	// higher probability goes with "no"), and writing that empty block would replace this model's line with one nothing can use (0.9.4 audit)
	return cal, ((ok && !underMin) || *force) && cal.Method != "", nil
}

// labelsFromFile converts a labeled file into store.Label form. If any rows were asked of the model, it also returns that actual model name.
// For rows without Raw, reuse a store label with the same input (inputs_hash) under the --model key (so recalibration works without the model).
func labelsFromFile(sp *spec.Spec, path string, ef endpointFlags, st store.Store, reports ...io.Writer) ([]store.Label, string, error) {
	stdout := reportWriter(reports...)
	if path == "" {
		return nil, "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var rows []labeled
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	eps := ef.endpoints()
	sess := decide.Session{Tainted: ef.tainted}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Earlier labels for the same input (to reuse raw and time). If several, take the oldest = the first time it was asked.
	// Borrowing the time of a correction a person added later would give the file's truth the same time and make it win in LatestPerInput (audit C2).
	stored := map[string]store.Label{}
	if prior, err := st.Labels(sp.ID, sp.Hash(), ef.model); err == nil {
		for _, l := range prior {
			if cur, ok := stored[l.InputsHash]; !ok || l.At.Before(cur.At) {
				stored[l.InputsHash] = l
			}
		}
	}
	// A file row's time is "when that input was first brought in" (the oldest label under the current wording, any model).
	// Using "now" every time would make it newer than a person's later correction, so it would win.
	firstSeen := map[string]time.Time{}
	if _, all, err := st.Counts(sp.ID); err == nil {
		for _, l := range all {
			if l.SpecHash != sp.Hash() || l.InputsHash == "" {
				continue
			}
			if cur, ok := firstSeen[l.InputsHash]; !ok || l.At.Before(cur) {
				firstSeen[l.InputsHash] = l.At
			}
		}
	}
	reused := 0

	model := ""
	out := make([]store.Label, 0, len(rows))
	for i, r := range rows {
		truth, err := parseTruth(sp.Type, r.Truth)
		if err != nil {
			return nil, "", fmt.Errorf("row %d: %w", i, err)
		}
		l := store.Label{SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Truth: truth, Weight: r.Weight, Source: "file", At: time.Now()}
		given, err := rawFromRow(sp, r.Raw)
		if err != nil {
			return nil, "", fmt.Errorf("row %d: %w", i, err)
		}
		switch {
		case given != nil:
			l.Raw = given // probability brought in: the model is not called
		case func() bool { _, ok := stored[store.HashInputs(r.Inputs)]; return ok }():
			prior := stored[store.HashInputs(r.Inputs)]
			l.Raw, l.Model, l.TraceID, l.InputsHash = prior.Raw, prior.Model, prior.TraceID, prior.InputsHash
			l.Inputs = copyInputs(r.Inputs)
			if !prior.At.IsZero() {
				l.At = prior.At
			}
			if model == "" {
				model = prior.Model
			}
			reused++
		default:
			d, err := decide.Decide(ctx, sp.WithoutContext(), r.Inputs, sess, eps) // the file's truths belong to the minimal-question rung
			if err != nil {
				return nil, "", fmt.Errorf("row %d: %w", i, err)
			}
			if d.Band == decide.Abstain {
				return nil, "", fmt.Errorf("row %d: abstained: %s", i, d.Reason)
			}
			l.Raw, l.Model, l.TraceID, l.InputsHash = d.Raw, d.Model, d.TraceID, d.InputsHash
			l.Inputs = copyInputs(r.Inputs) // keep the text along with the truth (material for re-asking)
			if model == "" {
				model = d.Model
			} else if model != d.Model {
				return nil, "", fmt.Errorf("row %d: model changed mid-run (%s → %s)", i, model, d.Model)
			}
		}
		if l.Weight <= 0 {
			l.Weight = 1
		}
		if first, ok := firstSeen[l.InputsHash]; ok && l.InputsHash != "" {
			l.At = first
		}
		out = append(out, l)
	}
	if reused > 0 {
		fmt.Fprintf(stdout, "reused %d stored labels for file rows (model=%s); queried %d\n", reused, ef.model, len(out)-reused)
	}
	return out, model, nil
}

// alreadyStored reports whether a label with the same (hash, model, inputs_hash) is in the store.
func storedInputs(st store.Store, sp *spec.Spec, model string) (map[string]bool, error) {
	existing, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(existing))
	for _, e := range existing {
		out[e.InputsHash] = true
	}
	return out, nil
}

// replayTruths brings the "saved truths" to the current (spec_hash, model).
// From all of the spec's labels in the store (including old hashes and other models), take the latest truth per input,
// and for those whose record still has its text, ask the current model again with the current wording and turn them into labels.
// Inputs in seen (handled on the file side) are skipped. If model is empty, return the model name of the first answer.
// Truths that cannot be re-asked (no text) are counted and the reason printed. Never silently 0.
func replayTruths(sp *spec.Spec, st store.Store, ef endpointFlags, model string, seen map[string]bool, reports ...io.Writer) ([]store.Label, string, error) {
	stdout := reportWriter(reports...)
	_, all, err := st.Counts(sp.ID)
	if err != nil {
		return nil, model, err
	}
	// The latest truth per input (if one input has several, the newer one)
	type truth struct {
		truth  string
		weight float64
		at     time.Time
	}
	bank := map[string]truth{}
	var human []store.Label
	for _, l := range all {
		if store.HumanSource(l.Source) { // do not re-ask machine truths (upper-rung answers): re-asking at another rung gives the same model's answer and it would pass as a human truth (audit round 3 item 1)
			human = append(human, l)
		}
	}
	for _, l := range store.LatestPerInput(human) {
		if l.InputsHash == "" || seen[l.InputsHash] {
			continue
		}
		bank[l.InputsHash] = truth{l.Truth, l.Weight, l.At}
	}
	if len(bank) == 0 {
		return nil, model, nil
	}
	// Do not re-ask what already exists for the current (hash, model). If model is not set yet, settle it with the first answer and then check.
	current := map[string]bool{}
	loadCurrent := func(m string) error {
		have, err := st.Labels(sp.ID, sp.Hash(), m)
		if err != nil {
			return err
		}
		for _, l := range have {
			current[l.InputsHash] = true
		}
		return nil
	}
	// Even with no model set, check once with the --model value (it has a default). If the actual model name differs, check again after the first answer.
	guess := model
	if guess == "" {
		guess = ef.model
	}
	if err := loadCurrent(guess); err != nil {
		return nil, model, err
	}
	texts, err := st.InputsByHash(sp.ID)
	if err != nil {
		return nil, model, err
	}
	eps := ef.endpoints()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var out []store.Label
	noText, noEndpoint, noContext, invalid := 0, 0, 0, 0
	rs := sp.WithoutContext() // re-ask at this rung (this hash) only. Moving up a rung would give an answer for another hash
	hashes := make([]string, 0, len(bank))
	for h := range bank {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	for _, h := range hashes {
		if current[h] {
			continue
		}
		stored, ok := texts[h]
		if !ok {
			noText++
			continue
		}
		inputs := stored.Inputs
		if len(eps) == 0 {
			noEndpoint++
			continue
		}
		// Text recorded as tainted is never sent to hosted, even if the current session is not tainted (audit S1)
		sess := decide.Session{Tainted: ef.tainted || stored.Tainted}
		d, err := decide.Decide(ctx, rs, inputs, sess, eps)
		if err != nil {
			return nil, model, fmt.Errorf("replay %s: %w", h, err)
		}
		if d.Band == decide.Abstain {
			if strings.HasPrefix(d.Reason, "missing required input") {
				noContext++ // re-asking at the context rung: no context to add was recorded with this text
			} else {
				noEndpoint++
			}
			continue
		}
		if model == "" {
			model = d.Model
			if err := loadCurrent(model); err != nil {
				return nil, model, err
			}
			if current[h] {
				continue
			}
		} else if d.Model != model {
			return nil, model, fmt.Errorf("replay: model changed mid-run (%s → %s)", model, d.Model)
		}
		rec := recordOf(sp, d, inputs)
		t := bank[h]
		l, err := store.LabelFor(&rec, t.truth, t.weight, "replay")
		if err != nil {
			invalid++ // e.g. the truth is an option not in the current spec. Count it instead of silently dropping it
			continue
		}
		out = append(out, l)
		current[h] = true
	}
	if len(out) > 0 {
		fmt.Fprintf(stdout, "replayed %d earlier truths against the current wording and model (hash=%s, model=%s); no human labels needed\n", len(out), sp.Hash(), model)
	}
	if noText > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) cannot be replayed: no text stored for them (recorded before text was kept)\n", noText)
	}
	if noEndpoint > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) not replayed: no endpoint to ask (pass --local or a key for --hosted)\n", noEndpoint)
	}
	if noContext > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) not replayed on this rung: their text has no context inputs (%s)\n", noContext, strings.Join(sp.State.Required, ", "))
	}
	if invalid > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) skipped: their answer no longer exists in the spec\n", invalid)
	}
	return out, model, nil
}

// calibrateNoul fits Platt and prints the metrics (design §7). If positive and negative are inverted, ok=false.
func calibrateNoul(sp *spec.Spec, labels []store.Label, bins int, reports ...io.Writer) (spec.Calibration, bool, error) {
	stdout := reportWriter(reports...)
	samples := make([]calib.Sample, len(labels))
	rawProbs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	for i, l := range labels {
		p := l.Raw["yes"]
		samples[i] = calib.Sample{Delta: calib.Logit(p), Truth: l.Truth == "yes", Weight: l.Weight}
		rawProbs[i], truths[i], weights[i] = p, l.Truth == "yes", l.Weight
	}
	sep := calib.CheckSeparation(samples)
	pl, err := calib.Fit(samples)
	if errors.Is(err, calib.ErrOneSided) {
		fmt.Fprintln(stdout, "WARNING: every truth is on one side; a line needs both answers. Not written (tried again as truths grow).")
		return spec.Calibration{}, false, nil
	}
	if errors.Is(err, calib.ErrSlopeNotPositive) {
		// a higher probability goes with "no": the answers cannot be told apart, the same as an overlap (not written; the caller says why)
		fmt.Fprintln(stdout, "WARNING: a higher probability goes with \"no\" (fitted slope is not positive); this question and model cannot tell the answers apart. Change the question or the model.")
		return spec.Calibration{}, false, nil
	}
	if err != nil {
		return spec.Calibration{}, false, fmt.Errorf("calibrate: %w", err)
	}
	calProbs := make([]float64, len(samples))
	for i, s := range samples {
		calProbs[i] = pl.Apply(s.Delta)
	}
	fmt.Fprintf(stdout, "platt: bias=%.4f temperature=%.4f\n", pl.Bias, pl.Temperature)
	fmt.Fprintf(stdout, "brier: raw=%.4f calibrated=%.4f\n", calib.Brier(rawProbs, truths, weights), calib.Brier(calProbs, truths, weights))
	fmt.Fprintf(stdout, "ece:   raw=%.4f calibrated=%.4f (bins=%d)\n", calib.ECE(rawProbs, truths, weights, bins), calib.ECE(calProbs, truths, weights, bins), bins)
	fmt.Fprintf(stdout, "separation(Δ, weight>=1): pos_min=%.3f neg_max=%.3f overlap=%d inverted=%v (weak labels excluded: %d)\n", sep.PosMin, sep.NegMax, sep.Overlap, sep.Inverted, sep.Weak)
	if sep.Overlap > 0 {
		// Show which examples overlap. The text is not printed; the label can be traced by inputs_hash.
		fmt.Fprintln(stdout, "overlapping (truth raw weight inputs_hash):")
		for i, l := range labels {
			if l.Weight > 0 && l.Weight < 1 {
				continue
			}
			d := samples[i].Delta
			if (l.Truth == "yes" && d < sep.NegMax) || (l.Truth == "no" && d > sep.PosMin) {
				fmt.Fprintf(stdout, "  %-3s %.3f %.1f %s\n", l.Truth, l.Raw["yes"], l.Weight, l.InputsHash)
			}
		}
	}
	fmt.Fprintf(stdout, "boundaries after calibration: neg_max→%.3f (ask=%.2f) pos_min→%.3f (act=%.2f)\n",
		pl.Apply(sep.NegMax), sp.Thresholds.Ask, pl.Apply(sep.PosMin), sp.Thresholds.Act)
	printThresholdReport(sp.Thresholds, calProbs, truths, weights, stdout)
	printReliability("calibrated", calProbs, truths, weights, bins, stdout)
	if sep.Inverted {
		fmt.Fprintln(stdout, "WARNING: positives and negatives overlap on Δ; a monotone calibration cannot fix this. Change the question or the model.")
		return spec.Calibration{Method: "platt", Bias: pl.Bias, Temperature: pl.Temperature,
			Brier: calib.Brier(calProbs, truths, weights), ECE: calib.ECE(calProbs, truths, weights, bins)}, false, nil
	}

	// Compare identity and platt. platt looks too good in-sample, so judge by the 5-fold CV Brier and precision@act.
	// For models whose raw probabilities are already calibrated (Jev, Kev's noul, etc.), platt does not improve, so choose identity.
	rawPrec := precisionAt(sp.Thresholds.Act, rawProbs, truths, weights)
	rawBrier := calib.Brier(rawProbs, truths, weights)
	cvPrec, cvBrier := crossValidatePlatt(sp, samples, 5)
	fmt.Fprintf(stdout, "platt 5-fold CV: precision@act=%.3f brier=%.4f (raw: precision@act=%.3f brier=%.4f)\n", cvPrec, cvBrier, rawPrec, rawBrier)
	identityOK, plattOK := rawPrec >= sp.Thresholds.Act, cvPrec >= sp.Thresholds.Act
	identity := spec.Calibration{Method: "identity", PrecisionAt: rawPrec, Brier: rawBrier, ECE: calib.ECE(rawProbs, truths, weights, bins)}
	// The Brier written to the spec is the CV value (in-sample is optimistic and too lenient as a baseline for drift; audit C6)
	platt := spec.Calibration{Method: "platt", Bias: pl.Bias, Temperature: pl.Temperature, PrecisionAt: cvPrec,
		Brier: cvBrier, ECE: calib.ECE(calProbs, truths, weights, bins)}
	switch {
	case identityOK && plattOK && cvBrier < rawBrier:
		fmt.Fprintf(stdout, "choose platt: both satisfy act; CV brier %.4f < raw %.4f\n", cvBrier, rawBrier)
		return platt, true, nil
	case identityOK:
		fmt.Fprintf(stdout, "choose identity: raw precision at act %.3f >= act and platt does not improve brier (CV %.4f vs raw %.4f)\n", rawPrec, cvBrier, rawBrier)
		return identity, true, nil
	case plattOK:
		fmt.Fprintf(stdout, "choose platt: CV precision at act %.3f >= act (raw was %.3f)\n", cvPrec, rawPrec)
		return platt, true, nil
	default:
		fmt.Fprintf(stdout, "WARNING: neither raw (%.3f) nor platt-calibrated (CV %.3f) precision at act reaches thresholds.act (%.2f): act must not be allowed for this model.\n", rawPrec, cvPrec, sp.Thresholds.Act)
		return platt, false, nil
	}
}

// crossValidatePlatt fits Platt with k folds and sums precision@act and Brier on the held-out folds.
func crossValidatePlatt(sp *spec.Spec, samples []calib.Sample, k int) (precision, brier float64) {
	if k < 2 || len(samples) < k {
		return 0, 1
	}
	var probs []float64
	var truths []bool
	var weights []float64
	for fold := 0; fold < k; fold++ {
		var train, test []calib.Sample
		for i, s := range samples {
			if i%k == fold {
				test = append(test, s)
			} else {
				train = append(train, s)
			}
		}
		pl, err := calib.Fit(train)
		if err != nil {
			continue
		}
		for _, s := range test {
			probs = append(probs, pl.Apply(s.Delta))
			truths = append(truths, s.Truth)
			weights = append(weights, s.Weight)
		}
	}
	if len(probs) == 0 {
		return 0, 1
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), calib.Brier(probs, truths, weights)
}

// verifyTop verifies and calibrates choice / score (design §7.1).
//  1. With the raw distribution, measure accuracy against the top probability and precision at the act threshold
//  2. If precision is at least act, identity (= the mark that allows act on raw values) is ok
//  3. If not, fit vector scaling and measure again. If at least act after calibration, vector is ok;
//     if still not, return vector with ok=false (writing back needs --force)
func verifyTop(sp *spec.Spec, labels []store.Label, bins int, reports ...io.Writer) (spec.Calibration, bool, error) {
	stdout := reportWriter(reports...)
	// The report uses all labels; fitting (FitVector / CV) uses only labels whose correct option is known.
	// Partial labels ("!edit" = it was not edit) count for evaluating precision but cannot be used to learn the distribution.
	ms := make([]calib.MultiSample, len(labels))
	var fit []calib.MultiSample
	partial := 0
	for i, l := range labels {
		ms[i] = calib.MultiSample{Raw: l.Raw, Truth: l.Truth, Weight: l.Weight}
		if store.Partial(l.Truth) {
			partial++
		} else {
			fit = append(fit, ms[i])
		}
	}
	if partial > 0 {
		fmt.Fprintf(stdout, "labels: %d full, %d partial (\"!option\"; used for precision, not for fitting)\n", len(fit), partial)
	}
	rawRep := topReport(sp, ms, nil)
	fmt.Fprintf(stdout, "raw:        top-1 accuracy=%.3f brier(top)=%.4f ece(top)=%.4f\n", rawRep.acc, rawRep.brier, rawRep.ece)
	printThresholdReport(sp.Thresholds, rawRep.pTop, rawRep.correct, rawRep.weights, stdout)
	printReliability("top, raw", rawRep.pTop, rawRep.correct, rawRep.weights, bins, stdout)
	printConfusion(rawRep.confusion, stdout)
	identityOK := rawRep.precAct >= sp.Thresholds.Act
	identity := spec.Calibration{Method: "identity", PrecisionAt: rawRep.precAct, Brier: rawRep.brier, ECE: rawRep.ece}

	// vector looks too good in-sample, so adopt or reject it by the k-fold CV metrics.
	v, err := calib.FitVector(fit)
	if err != nil {
		if identityOK {
			fmt.Fprintf(stdout, "identity: raw precision at act %.3f >= act (vector scaling could not be fitted: %v)\n", rawRep.precAct, err)
			return identity, true, nil
		}
		return spec.Calibration{}, false, fmt.Errorf("vector scaling: %w", err)
	}
	calRep := topReport(sp, ms, &v)
	cvPrec, cvRec := crossValidateVector(sp, ms, 5) // score on all labels (partial ones included); fit only on complete labels (split inside the function)
	fmt.Fprintf(stdout, "vector:     temperature=%.4f biases=%s\n", v.Temperature, fmtDist(v.Biases))
	fmt.Fprintf(stdout, "calibrated: top-1 accuracy=%.3f brier(top)=%.4f ece(top)=%.4f (in-sample)\n", calRep.acc, calRep.brier, calRep.ece)
	printThresholdReport(sp.Thresholds, calRep.pTop, calRep.correct, calRep.weights, stdout)
	printReliability("top, calibrated", calRep.pTop, calRep.correct, calRep.weights, bins, stdout)
	printConfusion(calRep.confusion, stdout)
	fmt.Fprintf(stdout, "vector 5-fold CV at act: precision=%.3f recall=%.3f\n", cvPrec, cvRec)
	vectorOK := cvPrec >= sp.Thresholds.Act
	vector := spec.Calibration{
		Method: "vector", Temperature: v.Temperature, Biases: v.Biases,
		PrecisionAt: cvPrec, Brier: calRep.brier, ECE: calRep.ece,
	}

	// Choose: among candidates meeting precision@act, the one with higher recall@act. On a tie, the simpler identity.
	switch {
	case identityOK && vectorOK && cvRec > rawRep.recAct:
		fmt.Fprintf(stdout, "choose vector: both satisfy act; vector recall %.3f > identity recall %.3f\n", cvRec, rawRep.recAct)
		return vector, true, nil
	case identityOK:
		fmt.Fprintf(stdout, "choose identity: raw precision at act %.3f >= act (recall %.3f)\n", rawRep.precAct, rawRep.recAct)
		return identity, true, nil
	case vectorOK:
		fmt.Fprintf(stdout, "choose vector: CV precision at act %.3f >= act (identity was %.3f)\n", cvPrec, rawRep.precAct)
		return vector, true, nil
	default:
		fmt.Fprintf(stdout, "WARNING: neither raw (%.3f) nor vector-calibrated (CV %.3f) precision at act reaches thresholds.act (%.2f): act must not be allowed for this model.\n", rawRep.precAct, cvPrec, sp.Thresholds.Act)
		return vector, false, nil
	}
}

// crossValidateVector fits vector with k folds and sums precision / recall at the act threshold on the held-out folds.
func crossValidateVector(sp *spec.Spec, ms []calib.MultiSample, k int) (precision, recall float64) {
	// Scoring uses the same set as identity / check (all labels, including partial "!x") and the same event (judge).
	// Fitting (FitVector) uses only complete labels whose correct option is known. Audit C1 / C4.
	if k < 2 || len(ms) < k {
		return 0, 0
	}
	var tp, fp, fn float64
	for fold := 0; fold < k; fold++ {
		var train, test []calib.MultiSample
		for i, s := range ms {
			switch {
			case i%k == fold:
				test = append(test, s)
			case !store.Partial(s.Truth):
				train = append(train, s)
			}
		}
		v, err := calib.FitVector(train)
		if err != nil {
			continue
		}
		for _, s := range test {
			_, p, correct := judge(sp, v.Apply(s.Raw), s.Truth)
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			switch {
			case p >= sp.Thresholds.Act && correct:
				tp += w
			case p >= sp.Thresholds.Act && !correct:
				fp += w
			case correct:
				fn += w
			}
		}
	}
	if tp+fp > 0 {
		precision = tp / (tp + fp)
	}
	if tp+fn > 0 {
		recall = tp / (tp + fn)
	}
	return precision, recall
}

// report is a bundle of metrics against the top probability.
type report struct {
	pTop      []float64
	correct   []bool
	weights   []float64
	acc       float64
	brier     float64
	ece       float64
	precAct   float64
	recAct    float64
	confusion map[string]map[string]float64
}

// topReport collects the top probability, correctness and the confusion table (after applying calibration if any).
func topReport(sp *spec.Spec, ms []calib.MultiSample, v *calib.Vector) report {
	r := report{
		pTop: make([]float64, len(ms)), correct: make([]bool, len(ms)), weights: make([]float64, len(ms)),
		confusion: map[string]map[string]float64{},
	}
	var hit, total float64
	for i, s := range ms {
		dist := s.Raw
		if v != nil {
			dist = v.Apply(s.Raw)
		}
		top, p, correct := judge(sp, dist, s.Truth)
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		r.pTop[i], r.correct[i], r.weights[i] = p, correct, w
		if r.confusion[s.Truth] == nil {
			r.confusion[s.Truth] = map[string]float64{}
		}
		r.confusion[s.Truth][top] += w
		total += w
		if r.correct[i] {
			hit += w
		}
	}
	if total > 0 {
		r.acc = hit / total
	}
	r.brier = calib.Brier(r.pTop, r.correct, r.weights)
	r.ece = calib.ECE(r.pTop, r.correct, r.weights, 10)
	r.precAct = precisionAt(sp.Thresholds.Act, r.pTop, r.correct, r.weights)
	r.recAct = recallAt(sp.Thresholds.Act, r.pTop, r.correct, r.weights)
	return r
}

func recallAt(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tp, fn float64
	for i, p := range probs {
		if !truths[i] {
			continue
		}
		if p >= th {
			tp += weights[i]
		} else {
			fn += weights[i]
		}
	}
	if tp+fn == 0 {
		return 0
	}
	return tp / (tp + fn)
}

func precisionAt(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tp, fp float64
	for i, p := range probs {
		if p < th {
			continue
		}
		if truths[i] {
			tp += weights[i]
		} else {
			fp += weights[i]
		}
	}
	if tp+fp == 0 {
		return 0
	}
	return tp / (tp + fp)
}

func printConfusion(confusion map[string]map[string]float64, reports ...io.Writer) {
	stdout := reportWriter(reports...)
	fmt.Fprintln(stdout, "confusion (truth → predicted):")
	truths := make([]string, 0, len(confusion))
	for t := range confusion {
		truths = append(truths, t)
	}
	sort.Strings(truths)
	for _, t := range truths {
		fmt.Fprintf(stdout, "  %s: %s\n", t, fmtDist(confusion[t]))
	}
}

// printThresholdReport prints precision / recall at the act / ask thresholds and returns precision at act.
func printThresholdReport(t spec.Thresholds, probs []float64, truths []bool, weights []float64, reports ...io.Writer) float64 {
	stdout := reportWriter(reports...)
	report := func(name string, th float64) float64 {
		var tp, fp, fn float64
		for i, p := range probs {
			w := weights[i]
			switch {
			case p >= th && truths[i]:
				tp += w
			case p >= th && !truths[i]:
				fp += w
			case p < th && truths[i]:
				fn += w
			}
		}
		prec, rec := 0.0, 0.0
		if tp+fp > 0 {
			prec = tp / (tp + fp)
		}
		if tp+fn > 0 {
			rec = tp / (tp + fn)
		}
		fmt.Fprintf(stdout, "at %s>=%.2f: precision=%.3f recall=%.3f (tp=%.0f fp=%.0f fn=%.0f)\n", name, th, prec, rec, tp, fp, fn)
		return prec
	}
	precAct := report("act", t.Act)
	report("ask", t.Ask)
	return precAct
}

func printReliability(name string, probs []float64, truths []bool, weights []float64, bins int, reports ...io.Writer) {
	stdout := reportWriter(reports...)
	fmt.Fprintf(stdout, "reliability (%s):\n", name)
	for _, b := range calib.Reliability(probs, truths, weights, bins) {
		if b.N > 0 {
			fmt.Fprintf(stdout, "  [%.1f,%.1f) n=%4.0f conf=%.3f acc=%.3f\n", b.Lo, b.Hi, b.N, b.Confidence, b.Accuracy)
		}
	}
}

func argmax(m map[string]float64) (string, float64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bp := "", -1.0
	for _, k := range keys {
		if m[k] > bp {
			best, bp = k, m[k]
		}
	}
	return best, bp
}

// ---------------------------------------------------------------- hash / helpers

func cmdHash(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shikii hash <spec.yaml>\n  prints the spec's hash (question, options, template; not display/variants/calibration)")
	}
	sp, err := spec.Load(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, sp.Hash())
	return nil
}

// ---------------------------------------------------------------- check (contract check, design §8)

// cmdCheck checks whether the spec's calibration block is still valid. Meant to run in CI; exits 1 on any violation.
//   - the calibration's spec_hash matches the current hash (the wording was not changed while keeping the constants)
//   - the calibration was not written by hand (method is known and the keys are complete)
//   - with the labels (store + raw rows of eval.labeled), precision at the calibrated act threshold is at least act
//   - for noul, positive and negative are not inverted
func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	storeDir := bindStore(fs)
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("usage: shikii check <spec.yaml>...")
	}
	var violations int
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			fmt.Fprintf(stdout, "FAIL %s: %v\n", path, err)
			violations++
			continue
		}
		for _, v := range checkSpec(sp, store.Store{Dir: *storeDir}) {
			fmt.Fprintf(stdout, "FAIL %s: %s\n", sp.ID, v)
			violations++
		}
		fmt.Fprintf(stdout, "ok   %s hash=%s calibrations=%d\n", sp.ID, sp.Hash(), len(sp.Calibration))
	}
	if violations > 0 {
		return fmt.Errorf("check: %d violation(s)", violations)
	}
	return nil
}

// checkSpec lists the violations of one spec.
func checkSpec(sp *spec.Spec, st store.Store) []string {
	var out []string
	h := sp.Hash()
	base := sp
	for _, c := range sp.Calibration {
		key := fmt.Sprintf("calibration(model=%s)", c.Model)
		sp := base // the spec for this line's rung (a context-rung line is verified with the context rung's hash and truths)
		if c.SpecHash == base.ContextHash() {
			sp = base.ContextSpec()
			key = fmt.Sprintf("calibration(model=%s, context rung)", c.Model)
		} else if c.SpecHash != h {
			out = append(out, fmt.Sprintf("%s: stale spec_hash %s (current %s): the question/template/options changed after calibration; re-run calibrate", key, c.SpecHash, h))
			continue
		}
		switch c.Method {
		case "identity":
		case "platt":
			if sp.Type != spec.Noul || c.Temperature <= 0 {
				out = append(out, key+": platt requires noul and temperature > 0")
				continue
			}
		case "vector":
			if sp.Type == spec.Noul || c.Temperature <= 0 {
				out = append(out, key+": vector requires choice/score and temperature > 0")
				continue
			}
		default:
			out = append(out, fmt.Sprintf("%s: unknown method %q (calibration blocks are generated, not hand-written)", key, c.Method))
			continue
		}
		// Reject hand-written blocks: what calibrate never writes (n below min_n, no date) does not pass (audit S5)
		if c.N < sp.Eval.MinN {
			out = append(out, fmt.Sprintf("%s: n=%d < eval.min_n=%d (calibrate would not have written this block; --force or hand-written)", key, c.N, sp.Eval.MinN))
			continue
		}
		if strings.TrimSpace(c.FittedAt) == "" {
			out = append(out, key+": fitted_at is empty (calibration blocks are generated, not hand-written)")
			continue
		}
		labels, err := collectLabels(sp, st, c.Model, c.FittedOn)
		if err != nil {
			out = append(out, key+": "+err.Error())
			continue
		}
		if len(labels) == 0 {
			out = append(out, key+": no labels to verify against (store or eval.labeled)")
			continue
		}
		prec, inverted, _ := evaluateCalibration(sp, c, labels)
		if prec < sp.Thresholds.Act {
			out = append(out, fmt.Sprintf("%s: precision at act is %.3f < thresholds.act %.2f on %d labels", key, prec, sp.Thresholds.Act, len(labels)))
		}
		// Also measure with human-decided truths only. Machine truths (context: upper-rung answers) can be wrong in the same direction as the same model, so the act precision guarantee comes from the human ones.
		// Sampling (1 in 20 act decisions shows up in the answer queue) builds the sample. Until min_n is reached only the count is reported; after that, falling below act fails.
		var human []store.Label
		for _, l := range labels {
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		hPrec, _, hAct := evaluateCalibration(sp, c, human)
		fmt.Fprintf(stdout, "note %s: %s: human-checked act decisions: %d (precision %.3f)\n", sp.ID, key, hAct, hPrec)
		if hAct >= sp.Eval.MinN && hPrec < sp.Thresholds.Act {
			out = append(out, fmt.Sprintf("%s: precision at act on human-checked decisions is %.3f < thresholds.act %.2f on %d act decisions (machine truths made it look like %.3f)", key, hPrec, sp.Thresholds.Act, hAct, prec))
		}
		if inverted {
			out = append(out, key+": positives and negatives overlap on Δ; a monotone calibration cannot separate them")
		}
	}
	return out
}

// collectLabels combines store labels with eval.labeled rows that have raw (noul). The model is not called.
// Raw rows do not say which model's probabilities they are, so use them only when the calibration was fitted on the same file (fitted_on matches).
func collectLabels(sp *spec.Spec, st store.Store, model, fittedOn string) ([]store.Label, error) {
	all, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return nil, err
	}
	labels := store.LatestPerInput(all) // counted like calibrate (the latest truth per input)
	if sp.Eval.Labeled == "" || sp.Eval.Labeled != fittedOn {
		return labels, nil
	}
	b, err := os.ReadFile(sp.Eval.Labeled)
	if err != nil {
		if os.IsNotExist(err) {
			return labels, nil
		}
		return nil, err
	}
	var rows []labeled
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w", sp.Eval.Labeled, err)
	}
	for _, r := range rows {
		given, err := rawFromRow(sp, r.Raw)
		if err != nil {
			return nil, err
		}
		if given == nil {
			continue // rows that need the model are not used in check
		}
		truth, err := parseTruth(sp.Type, r.Truth)
		if err != nil {
			return nil, err
		}
		w := r.Weight
		if w <= 0 {
			w = 1
		}
		labels = append(labels, store.Label{SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: model,
			Raw: given, Truth: truth, Weight: w, Source: "file"})
	}
	return labels, nil
}

// evaluateCalibration applies the calibration and gives precision at the act threshold. For noul it also checks inversion.
func evaluateCalibration(sp *spec.Spec, c spec.Calibration, labels []store.Label) (precision float64, inverted bool, nAct int) {
	probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	switch sp.Type {
	case spec.Noul:
		pl := calib.Platt{Bias: c.Bias, Temperature: c.Temperature}
		samples := make([]calib.Sample, len(labels))
		for i, l := range labels {
			d := calib.Logit(l.Raw["yes"])
			samples[i] = calib.Sample{Delta: d, Truth: l.Truth == "yes", Weight: l.Weight}
			p := l.Raw["yes"]
			if c.Method == "platt" {
				p = pl.Apply(d)
			}
			probs[i], truths[i], weights[i] = p, l.Truth == "yes", l.Weight
		}
		inverted = calib.CheckSeparation(samples).Inverted
	default:
		for i, l := range labels {
			dist := l.Raw
			if c.Method == "vector" {
				dist = calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(l.Raw)
			}
			_, p, correct := judge(sp, dist, l.Truth)
			probs[i], truths[i], weights[i] = p, correct, l.Weight
		}
	}
	for _, p := range probs {
		if p >= sp.Thresholds.Act {
			nAct++ // number of decisions in the act band after applying the line (the sample size when measuring precision on human truths only)
		}
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), inverted, nAct
}

// judge is one label's "probability used for the band" and "whether that answer was right".
//   - choice: the top probability; correct if top == truth ("!x" means anything but x)
//   - score with thresholds.level: P(level ≥ k); correct if the truth's level ≥ k (the event happened)
//   - score without level: same as choice
func judge(sp *spec.Spec, dist map[string]float64, truth string) (top string, p float64, correct bool) {
	top, p = argmax(dist)
	if sp.Type == spec.Score && sp.Thresholds.Level > 0 && !store.Partial(truth) {
		p = decide.MassAtOrAbove(dist, sp.Thresholds.Level)
		lvl, err := strconv.Atoi(truth)
		return top, p, err == nil && lvl >= sp.Thresholds.Level
	}
	return top, p, store.Correct(top, truth)
}

// ---------------------------------------------------------------- drift (watching for degradation, design §9)

// cmdDrift checks, over the latest N labels, how much the Brier and the precision at act got worse than at calibration time.
// It warns and exits 1 if Brier is worse by +0.05 or more than at calibration, or precision falls below act.
func cmdDrift(args []string) error {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	storeDir := bindStore(fs)
	model := fs.String("model", "", "model name used as the calibration key (required)")
	window := fs.Int("window", 50, "how many recent labels to look at")
	tol := fs.Float64("tolerance", 0.05, "how much Brier degradation to allow")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	if *model == "" {
		return fmt.Errorf("drift: --model is required")
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	c := sp.CalibrationFor(*model)
	if c == nil {
		return fmt.Errorf("drift: no current calibration for model %s (hash %s); nothing to drift from", *model, sp.Hash())
	}
	if (c.Method == "platt" || c.Method == "vector") && !(c.Temperature > 0) {
		return fmt.Errorf("drift: calibration for %s has temperature %g (not > 0); the block is broken or hand-written, run check", *model, c.Temperature)
	}
	all, err := (store.Store{Dir: *storeDir}).Labels(sp.ID, sp.Hash(), *model)
	if err != nil {
		return err
	}
	labels := store.LatestPerInput(all)
	if len(labels) == 0 {
		return fmt.Errorf("drift: no labels in store for (hash=%s, model=%s)", sp.Hash(), *model)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].At.Before(labels[j].At) })
	if len(labels) > *window {
		labels = labels[len(labels)-*window:]
	}
	prec, brier, agree, askN := driftMetrics(sp, *c, labels)
	fmt.Fprintf(stdout, "spec=%s model=%s window=%d (calibrated %s on n=%d)\n", sp.ID, *model, len(labels), c.FittedAt, c.N)
	fmt.Fprintf(stdout, "brier:            at calibration=%.4f recent=%.4f\n", c.Brier, brier)
	fmt.Fprintf(stdout, "precision at act: recent=%.3f (act=%.2f)\n", prec, sp.Thresholds.Act)
	if askN > 0 {
		fmt.Fprintf(stdout, "ask agreement:    %.3f (share of ask-sourced labels where the user picked the model's top)\n", agree)
	} else {
		fmt.Fprintln(stdout, "ask agreement:    n/a (no ask-sourced labels in this window)")
	}
	var warn []string
	if brier > c.Brier+*tol {
		warn = append(warn, fmt.Sprintf("brier worsened by %.4f (> %.2f)", brier-c.Brier, *tol))
	}
	if prec < sp.Thresholds.Act {
		warn = append(warn, fmt.Sprintf("precision at act %.3f < %.2f", prec, sp.Thresholds.Act))
	}
	if len(warn) > 0 {
		fmt.Fprintln(stdout, "WARNING: "+strings.Join(warn, "; ")+". Re-run calibrate, or drop act to ask for this model.")
		return fmt.Errorf("drift: %d warning(s)", len(warn))
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

// driftMetrics gives precision@act, Brier and agreement at ask for the latest labels with calibration applied.
func driftMetrics(sp *spec.Spec, c spec.Calibration, labels []store.Label) (precision, brier, askAgreement, askN float64) {
	probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	var askHit float64
	for i, l := range labels {
		var p float64
		var correct bool
		if sp.Type == spec.Noul {
			p = l.Raw["yes"]
			if c.Method == "platt" {
				p = calib.Platt{Bias: c.Bias, Temperature: c.Temperature}.Apply(calib.Logit(p))
			}
			correct = l.Truth == "yes"
		} else {
			dist := l.Raw
			if c.Method == "vector" {
				dist = calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(l.Raw)
			}
			_, p, correct = judge(sp, dist, l.Truth)
		}
		probs[i], truths[i], weights[i] = p, correct, l.Weight
		if l.Source == "ask" {
			askN += l.Weight
			if correct {
				askHit += l.Weight
			}
		}
	}
	if askN > 0 {
		askAgreement = askHit / askN
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), calib.Brier(probs, truths, weights), askAgreement, askN
}

// ---------------------------------------------------------------- bias (measuring order bias)

// cmdBias reorders a choice's options, asks the same state again, and measures how much the answer moves with the order.
//   - flip rate: share of cases where the top changed when the order changed
//   - mean |Δp|: how much each option's probability moved
//   - position mass: average probability per position (shows a bias such as "leans to the first")
//
// Records nothing and calibrates nothing. The model does not generate text, so running permutations is cheap.
func cmdBias(args []string) error {
	fs := flag.NewFlagSet("bias", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	labeledPath := fs.String("labeled", "", "where to take states from (default: the spec's eval.labeled; without it, the text of truths in the store)")
	storeDir := bindStore(fs)
	rows := fs.Int("rows", 20, "maximum number of rows to use")
	perms := fs.Int("permutations", 5, "number of permutations per row")
	seed := fs.Int64("seed", 1, "random seed for the permutations")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	if sp.Type != spec.Choice {
		return fmt.Errorf("bias: only choice has an option order (spec type is %s)", sp.Type)
	}
	if *labeledPath == "" {
		*labeledPath = sp.Eval.Labeled
	}
	// States come from the same saved truths as calibrate / tune: the --labeled file and the text of truths in the store (latest per input)
	bank, _, err := truthBank(sp, store.Store{Dir: *storeDir}, *labeledPath)
	if err != nil {
		return err
	}
	if len(bank) == 0 {
		return fmt.Errorf("bias: no states to ask: no truths in %s and no --labeled file (shikii label / shikii import first)", *storeDir)
	}
	lab := make([]labeled, 0, len(bank))
	for _, row := range bank {
		truth, _ := json.Marshal(row.truth)
		lab = append(lab, labeled{Inputs: row.inputs, Truth: truth, Weight: row.weight})
	}
	if len(lab) > *rows {
		lab = lab[:*rows]
	}
	ep := decide.PickEndpoint(sp, decide.Session{Tainted: ef.tainted}, ef.endpoints())
	if ep == nil {
		return fmt.Errorf("bias: no permitted endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	rep, model, err := measureBias(ctx, sp, lab, ep.Client, *perms, *seed)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s rows=%d permutations=%d queries=%d\n", sp.ID, sp.Hash(), model, rep.rows, *perms, rep.queries)
	fmt.Fprintf(stdout, "flip rate (top changes with order): %.3f (%d/%d)\n", rep.flipRate, rep.flips, rep.compared)
	fmt.Fprintln(stdout, "mean |Δp| by option:")
	for _, o := range sp.Options {
		fmt.Fprintf(stdout, "  %-16s %.3f\n", o.ID, rep.meanAbsDelta[o.ID])
	}
	fmt.Fprintln(stdout, "mean Δp by position vs spec order (1 = listed first; + means that slot gets extra probability):")
	for i, m := range rep.positionMass {
		fmt.Fprintf(stdout, "  pos %d: %+.3f\n", i+1, m)
	}
	fmt.Fprintf(stdout, "position shift spread (max−min): %.3f\n", rep.positionSpread)
	if rep.flipRate > 0.10 || rep.positionSpread > 0.10 {
		fmt.Fprintln(stdout, "WARNING: the answer depends on option order. Shuffle options at query time or average over permutations before trusting act.")
		return fmt.Errorf("bias: order sensitivity above tolerance")
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

type biasReport struct {
	rows, queries, flips, compared int
	flipRate                       float64
	meanAbsDelta                   map[string]float64
	positionMass                   []float64
	positionSpread                 float64
}

// measureBias asks each row in the spec's order plus N permutations and tallies how much the order shakes the answer.
func measureBias(ctx context.Context, sp *spec.Spec, lab []labeled, c *systemone.Client, perms int, seed int64) (biasReport, string, error) {
	K := len(sp.Options)
	rep := biasReport{meanAbsDelta: map[string]float64{}, positionMass: make([]float64, K)}
	posN := make([]float64, K)
	sumAbs := map[string]float64{}
	var nAbs float64
	rng := rand.New(rand.NewSource(seed))
	model := ""
	canonical := make([]int, K)
	for i := range canonical {
		canonical[i] = i
	}
	// Position bias is measured by "how much the probability of the option placed at that position moved from when it was asked in the spec's order".
	// The raw average probability per position would be skewed by which option happened to land there.
	addPos := func(order []int, dist, base map[string]float64) {
		for pos, idx := range order {
			id := sp.Options[idx].ID
			rep.positionMass[pos] += dist[id] - base[id]
			posN[pos]++
		}
	}
	for _, r := range lab {
		state, missing := sp.Render(r.Inputs)
		if len(missing) > 0 {
			continue
		}
		base, m, err := decide.AskOrdered(ctx, sp, state, canonical, c)
		if err != nil {
			return rep, "", err
		}
		model = m
		rep.rows++
		rep.queries++
		baseTop, _ := argmax(base)
		for p := 0; p < perms; p++ {
			order := rng.Perm(K)
			dist, _, err := decide.AskOrdered(ctx, sp, state, order, c)
			if err != nil {
				return rep, "", err
			}
			rep.queries++
			rep.compared++
			addPos(order, dist, base)
			if top, _ := argmax(dist); top != baseTop {
				rep.flips++
			}
			for _, o := range sp.Options {
				sumAbs[o.ID] += math.Abs(dist[o.ID] - base[o.ID])
			}
			nAbs++
		}
	}
	if rep.compared > 0 {
		rep.flipRate = float64(rep.flips) / float64(rep.compared)
	}
	for _, o := range sp.Options {
		if nAbs > 0 {
			rep.meanAbsDelta[o.ID] = sumAbs[o.ID] / nAbs
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range rep.positionMass {
		if posN[i] > 0 {
			rep.positionMass[i] /= posN[i]
		}
		lo, hi = math.Min(lo, rep.positionMass[i]), math.Max(hi, rep.positionMass[i])
	}
	if K > 0 {
		rep.positionSpread = hi - lo
	}
	return rep, model, nil
}

// ---------------------------------------------------------------- status (taking stock of the store)

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

// cmdGC clears out "old records with no label". Labeled records and labels are kept regardless of age (calibration material).
// By default it only shows. It rewrites only with --apply.
func cmdGC(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	storeDir := bindStore(fs)
	olderThan := fs.String("older-than", "30d", "delete unlabeled records older than this (e.g. 30d, 12h, 1w; 0 means any age)")
	apply := fs.Bool("apply", false, "actually delete; without it, only count")
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	age, err := parseAge(*olderThan)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	var ids []string
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			return err
		}
		ids = append(ids, sp.ID)
	}
	if len(ids) == 0 {
		if ids, err = st.SpecIDs(); err != nil {
			return err
		}
	}
	sort.Strings(ids)
	cutoff := time.Now().Add(-age)
	total := 0
	for _, id := range ids {
		res, err := st.GC(id, cutoff, *apply)
		if err != nil {
			return fmt.Errorf("gc %s: %w", id, err)
		}
		verb := "would remove"
		if *apply {
			verb = "removed"
		}
		fmt.Fprintf(stdout, "%s: %s %d unlabeled record(s) older than %s; keep %d labeled, %d recent, %d unparsable\n",
			id, verb, res.Removed, *olderThan, res.KeptLabeled, res.KeptRecent, res.KeptUnparsed)
		total += res.Removed
	}
	if !*apply && total > 0 {
		fmt.Fprintln(stdout, "dry run: nothing was changed. Re-run with --apply to remove them.")
	}
	return nil
}

// parseAge parses ages like "30d" "1w" "12h". time.ParseDuration does not know d and w, so convert them first. Negative values are rejected.
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("--older-than needs a value such as 30d, 12h or 0")
	}
	if s == "0" {
		return 0, nil
	}
	var d time.Duration
	var err error
	switch {
	case strings.HasSuffix(s, "d"):
		d, err = scaledDuration(strings.TrimSuffix(s, "d"), 24*time.Hour)
	case strings.HasSuffix(s, "w"):
		d, err = scaledDuration(strings.TrimSuffix(s, "w"), 7*24*time.Hour)
	default:
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("--older-than %q: use a duration such as 30d, 1w, 12h or 0", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("--older-than %q: must not be negative", s)
	}
	return d, nil
}

func scaledDuration(num string, unit time.Duration) (time.Duration, error) {
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(n * float64(unit)), nil
}

func fmtCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// parseSpecPath accepts both "<spec.yaml> flags..." and "flags... <spec.yaml>".
func parseSpecPath(fs *flag.FlagSet, args []string) (string, error) {
	pos, err := parseMixed(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", fmt.Errorf("spec path is required")
	}
	return pos[0], nil
}

// parseMixed lets flags come before or after positional arguments (`shikii status spec.yaml --store DIR` and
// `shikii status --store DIR spec.yaml` are the same). Go's flag stops at the first positional argument, so keep reading the rest.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			if err := resolveStoreFlag(fs); err != nil {
				return nil, err
			}
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func fmtDist(m map[string]float64) string {
	b, _ := json.Marshal(m)
	return string(b)
}
