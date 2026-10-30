package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.11: when Claude Code's language is one written in its own script (Japanese, Chinese, Korean, Russian, Ukrainian), a final reply written
// mostly in Latin letters is stopped once at Stop, and the agent is told to rewrite it in the person's language (the built-in ritual check reply-language).

const (
	// an English final reply long enough to be checked
	replyEnglish = "I updated the stop hook so that it checks the language of the final reply, ran the focused tests, and committed the change with a detailed message."
	// a Japanese final reply with a code block, inline code, a URL, a path and English identifiers: written in Japanese
	// (Japanese test data uses \u escapes so the file ships in English)
	replyJapanese = "\u6700\u5f8c\u306e\u8fd4\u4e8b\u306e\u8a00\u8449\u3092 Stop \u3067\u78ba\u304b\u3081\u308b\u3088\u3046\u306b\u3057\u307e\u3057\u305f\u3002" +
		"`ritualStop` \u306b `reply-language` \u3092\u8db3\u3057\u3001cmd/shikii/reply_language.go \u306b\u6570\u3048\u65b9\u3092\u7f6e\u304d\u307e\u3057\u305f\u3002\n\n" +
		"```go\nfunc replyLanguageCheck(lang, reply string) (checked, pass bool, share int) {\n\t// counts letters of the person's script among all letters\n\treturn\n}\n```\n\n" +
		"\u8a73\u3057\u304f\u306f https://github.com/hayaenfj/shikii/blob/main/docs/guide.md \u3092\u898b\u3066\u304f\u3060\u3055\u3044\u3002make test \u3082 make check-english \u3082\u901a\u308a\u307e\u3057\u305f\u3002"
	// a short English reply (under the minimum number of letters): not checked
	replyShort = "Done. Committed as abc1234."
)

// langStopTurn sets up a project with the given language (empty: not set), starts one turn and returns the repository.
func langStopTurn(t *testing.T, language string) string {
	t.Helper()
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	hookEnv(t, model, repo)
	if language != "" {
		setHookLanguage(t, language)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "lang", "cwd": repo, "prompt": "fix the stop hook"})
	return repo
}

func TestReplyLanguageBlocksEnglishReplyOnceWhenJapanese(t *testing.T) {
	repo := langStopTurn(t, "japanese")
	stop := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyEnglish})
	if !strings.Contains(stop, `"decision":"block"`) || !strings.Contains(stop, "reply-language") || !strings.Contains(stop, "Japanese") {
		t.Fatalf("an English final reply with language japanese is stopped once and told to rewrite it in Japanese: %s", stop)
	}
	// An unchanged English reply halts explicitly incomplete and records the failure.
	again := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "stop_hook_active": true, "last_assistant_message": replyEnglish})
	requireRecoveryHalt(t, again)
	// a later Stop of the same turn without stop_hook_active (after a notice) does not block either
	if again := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyEnglish}); strings.Contains(again, "block") {
		t.Fatalf("the same turn is never blocked twice: %s", again)
	}
	// a background task's notice reopens the judgment of the same turn (Judged is reset), but the turn was already blocked once: its Stop does not block
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "lang", "cwd": repo, "prompt": "<task-notification>done</task-notification>"})
	if again := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyEnglish}); strings.Contains(again, "block") {
		t.Fatalf("the Stop after a notice in the same turn is not blocked again: %s", again)
	}
	root := filepath.Join(repo, ".shikii")
	if out := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(out, "reply-language in the person's language 0 of 1 checked (after a stop 0)") {
		t.Fatalf("status counts the reply-language check on the rituals line:\n%s", out)
	}
}

func TestReplyLanguageFixedAfterBlockIsCounted(t *testing.T) {
	repo := langStopTurn(t, "Japanese")
	if stop := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyEnglish}); !strings.Contains(stop, "block") {
		t.Fatalf("blocked first: %s", stop)
	}
	if again := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "stop_hook_active": true, "last_assistant_message": replyJapanese}); strings.Contains(again, "block") {
		t.Fatalf("the rewritten reply ends the turn: %s", again)
	}
	root := filepath.Join(repo, ".shikii")
	if out := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(out, "reply-language in the person's language 0 of 1 checked (after a stop 1)") {
		t.Fatalf("status counts the reply rewritten after the stop:\n%s", out)
	}
}

func TestReplyLanguagePassesJapaneseReplyWithCode(t *testing.T) {
	repo := langStopTurn(t, "japanese")
	if stop := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyJapanese}); strings.Contains(stop, "block") {
		t.Fatalf("a Japanese reply with code blocks, a URL, a path and English identifiers passes: %s", stop)
	}
	root := filepath.Join(repo, ".shikii")
	if out := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(out, "reply-language in the person's language 1 of 1 checked") {
		t.Fatalf("a passing check is counted as done:\n%s", out)
	}
}

