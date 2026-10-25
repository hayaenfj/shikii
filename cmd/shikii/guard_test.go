package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each of the 3 kinds of checks that stop the turn (0.7.3) stops on its negative example and passes its positive example. Unknown observations (the file list or commits cannot be read) pass.
func TestGuardKindsStopOnNegativesAndPassPositives(t *testing.T) {
	pair := guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}}
	words := guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "根・指紋を使わない", Words: []string{"根", "指紋"}, Except: []string{"docs/operations.md", "wiki/**"}}
	body := guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "コミット本文に背景と検証", Required: []string{"元の問題", "検証"}}
	for _, g := range []guard{pair, words, body} {
		if err := g.validate(); err != nil {
			t.Fatalf("%s: %v", g.ID, err)
		}
		neg, what := guardNegative(g)
		if r := evalGuard(g, neg); r.Pass || r.Reason == "" || what == "" {
			t.Fatalf("%s stops on the negative example (%s): %+v", g.ID, what, r)
		}
		if r := evalGuard(g, guardPositive(g)); !r.Pass {
			t.Fatalf("%s passes the positive example: %+v", g.ID, r)
		}
		if r := evalGuard(g, guardInput{}); !r.Pass {
			t.Fatalf("%s passes when nothing was observed: %+v", g.ID, r)
		}
	}
	// paired-files: passes when both change, stops when only one changes (either way round), passes when the file list cannot be read
	if r := evalGuard(pair, guardInput{Changed: []string{"CHANGELOG_ja.md"}, Known: true}); r.Pass || !strings.Contains(r.Reason, "CHANGELOG.md did not") {
		t.Fatalf("stops the other way round too: %+v", r)
	}
	if r := evalGuard(pair, guardInput{Changed: []string{"CHANGELOG.md"}, Known: false}); !r.Pass {
		t.Fatalf("passes when the file list cannot be read: %+v", r)
	}
	// forbidden-words: paths in except are not checked; the matching line and word are given as the reason
	in := guardInput{Known: true, Changed: []string{"docs/operations.md", "wiki/x.md", "README.md"}, Added: map[string][]string{"docs/operations.md": {"根"}, "wiki/x.md": {"指紋"}, "README.md": {"ここは問題ない"}}}
	if r := evalGuard(words, in); !r.Pass {
		t.Fatalf("paths in except are not checked: %+v", r)
	}
	in.Added["README.md"] = append(in.Added["README.md"], "プロジェクトの根に置く")
	if r := evalGuard(words, in); r.Pass || !strings.Contains(r.Reason, `README.md: "根"`) {
		t.Fatalf("stops on a word in an added line: %+v", r)
	}
	// commit-body: lists the words missing from the body. A subject-only commit stops too. Passes when the commits are unknown
	if r := evalGuard(body, guardInput{CommitsKnown: true, Commits: []string{"fix: x\n\n元の問題: y"}}); r.Pass || !strings.Contains(r.Reason, `"検証"`) || strings.Contains(r.Reason, `"元の問題"`) {
		t.Fatalf("lists only the missing words: %+v", r)
	}
	if r := evalGuard(body, guardInput{CommitsKnown: false, Commits: []string{"x"}}); !r.Pass {
		t.Fatalf("passes when the commits are unknown: %+v", r)
	}
	// validate: kind and fields
	for _, bad := range []guard{{ID: "A", Kind: guardKindPairedFiles, Title: "t", Pairs: [][]string{{"a", "b"}}}, {ID: "x", Kind: "other", Title: "t"}, {ID: "x", Kind: guardKindForbiddenWords, Title: "t"}, {ID: "x", Kind: guardKindPairedFiles, Title: "t", Pairs: [][]string{{"a", "a"}}}, {ID: "x", Kind: guardKindCommitBody, Title: "t", Required: []string{" "}}} {
		if err := bad.validate(); err == nil {
			t.Fatalf("an invalid guard is refused: %+v", bad)
		}
	}
	// added lines: multiset difference. Empty lines are not counted
	if got := addedLines([]byte("a\nb\n"), []byte("a\nb\nc\n\nb\n")); len(got) != 2 || got[0] != "c" || got[1] != "b" {
		t.Fatalf("added lines: %v", got)
	}
}

