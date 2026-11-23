package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// What a `shikii wiki query` that finds nothing tells the search journal about the session that asked. Claude Code puts the id of
// its current session in CLAUDE_CODE_SESSION_ID and Codex puts its thread in CODEX_THREAD_ID, in the environment of the commands
// they run; the command takes the id only when this project's hook kept state for that session (a stale, made-up or mistyped id
// names no session), and never when the two variables name two sessions. The MCP body and the HTTP handler never read the
// environment: the process that serves them was started earlier, and what it holds is not the session that asks now.

const (
	claudeSessionVariable = "CLAUDE_CODE_SESSION_ID"
	codexSessionVariable  = "CODEX_THREAD_ID"
)

// journalMissSession is the session the search journal of root keeps for the miss of the words, as the journal file holds it (plain
// JSON); found is false when the journal has no miss of those words.
func journalMissSession(t *testing.T, root, query string) (session string, found bool) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(root, "records", "wiki-searches", "*", ".searches.json"))
	if len(paths) != 1 {
		t.Fatalf("the project has one search journal: %v", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Misses []struct{ Query, Session string } `json:"misses"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	for _, m := range file.Misses {
		if m.Query == query {
			return m.Session, true
		}
	}
	return "", false
}

// keepHookState leaves the state the hook keeps of a session in the project's store, by the writer the hook uses.
func keepHookState(t *testing.T, root, scope, session string) {
	t.Helper()
	if err := saveSession(root, session, &hookSession{Scope: scope}); err != nil {
		t.Fatal(err)
	}
}

// A search by words that finds nothing, run as the command of the session the environment names, keeps that session with the miss,
// and only the records of that session name it and tie it to their result; the record of another session in the same project does
// not. It is the same session a search through a receipt keeps (missSession of the hook's session_id), so a later search of the same
// words is pointed at the result of the session that searched.
func TestAWikiQueryRunInAHookSessionKeepsThatSessionWithItsMiss(t *testing.T) {
	for _, c := range []struct{ name, variable, unused string }{
		{"Claude Code", claudeSessionVariable, codexSessionVariable},
		{"Codex", codexSessionVariable, claudeSessionVariable},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			t.Setenv(c.unused, "")
			for _, session := range []string{"searcher", "bystander"} {
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Explain archive approval."})
			}
			t.Setenv(c.variable, "searcher")
			notebookCommand(t, "query", "saffron lanterns")
			if got, found := journalMissSession(t, root, "saffron lanterns"); !found || got != missSession("searcher") {
				t.Fatalf("the miss keeps the session the environment names (%s): %q %v", missSession("searcher"), got, found)
			}
			record := func(session string) map[string]any {
				t.Helper()
				result := map[string]any{"changed": "Explained archive approval.", "checked": "Searched the wiki for saffron lanterns and found no article."}
				saved, err := workCall(t, b, "record", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "result": result})
				if err != nil {
					t.Fatal(err)
				}
				return saved
			}
			if named, ok := record("bystander")["missed_searches"]; ok {
				t.Fatalf("the record of another session does not name the words: %v", named)
			}
			saved := record("searcher")
			if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"saffron lanterns"}) {
				t.Fatalf("the record of the session that searched names its words: %q", got)
			}
			want := resultID(t, saved)
			missed, err := b.OpenMisses(time.Time{}, 8, missSession("searcher"))
			if err != nil || len(missed) != 1 || missed[0].Result != want {
				t.Fatalf("the words are tied to the result of the session that searched (%s): %v %v", want, missed, err)
			}
		})
	}
}

// The session of a miss is taken only when it is certain. Each case is a state of the environment and of what the hook kept; want is
// the session the miss keeps ("" for none, as before).
func TestAWikiQueryTakesTheSessionOfTheEnvironmentOnlyWhenTheHookKnowsIt(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	scope := feedbackScope(repo)
	// the states a made-up id would reach if its odd characters were turned into underscores, as the path of a state is made
	for _, session := range []string{"claude-session", "codex-session", "other-session", "ab_c", "___x", "claude-session____other-session", "claude-session_", strings.Repeat("a", 201)} {
		keepHookState(t, root, scope, session)
	}
	sessions := filepath.Join(root, "sessions")
	if err := os.Symlink(filepath.Join(sessions, "other-session.json"), filepath.Join(sessions, "linked-session.json")); err != nil {
		t.Skipf("a symbolic link cannot be made here: %v", err)
	}
	cases := []struct {
		name, claude, codex, want string
	}{
		{"neither variable is set", "", "", ""},
		{"a session the hook kept, in the Claude Code variable", "claude-session", "", "claude-session"},
		{"a session the hook kept, in the Codex variable", "", "codex-session", "codex-session"},
		{"both variables name the same session", "claude-session", "claude-session", "claude-session"},
		{"a session the hook kept no state for", "stale-session", "", ""},
		{"the id of the other agent when only that one is known", "stale-session", "codex-session", "codex-session"},
		{"the first id when only that one is known", "claude-session", "stale-session", "claude-session"},
		{"two different sessions the hook kept", "claude-session", "codex-session", ""},
		{"an id with a dot, whose path would be another session's", "ab.c", "", ""},
		{"an id that climbs out of the folder", "../x", "", ""},
		{"an id with a slash", "claude-session/../other-session", "", ""},
		{"an id with a space", "claude-session ", "", ""},
		{"an id longer than a session id", strings.Repeat("a", 201), "", ""},
		{"the state of the session is a link", "linked-session", "", ""},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(claudeSessionVariable, c.claude)
			t.Setenv(codexSessionVariable, c.codex)
			words := "saffron lanterns " + string(rune('a'+i))
			notebookCommand(t, "query", words)
			got, found := journalMissSession(t, root, words)
			want := ""
			if c.want != "" {
				want = missSession(c.want)
			}
			if !found || got != want {
				t.Fatalf("the miss keeps %q, not %q (found %v)", want, got, found)
			}
		})
	}
}

// The MCP body and the HTTP handler know no session whatever their environment holds: the process of the daemon was started before
// the session that asks now, and the id it kept is an earlier one. A request cannot name a session either.
func TestAWikiQueryOverMCPKeepsNoSessionWhateverTheEnvironmentHolds(t *testing.T) {
	f := newMCPWikiFlow(t)
	keepHookState(t, f.book.Root, f.book.Scope, "claude-session")
	keepHookState(t, f.book.Root, f.book.Scope, "codex-session")
	// each variable alone names a session the hook kept, so a command line would take it (the cases of the table above)
	for i, variables := range []map[string]string{
		{claudeSessionVariable: "claude-session", codexSessionVariable: ""},
		{claudeSessionVariable: "", codexSessionVariable: "codex-session"},
	} {
		for name, value := range variables {
			t.Setenv(name, value)
		}
		words := "saffron lanterns " + string(rune('a'+i))
		if answer := f.call(map[string]any{"operation": "query", "query": words}); answer.IsError {
			t.Fatalf("the search is answered: %s", answer.Text)
		}
		if got, found := journalMissSession(t, f.book.Root, words); !found || got != "" {
			t.Fatalf("the miss of an MCP search keeps no session (%v): %q %v", variables, got, found)
		}
	}
	// and a request cannot name a session itself
	t.Setenv(claudeSessionVariable, "")
	t.Setenv(codexSessionVariable, "")
	if answer := f.call(map[string]any{"operation": "query", "query": "juniper meadow", "session": missSession("claude-session")}); !answer.IsError {
		t.Fatalf("a request that names a session is refused: %s", answer.Text)
	}
	if _, found := journalMissSession(t, f.book.Root, "juniper meadow"); found {
		t.Fatal("a refused request keeps no miss")
	}
}
