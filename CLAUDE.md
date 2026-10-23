@AGENTS.md

# このプロジェクトの記憶(要点。運用は wiki「記憶の正本は shikii の wiki、メモリのフォルダは薄い索引」)

- 記憶の正本は **このプロジェクトの `.shikii/wiki`**(2026-10-23 から)。規則・判断・訂正・不具合・検証結果は出典付きの教訓として wiki に書き(`shikii wiki ingest`、新しい教訓は `shikii reflect place` で差し込み先を見てから)、エージェント別の正本を作らない。hook が依頼の前に似た教訓を届ける
- `$HOME/.claude/projects/shikii-shared/memory/`(Claude Code のこのプロジェクトの `memory/` はそこへのリンク)は wiki の記事を指す **薄い索引**。`MEMORY.md` に記事 1 行ずつと、どの依頼にも効く規則(言語・使わない語・push の時・コミットの書き方)だけ残す。各ファイルは記事の id を指す 1 行(hook がこの指し先の無いファイルを「まだ wiki に無い」と数える)。長い記録は隣の `handoff/`
- セッションの始めに `MEMORY.md` を読む。本人から新しいことを言われたら wiki に教訓として書き、索引に 1 行足す。進捗の実体はローカル専用の docs/operations.md の「第 N 段」(Git管理外。無い環境では wiki と handoff を参照)
- 別の機械に入れる時は、`mkdir -p ~/.claude/projects/shikii-shared/{memory,handoff}` して `~/.claude/projects/<このフォルダの名>/memory` をリンクにする(索引は本人が持ち運ぶ。wiki はプロジェクトの `.shikii/` にあり Git 管理外)

# 言葉と作法(詳しくは共有メモリの各ファイル)

- 見える文は全部日本語。「根」は使わず「プロジェクトルート / リポジトリのルート」、「指紋」は使わず「ハッシュ」
- テストは RED を示してから GREEN(わざと壊して落ちるのを見てから直し、両方を報告する)
- commit は機能ごと、push は本人が言った時だけ(CI の費用)
- 新しいオプションは足さず、基本構成(`.shikii/` の下に decisions/ wiki/ records/ labels/)と固定の規則で済ませる
