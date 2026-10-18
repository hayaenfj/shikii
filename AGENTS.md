<!-- shikii -->
## shikii(判定の作法)

- 同じ種類の判断は自分で決めず `shikii decide`(MCP なら shikii_decide)を呼び、帯に従う: act だけ自動、ask は人に確かめる、discard は動かない、abstain は入力を揃える
- 正解は人(または結果)が決める。人の答えは `shikii label <trace_id> <truth>`。線(calibration:)や wiki の主張の状態を手で書き換えない
- 聞き方は spec の variants: に候補として書き、`shikii tune` が測る。採用は人が意味を確かめてから
- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)
<!-- shikii -->
