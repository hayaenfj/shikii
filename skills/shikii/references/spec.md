# spec の書き方

1 判定 = 1 YAML(`decisions/<id>.yaml`)。`shikii new <id> --type choice --question "…" --option id=説明 … --input name --pii true|false` で作るのが基本。既製の判定は `decisions/README.md`。

- 型: `noul`(はい/いいえ)、`choice`(多択。選択肢は id と説明)、`score`(段階。levels を低い順)
- `state.template`: 入力の並べ方。`{{ name }}` が入力。`required` に必須の入力
- `state.context`(任意): 迷った時だけ足す並べ方。required を全部使い、template に無い入力を 1 つ以上足す(例: 直前の発言 `{{ previous }}`)。文脈付きは別のハッシュで線も別
- `policy.send_to`: `local` / `hosted`。`pii: true` なら本文は外に出ない。`pii` は必ず書く(既定なし)
- `thresholds.act` / `ask`: 帯の境。`score` は `level` で「k 以上」の確率で帯を決められる
- `eval.labeled`: 正解の種のファイル。`eval.min_n`: 線を書くのに要る人の正解の数(50 が目安)
- `variants:`: 聞き方の候補。`question` / `options`(id → 説明の言い換え)/ `template` / `layout`(state-last | state-first | plain)/ `steps`(多択を はい/いいえ の列に)。tune が測る。候補を書くのはあなたでもよいが、採用は人
- `display.<lang>`: 画面の訳(題、質問、選択肢)。モデルには送らない
- `calibration:` は書かない。calibrate が書く

ハッシュ(hash)は質問・選択肢・template・分解・配置から決まる。1 語変えると線は消え、`ask` に戻る。それが正しい。
