package main

// shikii hook: as a Claude Code and Codex hook, measures the AI's own "way of thinking" at two points.
// Before thinking (UserPromptSubmit): will this request end up changing files (prompt-kind), and should the approach be agreed with the person before starting (needs-plan).
// After thinking (Stop): were the changes within what was asked (changes-in-scope), and was the request finished (done-as-asked).
// There is no per-tool-call hook: classifying each command is not the AI's way of thinking, and Claude Code's permission system already decides what is allowed.
//
// Storage is each project's .shikii/ (decisions/ wiki/ records/ labels/ sessions/). The shared side holds only templates, tools and the page entry. The model is the local :8080. The answer page is :8096.
// Advice from decisions is given by band only once a line is drawn (hookOutcome). Past exchanges can be quoted even without a line.
// No tool is stopped by the hooks' own judgment; the one deny is a guard the person enabled, refusing a command right before it runs (0.11). Stopping is the person's job.
// Failures (no model, slow, cannot write) exit 0 silently: a hook must never stop the work. The reason goes to hook.log as one line.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Fixed rules (no options). Only tests replace them.
var (
	hookLocalURL      = "http://127.0.0.1:8080"   // the local model (same as the MCP settings from init)
	hookHostedURL     = "https://api.typesafe.ai" // the standard TypeSafe Jev. Used only when a key (TYPESAFE_API_KEY / DECISIONS_API_KEY) is set (decided 2026-10-23)
	hookPageURL       = "http://127.0.0.1:8096"   // placeholder for the page URL inside messages. Replaced on output with that project's page URL (a free port) (0.8.0)
	hookDecideTimeout = 5 * time.Second
	hookProbeTimeout  = 300 * time.Millisecond
	hookSessionTTL    = 7 * 24 * time.Hour
	hookLockWait      = 2 * time.Second  // upper limit for waiting on the lock
	hookGitTimeout    = 3 * time.Second  // upper limit for reading the working tree state (git). Past it: none (no truth is attached)
	hookCalibrateGap  = 10 * time.Minute // gap before auto-calibration of the same spec is started again
	// hookCalibrate draws the line. By default it starts a detached separate process (shikii calibrate --write): even a long replay does not hit the hook's timeout. Tests swap in an in-process one
	hookCalibrate = spawnCalibrate
	// hookTune scores the wording variants (shikii tune, without --write: adoption is the person's call). Also a detached process. Tests swap in an in-process one
	hookTune      = spawnTune
	hookTuneEvery = 50 // run again once the person's truths (one per input) have grown by this many since the last auto-tune
	// hookRecalibrateEvery: for a spec whose line could not be drawn, auto-calibration (including replay) is not repeated until truths grow by this many
	hookRecalibrateEvery = 20
	// hookSpawn starts shikii itself in a detached process (the model `up`, the page `serve`). The person never has to type a command. Tests swap in a recorder
	hookSpawn    = spawnShikii      // returns the started child's pid (0 if unknown)
	hookStartGap = 10 * time.Minute // gap before starting the same thing again (time from start to answering, and the retry gap after a failure)
)

const (
	hookTimeout    = 15      // seconds. git 3s + model 5s (the two questions run in parallel) + writing (audit R5: does not wait for the page to start)
	requestMax     = 1500    // length (bytes) of text kept as request / summary
	filesMax       = 30      // upper limit of "changed files" listed in the Stop question (the rest only as a count)
	listingMax     = 4 << 20 // upper limit (bytes) of the kept working tree listing. Past it, changed files count as unknown
	hookLogLineMax = 400     // upper limit (bytes) of one hook.log line. Error text can carry the model's response body
)

// hookSpecs are the 4 questions the hook asks (ids of the embedded specs). The first 2 right after the request, the last 2 at the end of the turn.
var hookSpecs = []string{"prompt-kind", "needs-plan", "changes-in-scope", "done-as-asked"}

func agentHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shikii", "agent"), nil
}

