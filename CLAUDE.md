@AGENTS.md

# このプロジェクトの共有メモリ(要点。運用は共有メモリの feedback-shared-memory-with-claude.md)

- 記憶の正本は **`$HOME/.claude/projects/shikii-shared/memory/`**(索引 `MEMORY.md`。長い記録は隣の `handoff/`)。okurun-ai と同じ形。Claude Code のこのプロジェクトの `memory/` はそこへのリンク。worktree でも同じ場所を使う。repo には記憶を置かない
- 形は Claude Code の自動メモリと同じ(1 事実 1 ファイル、前書きに `name` `description` `metadata.type`、索引に 1 行)。同じテーマは既存のファイルを直す。更新の直前に読み直す
- セッションの始めに `MEMORY.md` を読む。本人から新しいことを言われたら共有メモリに書く。進捗の実体は docs/operations.md の「第 N 段」
- 別の機械に入れる時は、`mkdir -p ~/.claude/projects/shikii-shared/{memory,handoff}` して `~/.claude/projects/<このフォルダの名>/memory` をリンクにする(中身は本人が持ち運ぶ)

# 言葉と作法(詳しくは共有メモリの各ファイル)

- 見える文は全部日本語。「根」は使わず「プロジェクトルート / リポジトリのルート」、「指紋」は使わず「ハッシュ」
- テストは RED を示してから GREEN(わざと壊して落ちるのを見てから直し、両方を報告する)
- commit は機能ごと、push は本人が言った時だけ(CI の費用)
- 新しいオプションは足さず、基本構成(`.shikii/` の下に decisions/ wiki/ records/ labels/)と固定の規則で済ませる
