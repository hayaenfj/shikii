@AGENTS.md

# このプロジェクトの共有メモリ(要点。運用は .claude/memory/feedback-shared-memory-with-claude.md)

- 記憶の正本は **repo の `.claude/memory/`**(索引 `MEMORY.md`)。commit する。Claude Code・Codex・Cursor・OpenCode、別の機械、worktree で同じ記憶から始める
- 形は Claude Code の自動メモリと同じ(1 事実 1 ファイル、前書きに `name` `description` `metadata.type`、索引に 1 行)。同じテーマは既存のファイルを直す。更新の直前に読み直す
- 個人の機械にしか意味の無いもの(セッション id、家のパス、鍵、メール)は `~/.claude/projects/<このフォルダ>/memory/` に置き、そこの `MEMORY.md` には「正本は repo の .claude/memory/」の案内を残す
- セッションの始めに `.claude/memory/MEMORY.md` を読む。本人から新しいことを言われたら `.claude/memory/` に書く。進捗の実体は docs/operations.md の「第 N 段」

# 言葉と作法(詳しくは .claude/memory/ の各ファイル)

- 見える文は全部日本語。「根」は使わず「プロジェクトルート / リポジトリのルート」、「指紋」は使わず「ハッシュ」
- テストは RED を示してから GREEN(わざと壊して落ちるのを見てから直し、両方を報告する)
- commit は機能ごと、push は本人が言った時だけ(CI の費用)
- 新しいオプションは足さず、基本構成(`.shikii/` の下に decisions/ wiki/ records/ labels/)と固定の規則で済ませる
