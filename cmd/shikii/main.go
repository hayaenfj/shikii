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
	"fmt"
	"io"
	"os"

	shikii "github.com/hayaenfj/shikii"
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
	case "work":
		err = cmdWork(os.Args[2:])
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
  shikii init      (same as shikii --init) Turn on automatic recording for this project (the first project on a machine also gets the hooks; a machine with projects and no hooks keeps them off and is told the command) and set up the 4 kinds of decisions, the wiki, the work records folder, MCP, the working rules and the entry to the page. Safe to run again
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
  shikii work prepare | record (JSON on stdin)
                   Prepare knowledge; add searches/consult/reuse with the same receipt; record judgments, evidence and procedure outcomes
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
  shikii wiki query [search text or source ID] | consult / reuse / organize / reconcile / ingest / patch / diagnose / compare / capture / maintain (JSON on stdin) | classify / lint
                   reconcile previews {patch:{...}} against base/current/proposed without saving; apply only its ready patch. compare checks retrieval against recorded cases.
                   Turn everyday conversations into sourced lessons in the project's .shikii/. The AI writes the summaries; shikii manages the original text, index, versions and checks. Lessons are hypotheses and never count as truths or calibration
  shikii handoff   list | read <path> | write <path> | append <path> (body on stdin)
                   Work records (.shikii/handoff/): working files you rewrite as you go (audit reports, release steps, diffs, images). Lessons go in the wiki; evidence as of when it was checked goes in the wiki's sources
  shikii guard     list | add (JSON on stdin) | enable <id> | disable <id> | try
                   Turn a person's correction into a "blocking check" (paired-files / forbidden-words / commit-body / forbidden-commands). enable turns it on after confirming that it blocks a negative example
  shikii version                                  Print the version
  shikii review query | submit (JSON on stdin)       The AI gives provisional scores with reasons from conversations and outcomes. A person checks the samples and held items, and corrections feed the next scoring. Provisional scores are stored apart from truths and calibration
  shikii reflect query | submit (JSON on stdin)      Combine scoring and wiki organizing into one confirmation and one bulk save (query returns 2 at a time from the oldest conversations, submit takes any number). Pass reviews and wiki, then check complete and each item's error
  shikii check     <spec.yaml>... [--store DIR]      CI: fail lines whose wording changed, hand-written lines, lines with too little precision and lines that lift an option no truth confirmed
  shikii drift     <spec.yaml> --model M [--window N] [--store DIR]
  shikii bias      <spec.yaml> [--rows K] [--permutations N] [--store DIR] [--labeled file] [--local URL] [--hosted URL]
                   Measure whether the answer changes with the order of the options. Texts come from the same saved truths as calibrate (store, or --labeled)
  shikii gc        [spec.yaml...] [--older-than 30d] [--apply] [--store DIR]
  shikii hash      <spec.yaml>
`
}

// ---------------------------------------------------------------- decide

// ---------------------------------------------------------------- label

// ---------------------------------------------------------------- calibrate

// ---------------------------------------------------------------- hash / helpers

// ---------------------------------------------------------------- check (contract check, design §8)

// ---------------------------------------------------------------- drift (watching for degradation, design §9)

// ---------------------------------------------------------------- bias (measuring order bias)

// ---------------------------------------------------------------- status (taking stock of the store)
