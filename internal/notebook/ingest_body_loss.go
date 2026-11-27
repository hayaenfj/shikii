package notebook

import (
	"fmt"
	"strings"
)

// A save of an existing article through Ingest (shikii wiki ingest, shikii reflect submit, the MCP and HTTP calls of both) replaces the
// whole body with the one sent. An agent that sends only its new sentences, believing it adds them, used to wipe the article out
// (2026-11-27: three articles lost their bodies). Ingest therefore refuses a body that loses half of the previous one or more.
//
// The measure: the previous body is cut into lines (empty lines dropped, surrounding whitespace trimmed). A line is kept when it is
// a substring of the new body. The bytes of the kept lines are compared with the bytes of all the lines; half or fewer kept is a refusal.
// Weighing by bytes lets a heading that survives count for little against paragraphs that are gone. A body that is added to in full
// keeps every line, and a small rewording keeps the other lines.
//
// Only the agents' save is guarded. The person's edit on the Knowledge page (Manage) and the revision-checked patch (Patch), where the
// caller shows exactly what changes, call ingest without the guard and may shorten or rewrite a body as far as they want.

// bodyKeptMin: the kept bytes times this must exceed the bytes of all lines (exactly half kept, half lost, is refused).
const bodyKeptMin = 2

// bodyKeptBytes returns the bytes of the previous body's lines that the new body still contains, and the bytes of all those lines.
func bodyKeptBytes(previous, next string) (kept, total int) {
	for _, line := range strings.Split(previous, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		total += len(line)
		if strings.Contains(next, line) {
			kept += len(line)
		}
	}
	return kept, total
}

// bodyLossRefusal refuses a body that would lose half or more of the previous one in the sense above, and says why and what to send instead.
// idSent says whether the save named the article by its id; without one the article was found by the title (the id is made from the first
// title), so the refusal does not say an id was sent and tells a new lesson that has the same title to take another one.
func bodyLossRefusal(id string, idSent bool, previous, next string) error {
	kept, total := bodyKeptBytes(previous, next)
	if total == 0 || kept*bodyKeptMin > total {
		return nil
	}
	lost := (total - kept) * 100 / total
	// The measure is per line: a line counts as lost when any part of it changed, so a rewording inside a long line drops that whole line.
	measure := "A line counts as kept only when it appears whole in the new body, so changing even part of a line counts that whole line as dropped; make a small edit inside a long line with patch."
	replaces := "Saving with an id replaces the whole body; it does not add to it."
	if !idSent {
		replaces = fmt.Sprintf("Saving without an id finds the article by its title, and this title is the first title of article %s, so the save replaces the whole body of that article; it does not add to it. If this is a different lesson, give it another title.", id)
	}
	return fmt.Errorf("refused: this save would replace the body of article %s and drop %d%% of it (%d of %d bytes of its lines are not in the new body). %s %s To change or shorten part of the body, send shikii wiki patch (MCP: shikii_wiki operation patch) with before/after edits, sources and a change_reason. To add to it, read the article with shikii wiki query lesson:%s and send its whole body again with your additions", id, lost, total-kept, total, measure, replaces, id)
}
