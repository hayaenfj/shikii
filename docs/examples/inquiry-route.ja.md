[English](inquiry-route.md) · **日本語**

# 別の人の例: 問い合わせメールの振り分け（2026-10-18）

shikii を初めて使う人（この repo の中身を知らないエージェント）が、[日本語の README](../readme/README_ja.md) だけを頼りに、自分で決めた問題で一周した記録。
prompt-intent のような「AI への依頼の分類」ではない分野で、文書どおりに動くかを見るための試験。ソースコードは一度も読まずに済んだ。

## 問題

問い合わせメールを 4 つの窓口（請求 / 故障 / 解約 / その他）に回す。入力は日本語 22 件（各 5〜6 件）、個人情報は無し。

- spec: [`inquiry-route/inquiry-route.yaml`](inquiry-route/inquiry-route.yaml)（`shikii new inquiry-route --type choice --question … --option billing=… ×4 --input body --pii true --send-to local` で作り、聞き方の候補 `variants:` を 3 つ手で足した。較正ブロックは外してある。線は使う人が自分のモデルで引く）
- 過去の表: [`inquiry-route/past.csv`](inquiry-route/past.csv)（`body,truth` の 22 行。`shikii import past.csv --spec .shikii/decisions/inquiry-route.yaml --out truths.json` で答え合わせの材料になる）

## 一周の結果（Kev-4B Q4_K_M、手元の llama-server）

| 段 | 結果 |
|---|---|
| `shikii init` → `shikii new` → `shikii decide` 1 件 | 通った。`band=ask top=billing p=0.915 calibrated=false`（線が無いので人に確かめて） |
| `shikii serve` + `POST /v1/decide` 22 件 | top-1 は 22 件中 20 件が正解。帯は 21 件 ask、1 件 discard |
| `POST /v1/label` 22 件 | 間違った答え（`refund`）は選択肢の一覧つきで拒否された |
| `shikii calibrate --write` | 22 件 < `eval.min_n` 50 で書かない → spec の `eval.min_n` を 20 に下げて書けた。選ばれた方法は identity（生の確率がそのまま使える） |
| 較正後の `decide` | `band=act`、receipt 付き。serve は再起動なしで新しい線を使った |
| `shikii tune`（候補 3 つ） | どれも現状に勝てず `winner: current`（同点なら現状維持）。`.shikii/wiki/runs/` に表と一行の学びが残った |
| 抜き取り（act の 20 件に 1 件）に答える | `/v1/queue` の `audit: true` の判定に `/v1/label` で答えられた |
| MCP（`shikii mcp` に stdio で initialize / tools/list / tools/call） | 道具 5 つ、`shikii_decide` が JSON を返した。`init` が書いた 4 つの設定は文書どおり |

評点は 5 段階の 3。「README と help だけで init → new → decide → label まで迷わず進めた。50 件未満でどう始めるか、serve の API の体、spec の書き方の置き場所は README に無く、エラー文と skill を頼りに推測で越えた」。

## この試験で直したもの（同日）

- README の `curl` の例に `-H 'Content-Type: application/json'` が無く、そのまま打つと蹴られた → 例を直し、`scripts/check-readme.sh` が /v1 への curl の例に header があるかを見る
- 正解が `eval.min_n` に足りない時の `calibrate` と `tune` の文が「何をすればよいか」を言わなかった（`tune` は `--labeled` を渡しても同じ文で蹴った） → 「正解を集めるか、試しなら spec の `eval.min_n` を下げる」と言う
- `shikii init` が `wiki/` を空で作るので、`tune` の run ページのリンク（型・モデルのページ）が切れ、提案役が読む既製の知識も無かった → `init` が index.md・types/・patterns/・models/ を写す（あるページは触らない）。run ページは、あるページだけリンクにする
- `shikii bias` だけ `--store` が無く `--labeled` 必須だった → calibrate / tune と同じ貯金（store の正解の本文）で回る
- `drift` の ask agreement が該当 0 件でも `0.000` と出た → `n/a`
- `/v1/redo` に `spec` を付けないと `unknown spec ""` → 「spec が要る。decisions/ のファイル名」と言う
- `status` の「human-checked so far」と receipt の「human-checked act decisions」が別の数なのに同じ言葉だった → status は「audit answers so far」。receipt の数の意味は [bands.md](../../skills/shikii/references/bands.md)
- 文書: spec の書き方の置き場所、serve の HTTP API の一覧（[guide.ja.md](../guide.ja.md#serve-の-http-api)）、`.shikii/` の何を commit するか、`shikii new` は対話でもフラグでも、コマンドの出力は英語、`wiki/` に既製の知識が写ること

## 自分の問題で試すには

```sh
shikii init
cp <この repo>/docs/examples/inquiry-route/inquiry-route.yaml .shikii/decisions/   # または shikii new <id> で自分の問題を書く
shikii import <この repo>/docs/examples/inquiry-route/past.csv --spec .shikii/decisions/inquiry-route.yaml --out truths.json
shikii calibrate .shikii/decisions/inquiry-route.yaml --labeled truths.json --local http://127.0.0.1:8080 --write
shikii decide .shikii/decisions/inquiry-route.yaml --local http://127.0.0.1:8080 -i body='先月の請求額が二重に引き落とされているようです'
```

22 件は試しの数。業務で使うなら `eval.min_n` を 50 以上に戻し、`shikii serve` の画面で答え合わせを続けて、線を引き直す。