// add writes a draft (disabled); enable shows the negative example stop (RED) and the positive example pass (GREEN) before enabling. disable turns it back off.
func TestGuardEnableShowsRedThenGreen(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if _, err := os.Stat(filepath.Join(root, "guards")); err != nil {
		t.Fatal("init also prepares guards/: ", err)
	}
	old := stdin
	t.Cleanup(func() { stdin = old })
	stdin = strings.NewReader(`{"id":"changelog-pair","kind":"paired-files","title":"変更履歴は英語と日本語を揃える","reason":"本人の訂正","pairs":[["CHANGELOG.md","CHANGELOG_ja.md"]],"enabled":true}`)
	out := capture(t, func() error { return cmdGuard([]string{"add"}) })
	g, err := loadGuard(guardPath(root, "changelog-pair"))
	if err != nil || g.Enabled || g.Red != nil || !strings.Contains(out, "draft") {
		t.Fatalf("add ignores enabled and writes a disabled draft: %+v %v\n%s", g, err, out)
	}
	stdin = strings.NewReader(`{"id":"changelog-pair","kind":"paired-files","title":"x","pairs":[["a","b"]]}`)
	if err := cmdGuard([]string{"add"}); err == nil {
		t.Fatal("a draft with the same id is not overwritten")
	}
	out = capture(t, func() error { return cmdGuard([]string{"enable", "changelog-pair"}) })
	if !strings.Contains(out, "RED:") || !strings.Contains(out, "GREEN:") || !strings.Contains(out, ": enabled.") {
		t.Fatalf("enable shows RED → GREEN:\n%s", out)
	}
	if g, _ = loadGuard(guardPath(root, "changelog-pair")); !g.Enabled || g.Red == nil || g.Red.Negative == "" {
		t.Fatalf("enable keeps a record of the negative example and enables: %+v", g)
	}
	out = capture(t, func() error { return cmdGuard([]string{"list"}) })
	if !strings.Contains(out, "changelog-pair") || !strings.Contains(out, "enabled") {
		t.Fatalf("list shows the state:\n%s", out)
	}
	capture(t, func() error { return cmdGuard([]string{"disable", "changelog-pair"}) })
	if g, _ = loadGuard(guardPath(root, "changelog-pair")); g.Enabled {
		t.Fatal("disable turns it back off")
	}
	// try: adding only CHANGELOG.md to the working tree stops (while enabled)
	capture(t, func() error { return cmdGuard([]string{"enable", "changelog-pair"}) })
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdGuard([]string{"try"}) })
	if !strings.Contains(out, "STOP changelog-pair") {
		t.Fatalf("try runs against the working tree and shows the result:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG_ja.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = capture(t, func() error { return cmdGuard([]string{"try"}) }); !strings.Contains(out, "ok   changelog-pair") {
		t.Fatalf("passes when both are there:\n%s", out)
	}
}

// audit R2: commit-body ignores merge commits, and stops a commit with no subject and no body.
func TestCommitBodySkipsMergesAndStopsEmptyMessages(t *testing.T) {
	repo := gitRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	gitCommitAll(t, repo, "base\n\n元の問題: x\n変更後: y\n検証: z")
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	head, since := gitHead(repo), time.Now()
	run("checkout", "-q", "-b", "feature")
	write("b.txt", "b")
	gitCommitAll(t, repo, "feature work\n\n元の問題: a\n変更後: b\n検証: c")
	run("checkout", "-q", "-")
	write("c.txt", "c")
	gitCommitAll(t, repo, "main work\n\n元の問題: a\n変更後: b\n検証: c")
	run("merge", "--no-ff", "-q", "-m", "Merge branch 'feature'", "feature")
	in := guardTurnInput(repo, "", nil, false, head, since)
	g := guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "x", Required: []string{"元の問題", "変更後", "検証"}}
	if r := evalGuard(g, in); !r.Pass {
		t.Fatalf("does not stop on the automatic text of a merge commit: %+v %+v", r, in.Commits)
	}
	run("commit", "-q", "--allow-empty", "--allow-empty-message", "-m", "")
	in = guardTurnInput(repo, "", nil, false, head, since)
	if r := evalGuard(g, in); r.Pass {
		t.Fatalf("stops a commit with no subject and no body: %+v", in.Commits)
	}
}

// When a forbidden word is part of an ordinary word (e.g. 「根」 in 「根拠」), the words in allow are removed before checking.
func TestForbiddenWordsHonourAllowedWords(t *testing.T) {
	g := guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}, Allow: []string{"根拠", "根本"}}
	if err := g.validate(); err != nil {
		t.Fatal(err)
	}
	in := guardInput{Known: true, Changed: []string{"a.md"}, Added: map[string][]string{"a.md": {"根拠にしない。根本から直す。"}}}
	if r := evalGuard(g, in); !r.Pass {
		t.Fatalf("a forbidden word inside an allowed word is not counted: %+v", r)
	}
	in.Added["a.md"] = append(in.Added["a.md"], "プロジェクトの根に置く(根拠はある)")
	if r := evalGuard(g, in); r.Pass {
		t.Fatalf("stops if one remains after removing the allowed words: %+v", r)
	}
	bad := g
	bad.Allow = []string{"拠点"} // a word that does not contain a forbidden word cannot be in allow (a typo)
	if err := bad.validate(); err == nil {
		t.Fatal("an allow word without a forbidden word is refused")
	}
}

// audit R3: 2. forbidden-words' allow cannot hold a forbidden word with only a particle, space or punctuation added, nor another forbidden word itself.
func TestForbiddenWordsAllowCannotBeALoophole(t *testing.T) {
	for _, a := range []string{"根を", "根の", "根 ", "「根」", "根。", "根拠"} {
		g := guard{ID: "x", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根", "根拠"}, Allow: []string{a}}
		if err := g.validate(); err == nil {
			t.Fatalf("allow %q would be a loophole, so it is refused", a)
		}
	}
	g := guard{ID: "x", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}, Allow: []string{"根拠", "根本"}}
	if err := g.validate(); err != nil {
		t.Fatalf("ordinary words are allowed: %v", err)
	}
}
