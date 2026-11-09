[English](mcp.md) · **日本語**

# MCP接続

`shikii mcp`は、CLIのプロジェクト操作をstdioのMCPで公開します。`shikii serve`と同じHTTPハンドラとプロジェクトの記録を使い、判定や差し込み先の確認には設定したモデルへ問い合わせます。ログはstderrへ出し、stdoutはMCPの通信に使います。

## 導入と接続

shikiiを使うプロジェクトで`shikii --init`を実行します。プロジェクトのデータ・問い・wiki・作業記録とskillを準備し、対応するエージェントを設定します。人の既存設定は保持します。`AGENTS.md`と`CLAUDE.md`には書き込まず、節も消しません。以前の節があれば、手動で片付ける対象を案内します。

Claude Codeのプロジェクトの`.mcp.json`には、次のように登録できます。

```json
{
  "mcpServers": {
    "shikii": {
      "command": "shikii",
      "args": ["mcp", "--local", "http://127.0.0.1:8080"]
    }
  }
}
```

PATHに無ければ本体の絶対パスを使います。初期化はOpenCodeのプロジェクト登録も設定します。Codexは共通の`~/.codex/config.toml`を使います。

```toml
[mcp_servers.shikii]
command = "/Users/you/.local/bin/shikii"
args = ["mcp", "--local", "http://127.0.0.1:8080"]
```

すべてのツール呼び出しで、作業フォルダの絶対パスを`project`へ渡します。この機械で初期化したプロジェクト内のフォルダが対象です。共通サーバーはプロジェクト外で起動しても、呼び出しごとに対象を解決できます。`--store`付きで起動したサーバーは、`project`が無い時にその保存先を使えます。共通登録では`project`を指定します。[手引き](../docs/guide.ja.md)と`shikii doctor`で設定を確認してください。

## 共通手順から始める

0.30.1では、どのツールを使う前にも`shikii_wiki`の説明で共通手順を全文読みます。初期化は参照先を伝え、すぐに必要な規則を保持します。知識や会話の引用は証拠であり、操作の許可にはしません。自分の判断を正解として登録しません。

1. `shikii_wiki`の`operation:"prepare"`へ、現在のhookの識別子を使った`work:{query,session_id,turn_id}`を渡します。hookなしではエージェントの仕事として`work:{query,request}`を渡しますが、人の合意にはできません。
2. 今回の依頼、正確な条件、知識、保留の原資料、`how_to_record`を読み、receiptを保持します。追加が必要な時も同じreceiptを使い、1回につき検索・作業案・条件の1方式を渡します。
3. 作業と確認を行います。繰り返す判定では、`shikii_specs`でspecと正解の候補を読んでから`shikii_decide`を使い、帯に従います。`abstain`は未判定です。
4. 終了時は`shikii_wiki`の`operation:"record"`へ`work:{receipt,result:{changed,checked?,evidence?,remaining?},...}`を渡し、必要な観察・記事・振り返りをまとめます。例の結果を転記せず、実際の結果を記録します。
5. `complete`・`storage`・`errors`・`pending`と振り返りの残件を確かめ、失敗した参照だけを再送します。保存済みの証拠を重ねて書く必要はありません。

同じreceiptの`collective.notice_from`は、変わらない固定説明を以前の配送へ結ぶ場合があります。以前の応答を読めなければ、同じreceiptとqueryに`reasoning:{depth:focused}`を付けて準備し直し、全文を戻します。条件・例外・不足と同版の原文回復は引き続き必要です。

プログラムからは、`structuredContent`があれば優先し、`isError`と必要な状態を保持します。無ければ元の`content`を残します。本文はMarkdownやJSON、画像、資料への参照の場合があります。AIへは完全な結果を1回渡し、構造化結果に無い追加の内容も残します。サーバーは両形式を保持します。[MCPの知識配送](../docs/mcp-knowledge.ja.md)と[記録の契約](../skills/shikii/references/recording.md)を参照してください。

## ツール

どのツールも前述の`project`を受け取ります。引数の完全な現行仕様は、各ツールのschemaを参照してください。

| ツール | 目的 | 主な引数 |
| --- | --- | --- |
| `shikii_wiki` | 準備・保存、出典付きの知識と作業記録 | `operation`、`work`。個別の検索・控え・部分編集・照合・再利用など |
| `shikii_specs` | 判定前にspecのID・問い・正解の候補を読む | `project` |
| `shikii_decide` | 設定した判定と帯・証拠・traceを返す | `spec`、`inputs`、任意の`tainted` |
| `shikii_label` | 以前のtraceへ実際の人の答えや確認済みの結果を登録する | `trace_id`、`truth`、任意の`spec`・`weight` |
| `shikii_status` | 記録・正解・較正の状態 | `project` |
| `shikii_tune` | 保存された正解で問いの候補を比較する | `spec`、任意の`propose` |
| `shikii_review` | 人の正解と分けた、証拠に基づく仮評価 | `operation:"query"`または`"submit"`と評価の欄 |
| `shikii_reflect` | 原文を読み、必要な評価・wiki・記憶の整理を保存する | `operation:"query"`・`"place"`・`"submit"`と必要な欄 |

MCPからの正解登録は`source:mcp`となり、人の答えには自動で変わりません。本人が実際に言ったことや確認した結果だけを送り、エージェントの評価は送りません。知識を読んだことや保存に成功したことだけで、有用・成功・許可とは評価しません。聞き方の採用には別の意味の確認が必要で、提案や採点だけでは採用しません。

ツールの失敗は`isError:true`とエラー本文で返します。未知のJSON-RPCツールやメソッドは通信のエラーとして返します。利用側は両方を保持してください。詳しい操作と現在の上限は、古い固定件数ではなくリンク先のskillを参照します。

## 検証

MCPの検査は、初期化、共通手順、判定と正解登録の境界、出典と版、数値の精度、エラー、結果の全値、同receiptの配送と回復を確認します。`go test ./cmd/shikii -run '^TestMCP'`で実行でき、リポジトリ全体の検査はCLI・HTTP・作業記録の境界も確認します。