func cmdHook(args []string) error {
	const usage = "usage: shikii hook install [--yes] | uninstall [--yes] | user-prompt-submit | stop | subagent-start | session-start | agent-task | tool-lesson | plan query | plan submit | variants submit\n  install/uninstall: Claude Code and Codex hooks that measure the agent's own thinking before and after each turn (shown first; --yes does it)\n  the event hooks, plan query/submit and variants submit are run by the agent and read JSON on stdin; not for typing by hand\n"
	if len(args) == 0 {
		return errors.New(strings.TrimSpace(usage))
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return nil
	}
	switch args[0] {
	case "install":
		return hookInstall(args[1:], true)
	case "uninstall":
		return hookInstall(args[1:], false)
	case "plan":
		return cmdHookPlan(args[1:])
	case "variants":
		return cmdHookVariants(args[1:])
	case "user-prompt-submit", "stop", "subagent-start", "agent-task", "tool-lesson":
		// the hook never stops the work: exit 0 no matter what. The reason goes to hook.log
		if err := runHookEvent(args[0]); err != nil {
			hookLog(err.Error())
		}
		return nil
	case "session-start":
		// the project's own brief at the start and right after a summary (0.11). Never stops the session: exit 0, the reason goes to hook.log
		if err := runSessionStart(); err != nil {
			hookLog(err.Error())
		}
		return nil
	case "pre-tool-use":
		// an event an older version left in settings. Do nothing, silently (the next `shikii hook install --yes` removes it)
		return nil
	}
	return fmt.Errorf("hook: unknown event %q (install | uninstall | user-prompt-submit | stop | subagent-start | session-start | agent-task | tool-lesson)", args[0])
}

// ---------------------------------------------------------------- Claude Code / Codex JSON

