package main

import (
	"errors"
	"fmt"
	"strings"
)

// What an agent reads when it asks one of the commands it calls for help, or calls one in a way the command cannot read: the forms,
// runnable as written. Before, each command answered with one line naming its operations ("usage: shikii work prepare | record
// (JSON on stdin)"), which says neither what the JSON holds nor how it reaches the command, so the next call was a guess (the JSON
// as an argument, --json, --query, project inside it). TestHelpPrintsFormsThatRunAsWritten runs every form below through the real
// command, so a form the command would refuse cannot stay here.
//
// A form is one line `shikii <arguments>`, followed by its standard input when it reads one. <name> marks a value the agent fills
// in. Notes never start with the word shikii, so that only forms do.
type agentForm struct {
	When  string // when the agent uses this form
	Call  string // the arguments after shikii
	Input string // what goes on standard input; "" when the command reads none
}

type agentUsage struct {
	Line  string // the operations and how input reaches them
	Forms []agentForm
	Notes []string
}

var agentUsages = map[string]agentUsage{
	"work": {
		Line: "work prepare | record (one JSON object on standard input; run it in the project's folder)",
		Forms: []agentForm{
			{"at the start of a task, with the ids from the hook notice", "work prepare", `{"session_id":"<session_id>","turn_id":"<turn_id>","query":"<topic words>"}`},
			{"at the start of a task when no hook gave ids", "work prepare", `{"query":"<topic words>","request":"<the task>"}`},
			{"more knowledge during the task: the same receipt with query, or proposal (check an approach), or conditions (reuse a procedure)", "work prepare", `{"receipt":"<receipt>","query":"<other topic words>"}`},
			{"at the end of the task", "work record", `{"receipt":"<receipt>","result":{"changed":"<what changed>","checked":"<what you checked and how>"}}`},
		},
		Notes: []string{
			"The reply of prepare carries receipt and how_to_record: what record also takes (observations, notes, reviews, memory, reasoning).",
			"result.evidence takes references (current, newN for observation N of the same call, knowledgeN, or the id of a source this receipt saved), not text: what you checked goes in checked.",
			"A message from the person during the work starts a new request: prepare with the ids of its notice, and carried in the reply names the pages and sources of the interrupted work that stay valid with the new receipt.",
			"max_bytes (8192 to 393216) in prepare bounds its reply; without it the CLI keeps the reply within 28672 bytes, which an agent's shell shows whole, and output names what was left out.",
		},
	},
	"hook plan": {
		Line: "hook plan query | submit (one JSON object on standard input; run it in the project's folder)",
		Forms: []agentForm{
			{"the original text of this request, related lessons and how_to_submit (the shape of submit)", "hook plan query", `{"session_id":"<session_id>","turn_id":"<turn_id>","query":"<topic words>"}`},
		},
		Notes: []string{
			"submit takes the same ids and query plus agreed and open, as how_to_submit in the reply of query describes.",
			"work prepare and work record do the same in one call each, with the knowledge and the record of the result.",
		},
	},
	"reflect": {
		Line: "reflect query | place | submit (place and submit take one JSON object on standard input; run it in the project's folder)",
		Forms: []agentForm{
			{"every pending conversation with its sources, and how_to_submit (the shape of submit)", "reflect query", ""},
			{"where a draft lesson could go and what it may contradict, before saving a new lesson", "reflect place", `{"wiki":[{"sources":["<source id>"],"title":"<title>","lesson":"<one or two sentences>","applies_to":"<when it applies>","kind":"rule","scene":"<when ...>"}]}`},
		},
		Notes: []string{
			"query takes no arguments; read one source's original text with shikii wiki query <source id>.",
			"submit takes {reviews:[...], wiki:[...], memory:[...]} as how_to_submit in the reply of query describes; check complete and each item's error in its reply.",
			"kind of a lesson: rule, howto, criteria, fact or history.",
		},
	},
	"wiki": {
		Line: "wiki <operation> (run it in the project's folder)",
		Forms: []agentForm{
			{"search the lessons", "wiki query <search words>", ""},
			{"read sources by id (the original text)", "wiki query <source id>", ""},
			{"read one lesson (lesson:<page id>@<revision> for an exact revision; continue:<cursor> as the last argument reads on)", "wiki query lesson:<page id>", ""},
			{"save a source you checked", "wiki capture", `{"kind":"test","title":"<title>","content":"<what you observed>","reference":"<file, command or URL>","version":"<version or commit>","observed_at":"<now, RFC 3339>"}`},
			{"save a lesson from sources", "wiki ingest", `{"sources":["<source id>"],"title":"<title>","lesson":"<one or two sentences>","applies_to":"<when it applies>","kind":"rule","scene":"<when ...>","body":"<background, reasons, open points>"}`},
		},
		Notes: []string{
			"kind of a source: human, agent, code, test or release. kind of a lesson: rule, howto, criteria, fact or history.",
			"patch, maintain, consult, reuse, organize, reconcile, diagnose and compare also read one JSON object from standard input: patch takes the patch object, maintain {page_id, revision, key, reason}, the others the arguments of shikii_wiki of the MCP server.",
			"classify <page id> <kind> <scene>, lint and verify [pattern.md ...] take arguments only.",
		},
	},
	"handoff": {
		Line: "handoff list | read <path> | write <path> | append <path> (run it in the project's folder; the path is relative to .shikii/handoff/)",
		Forms: []agentForm{
			{"replace a work record with the text on standard input", "handoff write <work>/report.md", "<the report>"},
			{"add text to the end of a work record", "handoff append <work>/report.md", "<one more section>"},
			{"read a work record", "handoff read <work>/report.md", ""},
			{"list the work records", "handoff list", ""},
		},
	},
}

