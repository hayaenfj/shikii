package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Checks that stop the turn, and what was observed in the turn (0.7.3).
// Changed files are the difference between the listing taken at the request (sessions/<id>.files) and the current listing (same as hookStop). Added lines are
// lines found only in the current content when comparing, line by line, the content at the request with the current content. The content at the request is the listing's blob if it is in the repo,
// otherwise the HEAD content (the blob of a file that was uncommitted at the request disappears together with the temporary store the listing was taken in, so for those it is the diff from HEAD), and otherwise empty.
// This turn's commits are the log from the HEAD saved at the request (session.Head) to the current HEAD (not checked when HEAD cannot be followed).
// When something cannot be read, it counts as "unknown" and the check passes (it stops only when certain).

func gitRun(ctx context.Context, dir string, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return out.String(), true
}

// gitHead returns HEAD at the time of the request ("" for an empty repo or outside git).
func gitHead(cwd string) string {
	if cwd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookGitTimeout)
	defer cancel()
	out, ok := gitRun(ctx, cwd, "rev-parse", "HEAD")
	if !ok {
		return ""
	}
	return strings.TrimSpace(out)
}

// addedLines returns lines that are in new but not in old (multiset difference).
func addedLines(old, new []byte) []string {
	count := map[string]int{}
	for _, l := range strings.Split(string(old), "\n") {
		count[l]++
	}
	var out []string
	for _, l := range strings.Split(string(new), "\n") {
		if count[l] > 0 {
			count[l]--
			continue
		}
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func isBinary(b []byte) bool { return bytes.IndexByte(b, 0) >= 0 }

// guardTurnInput builds what was observed in the turn. before is the listing at the request (its first line is the request marker); changed is the changed files that hookStop found.
func guardTurnInput(cwd, before string, changed []string, known bool, head string, since time.Time) guardInput {
	in := guardInput{Changed: changed, Known: known, Added: map[string][]string{}}
	if cwd == "" {
		return in
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*hookGitTimeout)
	defer cancel()
	top, ok := gitRun(ctx, cwd, "rev-parse", "--show-toplevel")
	if !ok {
		return in
	}
	top = strings.TrimSpace(top)
	oldSHA := parseListing(before)
	for i, p := range changed {
		if i >= guardFilesMax {
			break
		}
		now, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(p)))
		if err != nil || len(now) > guardFileBytesMax || isBinary(now) {
			continue
		}
		var old []byte
		if sha := oldSHA[p]; sha != "" {
			if s, ok := gitRun(ctx, top, "cat-file", "-p", sha); ok {
				old = []byte(s)
			}
		}
		if old == nil {
			if s, ok := gitRun(ctx, top, "show", "HEAD:"+p); ok {
				old = []byte(s)
			}
		}
		if lines := addedLines(old, now); len(lines) > 0 {
			in.Added[p] = lines
		}
	}
	if head != "" {
		// If the request's HEAD is not an ancestor of the current HEAD (switched to another line of history), head..HEAD is someone else's or older commits, so do not check them.
		// Even when it is an ancestor, do not check commits made before the request (existing commits brought in by switching branches)
		if _, ancestor := gitRun(ctx, top, "merge-base", "--is-ancestor", head, "HEAD"); !ancestor {
			return in
		}
		if out, ok := gitRun(ctx, top, "log", "--no-merges", "--format=%ct%x1f%B%x1e", head+"..HEAD"); ok { // ignore the automatic text of merges
			in.CommitsKnown = true
			for _, rec := range strings.Split(out, "\x1e") {
				ts, body, found := strings.Cut(strings.TrimSpace(rec), "\x1f")
				if !found {
					continue
				}
				// a commit with no subject and no body (--allow-empty-message) goes to the check as a commit with no body (empty string)
				if sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64); err == nil && !since.IsZero() && sec < since.Unix()-1 {
					continue
				}
				in.Commits = append(in.Commits, strings.TrimSpace(body))
			}
		}
	}
	return in
}

