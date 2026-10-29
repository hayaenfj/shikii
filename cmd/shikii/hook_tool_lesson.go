package main

// hook tool-lesson (0.11 item H): right before Claude Code edits a file or runs a command (PreToolUse), deliver at most one lesson
// whose page names that file or that command. Lessons chosen once by the words of the request helped in about 1 of 6 deliveries;
// the moments that mattered were "right before editing version.go" and "right before git commit". This runs before every edit and
// command, so it calls no model, reads only the pages that name the target, and only adds context: it never allows, denies or asks (the deny of an enabled forbidden-commands guard comes from guardToolCall, before this).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// toolLessonMatcher is the PreToolUse matcher of hook tool-lesson: the tools that change a file, and Bash.
const toolLessonMatcher = "Edit|Write|MultiEdit|NotebookEdit|Bash"

// toolLessonEdits maps each file-changing tool to the input field that holds its file.
var toolLessonEdits = map[string]string{"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path"}

// toolLessonCommandMax is how much of a command is read for its leading words.
const toolLessonCommandMax = 8 << 10

// toolTarget is what a tool call is about to touch: a file, or the leading words of the commands in a command line.
type toolTarget struct {
	label   string   // kept in the delivery record and shown to the agent: "Edit cmd/shikii/version.go", "Bash git commit"
	file    string   // the file's path with forward slashes (file edits)
	phrases []string // "git commit", "gh release" (Bash)
}

// toolLessonTarget reads the target of the tool call, or false when the tool is not one this hook looks at or names nothing.
func toolLessonTarget(in *hookInput, scope string) (toolTarget, bool) {
	var input map[string]any
	if json.Unmarshal(in.ToolInput, &input) != nil {
		return toolTarget{}, false
	}
	if field, ok := toolLessonEdits[in.ToolName]; ok {
		file, _ := input[field].(string)
		if file == "" || !distinctiveFileName(filepath.Base(file)) {
			return toolTarget{}, false
		}
		shown := file
		for _, dir := range []string{scope, in.CWD} {
			if rel, err := filepath.Rel(dir, file); dir != "" && err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				shown = rel
				break
			}
		}
		return toolTarget{label: in.ToolName + " " + cut(filepath.ToSlash(shown), 400), file: filepath.ToSlash(file)}, true
	}
	if in.ToolName == "Bash" {
		command, _ := input["command"].(string)
		phrases := commandPhrases(cut(command, toolLessonCommandMax))
		if len(phrases) == 0 {
			return toolTarget{}, false
		}
		return toolTarget{label: "Bash " + cut(strings.Join(phrases, ", "), 400), phrases: phrases}, true
	}
	return toolTarget{}, false
}

// distinctiveFileName reports whether a file's base name is specific enough to look for in lessons: a name with an extension
// (version.go, README.md), a dotfile (.gitignore), or a capitalised name of 4 or more characters (Makefile, Dockerfile).
func distinctiveFileName(base string) bool {
	if len(base) < 3 {
		return false
	}
	for i := 0; i < len(base); i++ {
		if !nameByte(base[i]) {
			return false
		}
	}
	if dot := strings.LastIndexByte(base, '.'); dot > 0 && dot < len(base)-1 {
		return true
	}
	if base[0] == '.' {
		return len(base) >= 4
	}
	return len(base) >= 4 && base[0] >= 'A' && base[0] <= 'Z'
}

