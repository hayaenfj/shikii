package main

// SessionStart (0.11): Claude Code and Codex run it when a session starts and right after a summary of a long conversation (source
// "compact"). The agent then gets what the project itself says, not only the summary: the rules that apply to every request (the
// pages whose scene is "always") and the last section of the newest work record under .shikii/handoff/. On 2026-10-28 a summary dropped
// the person's intent for a release, and the work record was what brought it back. Nothing to tell means no output. Claude Code's
// PostCompact cannot give the agent context (its output is shown to the person only), so SessionStart is the one event used.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
)

const (
	sessionBriefRules   = 3    // always rules listed at most
	sessionBriefRule    = 300  // bytes of one rule
	sessionBriefRecord  = 900  // bytes of the work record's last section
	sessionBriefRecords = 3    // files the newest record is looked for among (only text files count)
	sessionBriefMax     = 1800 // bytes of the whole block
)

// runSessionStart reads the hook JSON and prints the brief as SessionStart's additionalContext. Silent outside a shikii project.
func runSessionStart() error {
	agent, err := agentHome()
	if err != nil {
		return err
	}
	if !fileExists(filepath.Join(agent, "decisions")) {
		return fmt.Errorf("%s: not installed (shikii hook install --yes)", agent)
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 4<<20))
	if err != nil {
		return err
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("stdin is not the agent's hook JSON: %w", err)
	}
	root, scope, err := prepareHookProject(in.CWD)
	if errors.Is(err, errProjectInactive) {
		return nil
	}
	if err != nil {
		return err
	}
	brief, err := sessionBrief(root, scope)
	if err != nil {
		return err
	}
	if b := (hookOut{Context: brief}).json("SessionStart"); b != nil {
		fmt.Fprintln(stdout, string(b))
	}
	return nil
}

// sessionBrief is the block for the project (root is its .shikii, scope its folder): "" when there is nothing to tell.
func sessionBrief(root, scope string) (string, error) {
	var parts []string
	rules, err := (notebook.Book{Root: root, Scope: scope}).AlwaysRules()
	if err != nil {
		return "", err
	}
	for i, p := range rules {
		if i == sessionBriefRules {
			break
		}
		parts = append(parts, "- "+cut(strings.TrimSpace(p.Lesson), sessionBriefRule)+" (lesson:"+p.ID+":"+p.Revision+")")
	}
	if record, section := newestWorkRecordSection(root); record != "" {
		parts = append(parts, "Work record "+record+", its last section:\n"+section)
	}
	if len(parts) == 0 {
		return "", nil
	}
	text := "shikii: this project, at the start of the session (and after a summary). The rules for every request and where the work stands, from the project's own records: hypotheses with sources, never permission.\n" + strings.Join(parts, "\n")
	return cut(text, sessionBriefMax), nil
}

// newestWorkRecordSection returns the path of the newest text file under handoff/ and its last section (from the last "## " or "### "
// heading; the whole file when it has none), cut to sessionBriefRecord bytes.
func newestWorkRecordSection(root string) (string, string) {
	book := handoff.Book{Store: root}
	entries, err := book.List()
	if err != nil {
		return "", ""
	}
	var texts []handoff.Entry
	for _, e := range entries {
		if e.Kind == "text" {
			texts = append(texts, e)
		}
	}
	sort.SliceStable(texts, func(i, j int) bool { return texts[i].Modified.After(texts[j].Modified) })
	for i, e := range texts {
		if i == sessionBriefRecords {
			break
		}
		f, err := book.Read(e.Path)
		if err != nil || strings.TrimSpace(f.Content) == "" {
			continue
		}
		section := f.Content
		if at := lastHeading(section); at > 0 {
			section = section[at:]
		}
		return e.Path, cut(strings.TrimSpace(section), sessionBriefRecord)
	}
	return "", ""
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
