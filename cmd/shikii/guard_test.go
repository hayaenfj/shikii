package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 止める検査(0.7.3)の 3 種類は、それぞれの負例で止まり、正例で通る。分からない観測(一覧やコミットが取れない)は通す。
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
			t.Fatalf("%s は負例(%s)で止まる: %+v", g.ID, what, r)
		}
		if r := evalGuard(g, guardPositive(g)); !r.Pass {
			t.Fatalf("%s は正例で通る: %+v", g.ID, r)
		}
		if r := evalGuard(g, guardInput{}); !r.Pass {
			t.Fatalf("%s は観測が無ければ通す: %+v", g.ID, r)
		}
	}
	// paired-files: どちらも変われば通る、片方だけなら止まる(逆向きも)、一覧が取れなければ通す
	if r := evalGuard(pair, guardInput{Changed: []string{"CHANGELOG_ja.md"}, Known: true}); r.Pass || !strings.Contains(r.Reason, "CHANGELOG.md が変わっていない") {
		t.Fatalf("逆向きも止める: %+v", r)
	}
	if r := evalGuard(pair, guardInput{Changed: []string{"CHANGELOG.md"}, Known: false}); !r.Pass {
		t.Fatalf("一覧が取れなければ通す: %+v", r)
	}
	// forbidden-words: except のパスは見ない、該当の行と語を理由に出す
	in := guardInput{Known: true, Changed: []string{"docs/operations.md", "wiki/x.md", "README.md"}, Added: map[string][]string{"docs/operations.md": {"根"}, "wiki/x.md": {"指紋"}, "README.md": {"ここは問題ない"}}}
	if r := evalGuard(words, in); !r.Pass {
		t.Fatalf("except のパスは見ない: %+v", r)
	}
	in.Added["README.md"] = append(in.Added["README.md"], "プロジェクトの根に置く")
	if r := evalGuard(words, in); r.Pass || !strings.Contains(r.Reason, "README.md: 「根」") {
		t.Fatalf("足した行の語で止める: %+v", r)
	}
	// commit-body: 本文に無い語を挙げる。件名だけのコミットも止める。コミットが分からなければ通す
	if r := evalGuard(body, guardInput{CommitsKnown: true, Commits: []string{"fix: x\n\n元の問題: y"}}); r.Pass || !strings.Contains(r.Reason, "「検証」") || strings.Contains(r.Reason, "「元の問題」") {
		t.Fatalf("無い語だけを挙げる: %+v", r)
	}
	if r := evalGuard(body, guardInput{CommitsKnown: false, Commits: []string{"x"}}); !r.Pass {
		t.Fatalf("コミットが分からなければ通す: %+v", r)
	}
	// validate: 種類と欄
	for _, bad := range []guard{{ID: "A", Kind: guardKindPairedFiles, Title: "t", Pairs: [][]string{{"a", "b"}}}, {ID: "x", Kind: "other", Title: "t"}, {ID: "x", Kind: guardKindForbiddenWords, Title: "t"}, {ID: "x", Kind: guardKindPairedFiles, Title: "t", Pairs: [][]string{{"a", "a"}}}, {ID: "x", Kind: guardKindCommitBody, Title: "t", Required: []string{" "}}} {
		if err := bad.validate(); err == nil {
			t.Fatalf("不正な guard は拒む: %+v", bad)
		}
	}
	// 足した行: 多重集合の差。空行は数えない
	if got := addedLines([]byte("a\nb\n"), []byte("a\nb\nc\n\nb\n")); len(got) != 2 || got[0] != "c" || got[1] != "b" {
		t.Fatalf("足した行: %v", got)
	}
}