// nameByte is a byte of a file name as lessons write it: ASCII letters, digits, '_', '-' and '.'. Bytes of Japanese text are not.
func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// fileMention scores how a lesson text names file (a path with forward slashes): 2 when it writes a path that ends the file's path
// (cmd/shikii/version.go), 1 when it writes the bare base name (version.go), 0 otherwise. A mention that is part of a longer name
// (myversion.go, version.go.bak) or the same base name under another folder (internal/version.go) is not the file.
func fileMention(text, file string) int {
	base := path.Base(file)
	best := 0
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], base)
		if i < 0 {
			break
		}
		at, end := from+i, from+i+len(base)
		from = at + 1
		if end < len(text) {
			if c := text[end]; nameByte(c) && !(c == '.' && (end+1 == len(text) || !nameByte(text[end+1]))) {
				continue // a longer name (a '.' that ends a sentence is not part of it)
			}
		}
		if at == 0 || !nameByte(text[at-1]) && text[at-1] != '/' {
			best = max(best, 1)
			continue
		}
		if text[at-1] != '/' {
			continue
		}
		start := at - 1
		for start > 0 && (nameByte(text[start-1]) || text[start-1] == '/') {
			start--
		}
		mention := strings.TrimPrefix(text[start:end], "./")
		if file == mention || strings.HasSuffix(file, "/"+mention) {
			best = 2
		}
	}
	return best
}

// commandPhrases are the leading words of each simple command in a command line: the program's base name and its first word
// argument, such as "git commit" (git -c k=v commit), "gh release", "make test", "go test". Options before that word are skipped,
// with the value of a lone short option (-c k=v, -C dir). Leading variable assignments and sudo, env, time, command, exec, nohup and
// the shell's own words (if, then, do...) are skipped. Words inside quotes are one argument, so `echo "git commit"` names only echo.
// A program alone (ls, gofmt) gives no phrase: one word is too common to pick a lesson by. shikii and npm nest their subcommands
// (shikii hook plan, shikii hook install, npm run check), so for them the next word is taken too when there is one (0.12): a lesson
// about shikii hook install is not about shikii hook plan query.
func commandPhrases(command string) []string {
	var out []string
	seen := map[string]bool{}
	for _, words := range shellSegments(readablePart(command)) {
		for len(words) > 0 && (assignment.MatchString(words[0]) || commandPrefixes[words[0]]) {
			words = words[1:]
		}
		if len(words) < 2 {
			continue
		}
		program := path.Base(words[0])
		if !programName.MatchString(program) {
			continue
		}
		phrase := ""
		for i := 1; i < len(words); i++ {
			w := words[i]
			if strings.HasPrefix(w, "-") {
				if len(w) == 2 {
					i++ // a lone short option's value
				}
				continue
			}
			if !subcommand.MatchString(w) {
				break
			}
			phrase = program + " " + w
			if nestedSubcommands[program] {
				for j := i + 1; j < len(words); j++ {
					next := words[j]
					if strings.HasPrefix(next, "-") {
						if len(next) == 2 {
							j++ // a lone short option's value (npm run -w web check: web is not the third word; audit A of 0.12)
						}
						continue
					}
					if subcommand.MatchString(next) {
						phrase += " " + next
					}
					break
				}
			}
			break
		}
		if phrase != "" && !seen[phrase] {
			seen[phrase] = true
			out = append(out, phrase)
		}
	}
	return out
}

