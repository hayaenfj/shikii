package main

// What an agent shows the model of a hook's additionalContext whole, and how it counts it. The limit is the agent's, so each agent has its
// own number and its own way of counting; the hook finds out which agent runs it (the first ancestor process with a known name,
// agent_process.go) and fits what it says to that agent's limit.
//
// Claude Code shows 10,000 characters, counted as JavaScript counts a string's length (hookContextMax); more is saved to a file and only
// a short start of it is shown.
//
// Codex 0.172.0 (2026-11-28) shows additionalContextLimit "tokens", 2,500 unless the hook's handler in hooks.json says another number, and
// counts them as ceil(bytes of UTF-8 / 4): a hook text of 10,485 bytes was "original token count: 2622", and the file it saved was 10,485
// bytes. More than that is saved to <temp dir>/hook_outputs/<session id>/<uuid>.txt and the model is given a preview: the first half of the
// budget, "…N tokens truncated…", the last half, and the path of the file. Cut in the middle of the text, the prepared work's JSON line
// was cut inside it: of 51 such texts in Codex's records of 2026-11-28, 38 had a line of JSON that no longer parsed and 13 a field with
// the mark in it, 5 of them how_to_record, the instruction of how to record the result. Japanese is 3 bytes a character, so 10,000
// characters of it are three times too long for Codex. 10,000 bytes is the limit; the 200 bytes below it are for what Codex may add to
// the text (a newline) and for a count made a little differently in another version of Codex.

const (
	codexHookContextMax = 9_800 // bytes of UTF-8 of one hook's additionalContext that Codex 0.172.0 shows whole (its limit is 10,000)
)

// hookBound is the most of one hook's additionalContext an agent shows whole, and the count it is measured by.
type hookBound struct {
	max    int
	length func(string) int
	unit   string // what length counts, for the hook's log
}

// hookBoundFor is the bound of the agent whose process name is kind (the names of agentNames; "" when it is not known). An agent that
// is not Codex has Claude Code's: it is the bound the hook has always kept, and what the others show is at least as much.
func hookBoundFor(kind string) hookBound {
	if kind == "codex" {
		return hookBound{max: codexHookContextMax, length: byteLength, unit: "bytes"}
	}
	return hookBound{max: hookContextMax, length: hookContextLength, unit: "characters"}
}

// byteLength is the length of s in bytes of UTF-8.
func byteLength(s string) int { return len(s) }