// guardWorkingTreeInput is for `shikii guard try`: the diff from HEAD (modified and untracked) counts as changed files, and added lines are compared with HEAD. It does not look at commits.
func guardWorkingTreeInput(dir string) guardInput {
	in := guardInput{Added: map[string][]string{}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*hookGitTimeout)
	defer cancel()
	top, ok := gitRun(ctx, dir, "rev-parse", "--show-toplevel")
	if !ok {
		return in
	}
	top = strings.TrimSpace(top)
	status, ok := gitRun(ctx, top, "status", "--porcelain", "--untracked-files=all", "--no-renames")
	if !ok {
		return in
	}
	in.Known = true
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if p == "" || strings.HasPrefix(p, ".shikii/") {
			continue
		}
		in.Changed = append(in.Changed, p)
	}
	for i, p := range in.Changed {
		if i >= guardFilesMax {
			break
		}
		now, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(p)))
		if err != nil || len(now) > guardFileBytesMax || isBinary(now) {
			continue
		}
		var old []byte
		if s, ok := gitRun(ctx, top, "show", "HEAD:"+p); ok {
			old = []byte(s)
		}
		if lines := addedLines(old, now); len(lines) > 0 {
			in.Added[p] = lines
		}
	}
	return in
}

// guardStop runs the enabled checks at Stop and, if one fails, returns output that blocks (decision: block). Results are kept in records/guards.jsonl.
func guardStop(root string, in *hookInput, snapshot *hookSession, before string, changed []string, known bool, recheck bool) (hookOut, error) {
	guards, err := enabledGuards(root)
	if err != nil || len(guards) == 0 {
		return hookOut{}, err
	}
	input := guardTurnInput(in.CWD, before, changed, known, snapshot.Head, snapshot.HeadAt)
	var recs []guardRecord
	var failed []guardResult
	now := time.Now().UTC()
	loc := hookLoc{Lang: hookLang()}
	for _, g := range guards {
		r := evalGuard(g, input)
		r.Reason = bi(r.Reason, r.ReasonJA, loc) // the record's reason is what the page shows as the last stop: in the person's language too
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: r.ID, Pass: r.Pass, Reason: r.Reason, Recheck: recheck})
		if !r.Pass {
			failed = append(failed, r)
		}
	}
	if err := appendGuardRecords(root, recs); err != nil {
		return hookOut{}, err
	}
	if len(failed) == 0 || recheck {
		return hookOut{}, nil // do not block again on the Stop after a block (avoids an endless loop). Whether it was fixed stays in the records
	}
	var b strings.Builder
	b.WriteString("shikii: a check that stops the turn failed. Fix it before finishing (these checks were set by a person).\n")
	for _, f := range failed {
		b.WriteString("- " + f.ID + ": " + f.Reason + "\n")
	}
	b.WriteString("Removing a check itself is the person's decision: shikii guard disable <id>")
	ids := make([]string, 0, len(failed))
	for _, f := range failed {
		ids = append(ids, f.ID)
	}
	return hookOut{Block: true, Reason: b.String(), System: bi(
		"shikii: the check that stops the turn ("+strings.Join(ids, ", ")+") failed, so the agent was told to fix it before finishing",
		"shikii: 止める検査("+strings.Join(ids, ", ")+")に引っかかったので、直してから終えるよう伝えました", loc)}, nil
}

// guardRecheck records, on a Stop with stop_hook_active (the continuation after a block), whether the problem was fixed. It does not block.
func guardRecheck(root string, in *hookInput) {
	snapshot := loadSession(root, in.SessionID)
	if snapshot.Request == "" {
		return
	}
	_, listing, listed, _, _ := treeStateWithScope(in.CWD)
	var changed []string
	known := false
	var before []byte
	if listed {
		var err error
		if before, err = os.ReadFile(sessionFilesPath(root, in.SessionID)); err == nil && strings.HasPrefix(string(before), listingHeader(snapshot.Request)) {
			changed, known = changedFiles(string(before), listing), true
		}
	}
	if _, err := guardStop(root, in, snapshot, string(before), changed, known, true); err != nil {
		hookLog("guard: " + err.Error())
	}
	if _, err := ritualStop(root, snapshot.Scope, in, snapshot, changed, known, true); err != nil {
		hookLog("ritual: " + err.Error())
	}
}