// hookInput is the part of the JSON Claude Code and Codex pass on stdin that we use (the keys are the same for both).
type hookInput struct {
	SessionID            string          `json:"session_id"`
	AgentID              string          `json:"agent_id"`   // if set, this is not the person's request (a subagent etc.). Do not change the kept request
	AgentType            string          `json:"agent_type"` // SubagentStart: the subagent's type (Explore etc.)
	CWD                  string          `json:"cwd"`
	Prompt               string          `json:"prompt"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	ToolName             string          `json:"tool_name"`  // PreToolUse: the tool about to be used (Agent / Task; the edit tools and Bash for tool-lesson)
	ToolInput            json.RawMessage `json:"tool_input"` // PreToolUse: that tool's input (for Agent: prompt and subagent_type; file_path, notebook_path or command)
	// stopRecords, when set, collects what a Stop would write to records/guards.jsonl (the routine check, the person's guards) until
	// the session record is committed: a Stop the next request overtook writes none of it (keepStopRecords).
	stopRecords *[]guardRecord
	// stateNoted: this event has changed the state of the session's turn (sessionLatest); when the hook leaves before it does,
	// sessionTurnOnExit changes it (work_handover.go)
	stateNoted bool
	workTurn   string // Only the request this prompt hook created may be prepared.
	// late is a Stop run at the next request. It checks that required work is saved before judging, but emits no recovery feedback
	// and writes no check receipts: a later human fix is not agent recovery.
	late bool
}

// hookOut is what the hook returns. Empty means output nothing (no judgment). No permission decision (allow / ask / deny) is output.
type hookOut struct {
	RecordMessage string          // localized explanation for the decision record only; never emitted as hook JSON
	Context       string          // text passed to the agent (additionalContext. UserPromptSubmit, SubagentStart, SessionStart and PreToolUse)
	System        string          // text shown to the person in the terminal (systemMessage)
	Block         bool            // stop the Stop (a stop check was hit. 0.7.3). Claude Code passes the reason and continues
	Deny          bool            // refuse the tool call (PreToolUse; a guard the person enabled stopped a command before it ran, 0.11). The reason goes to the agent
	Reason        string          // the reason for stopping (to the agent)
	Halt          string          // a failed recovery: end automatic continuation with an explicit incomplete result
	Recovery      *ritualRecovery // internal state, saved with the same turn; never emitted as hook JSON
}

func (o hookOut) empty() bool { return o == hookOut{} }

// message is the text kept for the page: terminal text, a stop reason, a localized record explanation, or the agent context.
func (o hookOut) message() string {
	for _, s := range []string{o.Halt, o.System, o.Reason, o.RecordMessage, o.Context} {
		if s != "" {
			return s
		}
	}
	return ""
}

func (o hookOut) json(event string) []byte {
	if o.empty() {
		return nil
	}
	out := map[string]any{}
	if o.Context != "" && event != "Stop" { // Stop does not output hookSpecificOutput, because returning context there keeps the conversation going (system text only)
		out["hookSpecificOutput"] = map[string]any{"hookEventName": event, "additionalContext": o.Context}
	}
	if o.System != "" {
		out["systemMessage"] = o.System
	}
	if o.Deny && event == "PreToolUse" && o.Reason != "" {
		out["hookSpecificOutput"] = map[string]any{"hookEventName": event, "permissionDecision": "deny", "permissionDecisionReason": o.Reason}
	}
	if o.Halt != "" && event == "Stop" {
		out["continue"] = false
		out["stopReason"] = o.Halt
	} else if o.Block && event == "Stop" && o.Reason != "" { // stop check: the Stop decision; Codex (0.164) reads the same {"decision":"block","reason"} and continues the turn with the reason
		out["decision"] = "block"
		out["reason"] = o.Reason
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// ---------------------------------------------------------------- session state

// hookSession is the per-project record (.shikii/sessions/<id>.json). The transcript is not read (its shape is undocumented).
// The working tree listing at request time is the neighbouring <id>.files (too large for the JSON).
type hookSession struct {
	MemoryUpgrade string                        `json:"memory_upgrade,omitempty"`
	KnowledgeSeen []notebook.CollectiveBaseline `json:"knowledge_seen,omitempty"`

	Request          string             `json:"request,omitempty"`          // the person's last request (kept at UserPromptSubmit)
	RequestText      string             `json:"request_text,omitempty"`     // original text for the page. Kept apart from the decision input
	RawPrompt        string             `json:"last_user_prompt,omitempty"` // the person's last message kept without joining. Used as the source of quotes
	TurnID           string             `json:"turn_id,omitempty"`          // a repeated request with the same text is still a different turn
	Scope            string             `json:"scope,omitempty"`            // the repo's real path. Conversations, scoring and lines are limited to the same work
	Completed        *completedTurn     `json:"completed,omitempty"`        // record of the reply to this request. Paired with the person's next message
	Memories         []memoryDelivery   `json:"memories,omitempty"`         // versions of memories delivered to this session (the latest 16, and all of the running request)
	Previous         string             `json:"previous,omitempty"`         // the assistant's previous message (kept at Stop)
	Reasoning        *reasoningProgress `json:"reasoning,omitempty"`
	Planning         *planningFrame     `json:"planning,omitempty"`           // context for the current request that the agent organizes with the original text
	Pending          []pendingItem      `json:"pending,omitempty"`            // prompt-kind decisions waiting for their outcome truth
	Watch            []scopeWatch       `json:"watch,omitempty"`              // changes-in-scope decisions waiting for their outcome truth (are the changed files reverted)
	Asked            []askedItem        `json:"asked,omitempty"`              // decisions in this session that ended up waiting for the person's answer (ask and spot checks)
	Reviews          []askedItem        `json:"reviews,omitempty"`            // decisions waiting for provisional scoring from the conversation and the later spot check
	Reported         []string           `json:"reported_traces,omitempty"`    // traces reported at Stop as waiting for an answer (only mention when there is a new one)
	Judged           bool               `json:"judged,omitempty"`             // the end-of-turn self-review (changes-in-scope / done-as-asked) for the current request is done (reset on the next request)
	Head             string             `json:"head,omitempty"`               // HEAD at request time (so the stop check can see this turn's commits. 0.7.3)
	HeadAt           time.Time          `json:"head_at,omitempty"`            // request time. Commits before it (existing commits of a checked-out branch) are not looked at
	MemoryNoticed    bool               `json:"memory_noticed,omitempty"`     // told once in this session that there is memory not yet in the wiki (0.8.0)
	StoreNoticed     bool               `json:"store_noticed,omitempty"`      // told in this session that a wiki store is left under the old path's name (0.9.2)
	OlderNoticed     bool               `json:"older_noticed,omitempty"`      // told in this session that an older shikii registered this project again (0.9.7)
	ReflectAsked     []string           `json:"reflect_asked,omitempty"`      // conversations asked to be reflected on before the request (the ritual check looks at it at Stop. 0.8.0)
	ReflectAskedTurn string             `json:"reflect_asked_turn,omitempty"` // the turn ReflectAsked was asked in (do not stop on leftovers from another turn. audit R5)
	RitualRecovery   *ritualRecovery    `json:"ritual_recovery,omitempty"`    // saved remaining work for the current recovery
	RitualStopped    string             `json:"ritual_stopped,omitempty"`     // the stopped turn, including continuations after a background notification
	GroupingAsked    string             `json:"grouping_asked,omitempty"`     // the turn whose Stop asked only for continues_previous and left the judgment to the next Stop (0.9.3)
	PageNoticed      bool               `json:"page_noticed,omitempty"`       // told the page location in this session (0.8.0)
	HabitsNoticed    bool               `json:"habits_noticed,omitempty"`     // told in this session how to keep work records and rules (0.8.1)
	// requests since the full organizing and reflection notices (0 = none yet in this session); the short form is used in between (0.9.7)
	ClassifyAsked     []string       `json:"classify_asked,omitempty"`      // pages asked before the request to get a kind and a scene (the ritual check wiki-kinds looks at it at Stop. 0.9.0)
	ClassifyAskedTurn string         `json:"classify_asked_turn,omitempty"` // the turn ClassifyAsked was asked in
	SubagentTasks     []subagentTask `json:"subagent_tasks,omitempty"`      // task text passed to the Agent tool (SubagentStart picks lessons with that text. 0.9.1)
	// GuardTurn is the stretch a slash command opened, until the next request: not a request (nothing is asked, delivered or judged),
	// only the person's guards compare the present with the working tree and HEAD kept when it began. The request's own record above is left as it is
	GuardTurn *guardTurn `json:"guard_turn,omitempty"`
	At        time.Time  `json:"at"`
}

// subagentTask is a task the parent passed to a child agent with the Agent tool (kept at PreToolUse; at SubagentStart the oldest one of the same type is used).
// When several children of the same type start at once, only the order tells which text belongs to which child (the texts come from the parent's work, so a mix-up is still closer than picking by the parent's request)
type subagentTask struct {
	Type   string    `json:"type"`
	Prompt string    `json:"prompt"`
	At     time.Time `json:"at"`
}

// Records older than subagentTaskTTL are not used. Up to subagentTaskMax are kept. When a new record is added, records of the same type older than subagentTaskBurst
// are dropped (so a record whose child never started, because the Agent tool was refused, is not used for a later child and shifts everything by one. audit 0.9.1 mid-1)
const (
	subagentTaskTTL   = 10 * time.Minute
	subagentTaskMax   = 8
	subagentTaskBurst = 30 * time.Second // records for children of the same type started side by side arrive within this span
)

type pendingItem struct {
	Trace string    `json:"trace"`
	Tree  string    `json:"tree"` // hash of the working tree (no content)
	At    time.Time `json:"at"`
	// Settled: after "unchanged" (!edit, partial) was attached at the end of the turn, this keeps the item until the person's next request. If background work later
	// changes files (the Stop after a notification), the same trace can be relabeled edit (weight 1, the newer answer wins)
	Settled bool `json:"settled,omitempty"`
}

type askedItem struct {
	Spec  string `json:"spec"`
	Trace string `json:"trace"`
}

// Session mutations share a checked flock. Model calls stay outside it, and a
// failed acquisition must never become an unlocked write.

// ---------------------------------------------------------------- decisions

// ---------------------------------------------------------------- configured language (shown alongside)

// ---------------------------------------------------------------- per event

// treeStateWithScope is the working tree state: the first 16 digits (hash) of the tree id from `git add -A` (respects .gitignore, includes untracked) into a temporary index and `git write-tree`,
// the `git ls-files --stage` listing of that index (listing. At Stop it is compared with the one at request time to get the "changed files". Empty in an empty repo. listed says whether it was obtained),
// plus the real-path scope and project (the work marker put on records: the folder name of the repo's root; outside git, the cwd's folder name).
// HEAD and index state are not included, so commit / stash / checkout alone do not change it; it changes only when file contents change.
// The one exception is a tracked file that .gitignore names: `git add -A` skips it, so it is taken from the repo's own index (a file is
// in the listing while it is tracked and present), or its changes would be invisible to the guards and the outcome truths.
// New blobs are written to temporary objects (GIT_OBJECT_DIRECTORY) and removed, so untracked file contents do not stay in the repo's .git/objects (the repo's objects are only read, as alternates).
// If the whole thing exceeds hookGitTimeout, "" (e.g. a huge node_modules with no .gitignore. No truth is attached, but the hook does not stop). Outside git, "".

// ---------------------------------------------------------------- start in the background (the person never types a command)

// ---------------------------------------------------------------- install / uninstall
