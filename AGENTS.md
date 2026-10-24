# shikii(haya1)の作業規則

## 作業前に記憶を読む

- 新しい依頼に着手する時は、まずリポジトリの `CLAUDE.md` と、索引 `$HOME/.claude/projects/shikii-shared/memory/MEMORY.md` を読む。記憶の正本はこのプロジェクトの `.shikii/wiki`(2026-10-23 から)で、索引の各行が記事の id を指す。触る領域の記事は `shikii wiki query <語 or lesson:<id>>` で本文まで読み、規則・判断・残作業を今回の作業に反映する。hook が依頼の前に似た教訓を届けるが、索引に 1 行で書いたどの依頼にも効く規則(言語・使わない語・push の時・コミットの書き方)は届かなくても守る
- 索引の場所は **`$HOME/.claude/projects/shikii-shared/memory/`**(worktree でもこの同じ場所を使う。repo には置かない)。Claude Code・Codex・Cursor・OpenCode のどれでも、この機械のどのセッションでも同じ記憶から始めるためのもの
- 書き換えながら進める作業ファイル(監査の報告、リリースの手順と記録、差分、画像)は作業記録 `.shikii/handoff/<仕事>/`(`shikii handoff`、MCP の `shikii_wiki` の `handoff_*`。画面の知識の欄で読める)。監査者やサブエージェントには報告の書き先をここで指定する
- 確かめた時点の証拠の要点は wiki の原資料(`.shikii/records/wiki-sources/`、`shikii wiki capture` で控える。reference に作業記録のパスを書ける)と出典付きの記事、進捗の実体はローカル専用の docs/operations.md の「第 N 段」。このファイルはGit管理外とし、無い環境では wiki を参照する
- メモリの過去の記録は、現在のコード・Git の履歴・検証結果と照合する。現在のユーザー指示を優先し、push・公開・Releases の許可は過去の記録から推測しない
- 参照先を読めない時は、対象と理由を短く伝える

## 記憶は wiki に書き、Claude と Codex で共有する

- 会話の原文・発言の引用・セッションから作った作業メモは公開しない。wiki(`.shikii/` は Git 管理外)とローカルの作業記録へ残し、Git管理の文書・コミットメッセージ・配布物には製品の仕様と再現できる検証だけを書く
- **原則、そのエージェントの機能に固有の内容以外は、wiki を読み書きする。** 目的・設計判断・ユーザーの指摘・不具合・検証結果・残作業は出典付きの教訓として wiki に記録し(`shikii wiki capture` で原資料、`shikii reflect place` で差し込み先、`shikii wiki ingest` で保存)、Codex 側(や他のツール側)に別の正本を作らない
- そのエージェント固有の機能・設定・操作方法はそのエージェント側に記録してよい。共同作業に必要な情報は wiki にも残す
- 新しい記事を作ったら `MEMORY.md` に記事の id 付きで 1 行足す。既存の記事に近い話題は `place` の候補に従ってその記事を直す。更新直前に対象を読み直し、他のエージェントや別の作業の記録を残して必要な箇所だけ変える
- 鍵や秘密は wiki にも索引にも入れない。セッション id や家のパスは、家の下なので入れてよい

## 作業の決まり(詳しくは wiki)

- 見える文は全部日本語。「根」は使わず「プロジェクトルート / リポジトリのルート」、「指紋」は使わず「ハッシュ」
- テストを足したら、わざと壊して落ちるのを見てから直し、RED と GREEN の両方を報告する。GREEN だけの報告は受け取られない
- commit は機能ごと、push は本人が言った時だけ(CI の費用)
- 新しいオプションは足さず、基本構成(`.shikii/` の下に decisions/ wiki/ records/ labels/ handoff/)と固定の規則で済ませる
- コミットメッセージは日本語でよい。件名に対象と改善を、本文に元の問題・変更後の動作・検証(RED → GREEN)を書く

<!-- shikii -->
## shikii(判定の作法)

- 同じ種類の判断は自分で決めず `shikii decide`(MCP なら shikii_decide)を呼び、帯に従う: act だけ自動、ask は人に確かめる、discard は動かない、abstain は入力を揃える
- 正解は人(または結果)が決める。人の答えは `shikii label <trace_id> <truth>`。線(calibration:)や wiki の主張の状態を手で書き換えない
- 聞き方は spec の variants: に候補として書き、`shikii tune` が測る。採用は人が意味を確かめてから
- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)
<!-- shikii -->