// add は草稿(無効)を置き、enable は負例で止まる(RED)・正例で通る(GREEN)のを見せてから有効にする。disable は無効に戻す。
func TestGuardEnableShowsRedThenGreen(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if _, err := os.Stat(filepath.Join(root, "guards")); err != nil {
		t.Fatal("init は guards/ も用意する: ", err)
	}
	old := stdin
	t.Cleanup(func() { stdin = old })
	stdin = strings.NewReader(`{"id":"changelog-pair","kind":"paired-files","title":"変更履歴は英語と日本語を揃える","reason":"本人の訂正","pairs":[["CHANGELOG.md","CHANGELOG_ja.md"]],"enabled":true}`)
	out := capture(t, func() error { return cmdGuard([]string{"add"}) })
	g, err := loadGuard(guardPath(root, "changelog-pair"))
	if err != nil || g.Enabled || g.Red != nil || !strings.Contains(out, "草稿") {
		t.Fatalf("add は enabled を無視して無効の草稿を置く: %+v %v\n%s", g, err, out)
	}
	stdin = strings.NewReader(`{"id":"changelog-pair","kind":"paired-files","title":"x","pairs":[["a","b"]]}`)
	if err := cmdGuard([]string{"add"}); err == nil {
		t.Fatal("同じ id の草稿は上書きしない")
	}
	out = capture(t, func() error { return cmdGuard([]string{"enable", "changelog-pair"}) })
	if !strings.Contains(out, "RED:") || !strings.Contains(out, "GREEN:") || !strings.Contains(out, "有効にした") {
		t.Fatalf("enable は RED → GREEN を見せる:\n%s", out)
	}
	if g, _ = loadGuard(guardPath(root, "changelog-pair")); !g.Enabled || g.Red == nil || g.Red.Negative == "" {
		t.Fatalf("enable は負例の記録を残して有効にする: %+v", g)
	}
	out = capture(t, func() error { return cmdGuard([]string{"list"}) })
	if !strings.Contains(out, "changelog-pair") || !strings.Contains(out, "有効") {
		t.Fatalf("list に状態が出る:\n%s", out)
	}
	capture(t, func() error { return cmdGuard([]string{"disable", "changelog-pair"}) })
	if g, _ = loadGuard(guardPath(root, "changelog-pair")); g.Enabled {
		t.Fatal("disable で無効に戻る")
	}
	// try: 作業ツリーに CHANGELOG.md だけ足すと止まる(有効な時)
	capture(t, func() error { return cmdGuard([]string{"enable", "changelog-pair"}) })
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdGuard([]string{"try"}) })
	if !strings.Contains(out, "STOP changelog-pair") {
		t.Fatalf("try は作業ツリーに当てて見せる:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG_ja.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = capture(t, func() error { return cmdGuard([]string{"try"}) }); !strings.Contains(out, "ok   changelog-pair") {
		t.Fatalf("両方あれば通る:\n%s", out)
	}
}

// 監査 R2: commit-body は merge コミットを見ず、件名も本文も無いコミットは止める。
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
		t.Fatalf("merge コミットの自動の文で止めない: %+v %+v", r, in.Commits)
	}
	run("commit", "-q", "--allow-empty", "--allow-empty-message", "-m", "")
	in = guardTurnInput(repo, "", nil, false, head, since)
	if r := evalGuard(g, in); r.Pass {
		t.Fatalf("件名も本文も無いコミットは止める: %+v", in.Commits)
	}
}

// 禁止語が普通の語の一部(例: 「根」と「根拠」)の時、allow に書いた語は先に取り除いてから見る。
func TestForbiddenWordsHonourAllowedWords(t *testing.T) {
	g := guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}, Allow: []string{"根拠", "根本"}}
	if err := g.validate(); err != nil {
		t.Fatal(err)
	}
	in := guardInput{Known: true, Changed: []string{"a.md"}, Added: map[string][]string{"a.md": {"根拠にしない。根本から直す。"}}}
	if r := evalGuard(g, in); !r.Pass {
		t.Fatalf("許す語の中の禁止語は数えない: %+v", r)
	}
	in.Added["a.md"] = append(in.Added["a.md"], "プロジェクトの根に置く(根拠はある)")
	if r := evalGuard(g, in); r.Pass {
		t.Fatalf("許す語を除いても残れば止める: %+v", r)
	}
	bad := g
	bad.Allow = []string{"拠点"} // 禁止語を含まない語は allow に置けない(書き間違い)
	if err := bad.validate(); err == nil {
		t.Fatal("禁止語を含まない allow は拒む")
	}
}

// 監査 R3: 2. forbidden-words の allow は、禁止語に助詞・空白・句読点を足しただけの語や、別の禁止語そのものを置けない。
func TestForbiddenWordsAllowCannotBeALoophole(t *testing.T) {
	for _, a := range []string{"根を", "根の", "根 ", "「根」", "根。", "根拠"} {
		g := guard{ID: "x", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根", "根拠"}, Allow: []string{a}}
		if err := g.validate(); err == nil {
			t.Fatalf("allow %q は抜け道になるので拒む", a)
		}
	}
	g := guard{ID: "x", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}, Allow: []string{"根拠", "根本"}}
	if err := g.validate(); err != nil {
		t.Fatalf("普通の語は置ける: %v", err)
	}
}
