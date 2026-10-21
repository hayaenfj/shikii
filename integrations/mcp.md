# 接続例: MCP（Claude Code などのエージェントから道具として使う）

`shikii mcp` は `shikii serve` と同じ中身を、stdio の MCP サーバーとして出す。推論は持たず、後ろの決定モデル（`--local` / `--hosted`）に聞く。
HTTP のハンドラを内部で呼ぶだけなので、`serve` と挙動は同じ（記録・正解・自動較正・store も共通）。

## 入れ方（Claude Code）

`shikii --init`（`shikii init`も同じ）が、既製の4問・wiki・画面への入口と、Claude Code(`.mcp.json`)、Cursor(`.cursor/mcp.json`)、OpenCode(`opencode.json` の `mcp`、`type: local`)、Codex(`~/.codex/config.toml` の `[mcp_servers.shikii]`)に同じ項を足す(既にあれば触らない)。使っていないツールの設定ファイルも作られる(要らなければ消すか `.gitignore` へ。2 回目の init はあるものを触らない)。`opencode.jsonc` を使っている人には別に `opencode.json` が生まれる(OpenCode は両方を読む)。Codex の設定に `mcp_servers` がどんな書き方でもあれば足さない(末尾に足すと TOML の二重定義になるため。手で足す行を表示する)。`CLAUDE.md` だけのプロジェクトには `AGENTS.md` を作らない(OpenCode が `CLAUDE.md` を読まなくなる)。手で書くならプロジェクトの `.mcp.json`:

```json
{
  "mcpServers": {
    "shikii": {
      "command": "shikii",
      "args": ["mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"]
    }
  }
}
```

`shikii` は `make install` で `~/.local/bin/shikii` に入る（PATH に無ければ絶対パスで）。ログは stderr に出る（stdout は通信路）。

## 道具

| 道具 | 何をするか | 引数 |
|---|---|---|
| `shikii_specs` | spec の一覧。id、型、質問、閾値、受け付ける正解（選択肢 id / yes, no / 段階の番号）。**最初に呼ぶ** | なし |
| `shikii_decide` | spec の質問を決定モデルに聞く。帯（act / ask / discard / abstain）、最上位の答え、確率、trace_id | `spec`, `inputs`（object）, `tainted`（任意） |
| `shikii_label` | 判定に正解を付ける。`!x`（x ではなかった）も可。正解が `eval.min_n` に届けば自動で較正し、結果に `calibrated: true` と報告が付く | `trace_id`, `truth`, `spec` / `weight` / `source`（任意） |
| `shikii_status` | spec ごとの記録・正解・(model, hash) の状態 | なし |
| `shikii_tune` | spec の聞き方の候補(`variants:` + 提案役が作る N 個)を正解の貯金で採点。候補ごとの数字、holdout、採用できるか | `spec`, `propose`（任意。`mcp` を `--proposer URL` 付きで起こした時だけ） |
| `shikii_wiki` | 同じ仕事の会話から出典付きの教訓を育てる。取り込み・検索・点検 | `operation`（`query` / `ingest` / `lint`）、`sources` / `title` / `lesson` / `applies_to` / `id` / `replaces` / `correction` / `query` |
| `shikii_review` | 会話と結果から根拠付きで仮採点。人の抜き取りと訂正例を参照 | `operation`（`query` / `submit`）、`source_id` / `trace_id` / `truth`（`yes` / `no` / `unknown`）/ `reason` |

| `shikii_reflect` | 最大2会話の仮採点・wiki整理・記憶と訂正の観察をまとめて確認・保存 | `operation`（`query` / `submit`）、`reviews` / `wiki` / `memory` |

`propose > 0` を使うには `shikii mcp` を `--proposer URL` 付きで起こす(`.mcp.json` の例には無い)。

`shikii_wiki` は 0.3 で追加した。文章は現在のエージェントが書き、別のモデルは使わない。置き場はhookと同じ各仕事の `.shikii/` で、MCP サーバーを起こしたプロジェクトだけを扱う。`query` は関連する教訓と未整理の会話を返し、`sources: [出典ID]` を渡すと原文を読める。`ingest` で題・教訓・適用する場面・出典を保存し、教訓がない会話は `sources` だけで整理済みにできる。教訓は仮説で、正解や較正には数えない。[作法の詳細](../skills/shikii/references/wiki.md)。

`shikii_review` は、まず `query` で同じ仕事の原文・採点対象・人の訂正例を確認してから `submit` で仮採点を保存する。人の正解は登録せず、人への確認を全件増やさない。抜き取りは shikii が選び、画面からの人の訂正を次の `query` に戻す。[採点の詳細](../skills/shikii/references/review.md)。

0.4のhookは `shikii_reflect` を案内する。`query` は原文を重ねず、採点対象・関連する教訓・人の訂正例を一緒に返す。`submit` は仮採点4件・wiki整理2件・記憶の仮評価4件まで。記憶の`result`と判断・確認結果を結び、訂正付きの引用には`correction_result`（followed / repeated / not_applicable / unknown）も保存する。wikiの`correction`は人の返答の原文と次に変える行動を持つ。0.4.1の訂正の追跡は0.4.1で利用できる。返答の `complete` と項目ごとの `error` を確認し、失敗分は直して再送する。成功分は保持し、同じ内容の再送は重複させない。[JSONと手順](../skills/shikii/references/review.md)。

**採用の道具は無い。** 言い換えてもまだ同じことを聞いているかは人にしか分からないので、採用は画面（`shikii serve`）で人が「意味は同じ」を押した時か、端末の `tune --write` だけ。エージェントは候補を作って測るところまで。

誤り（知らない spec、spec に無い正解、無い trace）は JSON-RPC のエラーではなく、道具の結果の `isError: true` と本文で返す。エージェントが読んで直せるように。

## エージェント側の使い方（例）

1. `shikii_specs` で spec の id と正解の候補を知る
2. 判断の場面で `shikii_decide` を呼び、`band` を見る。`act` なら進める、`ask` なら人に聞く、`discard` なら何もしない
3. 結果が分かった時（実際に変更したか、利用者が選んだか）に `shikii_label` を `trace_id` 付きで呼ぶ
4. 線は溜まれば自動で引かれる。`shikii_status` の `keys[].state` が `collecting`（`n` / `min_n` 付き）→ `calibrated` になるのが見える

## テスト

`cmd/shikii/mcp_test.go` の 3 本（initialize と tools/list、decide → label → 自動較正 → act の一周、誤りは isError / 未知の道具とメソッドは JSON-RPC エラー）。
実装前に落ちるのを確認してから GREEN（2026-10-17）。
