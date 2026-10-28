package main

// hook tool-lesson (0.11 item H): right before Claude Code edits a file or runs a command (PreToolUse), deliver at most one lesson
// whose page names that file or that command. Lessons chosen once by the words of the request helped in about 1 of 6 deliveries;
// the moments that mattered were "right before editing version.go" and "right before git commit". This runs before every edit and
// command, so it calls no model, reads only the pages that name the target, and only adds context: it never allows, denies or asks.

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
// A program alone (ls, gofmt) gives no phrase: one word is too common to pick a lesson by.
func commandPhrases(command string) []string {
	var out []string
	seen := map[string]bool{}
	for _, words := range shellSegments(command) {
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
		for i := 1; i < len(words); i++ {
			w := words[i]
			if strings.HasPrefix(w, "-") {
				if len(w) == 2 {
					i++ // a lone short option's value
				}
				continue
			}
			if subcommand.MatchString(w) {
				if p := program + " " + w; !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
			break
		}
	}
	return out
}

var (
	assignment      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	programName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	subcommand      = regexp.MustCompile(`^[a-z][a-z0-9-]+$`)
	commandPrefixes = map[string]bool{"sudo": true, "env": true, "time": true, "command": true, "exec": true, "nohup": true, "!": true, "{": true, "if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true}
)

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
// phrase, read in the title, the lesson and applies_to (not the body: it is long and names many things in passing). A page that names
// the target in its title or applies_to (its condition) ranks above one that names it only in the lesson.
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
	return max(condition, mention(p.Lesson)), condition > 0
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
	type candidate struct {
		page        notebook.Page
		score       int
		inCondition bool
	}
	scores := map[string]candidate{}
	// only the pages that name the target get their sources checked (LiveWhere), in the wiki's page order
	live, err := (notebook.Book{Root: agent, Scope: snapshot.Scope}).LiveWhere(func(p notebook.Page) bool {
		if p.Kind == "history" {
			return false
		}
		score, inCondition := toolLessonScore(p, target)
		scores[p.ID] = candidate{p, score, inCondition}
		return score > 0
	})
	if err != nil {
		return hookOut{}, err
	}
	var ranked []candidate
	for _, p := range live {
		ranked = append(ranked, scores[p.ID])
	}
	// stable: page order (by id) breaks ties
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && (ranked[j].score > ranked[j-1].score || ranked[j].score == ranked[j-1].score && ranked[j].inCondition && !ranked[j-1].inCondition); j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	var context string
	err = updateSession(agent, in.SessionID, func(s *hookSession) {
		if s.TurnID != snapshot.TurnID {
			return // a new request arrived meanwhile: this tool call belongs to it, and the next one gets its lesson
		}
		seen := map[string]bool{}
		for _, m := range s.Memories {
			if age := time.Since(m.At); age >= 0 && age < time.Hour {
				seen[m.Key] = true
			}
		}
		for _, c := range ranked {
			q := lessonQuote(c.page)
			if seen[q.Key] {
				continue
			}
			var derr error
			context, derr = deliverToolLesson(agent, s, q, target, in.AgentID)
			if derr != nil {
				err = derr
			}
			return
		}
	})
	if err != nil || context == "" {
		return hookOut{}, err
	}
	return hookOut{Context: context}, nil
}

// deliverToolLesson records one quote delivered at a tool call (its own delivery id, under the turn as parent, so the reflection
// assesses it with that turn's conversation) and returns the text for the agent.
func deliverToolLesson(agent string, s *hookSession, q memoryQuote, target toolTarget, agentID string) (string, error) {
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
		d := store.MemoryDelivery{ID: hex.EncodeToString(sum[:])[:16], Scope: s.Scope, Request: cut(s.Request, requestMax), Quotes: clipped, Parent: s.TurnID, Agent: agentID, Tool: target.label}
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
