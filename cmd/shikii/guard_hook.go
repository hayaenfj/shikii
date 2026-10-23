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

// 止める検査とターンの観測(0.7.3)。
// 変わったファイルは依頼の時の一覧(sessions/<id>.files)と今の一覧の差(hookStop と同じ)。足した行は、依頼の時の中身と今の中身を
// 行で比べて、今にしか無い行。依頼の時の中身は一覧の blob が repo にあればそれ、無ければ HEAD の中身(依頼の時に未コミットだった
// ファイルの blob は一覧を取った仮の置き場と共に消えるので、その分は HEAD との差になる)、それも無ければ空。
// このターンのコミットは、依頼の時に控えた HEAD(session.Head)から今の HEAD までの log(HEAD が辿れない時は見ない)。
// 取れない時は「分からない」として検査を通す(止めるのは確かな時だけ)。

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

// gitHead は依頼の時の HEAD(空の repo や git の外なら "")。
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

// addedLines は old に無く new にある行(多重集合の差)。
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

// guardTurnInput はターンの観測を組む。before は依頼の時の一覧(先頭行は依頼の印)、changed は hookStop が出した変わったファイル。
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
		// 依頼の HEAD が今の HEAD の祖先でなければ(別の系統に切り替えた)、head..HEAD は他人や前からのコミットなので見ない。
		// 祖先でも、依頼より前にコミットされたもの(枝を切り替えて入った既存のコミット)は見ない
		if _, ancestor := gitRun(ctx, top, "merge-base", "--is-ancestor", head, "HEAD"); !ancestor {
			return in
		}
		if out, ok := gitRun(ctx, top, "log", "--format=%ct%x1f%B%x1e", head+"..HEAD"); ok {
			in.CommitsKnown = true
			for _, rec := range strings.Split(out, "\x1e") {
				ts, body, found := strings.Cut(strings.TrimSpace(rec), "\x1f")
				if !found || strings.TrimSpace(body) == "" {
					continue
				}
				if sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64); err == nil && !since.IsZero() && sec < since.Unix()-1 {
					continue
				}
				in.Commits = append(in.Commits, strings.TrimSpace(body))
			}
		}
	}
	return in
}

// guardWorkingTreeInput は `shikii guard try` 用: HEAD との差(変更・未追跡)を変わったファイルとし、足した行は HEAD と比べる。コミットは見ない。
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

// guardStop は Stop で有効な検査を当て、引っかかれば止める出力(decision: block)を返す。結果は records/guards.jsonl に残す。
func guardStop(root string, in *hookInput, snapshot *hookSession, before string, changed []string, known bool, recheck bool) (hookOut, error) {
	guards, err := enabledGuards(root)
	if err != nil || len(guards) == 0 {
		return hookOut{}, err
	}
	input := guardTurnInput(in.CWD, before, changed, known, snapshot.Head, snapshot.HeadAt)
	var recs []guardRecord
	var failed []guardResult
	now := time.Now().UTC()
	for _, g := range guards {
		r := evalGuard(g, input)
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: r.ID, Pass: r.Pass, Reason: r.Reason, Recheck: recheck})
		if !r.Pass {
			failed = append(failed, r)
		}
	}
	if err := appendGuardRecords(root, recs); err != nil {
		return hookOut{}, err
	}
	if len(failed) == 0 || recheck {
		return hookOut{}, nil // 止めた後の Stop ではもう止めない(無限ループを避ける)。直ったかは記録に残る
	}
	var b strings.Builder
	b.WriteString("shikii: 止める検査に引っかかりました。直してから終えてください(人が決めた検査です)。\n")
	for _, f := range failed {
		b.WriteString("- " + f.ID + ": " + f.Reason + "\n")
	}
	b.WriteString("検査そのものを外すのは人の判断です: shikii guard disable <id>")
	ids := make([]string, 0, len(failed))
	for _, f := range failed {
		ids = append(ids, f.ID)
	}
	return hookOut{Block: true, Reason: b.String(), System: "shikii: 止める検査(" + strings.Join(ids, ", ") + ")に引っかかったので、直してから終えるよう伝えました"}, nil
}

// guardRecheck は stop_hook_active の Stop(止めた後の続き)で、直ったかを記録する。止めない。
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
}
