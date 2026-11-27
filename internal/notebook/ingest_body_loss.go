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

// The sentences that the refusals share. A refusal says two things: what happened (this save would drop a share of the body) and what to
// send next. Everything else an agent used to read there (how the measure works, what the other save did, what a patch also does) is left
// out, since the agent that is refused reads the whole of it and the two things were buried.
//
// bodyWayOn names the patch first: to add, a patch whose after is a unique ending of the body plus the new text keeps every other line
// exactly as it is, while a body copied out by hand can lose a part of it without any refusal (0.44.0 leftover U-11: 49% lost, below the
// line). It carries the shape of the call with the article's id and its current revision filled in, so the agent has nothing to look up
// but the ending of the body; the whole body sent again is the way only when a patch does not fit. The object is the patch itself, which
// the command line takes as it is; MCP and HTTP take it under the field "patch", so the sentence says so after the object (0.44.2 audit
// A-1: an agent that sent the object flat over MCP was refused for its unknown field "edits"). It stays one string literal, since the
// allowlist of the lines that mention a body (cmd/shikii/testdata/body_lines_allowlist.tsv) judges a line by what comes before the resend.
//
// bodyTitleNote is added when the save named no id: the article was found by the title (the id is made from the first title), so a
// different lesson that has the same title is told to take another one.
//
// bodyUnreadRevision is the revision of the patch in a refusal that has not read the article (a save with an id that lacks a lesson or its
// sources is refused before the article is read): the agent reads it with the query the patch asks for.
const (
	bodyWayOn     = "To add, send shikii wiki patch {\"id\":\"%[1]s\",\"revision\":\"%[2]s\",\"sources\":[...],\"change_reason\":\"...\",\"edits\":[{\"field\":\"body\",\"before\":\"<a unique ending of the body>\",\"after\":\"<that ending plus the new text>\"}]} (MCP: shikii_wiki operation patch, object under patch) after reading the body with shikii wiki query lesson:%[1]s. Resend the whole body, copied exactly, only when a patch does not fit."
	bodyTitleNote = "Without an id, the title found article %s; if it is a different lesson, give it another title."

	bodyUnreadRevision = "<revision shown by shikii wiki query lesson:%s>"
)

// bodyWayOnUnread is bodyWayOn for a refusal that has not read the article.
func bodyWayOnUnread(id string) string {
	return fmt.Sprintf(bodyWayOn, id, fmt.Sprintf(bodyUnreadRevision, id))
}

// bodyRefusalCount is the parenthesis both refusals share: the bytes of the lines that are not in the new body, the bytes of all the lines
// (whose says which: "its" or "that revision's"), and how a line is counted. The measure is per line: a line counts as lost when any part
// of it changed, so a rewording inside a long line drops that whole line.
func bodyRefusalCount(lostBytes, total int, whose string) string {
	return fmt.Sprintf("(%d of %d bytes of %s lines; a line changed anywhere counts as dropped)", lostBytes, total, whose)
}

// bodyRefusalTail is what both refusals send next: the title note when the save named no id (idSent false), then the patch. revision is the
// revision the article has now, which the patch has to be checked against (not the one a refusal measures against).
func bodyRefusalTail(id string, idSent bool, revision string) string {
	way := fmt.Sprintf(bodyWayOn, id, revision)
	if idSent {
		return way
	}
	return fmt.Sprintf(bodyTitleNote, id) + " " + way
}

// bodyLossRefusal refuses a body that would lose half or more of the previous one in the sense above, and says why and what to send instead.
// idSent says whether the save named the article by its id; without one the article was found by the title. revision is the article's
// revision now (the one the patch it asks for names).
func bodyLossRefusal(id string, idSent bool, revision, previous, next string) error {
	refused, lost, lostBytes, total := bodyKeptMeasure(previous, next)
	if !refused {
		return nil
	}
	return fmt.Errorf("refused: this save would replace the body of article %s and drop %d%% of it %s. %s", id, lost, bodyRefusalCount(lostBytes, total, "its"), bodyRefusalTail(id, idSent, revision))
}

// bodyBasisRefusal refuses a body that drops half or more of the lines of the revision the agents' saves started from, though it keeps more
// than half of the body before it. It says which revision it measures against, because the agent does not see the saves in between. idSent
// is as in bodyLossRefusal, and revision is the article's revision now (the one the patch names), not the revision measured against.
func bodyBasisRefusal(id string, idSent bool, revision, next string, basis Page) error {
	refused, lost, lostBytes, total := bodyKeptMeasure(basis.Body, next)
	if !refused {
		return nil
	}
	return fmt.Errorf("refused: with the saves since revision %s, this save would drop %d%% of the body article %s had then %s. %s", basis.Revision, lost, id, bodyRefusalCount(lostBytes, total, "that revision's"), bodyRefusalTail(id, idSent, revision))
}

// guardBody is what Ingest does with the body it is sent for an existing article: it refuses a body that loses half or more of the body before
// it or of the reference revision, and returns the reference the saved page carries (the revision a later save is measured against, empty
// when the new body has every line of the one measured against). The reference revision is read from the page's own history; one that cannot
// be read is not measured, and the body before it stands in for it.
func (b Book) guardBody(id string, idSent bool, old Page, next string) (string, error) {
	if err := bodyLossRefusal(id, idSent, old.Revision, old.Body, next); err != nil {
		return "", err
	}
	basis := old
	if old.KeptFrom != "" {
		if earlier, err := b.readRevision(old.ID + "@" + old.KeptFrom); err == nil {
			basis = earlier
			if err := bodyBasisRefusal(id, idSent, old.Revision, next, earlier); err != nil {
				return "", err
			}
		}
	}
	if kept, total := bodyKeptBytes(basis.Body, next); kept < total {
		return basis.Revision, nil
	}
	return "", nil
}
