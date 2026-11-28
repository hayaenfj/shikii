package main

// SessionStart (0.11): Claude Code and Codex run it when a session starts and right after a summary of a long conversation (source
// "compact"). The agent then gets what the project itself says, not only the summary: the rules that apply to every request (the
// pages whose scene is "always") and the last section of the newest work record under .shikii/handoff/. On 2026-10-28 a summary dropped
// the person's intent for a release, and the work record was what brought it back. Nothing to tell means no output. Claude Code's
// PostCompact cannot give the agent context (its output is shown to the person only), so SessionStart is the one event used.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
)

// Fixed rules (0.12). A rule is never cut: in 0.11 each rule was cut at 300 bytes, a number without grounds, and the rule listing the
// words never to use lost its last words, among them one the person had forbidden; the agent then used it. What gives way to keep the
// block within sessionBriefMax is the work record's section, never a rule. The project itself bounds the rules (scene "always").
const (
	sessionBriefRules     = 8    // always rules listed at most, each whole
	sessionBriefRecords   = 3    // files the newest record is looked for among (only text files count)
	sessionBriefMax       = 4000 // bytes the whole block aims at; the record's section is cut to the room left (the rules alone may exceed it)
	sessionBriefRecordMin = 120  // bytes of room below which the record is left out rather than cut to nothing
)

// runSessionStart reads the hook JSON and prints the brief as SessionStart's additionalContext. Silent outside a shikii project.
func runSessionStart() error {
	_, in, err := readHookInput()
	if err != nil {
		return err
	}
	defer beginEventGit()() // the project and its exclude file ask git where the repository is once (hook_git_place.go)
	root, scope, err := prepareHookProject(in.CWD)
	if errors.Is(err, errProjectInactive) {
		return nil
	}
	if err != nil {
		return err
	}
	rules, err := (notebook.Book{Root: root, Scope: scope}).AlwaysRules()
	if err != nil {
		return err
	}
	record, section := newestWorkRecordSection(root)
	brief := briefOf(rules, record, section, hookBoundFor(""), 0)
	// Codex shows 9,800 bytes whole, not Claude Code's 10,000 characters (hook_bound.go): a brief of Japanese rules is three times the
	// bytes. The agent is asked about only when the brief and the notice could pass what Codex shows, which a brief of English rules and a
	// short record never do.
	if byteLength(brief)+sessionNoticeReserve > codexHookContextMax && sessionAgentKind() == "codex" {
		brief = briefOf(rules, record, section, hookBoundFor("codex"), sessionNoticeReserve)
	}
	// Memory nap (nap.go) is told to the person's own session only, never to a subagent's, and never stops the brief.
	if in.AgentID == "" {
		if notice, err := napNotice(root, scope, time.Now()); err != nil {
			hookLog("nap: " + err.Error())
		} else if notice != "" {
			if brief != "" {
				brief += "\n\n"
			}
			brief += notice
		}
	}
	if b := (hookOut{Context: brief}).json("SessionStart"); b != nil {
		fmt.Fprintln(stdout, string(b))
	}
	return nil
}

// sessionNoticeReserve is the room the brief leaves for the notice of the memory nap (about 450 bytes) where the agent's limit is counted
// in bytes, so that the two together stay within it.
const sessionNoticeReserve = 520

// sessionAgentKind is the name of the agent that runs this hook, asked of the process table when the brief needs it ("" when none is
// found); a test swaps it.
var sessionAgentKind = func() string {
	if p := currentAgentProcess(); p != nil {
		return p.Name
	}
	return ""
}

// sessionBrief is the block for the project (root is its .shikii, scope its folder) as Claude Code is shown it: "" when there is nothing
// to tell.
func sessionBrief(root, scope string) (string, error) {
	rules, err := (notebook.Book{Root: root, Scope: scope}).AlwaysRules()
	if err != nil {
		return "", err
	}
	record, section := newestWorkRecordSection(root)
	return briefOf(rules, record, section, hookBoundFor(""), 0), nil
}

// ruleLines are the lines of the first n rules, each whole, and, when some are left, one line that names the rest by their ids rather
// than dropping them silently (audit B of 0.12); each is read with shikii wiki query lesson:<id> (confirmation audit 1: wiki query cannot
// filter by scene, so the ids are the way to read them).
func ruleLines(rules []notebook.Page, n int) []string {
	var parts []string
	for _, p := range rules[:n] {
		parts = append(parts, "- "+strings.TrimSpace(p.Lesson)+" (lesson:"+p.ID+":"+p.Revision+")")
		if len(p.Recheck) > 0 {
			parts = append(parts, "  Recheck this article's premise files before relying on its premise; a change does not invalidate the rule.")
		}
	}
	if n < len(rules) {
		rest := []string{}
		for _, r := range rules[n:] {
			rest = append(rest, "lesson:"+r.ID)
		}
		parts = append(parts, fmt.Sprintf("- %d more rules for every request, each read with shikii wiki query lesson:<id>: %s", len(rules)-n, strings.Join(rest, ", ")))
	}
	return parts
}

