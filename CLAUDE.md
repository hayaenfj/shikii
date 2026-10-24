@AGENTS.md

# このプロジェクトの記憶(要点。運用は wiki「記憶の正本は shikii の wiki、メモリのフォルダは薄い索引」)

- 記憶の正本は **このプロジェクトの `.shikii/wiki`**(2026-10-23 から)。規則・判断・訂正・不具合・検証結果は出典付きの教訓として wiki に書き(`shikii wiki capture` で原資料 → `shikii reflect place` で差し込み先(1〜2 件ずつ) → `shikii reflect submit` で保存。`wiki ingest` は place に通していない記事だけ)、エージェント別の正本を作らない。hook が依頼の前に似た教訓を届ける
- `$HOME/.claude/projects/shikii-shared/memory/`(Claude Code のこのプロジェクトの `memory/` はそこへのリンク)は wiki の記事を指す **薄い索引**。`MEMORY.md` に記事 1 行ずつと、どの依頼にも効く規則(言語・使わない語・push の時・コミットの書き方)だけ残す。各ファイルは記事の id を指す 1 行「正本: .shikii/wiki <id>」(hook が id 付きのこの印の無いファイルを「まだ wiki に無い」と数え、セッションに 1 回伝える)。書き換えながら進める作業ファイルや長い報告(監査の報告、リリースの手順と記録、差分、画像)は作業記録 `.shikii/handoff/<仕事>/`、確かめた時点の証拠の要点は wiki の原資料(`.shikii/records/wiki-sources/`、`shikii wiki capture`。reference に作業記録のパスを書ける)
- セッションの始めに `MEMORY.md` を読む。本人から新しいことを言われたら wiki に教訓として書き、索引に 1 行足す。進捗の実体はローカル専用の docs/operations.md の「第 N 段」(Git管理外。無い環境では wiki を参照)
- 別の機械に入れる時は、`mkdir -p ~/.claude/projects/shikii-shared/memory` して `~/.claude/projects/<このフォルダの名>/memory` をリンクにする(索引と、正本の `.shikii/wiki/` と `.shikii/records/wiki-sources/`、作業記録の `.shikii/handoff/` を本人が持ち運ぶ。どちらも Git 管理外。wiki が無い機械では索引の「どの依頼にも効く規則」だけが効く)

# 言葉と作法(詳しくは wiki: `shikii wiki query <語>`)

- 見える文は全部日本語。「根」は使わず「プロジェクトルート / リポジトリのルート」、「指紋」は使わず「ハッシュ」
- テストは RED を示してから GREEN(わざと壊して落ちるのを見てから直し、両方を報告する)
- commit は機能ごと、push は本人が言った時だけ(CI の費用)
- 新しいオプションは足さず、基本構成(`.shikii/` の下に decisions/ wiki/ records/ labels/ handoff/)と固定の規則で済ませる