func TestReplyLanguageOffWithoutLanguageOrForLatinScript(t *testing.T) {
	for _, language := range []string{"", "English", "german", "Fran\u00e7ais", "vietnamese"} {
		t.Run(language, func(t *testing.T) {
			repo := langStopTurn(t, language)
			if stop := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyEnglish}); strings.Contains(stop, "block") {
				t.Fatalf("language %q never blocks: %s", language, stop)
			}
			root := filepath.Join(repo, ".shikii")
			if out := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); strings.Contains(out, "reply-language") {
				t.Fatalf("language %q records nothing:\n%s", language, out)
			}
		})
	}
}

func TestReplyLanguageShortReplyPasses(t *testing.T) {
	repo := langStopTurn(t, "japanese")
	if stop := runHook(t, "stop", map[string]any{"session_id": "lang", "cwd": repo, "last_assistant_message": replyShort}); strings.Contains(stop, "block") {
		t.Fatalf("a short reply is not checked: %s", stop)
	}
}

// The counting rule itself: the share of letters of the person's script among the letters left after removing code, URLs, paths and identifiers.
func TestReplyLanguageCountingRule(t *testing.T) {
	for _, c := range []struct {
		name, lang, reply string
		checked, pass     bool
	}{
		{"english reply, japanese", "ja", replyEnglish, true, false},
		{"japanese reply with code", "ja", replyJapanese, true, true},
		{"short reply", "ja", replyShort, false, true},
		{"latin-script language", "de", replyEnglish, false, true},
		{"no language", "", replyEnglish, false, true},
		// English prose around a long code block: the code does not count, the prose is English
		{"english around code", "ja", replyEnglish + "\n```go\n" + strings.Repeat("\u65e5\u672c\u8a9e ", 40) + "\n```", true, false},
		// a short Japanese line followed by a long code block and a long path: only the Japanese line counts, and it is too short to check
		{"japanese line and code only", "ja", "\u76f4\u3057\u307e\u3057\u305f\u3002\n```\nfunc main() { fmt.Println(\"hello world, this is a long line of code\") }\n```\n/Users/someone/projects/example/internal/something/long_file_name.go", false, true},
		{"russian reply", "ru", "\u042f \u0438\u0441\u043f\u0440\u0430\u0432\u0438\u043b \u043f\u0440\u043e\u0432\u0435\u0440\u043a\u0443 \u0432 \u043a\u043e\u043d\u0446\u0435 \u0445\u043e\u0434\u0430 \u0438 \u0437\u0430\u043f\u0443\u0441\u0442\u0438\u043b \u0432\u0441\u0435 \u0442\u0435\u0441\u0442\u044b, \u043e\u043d\u0438 \u043f\u0440\u043e\u0445\u043e\u0434\u044f\u0442 \u0431\u0435\u0437 \u043e\u0448\u0438\u0431\u043e\u043a.", true, true},
		{"english reply, russian", "ru", replyEnglish, true, false},
		{"english reply, korean", "ko", replyEnglish, true, false},
		{"english reply, chinese", "zh-TW", replyEnglish, true, false},
	} {
		checked, pass, _ := replyLanguageCheck(c.lang, c.reply)
		if checked != c.checked || pass != c.pass {
			t.Errorf("%s: checked=%v pass=%v, want checked=%v pass=%v", c.name, checked, pass, c.checked, c.pass)
		}
	}
}

// With another ritual missing in the same turn, the language line rides in the same block; a reply in the person's language adds no line.
func TestReplyLanguageJoinsOtherRitualsInOneBlock(t *testing.T) {
	for _, c := range []struct {
		name, reply string
		lang        bool
	}{{"english reply", replyEnglish, true}, {"japanese reply", replyJapanese, false}} {
		t.Run(c.name, func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			setHookLanguage(t, "japanese")
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": c.reply})
			var parsed map[string]any
			if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
				t.Fatalf("the missing plan blocks: %s", out)
			}
			reason, _ := parsed["reason"].(string)
			if !strings.Contains(reason, "hook plan query") || strings.Contains(reason, "person's language setting is Japanese") != c.lang {
				t.Fatalf("one block with the plan line, and the language line only for an English reply (%v): %s", c.lang, reason)
			}
			for _, line := range strings.Split(reason, "\n") {
				if strings.TrimSpace(line) == "-" {
					t.Fatalf("no empty line in the block: %q", reason)
				}
			}
		})
	}
}
