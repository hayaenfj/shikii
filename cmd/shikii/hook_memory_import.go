package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// memoryImportMarker は、メモリのファイルが wiki へ移された印。移した後のファイルはこの行(とページ id)だけを本文に残す(skill の import-memory.md)。
const memoryImportMarker = ".shikii/wiki"

// agentMemoryDir は、このプロジェクトのエージェントのメモリの置き場(Claude Code の自動メモリ:
// ~/.claude/projects/<経路の英数字以外を - にしたもの>/memory。共有の置き場へのリンクでもよい)。無ければ空。
func agentMemoryDir(scope string) string {
	home, err := os.UserHomeDir()
	if err != nil || scope == "" {
		return ""
	}
	slug := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, scope)
	dir := filepath.Join(home, ".claude", "projects", slug, "memory")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// unimportedMemoryFiles は、メモリの置き場にあって、まだ wiki へ移されていないファイル(索引 MEMORY.md を除く .md)。
// 「移された」= 本文に .shikii/wiki への指し先がある。途中まで移した置き場でも、残りだけを数える。
func unimportedMemoryFiles(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || strings.Contains(string(b), memoryImportMarker) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// codexMemoryDir は Codex のメモリの置き場(~/.codex/memories)。全プロジェクト共通なので、このプロジェクトを名指しするファイルだけを見る。
func codexMemoryDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".codex", "memories")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// projectNames は、このプロジェクトを名指しする語: フォルダ名と、登録した名前(リポジトリの名)。
func projectNames(scope string) []string {
	names := []string{filepath.Base(scope)}
	if agent, err := agentHome(); err == nil {
		for _, p := range registeredProjects(agent) {
			if p.Path == scope && p.Name != "" && p.Name != names[0] {
				names = append(names, p.Name)
			}
		}
	}
	return names
}

// unimportedCodexMemoryFiles は、Codex の共通のメモリのうち、このプロジェクトを名指しし、まだ wiki へ移されていないファイル。
func unimportedCodexMemoryFiles(dir string, names []string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || strings.Contains(string(b), memoryImportMarker) {
			continue
		}
		for _, n := range names {
			if strings.Contains(string(b), n) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// memoryImportNotice は、まだ wiki に無いメモリがあれば、作法の手順で移すようエージェントへ伝える 1 行。
// 言われなくても気付くための通知で、消すのは人。すでに wiki のあるプロジェクト(途中の版から使っている)でも、未移行のファイルがある限り出す。
// 見るのは Claude Code のこのプロジェクトのメモリと、Codex の共通のメモリのうちこのプロジェクトを名指しするファイル。
func memoryImportNotice(scope string) string {
	var parts []string
	if dir := agentMemoryDir(scope); dir != "" {
		if files := unimportedMemoryFiles(dir); len(files) > 0 {
			parts = append(parts, fmt.Sprintf("%s に %d 件(%s)", homeTilde(dir), len(files), strings.Join(files, ", ")))
		}
	}
	if dir := codexMemoryDir(); dir != "" {
		if files := unimportedCodexMemoryFiles(dir, projectNames(scope)); len(files) > 0 {
			parts = append(parts, fmt.Sprintf("Codex の %s にこのプロジェクトを名指しする %d 件(%s)", homeTilde(dir), len(files), strings.Join(files, ", ")))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: エージェントのメモリに、まだ wiki へ移していないファイルがあります: %s。今回の依頼を終えたら、作法の references/import-memory.md の手順で各ファイルを原資料として残し、規則ごとに教訓にして移し、ファイルは wiki の記事を指す 1 行にしてください(全プロジェクト共通のファイルは指す行を足すだけ。消すのは人)。", strings.Join(parts, "、"))
}

// homeTilde は家の下のパスを ~ 表記にする(表示用)。
func homeTilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
