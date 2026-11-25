package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The ways next_action and the guides tell the agent how to say which conversations it read are commands with the JSON they read on
// standard input. A test takes each of them out of the text, as written, fills in the ids of the conversations a run printed, and runs it
// through the real command: a form that the command refuses, or that does not move the window over the conversations, cannot stay in a text.

// napReadForm is `shikii <reflect submit | wiki ingest> <<'JSON'`, the one line of JSON and `JSON`, each on a line of its own (a form in a
// list of a guide is indented by that list).
var napReadForm = regexp.MustCompile(`(?m)^[ \t]*shikii (reflect submit|wiki ingest) <<'JSON'\n[ \t]*(\{[^\n]*\})\n[ \t]*JSON$`)

// napConversationIDs is the placeholder of a form for the ids of the conversations read: one or more of them in a list.
var napConversationIDs = regexp.MustCompile(`"<conversation id>"(?:,\s*"<conversation id>")*`)

type napFormText struct{ command, input string }

func napReadFormsIn(text string) []napFormText {
	var forms []napFormText
	for _, m := range napReadForm.FindAllStringSubmatch(text, -1) {
		forms = append(forms, napFormText{m[1], m[2]})
	}
	return forms
}

// napGuideSection is the section of a guide on the nap, from its heading to the next one.
func napGuideSection(t *testing.T, file, heading string) string {
	t.Helper()
	text := string(mustRead(t, file))
	at := strings.Index(text, heading)
	if at < 0 {
		t.Fatalf("%s has no section %q", file, heading)
	}
	section := text[at+len(heading):]
	if end := strings.Index(section, "\n### "); end >= 0 {
		section = section[:end]
	}
	return section
}

// pendingReflectSources is the ids of the conversations shikii reflect query lists.
func pendingReflectSources(t *testing.T, st store.Store, scope string) []string {
	t.Helper()
	view, err := reflectQuery(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range view.Sources {
		ids = append(ids, s.Source.ID)
	}
	return ids
}

func TestNapFormsThatSayWhichConversationsWereReadRunAsWritten(t *testing.T) {
	for _, src := range []struct {
		name string
		text func(t *testing.T) string
	}{
		{"next_action", func(*testing.T) string { return napNextAction }},
		{"docs/guide.md", func(t *testing.T) string { return napGuideSection(t, "../../docs/guide.md", "### Memory nap") }},
		{"docs/guide.ja.md", func(t *testing.T) string {
			return napGuideSection(t, "../../docs/guide.ja.md", "### \u8a18\u61b6\u306e\u3072\u3068\u4f11\u307f")
		}},
	} {
		forms := napReadFormsIn(src.text(t))
		kinds := map[string]int{}
		for _, f := range forms {
			kinds[f.command]++
		}
		if kinds["reflect submit"] == 0 || kinds["wiki ingest"] == 0 {
			t.Errorf("%s shows no form of shikii reflect submit and of shikii wiki ingest that sends the ids as they are read: %v", src.name, kinds)
			continue
		}
		for i, form := range forms {
			variants := []string{""}
			if form.command == "reflect submit" {
				variants = append(variants, `"session_id":"session-0000000000000002",`) // the guides say it may be added
			}
			for _, extra := range variants {
				t.Run(fmt.Sprintf("%s/%s %d %s", src.name, form.command, i, extra), func(t *testing.T) {
					st, scope, _, _, _ := memoryEffectFixture(t)
					saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-2*time.Hour), "second")
					saveConversation(t, st, scope, "0000000000000003", time.Now().Add(-time.Hour), "third")
					ids := conversationIDs(decodeNap(t))
					if fmt.Sprint(ids) != hexIDs(1, 3) {
						t.Fatalf("premise: the run prints the three conversations: %v", ids)
					}
					if pending := pendingReflectSources(t, st, scope); !slices.Contains(pending, ids[1]) || !slices.Contains(pending, ids[2]) {
						t.Fatalf("premise: the conversations wait for the reflection of their session: %v", pending)
					}
					quoted := make([]string, len(ids))
					for j, id := range ids {
						quoted[j] = `"` + id + `"`
					}
					input := napConversationIDs.ReplaceAllString(form.input, strings.Join(quoted, ","))
					input = strings.Replace(input, "{", "{"+extra, 1)
					if strings.Contains(input, "<") {
						t.Fatalf("the form has a placeholder this test does not fill: %s", form.input)
					}
					var out string
					var err error
					withStdin(t, input, func() {
						out, err = captureErr(func() error { return cliCall(strings.Fields(form.command)) })
					})
					if err != nil {
						t.Fatalf("shikii %s is refused as the text writes it: %v\n%s", form.command, err, input)
					}
					if form.command == "reflect submit" {
						var reply struct {
							Complete bool `json:"complete"`
							Wiki     []struct {
								Error string `json:"error"`
							} `json:"wiki"`
						}
						if json.Unmarshal([]byte(out), &reply) != nil || !reply.Complete || len(reply.Wiki) != 1 || reply.Wiki[0].Error != "" {
							t.Fatalf("the save is complete and its one entry has no error: %s", out)
						}
					}
					if got := conversationIDs(decodeNap(t)); len(got) != 0 {
						t.Fatalf("the ids were sent as the text says, yet the window did not move over them: %v", got)
					}
					if pending := pendingReflectSources(t, st, scope); slices.Contains(pending, ids[1]) || slices.Contains(pending, ids[2]) {
						t.Fatalf("a conversation named as a source is reviewed, so it leaves the list shikii reflect query gives its session: %v", pending)
					}
				})
			}
		}
	}
}

