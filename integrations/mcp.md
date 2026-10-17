# 接続例: MCP（Claude Code などのエージェントから道具として使う）

`shikii mcp` は `shikii serve` と同じ中身を、stdio の MCP サーバーとして出す。推論は持たず、後ろの決定モデル（`--local` / `--hosted`）に聞く。
HTTP のハンドラを内部で呼ぶだけなので、`serve` と挙動は同じ（記録・正解・自動較正・store も共通）。

## 入れ方（Claude Code）

プロジェクトの `.mcp.json`:

```json
{
  "mcpServers": {
    "shikii": {
      "command": "shikii",
      "args": ["mcp", "--specs", "decisions", "--store", ".shikii", "--local", "http://127.0.0.1:8080"]
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

誤り（知らない spec、spec に無い正解、無い trace）は JSON-RPC のエラーではなく、道具の結果の `isError: true` と本文で返す。エージェントが読んで直せるように。

## エージェント側の使い方（例）

1. `shikii_specs` で spec の id と正解の候補を知る
2. 判断の場面で `shikii_decide` を呼び、`band` を見る。`act` なら進める、`ask` なら人に聞く、`discard` なら何もしない
3. 結果が分かった時（実際に変更したか、利用者が選んだか）に `shikii_label` を `trace_id` 付きで呼ぶ
4. 線は溜まれば自動で引かれる。`shikii_status` で `collecting n/50` → `calibrated` になるのが見える

## テスト

`cmd/shikii/mcp_test.go` の 3 本（initialize と tools/list、decide → label → 自動較正 → act の一周、誤りは isError / 未知の道具とメソッドは JSON-RPC エラー）。
実装前に落ちるのを確認してから GREEN（2026-10-17）。
