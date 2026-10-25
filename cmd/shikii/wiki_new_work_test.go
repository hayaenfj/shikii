package main

import (
	"os"
	"strings"
	"testing"
)

// 0.9.1: エージェントが自分で新しく作った・変えた決まり・テスト・手順(コマンドやスクリプト)も、その場で wiki に書く(既存の記事があれば
// 足すか直す)。本人の訂正だけを書く作法だったので、テストの流し方(scripts/go-test.sh)を作っても wiki に残らなかった(本人「新しい何かを
// ルールとかテストで作ったらwikiに必ず記載すること」)。skill を読まないエージェント(hook の無い Codex、OpenCode)にも届くよう、
// 全部の入り口(作法の一言・振り返りの文・AGENTS.md の節・MCP の案内・skill)に入れる
func TestNewRulesTestsAndProceduresGoToTheWiki(t *testing.T) {
	ja := "新しく作った・変えた決まり・テスト・手順"
	en := "rule, test or procedure"
	reflectText := reflectAsk("shikii")
	skill, _ := os.ReadFile("../../skills/shikii/SKILL.md")
	wikiRef, _ := os.ReadFile("../../skills/shikii/references/wiki.md")
	// 着手前の整理の案内は依頼のたびに届く(作法の一言はセッションに 1 回だけで、作業規則の無いプロジェクトの Claude Code は作り終えた時に使わなかった。0.9.1 の実測)
	for name, text := range map[string]string{"AGENTS.md の節": agentsSection, "振り返りを頼む文": reflectText} {
		if !strings.Contains(text, ja) {
			t.Errorf("%s に「%s」も wiki に書く決まりが無い", name, ja)
		}
	}
	for name, text := range map[string]string{"作法の一言(habitsNotice)": habitsNotice, "着手前の整理の案内(毎回)": planningNotice("s", "t"), "MCP の案内": mcpInstructions, "SKILL.md": string(skill), "references/wiki.md": string(wikiRef)} {
		if !strings.Contains(text, en) {
			t.Errorf("%s に %q も wiki に書く決まりが無い", name, en)
		}
	}
}
