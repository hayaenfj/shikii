# 既製の判定

自分のプロジェクトでは `.shikii/decisions/` に写してそのまま使うか、`shikii new --from <この repo>/decisions/<id>.yaml` で写して書き換える（spec の置き場は `.shikii/decisions/`。`shikii init` が作る）。正解の種（`testdata/`）があるものは、`shikii calibrate <spec> --labeled <種> --local <URL>` でその場で線が引ける。wiki の細則（`wiki/specs/<id>.md`）は、人が正解を付ける時と審判が読む手引き。

| spec | 何を決めるか | 型 | 入力 | 正解の種 | 細則 |
|---|---|---|---|---|---|
| `prompt-intent-v2.yaml` | AI への依頼の種類（変更 / 質問 / レビュー / 操作 / 依頼ではない） | choice | `message`、迷った時 `previous` | 62 件（英 39・日 23）`testdata/prompt-intent-labeled.json` | [wiki](../wiki/specs/prompt-intent.md) |
| `prompt-intent.yaml` | 同じ判定の v1 の文言（比較用。v2 を使う） | choice | `message`、`previous` | 同上 | 同上 |
| `prompt-kind.yaml` | 依頼がファイルを変える依頼か（Claude Code の hook が使う 5 択。正解は作業ツリーの変化から自動で付く） | choice | `request`、迷った時 `previous` | 実運用で溜まる（`~/.shikii/agent/.shikii`、`shikii hook install` の後） | [手引き](../integrations/claude-code.md) |
| `same-issue.yaml` | 2 つの不具合報告が同じ不具合か（重複判定） | noul | `report1`、`report2` | 32 組（日本語、境界 8 組は重み 0.5）`testdata/same-issue-labeled.json` | [wiki](../wiki/specs/same-issue.md) |
| `same-inquiry.yaml` | 2 つの問い合わせが同じ案件か | noul | `inquiry1`、`inquiry2` | 32 組（日本語）`testdata/same-inquiry-labeled.json` | （まだ） |
| `relatedness-level.yaml` | 2 つの報告の関係の強さ（段階） | score | `report1`、`report2` | （まだ） | （まだ） |

聞き方の候補（`variants:`）が付いているものは、`shikii tune <spec> --labeled <種> --local <URL>` で候補を測れる。勝った候補は wiki の主張の下書きになる。
