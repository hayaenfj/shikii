# examples

較正ブロックの無い、書き始めの設定ファイル。数字は付いていない(測っていない)。`decisions/` の方は実際に使っているもので、一部は較正まで書いてある。

| ファイル | 型 | 用途 | 見どころ |
|---|---|---|---|
| `prompt-intent.yaml` | choice | AI への依頼の種類(5 択) | `scripts/demo.sh` が使う。`decisions/prompt-intent-v2.yaml` の測る前の姿 |
| `inquiry-kind.yaml` | choice | 問い合わせの種類(ビジネス) | `pii: true` と `send_to: [local]`。本文を外の AI に出さない(手元の記録には残り、担当者が画面で答えられる) |
| `clause-conflict.yaml` | noul | 契約条項の矛盾(法律) | act が「自動で直す」ではなく「人のレビュー待ちに入れる」であること |
| `drone-landing-abort.yaml` | noul | 着陸の中止判断(ロボット・ドローン) | act / ask / discard / abstain がそれぞれ何を意味するかを、コメントで 1 行ずつ |

どれも `shikii decide examples/<file>.yaml --local <url> -i key=value …` で動く。較正が無いので、答えは高くても `ask` 止まり。
正解を溜めて `shikii calibrate --write` を回すと、初めて `act` が出る。
