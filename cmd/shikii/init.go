package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/handoff"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	shikii "github.com/hayaenfj/shikii"
)

// cmdInit は 1 回でエージェントの準備を済ませる(オプションは無い。基本構成と固定の規則で決める):
//  1. プロジェクトルート(実行する場所)に .shikii/(その下に decisions/ wiki/ records/ labels/。他には何も増やさない)
//  2. .mcp.json に shikii の MCP サーバーの項(Claude Code が読む)。既に shikii の項があれば触らない(人が --proposer などを足していても残る)。他の項と鍵も残す
//  3. skill(作法)を ~/.claude/skills/shikii に置き直す(毎回。手で直す場所ではない。直すなら repo の skills/shikii)
//
// 2 回目以降は同じ結果で、人が書いたものを壊さない。
func cmdInit(args []string) error {
	if len(args) > 0 {
		return errors.New("使い方: shikii init または shikii --init（追加の引数は不要です）")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	if isSharedPlaceScope(scope) {
		return errors.New("ホームフォルダは初期化しません(~/.shikii は共通の置き場です)。プロジェクトのフォルダで実行してください")
	}
	version := projectInitVersionNow
	if wipePending(root) { // 置き場を作る前に見る(作った後では、消去の途中の初期化し直しと見分けられない)
		version = projectInitVersion(root)
	}
	if err := checkProjectStore(root, true); err != nil {
		return err
	}
	if err := excludeProjectStore(scope); err != nil {
		return err
	}
	for _, d := range []string{"specs", "runs"} {
		if err := os.MkdirAll(filepath.Join(root, "wiki", d), 0700); err != nil {
			return err
		}
	}
	if err := (handoff.Book{Store: root}).Ensure(); err != nil { // 作業記録の置き場(0.8.1)
		return err
	}
	fmt.Fprintln(stdout, "保存先: .shikii/（decisions/・wiki/・records/・labels/・handoff/）")
	nSpecs, err := seedAgentSpecs(filepath.Join(root, "decisions"))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "既製の判定: %d種類を準備（hookの%d問とwikiの%d問。新規%d件、既存の設定は保持）\n", len(hookSpecs)+len(wikiSpecs), len(hookSpecs), len(wikiSpecs), nSpecs)
	// 既製の知識: 型・パターン・モデルのページを、無いものだけ写す(tune --propose がここを読んで候補を作る。人が直したページは触らない)
	n, err := seedWiki(filepath.Join(root, "wiki"))
	if err != nil {
		return fmt.Errorf("wiki: %w", err)
	}
	if n > 0 {
		fmt.Fprintf(stdout, ".shikii/wiki/: 既製の知識を%d件追加（既存のページは保持）\n", n)
	}

	// MCP の設定: Claude Code(.mcp.json)、Cursor(.cursor/mcp.json)、OpenCode(opencode.json の mcp)、Codex(~/.codex/config.toml)。
	// 既に shikii の項があれば触らない(人が --proposer などを足していても残る)。他の項と鍵も残す。
	mcpArgs := []string{"mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}
	claude := map[string]any{"command": "shikii", "args": mcpArgs}
	for _, f := range []struct {
		path, key string
		entry     map[string]any
		who       string
	}{
		{".mcp.json", "mcpServers", claude, "Claude Code"},
		{filepath.Join(".cursor", "mcp.json"), "mcpServers", claude, "Cursor"},
		{"opencode.json", "mcp", map[string]any{"type": "local", "command": append([]string{"shikii"}, mcpArgs...)}, "OpenCode"},
	} {
		did, err := addJSONEntry(filepath.Join(scope, f.path), f.key, "shikii", f.entry)
		if err != nil {
			return err
		}
		if did {
			fmt.Fprintf(stdout, "%s: shikiiを追加（%s）\n", f.path, f.who)
		} else {
			fmt.Fprintf(stdout, "%s: 既存のshikii設定を保持（%s）\n", f.path, f.who)
		}
	}
	var skipped agentsSkipped
	switch did, err := addAgentsSection(filepath.Join(scope, "AGENTS.md")); {
	case errors.As(err, &skipped):
		fmt.Fprintln(stdout, "AGENTS.md: "+skipped.reason)
	case err != nil:
		return err
	case did:
		fmt.Fprintln(stdout, "AGENTS.md: shikiiの作法を追加（Codex・OpenCode向け）")
	case fileExists(filepath.Join(scope, "CLAUDE.md")) && !fileExists(filepath.Join(scope, "AGENTS.md")):
		fmt.Fprintln(stdout, "AGENTS.md: 作成を省略（既存のCLAUDE.mdを優先）")
	}
	// 家のフォルダに置くもの(Codex の設定、skill)。HOME が無い環境(容れ物の CI など)では飛ばす: プロジェクトの準備は済んでいる
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stdout, "HOMEが無いためCodexの共通設定とskillを省略し、プロジェクト側だけ準備します")
		return completeProjectInit(root, scope, version)
	}
	codex := filepath.Join(home, ".codex", "config.toml")
	switch did, err := addCodexServer(codex, mcpArgs); {
	case err != nil:
		return err
	case did:
		fmt.Fprintf(stdout, "%s: shikiiを追加（Codex）\n", filepath.Join("~", ".codex", "config.toml"))
	default:
		fmt.Fprintf(stdout, "%s: 既存のMCP設定を保持（Codex）\n", filepath.Join("~", ".codex", "config.toml"))
	}

	// skill: 置き直す(古いファイルも消す)
	dst := filepath.Join(home, ".claude", "skills", "shikii")
	if err := copySkill(dst); err != nil {
		return fmt.Errorf("skill: %w", err)
	}
	fmt.Fprintf(stdout, "skill: %sを更新\n", filepath.Join("~", ".claude", "skills", "shikii"))
	return completeProjectInit(root, scope, version)
}