var (
	assignment      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	programName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	subcommand      = regexp.MustCompile(`^[a-z][a-z0-9-]+$`)
	commandPrefixes = map[string]bool{"sudo": true, "env": true, "time": true, "command": true, "exec": true, "nohup": true, "!": true, "{": true, "if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true}
	// programs whose subcommands nest one level (program word word), read with their third word (fixed list, 0.12)
	nestedSubcommands = map[string]bool{"shikii": true, "npm": true}
)

// readablePart is the part of a command line that is read for commands: a comment (a # that starts a word outside quotes, to the end
// of its line) is left out, and reading stops at the first <<, wherever it stands: a here-document's body follows, and whatever comes
// after it is not read. 0.12 first parsed the here-document's terminator, and three audits in a row found a form it misread (a quoted
// <<, a glued cat<<EOF, a glued EOF>out.txt, an arithmetic <<), so the rule was cut down to this one line: no body line is ever read
// as a command, at the price of not reading what follows a here-document either (the lesson for that command arrives when it runs alone).
func readablePart(command string) string {
	var out strings.Builder
	for i, line := range strings.Split(command, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		line = stripComment(line)
		if at := strings.Index(line, "<<"); at >= 0 {
			out.WriteString(line[:at])
			return out.String()
		}
		out.WriteString(line)
	}
	return out.String()
}

// stripComment cuts a line at a # that starts a word outside quotes ($# and a#b are not comments).
func stripComment(line string) string {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				i++
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}

// shellSegments splits a command line into the words of its simple commands: outside quotes, ; & | ( ) ` and newlines end a
// command, and spaces end a word. Quotes and backslashes are taken off as the shell does; what is inside quotes stays one word.
func shellSegments(command string) [][]string {
	var segments [][]string
	var words []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endSegment := func() {
		endWord()
		if len(words) > 0 {
			segments = append(segments, words)
			words = nil
		}
	}
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(command) {
				i++
				word.WriteByte(command[i])
			} else {
				word.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == '\\' && i+1 < len(command):
			i++
			if command[i] != '\n' {
				word.WriteByte(command[i])
				inWord = true
			}
		case c == ' ' || c == '\t':
			endWord()
		case strings.IndexByte(";&|()`\n", c) >= 0:
			endSegment()
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	endSegment()
	return segments
}

// toolLessonScore scores how a page names the target: the file mention (2 for its path, 1 for its base name) or 1 for a command
// phrase, read in the title, the lesson and applies_to, plus one tier (3 for a path, 2 for a base name or a command phrase); when none
// of those names it, 1 for a mention inside a code span of the body (`cmd/shikii/version.go`, `make test`, a fenced block; 0.12: in
// hayabusa-lang 104 of 152 pages named their command or file only there). Plain body text is not read: it is long and names many
// things in passing. A page that names the target in its title or applies_to (its condition) ranks above one that names it only in
// the lesson, and both above one that names it only in the body (the body's tier is below every other mention; audit C of 0.12).
func toolLessonScore(p notebook.Page, target toolTarget) (score int, inCondition bool) {
	mention := func(text string) int {
		text = strings.Join(strings.Fields(text), " ")
		if target.file != "" {
			return fileMention(text, target.file)
		}
		for _, phrase := range target.phrases {
			if phraseMention(text, phrase) {
				return 1
			}
		}
		return 0
	}
	condition := max(mention(p.Title), mention(p.AppliesTo))
	if score = max(condition, mention(p.Lesson)); score > 0 {
		return score + 1, condition > 0
	}
	for _, span := range codeSpans(p.Body) {
		if mention(span) > 0 {
			return 1, false
		}
	}
	return 0, false
}

// codeSpans are the texts inside the body's code spans, as CommonMark reads them: a run of n backticks opens a span that the next run
// of exactly n backticks closes (`...`, “ ...`...`... “, ```...```). An inline span (fewer than 3 backticks) ends with its line;
// a fence of 3 or more that is never closed runs to the end of the body (the rest is code).
func codeSpans(body string) []string {
	var out []string
	for i := 0; i < len(body); {
		open := strings.IndexByte(body[i:], '`')
		if open < 0 {
			break
		}
		at := i + open
		n := 0
		for at+n < len(body) && body[at+n] == '`' {
			n++
		}
		run := body[at : at+n]
		start := at + n
		text := body[start:]
		if n < 3 {
			if nl := strings.IndexByte(text, '\n'); nl >= 0 {
				text = text[:nl]
			}
		}
		end := -1
		for from := 0; from < len(text); {
			j := strings.Index(text[from:], run)
			if j < 0 {
				break
			}
			k := from + j
			if k+n < len(text) && text[k+n] == '`' { // a longer run does not close this span
				for k+n < len(text) && text[k+n] == '`' {
					k++
				}
				from = k + n
				continue
			}
			end = k
			break
		}
		if end < 0 {
			if n >= 3 {
				out = append(out, text)
				break
			}
			i = start + len(text)
			continue
		}
		out = append(out, text[:end])
		i = start + end + n
	}
	return out
}

// phraseMention reports whether text writes phrase as whole words (not inside a longer word or after a '/' or '.').
func phraseMention(text, phrase string) bool {
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], phrase)
		if i < 0 {
			return false
		}
		at, end := from+i, from+i+len(phrase)
		from = at + 1
		if at > 0 && (nameByte(text[at-1]) || text[at-1] == '/') {
			continue
		}
		if end < len(text) && nameByte(text[end]) && text[end] != '.' {
			continue
		}
		return true
	}
	return false
}

