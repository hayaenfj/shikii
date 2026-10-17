# testdata

全部**作り物**。実在の製品・注文・人物とは無関係で、本文はこのリポジトリのために書いた。

## same-issue-labeled.json

不具合報告の重複判定(`decisions/same-issue.yaml`、はい/いいえ)の較正用 32 組、日本語。
同じ不具合 12(`truth: true`)、別の不具合 12(`false`)、同じ画面や機能だが別の不具合 8(境界。`false` に重み 0.5)。
境界は「自動で同じと扱ってはいけない」例なので false 側に置く。`raw` が無いのでモデルに聞く。

## same-inquiry-labeled.json

問い合わせの同一判定(`decisions/same-inquiry.yaml`、はい/いいえ)の較正用 32 組、日本語。
同じ案件 12、別の案件 12、同じ種類の問題だが別の案件 8(境界。重み 0.5)。注文番号は架空。

## noul-synthetic-labeled.json

はい/いいえ の**合成データ**(本文無し、`"s"`)。関連ペアの Δ(ロジット)の下限 ≈ 2.54、無関係ペアの上限 ≈ 1.96、境界 ≈ 1.91 という
統計だけを写したもので、`raw` 付きなのでモデルを呼ばずに `shikii calibrate` の動きを確かめられる。テストと `--overconfident` 無しのデモ用。

## prompt-intent-labeled.json / -ja.json / -en.json

prompt-intent(5 択)の**種集合**。英語・日本語の依頼文 62 件(日 23・英 39)、正解ラベルは作業者(Claude)が付けた。

## store/labels/

`decisions/*.yaml` に書いてある較正ブロックの**裏付け**。`check` は (spec_hash, model) ごとに store のラベルで act での precision を測り直すので、
手元の `.shikii/`(ignore 対象)に無い CI では、ここを `--store testdata/store` で渡す。
中身は `calibrate --write` が永続化したラベルをそのまま写したもの(trace_id・spec_hash・model・inputs_hash・raw・truth・weight)。
**本文は入っていない**(入力はハッシュだけ)。較正を書き直したら、同じ (spec_hash, model) の行を写し直す。