func completeProjectInit(root, scope string, version int) error {
	if err := finishProjectInit(root, scope, version); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "利用可能: %s\n画面: エージェントの最初の依頼で hook が起こし(空いているポート)、ブラウザで開きます。URL は shikii page\n", scope)
	fmt.Fprintln(stdout, "次: 普段どおり、このプロジェクトで依頼してください。新しいMCP設定は次のセッションから反映されます。")
	fmt.Fprintln(stdout, "業務の判定を追加する時は shikii new <id>。モデルの手動起動は shikii up。")
	return nil
}

// seedWiki は埋め込みの既製の知識(shikii.WikiFS)を dst(プロジェクトの wiki/)に写す。既にあるファイルは触らない。写した数を返す。
func seedWiki(dst string) (int, error) {
	n := 0
	err := fs.WalkDir(shikii.WikiFS, "wiki", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "wiki/")
		if p == "wiki" {
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := shikii.WikiFS.ReadFile(p)
		if err != nil {
			return err
		}
		created, err := seedFile(target, b)
		if err != nil {
			return err
		}
		if created {
			n++
		}

		return nil
	})
	return n, err
}

// seedFile は書き終えた内容だけを公開し、既存の通常ファイルは保持する。
func seedFile(path string, data []byte) (bool, error) {
	checkExisting := func() error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("既製のファイルの置き場にリンクは使えません")
		}
		return nil
	}
	if err := checkExisting(); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".seed-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0644); err != nil {
		f.Close()
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Link(f.Name(), path); err != nil {
		if os.IsExist(err) {
			return false, checkExisting()
		}
		return false, err
	}
	return true, nil
}

// addJSONEntry は JSON の設定ファイルの key(object)に name の項を足す。既にあれば触らず false。
// 数字は丸めず(UseNumber)、& < > は \u に変えず、一時ファイル + rename で書く。壊れた JSON は上書きしない。並びは変わる(JSON の object に順序は無い)。
func addJSONEntry(path, key, name string, entry map[string]any) (bool, error) {
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("%s: %w (init does not replace a file it cannot read)", path, err)
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			return false, fmt.Errorf("%s: JSONを確認してください（壊れた設定は上書きしません）: %w", path, err)
		}
	}
	section, _ := cfg[key].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	if _, exists := section[name]; exists {
		return false, nil
	}
	section[name] = entry
	cfg[key] = section
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path+".tmp", buf.Bytes(), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(path+".tmp", path)
}

// addCodexServer は Codex の ~/.codex/config.toml に [mcp_servers.shikii] を足す。TOML は読まないので、mcp_servers という語が
// どこかにあれば(見出し、インラインテーブル、[mcp_servers] の下の鍵、引用した鍵)触らない: 末尾に足すと二重定義で Codex が設定を読めなくなる(監査 6 の 1)。
func addCodexServer(path string, args []string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(b), "mcp_servers") {
		return false, nil
	}
	block := "\n[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(args) + "]\n"
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		block = "\n" + block
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return true, err
}