// hookToolLesson delivers, right before an edit or a command, the one lesson that names its file or command best (toolLessonScore;
// ties keep the wiki's page order), leaving out revisions this session got within the hour (the same record the request's delivery
// uses). Nothing before the session's first request: the delivery is assessed with that turn's conversation. History pages are not
// delivered, as at the request. The output is additionalContext only.
func hookToolLesson(agent string, in *hookInput, snapshot *hookSession) (hookOut, error) {
	if snapshot.TurnID == "" || snapshot.Scope == "" || snapshot.Request == "" {
		return hookOut{}, nil
	}
	target, ok := toolLessonTarget(in, snapshot.Scope)
	if !ok {
		return hookOut{}, nil
	}
	scores := map[string]toolCandidate{}
	// only the pages that name the target get their sources checked (LiveWhere), in the wiki's page order
	live, err := (notebook.Book{Root: agent, Scope: snapshot.Scope}).LiveWhere(func(p notebook.Page) bool {
		if p.Kind == "history" {
			return false
		}
		score, inCondition := toolLessonScore(p, target)
		scores[p.ID] = toolCandidate{page: p, score: score, inCondition: inCondition}
		return score > 0
	})
	if err != nil {
		return hookOut{}, err
	}
	if len(live) == 0 {
		return hookOut{}, nil
	}
	// what the reflection said about earlier deliveries of these pages through the same door (0.12): read only when a page names the target
	effects, err := memoryEffects(agent, snapshot.Scope)
	if err != nil {
		return hookOut{}, err
	}
	var ranked []toolCandidate
	for _, p := range live {
		c := scores[p.ID]
		// the door is the first one the page names that is not silent (unused twice and never helpful): a lesson silent at git commit
		// still comes through gh release when one command line runs both (audit A of 0.12); silent at every door, it is left out
		for _, door := range namedDoors(p, target) {
			if e := effects[store.PairKey(lessonQuote(p).Key, door)]; !silenced(e) {
				c.phrase, c.helpful = door, e.Helpful
				break
			}
		}
		if c.phrase == "" {
			continue
		}
		ranked = append(ranked, c)
	}
	// stable: page order (by id) breaks ties; a page found helpful before comes before one with the same words
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].before(ranked[j-1]); j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	// the lesson to deliver: the best-ranked page this session has not had within the hour, unless the decision model is confident it does
	// not apply right now (lesson-applies, 0.12; asked outside the session lock). Its trace goes into the delivery for the outcome truth.
	seen := map[string]bool{}
	for _, m := range snapshot.Memories {
		if age := time.Since(m.At); age >= 0 && age < time.Hour {
			seen[m.Key] = true
		}
	}
	var chosen *toolCandidate
	var quote memoryQuote
	asked := 0
	for i := range ranked {
		c := ranked[i]
		q := lessonQuote(c.page)
		if seen[q.Key] {
			continue
		}
		if asked == lessonAppliesMaxAsks {
			break // every candidate so far was a trusted no: stop asking rather than pay a model call per page (audit A of 0.12)
		}
		asked++
		trace, skip := lessonApplies(agent, snapshot.Scope, snapshot.Request, target.label, c.page)
		if skip {
			continue
		}
		q.Trace = trace
		chosen, quote = &c, q
		break
	}
	if chosen == nil {
		return hookOut{}, nil
	}
	var context string
	err = updateSession(agent, in.SessionID, func(s *hookSession) {
		if s.TurnID != snapshot.TurnID {
			return // a new request arrived meanwhile: this tool call belongs to it, and the next one gets its lesson
		}
		for _, m := range s.Memories {
			if age := time.Since(m.At); age >= 0 && age < time.Hour && m.Key == quote.Key {
				return // delivered meanwhile by another call of this turn
			}
		}
		context, err = deliverToolLesson(agent, s, quote, target, chosen.phrase, in.AgentID)
	})
	if err != nil || context == "" {
		return hookOut{}, err
	}
	return hookOut{Context: context}, nil
}