// What the audit of the guides of 0.43.0 found: the record of what was shown has four fields, the place can name a copy of a line, a
// conversation is counted once, the ids are sent on standard input in a form the commands read, and the sentences in Japanese say who does
// what in words a reader who knows nothing of shikii's insides can follow.
func TestNapGuidesSayWhatTheLastAuditFound(t *testing.T) {
	for _, d := range []struct {
		file, heading string
		want, old     []string
	}{
		{"../../docs/guide.md", "### Memory nap", []string{"`.shikii/nap-first`", "whether an article cited it then", "which copy", "as long as that line still ends at the byte position the place holds", "earlier lines of the record were removed or rewritten", "at the moment the place is written", "`.shikii/nap-first.lock`", "by a rename", "under that one lock", "a later run moves the place over them and counts them", "records nothing of that print", "`shikii forget` takes out the lines", "without the `wiki` list", "`session_id`", "`left`", "`shikii reflect query`", "once, by the run that moves the place over it", "printed no conversation"}, []string{"{\"session_id\": ..., \"wiki\": [{\"sources\": [ids]}]}", "`shikii forget` removed some", "and whether an article cited it then"}},
		{"../../docs/guide.ja.md", "### \u8a18\u61b6\u306e\u3072\u3068\u4f11\u307f", []string{"`.shikii/nap-first`", "\u6700\u521d\u306b\u51fa\u3057\u305f\u6642\u306b\u8a18\u4e8b\u304c\u5f15\u3044\u3066\u3044\u305f\u304b", "\u4f55\u756a\u76ee\u304b", "\u305d\u306e\u884c\u306e\u7d42\u308f\u308a\u304c\u4f4d\u7f6e\u306b\u66f8\u3044\u3066\u3042\u308b\u30d0\u30a4\u30c8\u4f4d\u7f6e\u3068\u540c\u3058\u6642\u3060\u3051", "\u4f4d\u7f6e\u304c\u5408\u308f\u306a\u304f\u306a\u3063\u305f\u6642", "\u4f4d\u7f6e\u3092\u66f8\u3044\u305f\u305d\u306e\u6642", "`.shikii/nap-first.lock`", "\u540d\u524d\u306e\u4ed8\u3051\u66ff\u3048", "1 \u56de\u306e\u30ed\u30c3\u30af\u306e\u4e0b\u3067", "\u30ed\u30c3\u30af\u306a\u3057\u3067\u5148\u3078\u9032\u307f\u307e\u305b\u3093", "\u4f55\u3082\u8a18\u9332\u305b\u305a", "`shikii forget` \u306f\u5fd8\u308c\u308b\u4f1a\u8a71\u306e\u884c\u3092\u6d88\u3057\u307e\u3059", "`wiki` \u306e\u4e00\u89a7\u3092\u4ed8\u3051\u305a", "`session_id`", "`left`", "`shikii reflect query`", "1 \u5ea6\u3060\u3051\u6570\u3048\u307e\u3059", "\u51fa\u3059\u4f1a\u8a71\u304c 1 \u4ef6\u3082\u7121\u3044\u56de", "\u521d\u3081\u3066\u306e\u6642\u306f\u5168\u90e8\u3092\u51fa\u3057\u307e\u3059", "\u51fa\u5178(sources)\u306b\u5165\u308c\u308b\u3068", "\u5207\u3089\u308c\u305a\u306b\u8aad\u3081\u308b\u3088\u3046", "\u53d6\u308a\u8fbc\u307e\u308c\u305f\u7269\u306e\u6570\u3092\u793a\u3057\u307e\u3059", "\u8aad\u307e\u308c\u3066\u4f4d\u7f6e\u304c\u9032\u3093\u3060\u56de\u306b 1 \u5ea6\u3060\u3051\u6570\u3048\u307e\u3059", "`last_nap` \u3092\u51fa\u3057\u305f\u56de\u3054\u3068\u306b", "\u5225\u306e `shikii nap` \u304c\u3059\u3067\u306b\u4f4d\u7f6e\u3092\u9032\u3081\u3066\u3044\u305f\u6642\u306f", "\u4eba\u304c\u5225\u306e\u76ee\u7684\u3067\u7f6e\u3044\u305f\u30d5\u30a1\u30a4\u30eb\u304b\u3082\u3057\u308c\u306a\u3044"}, []string{"\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u304c\u8aad\u307f\u7d42\u3048\u305f\u3001\u4f1a\u8a71\u306e\u8a18\u9332\u306e\u4e2d\u306e\u4f4d\u7f6e\u3088\u308a\u5f8c\u306b\u66f8\u304b\u308c\u305f\u4f1a\u8a71\u3067\u3001\u521d\u56de\u306f\u5168\u90e8\u3067\u3059", "wiki \u306e\u9805\u76ee\u306e\u51fa\u5178\u306b\u305d\u306e\u4f1a\u8a71\u306e id \u3092\u66f8\u3044\u305f\u6642\u306b", "\u51fa\u529b\u304b\u3089\u4f1a\u8a71\u3092\u843d\u3068\u3057\u3066\u898b\u305b\u305f\u5834\u5408", "\u305d\u308c\u4ee5\u5916\u3067\u540c\u3058\u72b6\u614b\u306e id \u306f", "\u51fa\u529b\u3092\u59cb\u3081\u305f\u6642\u306e\u307e\u307e\u306e\u9593\u3060\u3051\u3067\u3059", "\u3042\u306a\u305f\u306e\u30d5\u30a1\u30a4\u30eb\u306a\u306e\u3067", "\u3069\u308c\u3060\u3051\u8a18\u4e8b\u306b\u5c4a\u3044\u305f\u304b", "\u4f4d\u7f6e\u3092\u9032\u3081\u305f\u56de\u306b 1 \u5ea6\u3060\u3051", "\u8a18\u9332\u304c\u3042\u3063\u305f\u56de\u3054\u3068\u306b", "\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306e\u30b3\u30de\u30f3\u30c9\u306e\u51fa\u529b\u304c\u305d\u306e\u307e\u307e\u898b\u3048\u308b\u5927\u304d\u3055\u3068\u3057\u3066", "{\"session_id\": ..., \"wiki\": [{\"sources\": [ids]}]}", "`shikii forget` \u3067\u884c\u304c\u6e1b\u3063\u3066\u305d\u306e\u756a\u76ee\u304c\u7121\u3044\u6642", "\u305d\u306e\u6642\u306b\u8a18\u4e8b\u304c\u5f15\u3044\u3066\u3044\u305f\u304b"}},
		{"../../docs/memory-and-learning.ja.md", "", []string{"\u8aad\u3093\u3060\u4f1a\u8a71\u306e id \u3092\u51fa\u5178\u306b\u5165\u308c\u3066\u4fdd\u5b58\u3059\u308b\u3053\u3068\u3067\u3001\u8aad\u3093\u3060\u3053\u3068\u3092\u4f1d\u3048\u307e\u3059"}, []string{"wiki \u306e\u9805\u76ee\u306e\u51fa\u5178\u306b\u66f8\u3044\u3066\u4f1d\u3048\u3001\u6b21\u306f\u305d\u306e\u7d9a\u304d\u304b\u3089\u51fa\u3057\u307e\u3059"}},
	} {
		text := string(mustRead(t, d.file))
		if d.heading != "" { // the whole file when there is no heading
			text = napGuideSection(t, d.file, d.heading)
		}
		text = strings.Join(strings.Fields(text), " ")
		for _, want := range d.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not say %q", d.file, want)
			}
		}
		for _, old := range d.old {
			if strings.Contains(text, old) {
				t.Errorf("%s still says %q", d.file, old)
			}
		}
	}
}
