package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.12 item 4: the leading words of a command are read more exactly. shikii and npm take a third word (shikii hook plan, npm run
// check), so a lesson about shikii hook install does not arrive before shikii hook plan query. A command is read up to its first <<:
// the lines of a here-document are text handed to the command, and what follows the document is not read either (three audits found a
// form of the terminator misread, so the rule was cut down to this).
func TestCommandPhrasesReadNestedSubcommandsAndSkipHereDocuments(t *testing.T) {
	for _, c := range []struct {
		command string
		want    []string
	}{
		{"shikii hook plan query", []string{"shikii hook plan"}},
		{"shikii hook install --yes", []string{"shikii hook install"}},
		{"shikii update", []string{"shikii update"}},
		{"shikii wiki query lesson:0ac559f6d62b03a9", []string{"shikii wiki query"}},
		{"npm run check && go test ./...", []string{"npm run check", "go test"}},
		{"npm install", []string{"npm install"}},
		{"gh release edit v0.12.0 --draft=false", []string{"gh release"}},
		{"cat <<'EOF' > notes.txt\ngit commit -m hi\nEOF\ngo test ./...", nil}, // nothing after the first << is read
		{"python3 - <<EOF\nimport json\nmake test\nEOF", nil},
		{"cat <<-EOF\n\tgit commit\n\tEOF\ngit status", nil},
		{"cat << EOF | sort\ngit commit\nEOF", nil},
		{"cat <<\"EOF\"\ngit commit\nEOF\nmake test", nil},
		{"cat <<EOF\ngit commit\nEOF", nil},
		{"make test && cat <<EOF\ngit commit\nEOF", []string{"make test"}}, // what stands before the << on its line is read
	} {
		if got := commandPhrases(c.command); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("commandPhrases(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestToolLessonUsesTheExactSubcommandAndIgnoresHereDocuments(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	install, err := book.Ingest(notebook.Draft{Sources: draft.Sources, Title: "Replace this machine's shikii", Lesson: "Back up the binary before shikii hook install --yes.", AppliesTo: "when running shikii hook install", Kind: "howto", Scene: "when replacing the binary"})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := book.Ingest(notebook.Draft{Sources: draft.Sources, Title: "Check the exit code before committing", Lesson: commitLessonText, AppliesTo: "when committing in shikii", Kind: "rule", Scene: "when committing"})
	if err != nil {
		t.Fatal(err)
	}
	if out := toolHook(t, scope, "plan", toolPrompt, "Bash", map[string]any{"command": "shikii hook plan query"}); out != "" {
		t.Fatalf("a lesson about shikii hook install does not arrive before another hook subcommand:\n%s", out)
	}
	ctx := feedbackContext(t, toolHook(t, scope, "install", toolPrompt, "Bash", map[string]any{"command": "shikii hook install --yes"}))
	if !strings.Contains(ctx, "lesson:"+install.Page.ID+":") || !strings.Contains(ctx, "shikii hook install") {
		t.Fatalf("the lesson arrives before the subcommand it names:\n%s", ctx)
	}
	if out := toolHook(t, scope, "heredoc", toolPrompt, "Bash", map[string]any{"command": "cat <<'EOF' > msg.txt\ngit commit -m x\nEOF"}); out != "" {
		t.Fatalf("git commit inside a here-document is text, not a command:\n%s", out)
	}
	if out := toolHook(t, scope, "after", toolPrompt, "Bash", map[string]any{"command": "cat <<'EOF' > msg.txt\nfix: x\nEOF\ngit commit -F msg.txt"}); out != "" {
		t.Fatalf("what follows a here-document is not read (the lesson comes when git commit runs on its own):\n%s", out)
	}
	ctx = feedbackContext(t, toolHook(t, scope, "alone", toolPrompt, "Bash", map[string]any{"command": "git commit -F msg.txt"}))
	if !strings.Contains(ctx, "lesson:"+commit.Page.ID+":") {
		t.Fatalf("the command on its own gets the lesson:\n%s", ctx)
	}
}
