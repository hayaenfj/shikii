package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	shikii "github.com/hayaenfj/shikii"
)

// cmdInit は 1 回でエージェントの準備を済ませる(オプションは無い。基本構成と固定の規則で決める):
//  1. 実行する場所に decisions/ wiki/ .shikii/(基本構成)
//  2. .mcp.json に shikii の MCP サーバーの項(他の項は残す。Claude Code / Cursor が読む)
//  3. skill(作法)を ~/.claude/skills/shikii に写す(Claude Code が読む)
//
// 2 回目以降は同じ結果で、人が書いたものを壊さない。
func cmdInit(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: shikii init  (no flags: it prepares the current folder and the agent)")
	}
	for _, d := range []string{"decisions", "wiki", ".shikii"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	fmt.Fprintln(stdout, "folders: decisions/ (specs) wiki/ (knowledge) .shikii/ (records and truths)")

	// .mcp.json: あれば shikii の項だけ足す(置き換える)。無ければ作る
	cfg := map[string]any{}
	if b, err := os.ReadFile(".mcp.json"); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf(".mcp.json: %w (fix it by hand; init does not overwrite a broken file)", err)
		}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["shikii"] = map[string]any{"command": "shikii", "args": []string{"mcp", "--specs", "decisions", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}}
	cfg["mcpServers"] = servers
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(".mcp.json", append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, ".mcp.json: shikii added (tools shikii_decide / shikii_label / shikii_status / shikii_tune for Claude Code and Cursor)")

	// skill
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, ".claude", "skills", "shikii")
	if err := copySkill(dst); err != nil {
		return fmt.Errorf("skill: %w", err)
	}
	fmt.Fprintf(stdout, "skill: %s (the agent's rules: when to decide, follow the bands, truths and statuses belong to people)\n", filepath.Join("~", ".claude", "skills", "shikii"))
	fmt.Fprintln(stdout, "next: shikii up  (start a local model), then shikii new <id> or copy decisions/*.yaml from the repo; shikii serve for the answer page")
	return nil
}

// copySkill は埋め込んだ skills/shikii を dst に写す(毎回上書き。人が手で直す場所ではない)。
func copySkill(dst string) error {
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
