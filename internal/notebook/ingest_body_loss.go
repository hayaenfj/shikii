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
// A save is also measured against the revision the agents' saves started from (Page.KeptFrom). Saves that each keep a bit more than half
// of the body before them still wear an article away (8 lines, then 6 kept and 1 added, then 5 and 1, then 4 and 1: 2026-11-27 leftover C-4),
// so a save that drops any line of the body before it records that body's revision as the reference, a later save carries the reference on
// while a line of it is still missing, and a save is refused when it drops half or more of the reference as well. A save that has every
// line of the reference again clears it. The person's edit and the patch clear it, since what they save is settled by a person or shown line
// by line; a reference that cannot be read (its file is gone) is not measured and is not carried on, so it never stops a save.
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

// bodyKeptMeasure is the one measure both refusals use: it says whether next keeps more than half of the bytes of the previous lines, and
// how much is lost (the share in percent, the bytes lost and the bytes of all the lines). total is 0 when there is nothing to lose.
func bodyKeptMeasure(previous, next string) (refused bool, lost, lostBytes, total int) {
	kept, total := bodyKeptBytes(previous, next)
	if total == 0 || kept*bodyKeptMin > total {
		return false, 0, total - kept, total
	}
	return true, (total - kept) * 100 / total, total - kept, total
}

// bodyLossMeasure and bodyWayOn are the sentences that both refusals share. The measure is per line: a line counts as lost when any part of
// it changed, so a rewording inside a long line drops that whole line. The way on names the patch first: to add, a patch whose after is
// a unique ending of the body plus the new text keeps every other line exactly as it is, while a body copied out by hand can lose a part
// of it without any refusal (0.44.0 leftover U-11: 49% lost, below the line).
const (
	bodyLossMeasure = "A line counts as kept only when it appears whole in the new body, so changing even part of a line counts that whole line as dropped; make a small edit inside a long line with patch."
	bodyWayOn       = "To add to the body, or to change or shorten part of it, send shikii wiki patch (MCP: shikii_wiki operation patch) with the article's id and current revision, sources, a change_reason and before/after edits (read the article, its id and its current revision with shikii wiki query lesson:%s first); to add at the end, make before a unique ending of the body and after that ending plus the new text. Only when a patch does not fit, send its whole body again with your additions, copied exactly"
)

// bodyLossRefusal refuses a body that would lose half or more of the previous one in the sense above, and says why and what to send instead.
// idSent says whether the save named the article by its id; without one the article was found by the title (the id is made from the first
// title), so the refusal does not say an id was sent and tells a new lesson that has the same title to take another one.
func bodyLossRefusal(id string, idSent bool, previous, next string) error {
	refused, lost, lostBytes, total := bodyKeptMeasure(previous, next)
	if !refused {
		return nil
	}
	return fmt.Errorf("refused: this save would replace the body of article %s and drop %d%% of it (%d of %d bytes of its lines are not in the new body). %s %s "+bodyWayOn, id, lost, lostBytes, total, bodyLossMeasure, bodyReplacesNote(id, idSent), id)
}

// bodyReplacesNote is the sentence both refusals use to say what the save did to the body. A save with an id names the article itself. Without
// one the article was found by the title (the id is made from the first title), so the sentence does not say an id was sent and tells a new
// lesson that has the same title to take another one: the refusal that measures against the reference revision says it as well, since a save
// by the title reaches that refusal too (0.44.1 audit W5).
func bodyReplacesNote(id string, idSent bool) string {
	if idSent {
		return "Saving with an id replaces the whole body; it does not add to it."
	}
	return fmt.Sprintf("Saving without an id finds the article by its title, and this title is the first title of article %s, so the save replaces the whole body of that article; it does not add to it. If this is a different lesson, give it another title.", id)
}

// bodyBasisRefusal refuses a body that drops half or more of the lines of the revision the agents' saves started from, though it keeps more
// than half of the body before it. It says which revision it measures against, because the agent does not see the saves in between. idSent is
// as in bodyLossRefusal.
func bodyBasisRefusal(id string, idSent bool, next string, basis Page) error {
	refused, lost, lostBytes, total := bodyKeptMeasure(basis.Body, next)
	if !refused {
		return nil
	}
	return fmt.Errorf("refused: this save would drop %d%% of the body article %s had at revision %s (%d of %d bytes of that revision's lines are not in the new body). Each save since then kept more than half of the body before it, but together they would wear the article away, and revision %s is the last one that a person's edit or a patch settled, or that a save kept whole. %s %s A patch also makes its result the revision that later saves are measured against. "+bodyWayOn, lost, id, basis.Revision, lostBytes, total, basis.Revision, bodyLossMeasure, bodyReplacesNote(id, idSent), id)
}

// guardBody is what Ingest does with the body it is sent for an existing article: it refuses a body that loses half or more of the body before
// it or of the reference revision, and returns the reference the saved page carries (the revision a later save is measured against, empty
// when the new body has every line of the one measured against). The reference revision is read from the page's own history; one that cannot
// be read is not measured, and the body before it stands in for it.
func (b Book) guardBody(id string, idSent bool, old Page, next string) (string, error) {
	if err := bodyLossRefusal(id, idSent, old.Body, next); err != nil {
		return "", err
	}
	basis := old
	if old.KeptFrom != "" {
		if earlier, err := b.readRevision(old.ID + "@" + old.KeptFrom); err == nil {
			basis = earlier
			if err := bodyBasisRefusal(id, idSent, next, earlier); err != nil {
				return "", err
			}
		}
	}
	if kept, total := bodyKeptBytes(basis.Body, next); kept < total {
		return basis.Revision, nil
	}
	return "", nil
}
