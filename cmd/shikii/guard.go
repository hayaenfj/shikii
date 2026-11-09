package main

// shikii guard: turn a person's correction into a "check that stops the turn" (0.7.3).
//
// Delivering lessons alone did not change how an agent that can read the repo and CLAUDE.md behaves (the 2026-10-23 experiment, 5 tasks x 2 conditions: 4 ties).
// On the other hand, a rule a person has stated once ("keep the English and Japanese changelogs in step", "do not use the words 根 or 指紋", "write the background and verification in the commit body")
// can be made a deterministic check, which works reliably regardless of how smart the model is. At the end of a turn (Stop) the enabled guards run; if one fails, Stop is blocked and
// the reason is returned (Claude Code's decision: block. On a Stop with stop_hook_active it does not block; it only records whether the problem was fixed).
//
// Guards live in each project's .shikii/guards/<id>.yaml (fixed location). There are 4 kinds and no new options (forbidden-commands, 0.11: a command about to run must match none of the patterns; refused right before it runs):
//   paired-files    when one file changes, the other must change too (e.g. CHANGELOG.md and CHANGELOG_ja.md)
//   forbidden-words words that must not appear in added lines (e.g. 根, 指紋). Paths in except are not checked
//   commit-body     words that the body of every commit in this turn must contain (e.g. 元の問題, 変更後, 検証)
// Enabling (enable) first confirms that the kind's negative example really stops (RED) and the positive example passes (GREEN), then sets enabled: true.
// Drafts (add) are written with enabled: false; enabling is done by a person (or by an agent a person told to).

import (
	"regexp"
	"time"
)

const (
	guardKindPairedFiles       = "paired-files"
	guardKindForbiddenWords    = "forbidden-words"
	guardKindCommitBody        = "commit-body"
	guardKindForbiddenCommands = "forbidden-commands"
	guardKinds                 = guardKindPairedFiles + " | " + guardKindForbiddenWords + " | " + guardKindCommitBody + " | " + guardKindForbiddenCommands
	guardFilesMax              = 200     // max number of files whose added lines are read (the rest are not checked)
	guardFileBytesMax          = 1 << 20 // max size of one file
	guardNegativePath          = "shikii-guard-negative.txt"
)

var guardIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type guard struct {
	ID       string     `yaml:"id" json:"id"`
	Kind     string     `yaml:"kind" json:"kind"`
	Title    string     `yaml:"title" json:"title"`
	Reason   string     `yaml:"reason,omitempty" json:"reason,omitempty"` // origin (the gist of the person's correction)
	Source   string     `yaml:"source,omitempty" json:"source,omitempty"` // source (id of the conversation or observation)
	Enabled  bool       `yaml:"enabled" json:"enabled"`
	Pairs    [][]string `yaml:"pairs,omitempty" json:"pairs,omitempty"`       // paired-files
	Words    []string   `yaml:"words,omitempty" json:"words,omitempty"`       // forbidden-words
	Except   []string   `yaml:"except,omitempty" json:"except,omitempty"`     // forbidden-words: paths not checked (glob; a trailing /** means everything below)
	Allow    []string   `yaml:"allow,omitempty" json:"allow,omitempty"`       // forbidden-words: ordinary words that contain a forbidden word (e.g. 根拠 for 根). Removed before checking
	Required []string   `yaml:"required,omitempty" json:"required,omitempty"` // commit-body
	Red      *guardRed  `yaml:"red,omitempty" json:"red,omitempty"`           // records that enable saw the negative example stop
	Patterns []string   `yaml:"patterns,omitempty" json:"patterns,omitempty"` // forbidden-commands: regular expressions no command about to run may match (0.11)
	Example  string     `yaml:"example,omitempty" json:"example,omitempty"`   // forbidden-commands: a command the check must stop (its negative example)
}

type guardRed struct {
	At       time.Time `yaml:"at" json:"at"`
	Negative string    `yaml:"negative" json:"negative"`
}

// guardInput is what was observed in the turn: changed files, added lines, and this turn's commits. When something is unknown, Known/CommitsKnown is false (that check passes).
type guardInput struct {
	Changed      []string
	Known        bool
	Added        map[string][]string
	Commits      []string
	CommitsKnown bool
	// Commands are the commands about to run (PreToolUse Bash, 0.11); the turn's commands are not observed at Stop, so CommandsKnown is false there
	Commands      []string
	CommandsKnown bool
}

type guardResult struct {
	ID     string `json:"guard"`
	Pass   bool   `json:"pass"`
	Reason string `json:"reason,omitempty"`
	// ReasonJA is the same reason in Japanese. The hook shows it next to the English one (bi) when the person's language is Japanese
	ReasonJA string `json:"-"`
}

// guardRecord is one line of records/guards.jsonl: which check stopped (or passed) in which turn. recheck is the Stop after a block (recording whether it was fixed).
type guardRecord struct {
	At      time.Time `json:"at"`
	Session string    `json:"session_id"`
	Turn    string    `json:"turn_id"`
	Guard   string    `json:"guard"`
	Pass    bool      `json:"pass"`
	Reason  string    `json:"reason,omitempty"`
	Recheck bool      `json:"recheck,omitempty"`
	// Tool marks a stop right before a file change (PreToolUse, 0.11), not at the end of the turn
	Tool bool `json:"tool,omitempty"`
}

// ---------------------------------------------------------------- storage

// ---------------------------------------------------------------- CLI
