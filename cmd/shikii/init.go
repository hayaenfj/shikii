package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
		return errors.New("usage: shikii init  (no flags: it prepares the current folder and the agent)")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
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
	fmt.Fprintln(stdout, "folders: .shikii/decisions/ (specs) .shikii/wiki/ (knowledge) .shikii/records/ and .shikii/labels/ (records and truths). Nothing else is added to the project root")
	// 既製の知識: 型・パターン・モデルのページを、無いものだけ写す(tune --propose がここを読んで候補を作る。人が直したページは触らない)
	n, err := seedWiki(filepath.Join(root, "wiki"))
	if err != nil {
		return fmt.Errorf("wiki: %w", err)
	}
	if n > 0 {
		fmt.Fprintf(stdout, ".shikii/wiki/: %d pages of shared knowledge added (index.md, types/, patterns/, models/; pages you already have are never overwritten)\n", n)
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
			fmt.Fprintf(stdout, "%s: shikii added (%s)\n", f.path, f.who)
		} else {
			fmt.Fprintf(stdout, "%s: shikii entry kept as it is (%s; edit it by hand, init never rewrites it)\n", f.path, f.who)
		}
	}
	switch did, err := addAgentsSection(filepath.Join(scope, "AGENTS.md")); {
	case err != nil:
		return err
	case did:
		fmt.Fprintln(stdout, "AGENTS.md: shikii section added (Codex and OpenCode read it; the full rules are in the skill)")
	case fileExists(filepath.Join(scope, "CLAUDE.md")) && !fileExists(filepath.Join(scope, "AGENTS.md")):
		fmt.Fprintln(stdout, "AGENTS.md: not created, because this project has CLAUDE.md and OpenCode stops reading CLAUDE.md once AGENTS.md exists. Add the shikii section to CLAUDE.md by hand, or create AGENTS.md with `@CLAUDE.md` in it and run init again")
	}
	fmt.Fprintln(stdout, "all four MCP entries point at a local model on :8080 (started by shikii up). Hosted only? remove --local from each of them by hand")
	// 家のフォルダに置くもの(Codex の設定、skill)。HOME が無い環境(容れ物の CI など)では飛ばす: プロジェクトの準備は済んでいる
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stdout, "no HOME: skipped Codex (~/.codex/config.toml) and the skill (~/.claude/skills/shikii)")
		return nil
	}
	codex := filepath.Join(home, ".codex", "config.toml")
	switch did, err := addCodexServer(codex, mcpArgs); {
	case err != nil:
		return err
	case did:
		fmt.Fprintf(stdout, "%s: [mcp_servers.shikii] added (Codex)\n", filepath.Join("~", ".codex", "config.toml"))
	default:
		fmt.Fprintf(stdout, "%s: has mcp_servers already, kept as it is (Codex; add by hand if shikii is missing: [mcp_servers.shikii] command = \"shikii\" args = [%s])\n", filepath.Join("~", ".codex", "config.toml"), quotedList(mcpArgs))
	}

	// skill: 置き直す(古いファイルも消す)
	dst := filepath.Join(home, ".claude", "skills", "shikii")
	if err := copySkill(dst); err != nil {
		return fmt.Errorf("skill: %w", err)
	}
	fmt.Fprintf(stdout, "skill: %s replaced (the agent's rules; not a place to edit by hand, change skills/shikii in the repo instead)\n", filepath.Join("~", ".claude", "skills", "shikii"))
	fmt.Fprintln(stdout, "next: shikii up  (start a local model), then shikii new <id> (writes .shikii/decisions/<id>.yaml) or copy a spec from the repo's decisions/ into .shikii/decisions/; shikii serve for the answer page")
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
			return false, fmt.Errorf("%s: %w (fix it by hand; init does not overwrite a broken file)", path, err)
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

// addAgentsSection は AGENTS.md(Codex と OpenCode が読む)に shikii の節を足す(無い時だけ。印で見分ける)。中身は作法の要点と skill への案内。
func addAgentsSection(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(b), agentsMarker) {
		return false, nil
	}
	if os.IsNotExist(err) && fileExists("CLAUDE.md") {
		return false, nil // OpenCode は AGENTS.md があると CLAUDE.md を読まなくなる。黙って作らない(呼び出し側が言う)
	}
	section := agentsMarker + "\n## shikii(判定の作法)\n\n" +
		"- 同じ種類の判断は自分で決めず `shikii decide`(MCP なら shikii_decide)を呼び、帯に従う: act だけ自動、ask は人に確かめる、discard は動かない、abstain は入力を揃える\n" +
		"- 正解は人(または結果)が決める。人の答えは `shikii label <trace_id> <truth>`。線(calibration:)や wiki の主張の状態を手で書き換えない\n" +
		"- 聞き方は spec の variants: に候補として書き、`shikii tune` が測る。採用は人が意味を確かめてから\n" +
		"- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)\n" + agentsMarker + "\n"
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		section = "\n" + section
	}
	if len(b) > 0 {
		section = "\n" + section
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.WriteString(section)
	return true, err
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