// briefOf is the block made of the always rules and the section of the newest work record, fitted to what the agent shows whole (bound,
// less reserve for what the hook adds after it). The rules come first and whole, up to sessionBriefRules of them, fewer when more would
// pass the bound (the rest are named by their ids, as they are past sessionBriefRules); the record's section is cut to the room left, which
// is at most sessionBriefMax. Claude Code's bound is 10,000 characters and a brief of rules is far from it, so what Claude Code is shown is
// what it always was whenever the rules fit in those 10,000 characters (past them the later rules are named by their ids).
func briefOf(rules []notebook.Page, record, section string, bound hookBound, reserve int) string {
	head := "shikii: this project, at the start of the session (and after a summary). The rules for every request and where the work stands, from the project's own records: hypotheses with sources, never permission.\n"
	limit := bound.max - reserve
	n := min(len(rules), sessionBriefRules)
	for n > 0 && bound.length(head+strings.Join(ruleLines(rules, n), "\n")) > limit {
		n--
	}
	parts := ruleLines(rules, n)
	if record != "" {
		intro := "Work record " + record + ", its last section:\n"
		used := len(head) + len(strings.Join(parts, "\n")) + len("\n") + len(intro)
		room := min(sessionBriefMax-used, limit-bound.length(head+strings.Join(parts, "\n"))-len("\n")-len(intro))
		if room >= sessionBriefRecordMin {
			parts = append(parts, intro+cut(section, room))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return head + strings.Join(parts, "\n")
}

// sessionRecordLimit is how long the brief may spend reading the folders of .shikii/handoff/ to find the newest work record. A project
// whose handoff/ holds the output of tools (970,000 files in 45,000 folders, 14 GB) took 15 to 21 seconds to read in turn, and the
// agent stops the hook when its 15 passes: no brief came at all. The folders are read nearest the top first, 8 at once, and past the limit
// the brief goes out with the newest record among those read (none when no folder was), with the rules, and hook.log says so: the
// record helps to resume, the rules are what must arrive. Reading all of them took 1 to 6 seconds there, on a machine that was busy and on
// one that was not; 3 is the longest the brief waits for a help, and with napCountLimit (3 too) the two leave 9 of the hook's 15 seconds for
// the rest. A variable so that a test can make it a moment already past.
var sessionRecordLimit = 3 * time.Second

// isWorkRecordName is whether a file of handoff/ can be the work record to resume from, by its path alone: a record a person writes to
// read later, in markdown or plain text. Data files (.json, .csv, .log) are text too but not a record to resume from (audit B of 0.12: a
// measurement dump newer than the plan became "the work record").
func isWorkRecordName(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return handoff.KindOf(p) == "text" && (ext == ".md" || ext == ".txt" || ext == "")
}

// newestWorkRecordSection returns the path of the newest text file under handoff/ and its last section (from the last "## " or "### "
// heading; the whole file when it has none), whole: the caller cuts it to the room left. The folders are read, but the information of a
// file is asked for only when isWorkRecordName accepts it, so the files of other kinds that a tool wrote in thousands cost no more than
// the reading of their folders.
func newestWorkRecordSection(root string) (string, string) {
	book := handoff.Book{Store: root}
	texts, complete, err := sessionRecordsOf(book, time.Now().Add(sessionRecordLimit))
	if err != nil {
		hookLog("session start: the brief goes without a work record: " + err.Error())
		return "", ""
	}
	if !complete {
		hookLog("session start: the folders of .shikii/handoff/ were not all read within " + sessionRecordLimit.String() + ", the work record in the brief is the newest of those read first")
	}
	for _, e := range texts {
		f, err := book.Read(e.Path)
		if err != nil || strings.TrimSpace(f.Content) == "" {
			continue
		}
		section := f.Content
		if at := lastHeading(section); at > 0 {
			section = section[at:]
		}
		if !complete {
			// the agent is told, not only hook.log: it would otherwise resume from a record that a newer one replaced (audit of 0.45.0)
			return e.Path + sessionRecordPartial, strings.TrimSpace(section)
		}
		return e.Path, strings.TrimSpace(section)
	}
	return "", ""
}

// sessionRecordPartial follows the path of the work record in the brief when the folders were not all read in time.
const sessionRecordPartial = " (the newest found within the time limit; a newer one may exist, see `shikii handoff list`)"

// sessionRecordsOf is the search for the newest work records. A variable so that a test can answer as a search cut by the limit does.
var sessionRecordsOf = func(book handoff.Book, deadline time.Time) ([]handoff.Entry, bool, error) {
	return book.Newest(sessionBriefRecords, isWorkRecordName, deadline)
}

// lastHeading is the offset of the last "## " or "### " heading line, or -1.
func lastHeading(text string) int {
	at := -1
	for _, mark := range []string{"\n## ", "\n### "} {
		if i := strings.LastIndex(text, mark); i+1 > at {
			at = i + 1
		}
	}
	if strings.HasPrefix(text, "## ") || strings.HasPrefix(text, "### ") {
		if at < 0 {
			at = 0
		}
	}
	return at
}
