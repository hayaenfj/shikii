# 接続例: Claude Code の hook（claude-jev）

`shikii` 本体は汎用で、この接続は 1 つの例。実装は `~/dotfiles/claude-jev/src/{decisions.mjs, decisions-label.mjs, prompt-intent.mjs}`。

## 形

```
UserPromptSubmit ─ prompt-intent.mjs ─ shikii decide decisions/prompt-kind.yaml --json
                                        └ pending に trace と作業ツリーのハッシュを残す
Stop ─────────── decisions-label.mjs ── ハッシュが変わった → label <trace> edit (w=1)
                                        変わらない    → label <trace> !edit (w=0.7, 部分ラベル)
```

- 設定: `~/.claude/jev/decisions.json` `{"bin","spec","store","local"}`。無ければ hook は従来の経路（Jev 直叩き）
- 文脈の段: hook は transcript の最後のアシスタントの発言を `-i previous=…`（1500 字まで、thinking は含めない）で渡す。`prompt-kind.yaml` の `state.context` が、最小の質問で迷った時だけそれを足して聞き直す（「yes please」「go ahead」のような短い返事はこれで決まる）。transcript が無ければ渡さず、最小の質問だけで答える。文脈付きの聞き方は別のハッシュなので線も別に引かれる（`calibrate` が両方引く）
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

## skill(作法)と MCP(口)の使い分け

- **hook**(この文書の上半分): 常に自動で効く判定。人も Claude も意識しない
- **MCP**(`shikii mcp`、[mcp.md](mcp.md)): エージェントやプログラムが「今この判定が要る」と自分で呼ぶ口。型と帯の契約が道具の説明に固定されている
- **skill**(`skills/shikii/SKILL.md`): shikii を使って仕事をする時の作法。いつ判定を呼ぶか、帯にどう従うか、正解と線と状態は人のもの、tune の採用は人の「意味は同じ」の後、本文を外に出さない判定。手順と判断だけで数字や判定の論理は書かない(CLI と MCP を指す)。参照は `references/`(帯と領収書、spec の書き方、正解・較正・審判の約束)に分けて必要な時だけ読ませる。リポジトリのルートが plugin(`.claude-plugin/plugin.json`)なので、Claude Code の plugin として入れれば skill が載る。CI は `scripts/check-skill.sh` で前書きと参照の実在を見る