// toolCandidate is a page that names the target, with its word score, whether its condition (title or applies_to) names it, the door
// (phrase) it matched through, and how often the reflection found it helpful through that door.
type toolCandidate struct {
	page        notebook.Page
	score       int
	inCondition bool
	phrase      string
	helpful     int
}

// before orders candidates: a higher word score, then a match in the condition, then more helpful assessments through this door.
func (c toolCandidate) before(o toolCandidate) bool {
	if c.score != o.score {
		return c.score > o.score
	}
	if c.inCondition != o.inCondition {
		return c.inCondition
	}
	return c.helpful > o.helpful
}

// memorySilenceUnused is how many "unused" assessments through one door, with no "helpful", keep a lesson revision silent there (0.12).
const memorySilenceUnused = 2

// silenced reports whether the reflection's assessments through this door keep the lesson silent there.
func silenced(e store.PairEffect) bool { return e.Unused >= memorySilenceUnused && e.Helpful == 0 }

// memoryEffects reads the assessed deliveries of this project (helpful / unused per lesson revision and door) from the assessment
// files; it never fails (a file that cannot be read is skipped), so a broken assessment never silences the delivery.
func memoryEffects(agent, scope string) (map[string]store.PairEffect, error) {
	return (store.Store{Dir: agent}).PairEffects(scope), nil
}

// namedDoors are the doors a page matches the target through, in the command's order: the file's base name for an edit, or every
// command phrase the page names (in its title, lesson, applies_to or a code span of its body).
func namedDoors(p notebook.Page, target toolTarget) []string {
	if target.file != "" {
		return []string{path.Base(target.file)}
	}
	texts := []string{p.Title, p.AppliesTo, p.Lesson}
	texts = append(texts, codeSpans(p.Body)...)
	var doors []string
	for _, phrase := range target.phrases {
		for _, text := range texts {
			if phraseMention(strings.Join(strings.Fields(text), " "), phrase) {
				doors = append(doors, phrase)
				break
			}
		}
	}
	if len(doors) == 0 && len(target.phrases) > 0 {
		doors = target.phrases[:1]
	}
	return doors
}

// deliverToolLesson records one quote delivered at a tool call (its own delivery id, under the turn as parent, so the reflection
// assesses it with that turn's conversation) and returns the text for the agent. phrase is the door it came through (recorded, 0.12).
func deliverToolLesson(agent string, s *hookSession, q memoryQuote, target toolTarget, phrase, agentID string) (string, error) {
	what := "the file you are about to change"
	if target.file == "" {
		what = "the command you are about to run"
	}
	prefix := "shikii: a lesson from this project's wiki that names " + what + " (" + target.label + "). It is a hypothesis with sources: check its condition, put this request and the rules first, and do not treat it as permission to act.\n"
	for budget := 400; budget >= 25; budget /= 2 {
		clipped := clipQuotes([]memoryQuote{q}, budget)
		data, err := json.Marshal(clipped)
		if err != nil {
			return "", err
		}
		if len(prefix)+len(data) > 1800 {
			continue
		}
		sum := sha256.Sum256([]byte(s.TurnID + "/tool/" + target.label + "/" + q.Key))
		d := store.MemoryDelivery{ID: hex.EncodeToString(sum[:])[:16], Scope: s.Scope, Request: cut(s.Request, requestMax), Quotes: clipped, Parent: s.TurnID, Agent: agentID, Tool: target.label, Phrase: phrase}
		if err := (store.Store{Dir: agent}).SaveMemoryDelivery(d); err != nil {
			return "", err
		}
		s.Memories = append(s.Memories, memoryDelivery{Key: q.Key, At: time.Now().UTC()})
		if len(s.Memories) > 16 {
			s.Memories = s.Memories[len(s.Memories)-16:]
		}
		return prefix + string(data), nil
	}
	return "", nil
}