func quotedList(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	return strings.Join(quoted, ", ")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

const agentsMarker = "<!-- shikii -->"

// agentsSection は AGENTS.md(Codex と OpenCode が読む)の shikii の節。印で囲む。
var agentsSection = agentsMarker + "\n## shikii(判定の作法)\n\n" +
	"- 同じ種類の判断は自分で決めず `shikii decide`(MCP なら shikii_decide)を呼び、帯に従う: act だけ自動、ask は人に確かめる、discard は動かない、abstain は入力を揃える\n" +
	"- 正解は人(または結果)が決める。人の答えは `shikii label <trace_id> <truth>`。線(calibration:)や wiki の主張の状態を手で書き換えない\n" +
	"- 聞き方は spec の variants: に候補として書き、`shikii tune` が測る。上回った候補は、hook に頼まれたら意味が同じかを確かめて(人の設定の言語が英語以外なら)両方をその言語に訳し、`shikii hook variants check` に渡す(同じなら採用され、人は元に戻せる。決めきれなければ人に聞く)\n" +
	"- 新しく作った・変えた決まり・テスト・手順(コマンドやスクリプト)は、頼まれなくてもそのターンのうちに wiki のやり方(howto)かルール(rule)の記事に書く。近い記事があれば足すか直す\n" +
	"- wiki の教訓には種類(kind: rule / howto / criteria / fact / history)と場面(scene: その仕事の言葉で「〜する時」)を付ける。wiki の返事に unclassified(種類と場面の無い前の版の記事)があれば、そのターンのうちに `shikii wiki classify <id> <kind> <scene>`(MCP なら shikii_wiki の classify)で付ける。教訓の文は変わらない\n" +
	"- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)\n" + agentsMarker + "\n"

// agentsSkipped は、AGENTS.md に触らなかった理由(人に伝えるだけで、init もアップデートも止めない)。
type agentsSkipped struct{ reason string }

func (e agentsSkipped) Error() string { return e.reason }

// addAgentsSection は AGENTS.md に shikii の節を足す。前の版の節があれば新しい中身に置き換え、人が書いた部分は触らない。
// 同じ中身なら何もしない(監査 0.9.0 の A1〜A3):
//   - 節は「印だけの行」で始まり、次の行が「## shikii(」で、次の「印だけの行」で終わる組。人の文の中の印(`…` の中など)は数えない。
//     その組がちょうど 1 つの時だけ置き換え、印だけの行があるのに組が 1 つでない時は触らずに理由を返す
//   - AGENTS.md が repo の中のファイルへのリンク(CLAUDE.md へのシンボリックリンクなど)ならリンクのまま先を書き換え、外を指すなら触らない
//   - 改行は今のファイルの形(CRLF か LF)に合わせる
func addAgentsSection(path string) (bool, error) {
	target := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false, agentsSkipped{"AGENTS.md のリンク先を読めないので触りません"}
		}
		dir, _ := filepath.EvalSymlinks(filepath.Dir(path))
		if rel, err := filepath.Rel(dir, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false, agentsSkipped{"AGENTS.md は repo の外を指すリンクなので触りません"}
		}
		target = real
	}
	b, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	text := string(b)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	section := strings.ReplaceAll(agentsSection, "\n", nl)
	if start, end, found, ok := agentsSectionSpan(text); found {
		if !ok {
			return false, agentsSkipped{"AGENTS.md の shikii の印の組が 1 つに決まらないので触りません(印だけの行を確かめてください)"}
		}
		if text[start:end] == section {
			return false, nil
		}
		return true, replaceFile(target, text[:start]+section+text[end:])
	}
	if os.IsNotExist(err) && fileExists(filepath.Join(filepath.Dir(path), "CLAUDE.md")) {
		return false, nil // OpenCode は AGENTS.md があると CLAUDE.md を読まなくなる。黙って作らない(呼び出し側が言う)
	}
	if len(b) > 0 {
		if !strings.HasSuffix(text, "\n") {
			section = nl + section
		}
		section = nl + section
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.WriteString(section)
	return true, err
}

// agentsSectionSpan は shikii の節の範囲(開きの印の行の頭から、閉じの印の行の改行の後まで)。found は印だけの行があったか、
// ok は開き(次の行が「## shikii(」)と閉じの組がちょうど 1 つだったか。
func agentsSectionSpan(text string) (start, end int, found, ok bool) {
	type line struct{ start, end int } // end は改行を含む
	var marks []line
	var lines []line
	for pos := 0; pos < len(text); {
		e := strings.IndexByte(text[pos:], '\n')
		next := len(text)
		if e >= 0 {
			next = pos + e + 1
		}
		lines = append(lines, line{pos, next})
		pos = next
	}
	body := func(l line) string { return strings.TrimRight(text[l.start:l.end], "\r\n") }
	pairs := 0
	for i, l := range lines {
		if body(l) != agentsMarker {
			continue
		}
		marks = append(marks, l)
		if i+1 >= len(lines) || !strings.HasPrefix(body(lines[i+1]), "## shikii(") {
			continue
		}
		for _, c := range lines[i+1:] {
			if body(c) == agentsMarker {
				pairs++
				start, end = l.start, c.end
				break
			}
		}
	}
	if len(marks) == 0 {
		return 0, 0, false, false
	}
	return start, end, true, pairs == 1 && len(marks) == 2
}

// replaceFile は path の中身を一時ファイルからの置き換えで書き換える(元の権限のまま。一時ファイルは毎回ちがう名前)。
func replaceFile(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm() // repo のファイルなので元の権限のまま
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shikii-agents-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// copySkill は埋め込んだ skills/shikii を dst に置き直す(前の中身は消す。古い参照が混ざらない)。
func copySkill(dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return fs.WalkDir(shikii.SkillFS, "skills/shikii", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("skills/shikii", p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := shikii.SkillFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
