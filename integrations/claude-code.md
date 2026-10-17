# 接続例: Claude Code の hook（claude-jev）

`shikii` 本体は汎用で、この接続は 1 つの例。実装は `~/dotfiles/claude-jev/src/{decisions.mjs, decisions-label.mjs, prompt-intent.mjs}`。

## 形

```
UserPromptSubmit ─ prompt-intent.mjs ─ shikii decide decisions/prompt-kind.yaml --json
                                        └ pending に trace と作業ツリーの指紋を残す
Stop ─────────── decisions-label.mjs ── 指紋が変わった → label <trace> edit (w=1)
                                        変わらない    → label <trace> !edit (w=0.7, 部分ラベル)
```

- 設定: `~/.claude/jev/decisions.json` `{"bin","spec","store","local"}`。無ければ hook は従来の経路（Jev 直叩き）
- 文脈の段: hook は transcript の最後のアシスタントの発言を `-i previous=…`（1500 字まで、thinking は含めない）で渡す。`prompt-kind.yaml` の `state.context` が、最小の質問で迷った時だけそれを足して聞き直す（「yes please」「go ahead」のような短い返事はこれで決まる）。transcript が無ければ渡さず、最小の質問だけで答える。文脈付きの聞き方は別の指紋なので線も別に引かれる（`calibrate` が両方引く）
- 本番 DB を読んだ（tainted）セッションでは hosted に送らず、`local`（llama.cpp の Kev-4B）があればそれで判定する
- 記録は `~/.claude/jev/decisions-store/`。依頼文も本文として残る(`pii: true` なので外の AI には送らない。手元の Kev-4B だけ)
- 50 件溜まったら `shikii calibrate decisions/prompt-kind.yaml --model '<実モデル名>' --store ~/.claude/jev/decisions-store --write`

## 他のエージェントに付けるとき

必要なのは 3 つだけ:

1. 判定したい場所で `shikii decide <spec> --json -i key=value` を呼び、`band` と `trace_id` を受け取る
2. 結果が分かる場所（終了時・取り消し時・利用者が選んだ時）で `shikii label <trace_id> <truth> --source <どこから>` を呼ぶ。
   「それではなかった」しか分からなければ `!option`、弱ければ `--weight 0.7`
3. 送ってはいけない文脈（個人情報・本番データ）では `--tainted` を付ける。spec の `send_to` に `local` が無ければ abstain になる

## 止め方

[`../docs/operations.md`](../docs/operations.md) §2。設定ファイル 1 つ退避すれば従来に戻る。
