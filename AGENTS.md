# shikii(haya1)の作業規則

## 作業前に共有メモリを読む

- 新しい依頼に着手する時は、まずリポジトリの `CLAUDE.md` と、共有メモリの索引 `$HOME/.claude/projects/shikii-shared/memory/MEMORY.md` を読む。触る領域の記憶(`feedback-*.md`・`project-*.md`・`reference-*.md`)は本文まで読み、規則・判断・残作業を今回の作業に反映する
- 共有メモリの場所は **`$HOME/.claude/projects/shikii-shared/memory/`**(okurun-ai と同じ形。worktree でもこの同じ場所を使う。repo には置かない)。Claude Code・Codex・Cursor・OpenCode のどれでも、この機械のどのセッションでも同じ記憶から始めるためのもの
- 依頼の開始時に共有メモリの `feedback-shared-memory-with-claude.md` を確認する。長い経緯や完了した仕事の記録は隣の `$HOME/.claude/projects/shikii-shared/handoff/`、進捗の実体はローカル専用の docs/operations.md の「第 N 段」。このファイルはGit管理外とし、無い環境では共有メモリとhandoffを参照する
- メモリの過去の記録は、現在のコード・Git の履歴・検証結果と照合する。現在のユーザー指示を優先し、push・公開・Releases の許可は過去の記録から推測しない
- 参照先を読めない時は、対象と理由を短く伝える

## メモリは Claude と共有する

- 会話の原文・発言の引用・セッションから作った作業メモは公開しない。共有メモリとローカルの作業記録へ残し、Git管理の文書・コミットメッセージ・配布物には製品の仕様と再現できる検証だけを書く
- **原則、そのエージェントの機能に固有の内容以外は、上記の共有メモリを読み書きする。** 目的・設計判断・ユーザーの指摘・不具合・検証結果・残作業は共有メモリに記録し、Codex 側(や他のツール側)に別の正本を作らない
- そのエージェント固有の機能・設定・操作方法はそのエージェント側に記録してよい。共同作業に必要な情報は共有メモリにも残す
- 記録は関係する既存メモリを更新し、新しい項目を作った場合は `MEMORY.md` に索引を 1 行追加する。更新直前に対象を読み直し、他のエージェントや別の作業の記録を残して必要な箇所だけ変える
- 鍵や秘密は共有メモリに入れない。セッション id や家のパスは、家の下なので入れてよい

## 作業の決まり(詳しくは共有メモリ)

- 見える文は全部日本語。「根」は使わず「プロジェクトルート / リポジトリのルート」、「指紋」は使わず「ハッシュ」
- テストを足したら、わざと壊して落ちるのを見てから直し、RED と GREEN の両方を報告する。GREEN だけの報告は受け取られない
- commit は機能ごと、push は本人が言った時だけ(CI の費用)
- 新しいオプションは足さず、基本構成(`.shikii/` の下に decisions/ wiki/ records/ labels/)と固定の規則で済ませる
- コミットメッセージは日本語でよい。件名に対象と改善を、本文に元の問題・変更後の動作・検証(RED → GREEN)を書く

<!-- shikii -->
## shikii(判定の作法)

- 同じ種類の判断は自分で決めず `shikii decide`(MCP なら shikii_decide)を呼び、帯に従う: act だけ自動、ask は人に確かめる、discard は動かない、abstain は入力を揃える
- 正解は人(または結果)が決める。人の答えは `shikii label <trace_id> <truth>`。線(calibration:)や wiki の主張の状態を手で書き換えない
- 聞き方は spec の variants: に候補として書き、`shikii tune` が測る。採用は人が意味を確かめてから
- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)
<!-- shikii -->