func (f agentForm) text() string {
	if f.Input == "" {
		return "  " + f.When + ":\n    shikii " + f.Call + "\n"
	}
	tag := "EOF"
	if strings.HasPrefix(f.Input, "{") {
		tag = "JSON"
	}
	return "  " + f.When + ":\n    shikii " + f.Call + " <<'" + tag + "'\n    " + f.Input + "\n    " + tag + "\n"
}

func (u agentUsage) text() string {
	var b strings.Builder
	b.WriteString("usage: shikii " + u.Line + "\n")
	for _, f := range u.Forms {
		b.WriteString(f.text())
	}
	for _, note := range u.Notes {
		b.WriteString("  " + note + "\n")
	}
	return b.String()
}

func helpFlag(arg string) bool { return arg == "-h" || arg == "--help" }

// helpAsked reports whether the arguments ask for help: -h or --help anywhere, or the word help within the first words arguments
// (where an operation is expected, never where a path or search words are).
func helpAsked(args []string, words int) bool {
	for i, arg := range args {
		if helpFlag(arg) || (arg == "help" && i < words) {
			return true
		}
	}
	return false
}

// printAgentUsage answers a request for help: the forms on standard output, and the command succeeds.
func printAgentUsage(name string) error {
	_, err := fmt.Fprint(stdout, agentUsages[name].text())
	return err
}

// usageRefusal answers a call the command cannot read: what was wrong, then the forms. With a call (the arguments after shikii, as
// in agentForm.Call) that has forms of its own, only those are shown.
func usageRefusal(name, problem string, call ...string) error {
	usage := agentUsages[name]
	text := usage.text()
	if len(call) == 1 {
		own := ""
		for _, f := range usage.Forms {
			if f.Call == call[0] {
				own += f.text()
			}
		}
		if own != "" {
			text = "usage: shikii " + usage.Line + "\n" + own
		}
	}
	text = strings.TrimRight(text, "\n")
	if problem == "" {
		return errors.New(text)
	}
	return errors.New(problem + "\n" + text)
}

// operationProblem says what is wrong with the arguments of a command whose operations read standard input and take no arguments
// of their own ("" when only the operation is missing). ok is true when the arguments are exactly one known operation.
func operationProblem(name string, operations, args []string) (problem string, ok bool) {
	if len(args) == 0 {
		return "", false
	}
	known := false
	for _, operation := range operations {
		known = known || args[0] == operation
	}
	if known && len(args) == 1 {
		return "", true
	}
	stray := args[0]
	if known {
		stray = args[1]
	}
	switch {
	case strings.HasPrefix(stray, "{") || strings.HasPrefix(stray, "["):
		return name + " reads its JSON on standard input, not as an argument", false
	case strings.HasPrefix(stray, "-"):
		return name + " takes no flags", false
	case known:
		return name + " " + args[0] + " takes no arguments", false
	}
	return fmt.Sprintf("%s has no operation %q", name, stray), false
}

// emptyInputRefusal answers an operation that reads standard input and got nothing (an agent's shell gives an empty standard input
// when nothing is piped).
func emptyInputRefusal(name, call string) error {
	return usageRefusal(name, "standard input was empty: shikii "+call+" reads one JSON object from it", call)
}
