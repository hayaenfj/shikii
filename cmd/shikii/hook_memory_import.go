package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// memoryImportMark は、メモリのファイルが wiki へ移された印: 「.shikii/wiki」に続けて記事の id(16 桁の hex)。
// 移した後のファイルはこの行(正本: .shikii/wiki <id>)を本文に残す(skill の import-memory.md)。wiki に触れただけの文は印にしない(監査 R4)。
var memoryImportMark = regexp.MustCompile(`\.shikii/wiki\s+[0-9a-f]{16}`)

// memoryReadLimit は印を探すために読む上限。印は本文の先頭近くに置く手順なので、巨大なファイルを毎依頼読まない(監査 R4)。
const memoryReadLimit = 64 << 10

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

// readMemoryHead はメモリのファイルの先頭(memoryReadLimit まで)を読む。
func readMemoryHead(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, memoryReadLimit))
	return string(b), err
}

// unimportedMemoryFiles は、メモリの置き場にあって、まだ wiki へ移されていないファイル(索引 MEMORY.md を除く .md)。
// 「移された」= 本文に記事の id 付きの印がある。途中まで移した置き場でも、残りだけを数える。
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
		head, err := readMemoryHead(filepath.Join(dir, name))
		if err != nil || memoryImportMark.MatchString(head) {
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

// projectNames は、このプロジェクトを名指しする語: フォルダの経路そのもの、フォルダ名(4 文字以上)、git の origin のリポジトリの名(違う時)。
// 短いフォルダ名(app、go)は他のファイルに偶然含まれるので経路だけで見る(監査 R4)。
func projectNames(scope string) []string {
	base := filepath.Base(scope)
	if scope == "" || base == "." || base == "/" || base == string(filepath.Separator) {
		return nil
	}
	names := []string{scope}
	if len(base) >= 4 {
		names = append(names, base)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if remote, ok := gitRun(ctx, scope, "remote", "get-url", "origin"); ok {
		repo := strings.TrimSuffix(filepath.Base(strings.TrimSpace(remote)), ".git")
		if len(repo) >= 4 && repo != base {
			names = append(names, repo)
		}
	}
	return names
}

// namesProject は、本文がこのプロジェクトを名指しするか: 語の境目(前後が英数字でない)で名を含む。経路は部分一致でよい。
func namesProject(text string, names []string) bool {
	for _, n := range names {
		if strings.Contains(n, string(filepath.Separator)) {
			if strings.Contains(text, n) {
				return true
			}
			continue
		}
		for i := 0; ; {
			j := strings.Index(text[i:], n)
			if j < 0 {
				break
			}
			at := i + j
			before, after := rune(0), rune(0)
			if at > 0 {
				before = []rune(text[:at])[len([]rune(text[:at]))-1]
			}
			if rest := text[at+len(n):]; rest != "" {
				after = []rune(rest)[0]
			}
			wordChar := func(r rune) bool {
				return r != 0 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') && r < 128
			} // haya1-old は haya1 の名指しではない
			if !wordChar(before) && !wordChar(after) {
				return true
			}
			i = at + len(n)
		}
	}
	return false
}

// unimportedCodexMemoryFiles は、Codex の共通のメモリのうち、このプロジェクトを名指しし、まだ wiki へ移されていないファイル。
func unimportedCodexMemoryFiles(dir string, names []string) []string {
	if dir == "" || len(names) == 0 {
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
		head, err := readMemoryHead(filepath.Join(dir, name))
		if err != nil || memoryImportMark.MatchString(head) || !namesProject(head, names) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// listFiles は通知に並べるファイル名(先頭 5 件と残りの件数)。
func listFiles(files []string) string {
	if len(files) <= 5 {
		return strings.Join(files, ", ")
	}
	return strings.Join(files[:5], ", ") + fmt.Sprintf(" ほか %d 件", len(files)-5)
}

// memoryImportNotice は、まだ wiki に無いメモリがあれば、作法の手順で移すようエージェントへ伝える 1 行(セッションに 1 回)。
// 言われなくても気付くための通知で、消すのは人。0.7.6 より前から wiki のあるプロジェクトでも、未移行のファイルがある限り出す。
// 見るのは Claude Code のこのプロジェクトのメモリと、Codex の共通のメモリのうちこのプロジェクトを名指しするファイル。
func memoryImportNotice(scope string) string {
	var parts []string
	if dir := agentMemoryDir(scope); dir != "" {
		if files := unimportedMemoryFiles(dir); len(files) > 0 {
			parts = append(parts, fmt.Sprintf("%s に %d 件(%s)", homeTilde(dir), len(files), listFiles(files)))
		}
	}
	if dir := codexMemoryDir(); dir != "" {
		if files := unimportedCodexMemoryFiles(dir, projectNames(scope)); len(files) > 0 {
			parts = append(parts, fmt.Sprintf("Codex の %s にこのプロジェクトを名指しする %d 件(%s)", homeTilde(dir), len(files), listFiles(files)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: エージェントのメモリに、まだ wiki へ移していないファイルがあります: %s。今回の依頼を終えたら、作法の references/import-memory.md の手順で各ファイルを原資料として残し、規則ごとに教訓にして移し、ファイルは wiki の記事を指す 1 行(正本: .shikii/wiki <記事の id>)にしてください(全プロジェクト共通のファイルは指す行を足すだけ。消すのは人)。", strings.Join(parts, "、"))
}

// homeTilde は家の下のパスを ~ 表記にする(表示用)。
func homeTilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
