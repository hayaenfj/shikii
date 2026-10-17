# 決定モデルの運用フレームワーク — 設計メモ

作成: 2026-10-16
状態: 構想。実装前。(以後、実装しながら追記している)

> 初めて読む人向けの読み物版は [`why-shikii.ja.md`](why-shikii.ja.md)。この文書は数字と出典を残すための記録で、用語は説明なしに使う。

## 1. 一言で

決定モデル（Jev / d1-ultra / OpenJev など、文を生成せず typed な確率だけを返すモデル）を
**業務に組み込んで運用し続ける**ための層。モデルの呼び出しは他人のもの（System One 形式）を
そのまま使い、自分たちは**判定のライフサイクル**だけを所有する。

```
定義 → 実行 → 記録 → ラベル収集 → 較正 → 契約テスト → 配備 → ドリフト監視
```

売りは「較正」と「ラベルの輪」。モデルの読み出し部分は作らない。

新規設計である。既存の claude-jev / had-scorer（§2.4）は設計の根拠として踏まえるだけで、
コードは流用しない。

## 2. 背景

### 2.1 決定モデルとは

- 入力: `state`（テキスト / JSON / 画像）と 1 つ以上の質問
- 出力: 質問ごとの確率分布。生成トークン 0、1 forward pass
- 質問の型は 3 つで、各社・llama.cpp・OpenRouter が同じ形（System One 形式）に収束した
  - `noul`: 命題が真である確率 p
  - `choice`: 選択肢ごとの確率 + confidence（分布の集中度 `(p_max − 1/n)/(1 − 1/n)`）
  - `score`: 順序付きレベルの確率 + 期待値 `Σ(level × p)`（レベル間の値を取る）
- 作り: LLM の最終位置のロジットから選択肢トークンだけを読んで softmax（= 分類ヘッド利用）
  + 蒸留 SFT + キャリブレーション学習（TypeSafe は RLCD と呼ぶ）。詳細は各社とも非公開

### 2.2 2026-10 時点で既にあるもの（作らないもの）

| 層 | 既にあるもの |
|---|---|
| ホスト API | TypeSafe Jev、Liquid d1 / d1-ultra、OpenAI gpt-6-luna-decisions、Perplexity、Cloudflare Clef、Microsoft Decision-1 … |
| 共通の口 | OpenRouter Decisions API（`POST /api/alpha/decisions`、14 モデル） |
| ローカル実行 | llama.cpp `/v1/systemone`（2026-10-02 追加）。既存クライアントはベース URL を変えるだけ |
| オープンウェイト | Julia-1（144M）、Laya（421M）、Kev-4B（Apache 2.0）、lev、OpenJev（27B、日本語可、CC BY-NC）、Clef（27B、Apache 2.0） |
| 普通の GGUF で同じ機構 | GitLuci/local-typed-decisions、allcodernet/llama.cpp-decide、NotXf1le/choosekit |

**結論: 「ロジット読み出しで typed decision を返す」部分は商品化済み。ここに参入しない。**

### 2.3 全員が免責している穴（作るもの）

ベンダー・llama.cpp・OpenRouter の文書が揃って書いていること:

- 「確率はあなたのデータに対して較正されていない」
- 「閾値はモデル間・質問間で持ち越せない」
- 「confidence は分布の集中度であり、正しさではない」

言うだけで、これを扱う道具を誰も出していない。

### 2.4 踏まえた既存の実装（参考であり、部品ではない）

このフレームワークは**新規に設計する**。claude-jev と had-scorer のコードは流用せず、
そこで同じ構成が別々に手で組まれていた事実と、そこで得た教訓だけを設計の根拠にする。

| 要素 | claude-jev（Claude Code の hook） | had-scorer（別プロジェクトの記録の自動リンク） |
|---|---|---|
| 質問の定義 | 5 択 choice。はい/いいえより当たると 12 例で確認 | yes/no。質問文を「同一の案件か」に固定 |
| state の組み立て | 報告と差分を**必ず一緒に**渡す。文章だけでは判定しない | 2 つの記録をラベル付きのテンプレで並べる |
| 較正 | なし（Jev 任せ） | `σ((Δ−bias)/T)`。質問ごと・モデルごと。78 ペアで当てた |
| 閾値 → 行動 | ≥0.75 自動 / 0.6〜0.75 確認 / <0.6 捨てる | ≥0.7 auto / ≥0.5 suggest |
| 質問と定数の対の固定 | — | 契約テスト（プロンプト逐語 + 定数の境界 4 点） |
| 送ってよいかの統制 | tainted セッションでは外部に送らない | ローカルのみ |
| 誤報の計測と撤退 | post-tool-use 判定を「当たり 0 件」で外した | ドメイン中立な質問文を「境界と関連が逆転」で棄却 |

この表の行がそのまま部品になる。

had-scorer が残した教訓（フレームワークが**構造で**防ぐべきもの）:

1. 較正は質問専用。similarity の較正を relevance に流用すると全クエリがほぼ 0 点になった
2. プロンプトと較正定数は一対。片方だけ変えると静かに壊れる
3. 較正では直せない失敗がある（質問文を変えたら関連ペアの下限 Δ=2.54→1.80、境界ペア 1.82 と逆転）
4. チャットテンプレの不一致はモデルの地力以前に判定を壊す（Qwen の `<think>` が第 1 トークンを占めて yes/no のロジットが残滓になった）
5. モデルを替えたら必ず再較正。80B に 0.6B の定数を当てると全部 1.00 で境界まで自動リンクした

## 3. 範囲

### 3.1 所有するもの

- 判定の**仕様**（宣言的な 1 単位）
- **ランタイム**: 仕様 + state → 帯（act / ask / discard）。エンドポイントの切り替え、送信可否の統制
- **記録**: 全判定を生の確率付きで残す
- **ラベル収集**: 利用者の操作を正解ラベルとして拾う
- **較正**: ラベルから (仕様, モデル) ごとに定数を当てる。Brier / ECE / 信頼度図
- **契約テスト**: 仕様・定数・プロンプトの対を固定するテストの生成
- **ドリフト監視・撤退**: 当たりが落ちた判定の warning

### 3.2 所有しないもの

- モデルの呼び出し形式（System One に乗る。独自形式を作らない）
- モデルの学習・微調整
- ロジット読み出しの実装（llama.cpp / 各社 API に任せる）
- 生成モデルの評価（別の問題）

## 4. 判定の仕様（Decision Spec）

1 判定 = 1 ファイル。モデル非依存の部分（質問・state・閾値）と、モデル依存の部分（較正）を
分けて持つ。較正は `(spec の内容ハッシュ, モデル ID)` に鍵付けし、どちらかが変わると無効になる。

```yaml
# decisions/same-issue.yaml
id: same-issue
version: 1
type: noul                          # noul | choice | score

question: |
  Do these two bug reports describe the same underlying defect
  (one is a duplicate of the other, or both are symptoms of the same root cause)?
  Answer "no" if they merely concern the same screen, feature or technology.

state:
  template: |
    Report 1: {{ report1 }}

    Report 2: {{ report2 }}
  required: [report1, report2]       # 欠けていたら判定しない（文章だけで判定しない、の一般化）

policy:
  send_to: [local, hosted]           # local | hosted。tainted セッションは hosted を自動で外す
  pii: possible                      # state に個人情報が入り得るか。possible なら記録に本文を残さない

thresholds:                          # 名前付き。確率の生値を業務コードに漏らさない
  act: 0.70                          # ≥ act  → 自動で実行
  ask: 0.50                          # ≥ ask  → 利用者に確認
                                     # < ask  → 捨てる

eval:
  labeled: testdata/same-issue-labeled.json   # (state, 正解) の組
  min_n: 20                                   # これ未満なら較正しない

calibration:                         # 較正コマンドが生成。手で書かない
  - model: ggml-org/Kev-4B-GGUF:Q4_K_M
    spec_hash: 39e5d9072ae0c8c2      # question + state.template + type のハッシュ
    method: platt
    bias: -0.404
    temperature: 0.874
    fitted_on: testdata/same-issue-labeled.json
    n: 32
    brier: 0.031
    ece: 0.095
    fitted_at: "2026-10-17"
```

`choice` と `score` の場合:

```yaml
type: choice
options:
  - id: change_request
    description: コードや設定の変更を求めている
  - id: question
    description: 説明や調査を求めていて、変更は求めていない
  - id: review
    description: 既存の差分の評価を求めている
  - id: operation
    description: コミット・配備・実行などの操作を求めている
  - id: other
    description: 上のどれでもない
order_bias_check: true               # 較正時に選択肢の順列を回して順序バイアスを測る
```

```yaml
type: score
levels:                              # 2〜10。低い順
  - 無関係
  - 同じ機能
  - 関連あり
  - 強く関連
  - 同一の不具合
```

設計上の決め事:

- **閾値は名前で持つ**。業務コードは `band == act` を見る。0.7 を見ない
- **選択肢には説明を付ける**。ラベルだけだと誤ルーティングする（llama.cpp のブログも Julia-1 で同じ報告）
- **required を欠いた state は判定しない**。claude-jev の「報告と差分を必ず一緒に」を仕様に昇格
- **較正ブロックは生成物**。手で編集したら契約テストが落ちる

## 5. ランタイム

```
decide(spec, inputs) → Decision
```

```go
type Decision struct {
    SpecID     string
    SpecHash   string
    Model      string
    Endpoint   string            // どこに送ったか
    Raw        Distribution      // モデルが返した確率
    Calibrated Distribution      // 較正後。較正が無ければ Raw と同じで Calibrated=false
    Band       Band              // act | ask | discard | abstain
    Confidence float64           // choice/score のみ
    TraceID    string
    At         time.Time
}
```

振る舞い:

1. `inputs` に `required` が欠けていれば `abstain`（判定していない、を明示）
2. `policy.send_to` とセッションの taint 状態からエンドポイントを選ぶ。tainted で local が無ければ `abstain`
3. System One 形式で送る。ホスト API / OpenRouter / llama.cpp はベース URL の違いだけ
4. `(spec_hash, model)` に一致する較正があれば適用。無ければ生値で返し、`Calibrated=false` を立てる
5. 帯を決める。較正が無い判定は **act を出さない**（ask 止まり）。「較正されていない確率で自動実行しない」を既定にする
6. 記録に残す（§6）

エンドポイントの差し替えは運用の話で、業務コードは触らない。
claude-jev が今 Jev ホスト版に出している判定を、tainted セッションだけ llama.cpp の Kev-4B に落とす、
という使い方が最初の具体例になる。

## 6. 記録とラベル

決定モデルの出力は型が決まっているので、**ラベルが安い**。これが生成モデルの評価と決定的に違う。

記録する: `Decision` 全体 + inputs のハッシュ（本文は spec の `pii` に従う。`never` 以外は本文を保存しない）。

ラベルの拾い方（利用者に「評価してください」とは言わない）:

| 操作 | ラベル |
|---|---|
| `ask` 帯で利用者が選んだ答え | その答えが正解 |
| `act` を後で取り消した | モデルの答えは不正解 |
| `act` が取り消されずに一定期間過ぎた | 正解（弱いラベル。重み付きで扱う） |
| `discard` されたものを利用者が手で実行した | 捨てたのは不正解 |

ラベルは `(spec_id, spec_hash, model, inputs_hash, raw, truth, weight, at)` で溜める。
spec が変われば hash が変わるので、古いラベルは自動で較正から外れる。

試作での追加(2026-10-16): `calibrate` がラベル付きファイルの行をモデルに聞いて得たラベルも、同じ形で store に残す
(`source: file`)。以後の `check` / `drift` はモデルを呼ばずに回る。同じ入力(inputs_hash)は二重に数えず、再実行で増えない。

試作での追加(2026-10-17、**正解の貯金と聞き直し**): 「文言を一語変えるたびに人が判断し直すのか」という問いへの答え。
ラベルの truth は文言にもモデルにも依らない(人が決めた事実)。依るのは raw だけで、raw は取り直せる。そこで

- `policy.pii` に `local` を足した: 本文を手元の記録とラベルに残す(聞き直しの材料)代わりに、`send_to` に hosted があっても外には送らない(`PickEndpoint` が弾く)
- ラベルにも `inputs` を持たせた(pii が never / local の時だけ)。ファイルから較正した正解は記録を持たないため
- `calibrate` は、今の (spec_hash, model) にラベルが無い入力について、store にある spec の全ラベル(古いハッシュ・別モデル)から入力ごとの最新の truth を取り、
  本文があれば今の文言で今のモデルに聞き直して `source: replay` のラベルにする(永続化。以後 check / drift はオフライン)。本文が無い・口が無い・正解が今の spec に無い、はそれぞれ数えて理由を出す
- `PutCalibration` は同じモデルの古いハッシュの線を置き換える(残すと check が stale で落ち続ける。demo で発見)

結果: 文言やモデルの変更は「人の判断」ではなく「数分の計算」になる。人が決めるのは入力 1 件につき正解 1 回。
テストは、文言を変えた後に人のラベル無しで線が引き直される / 本文が無いと理由を言って失敗する / ファイル由来の正解も聞き直せる /
pii: local は hosted に行かない / 同じモデルの古い線が置き換わる、の 5 本。全部、実装前に落ちるのを確認してから GREEN。demo の 6b 段で見える。

## 7. 較正

```
shikii calibrate <spec> --model <id>
```

1. `eval.labeled` と §6 で溜まったラベルのうち `spec_hash` と `model` が一致するものを集める
2. `n < eval.min_n` なら較正しない（had-scorer の 78 ペアは、この下限の目安）
3. 方法
   - `noul`: Platt（bias, temperature）。ラベルが増えたら等張回帰も選べる
   - `choice`: 温度 1 つ + 選択肢ごとのバイアス（vector scaling。試作では `method: vector`。
     実機の Kev-4B で 4 件に当てたら `other` に −2.2、`change_request` に +2.3 が付いた——
     「短い state で other に逃げる」癖がそのまま bias に出る。位置バイアスの順列分離は未実装）
   - `score`: 温度 1 つ + レベルごとのバイアス。期待値は較正後の分布で取り直す
4. 出すもの
   - 定数（spec の `calibration` ブロックに書き戻す）
   - Brier、ECE、信頼度図（確信度 vs 正答率。d1-ultra が出しているグラフと同じ）
   - `thresholds.act` / `ask` での precision / recall
   - **較正では直せない失敗の検出**: 正例の下限と負例の上限が逆転していたら、定数を出さずに「質問文を変えろ」と言う（had-scorer の Δ=1.80 vs 1.82 の再現）。
     重み 1 未満の弱いラベル（境界・「取り消されなかっただけ」等）は逆転の判定に入れない。実測（はい/いいえ 32 組）で
     端に来た負例は全部 w=0.5 の境界ペア（同じ画面の別の不具合、のような例）だった。曖昧と分かっている例で「逆転」と言うのは誤報
   - **identity との比較**: platt / vector は in-sample で甘く見えるので 5-fold CV で測り、生の分布のまま act を満たす（identity）方が Brier でも劣らなければ identity を選ぶ。
     Kev-4B の noul は既に較正されていて（raw Brier 0.046）、platt は CV で悪化（0.057）→ identity
5. 契約テストを生成する（§8）

### 7.1 較正が要るモデルと、検証が要るモデル

モデルは 2 種類に分かれる。実機で確かめた(2026-10-16、ホスト版 jev-1.13.0 に prompt-intent を投げた):

```
raw: {"change_request":1,"operation":0,"other":0,"question":0,"review":0}
```

Jev は RLCD で較正済みなので、確率が**端まで振り切る**。ロジットにすると `Logit(1.0)` はクランプで約 ±13.8 になり、
Platt に入れる Δ がほぼ 2 値に潰れる。bias/T を当てる余地がない。

| 種類 | 例 | 出力の形 | やること |
|---|---|---|---|
| 較正が要る | 普通の GGUF（実例: Bonsai 2 27B を BonJev で読んだもの。生 Brier 0.026 → Platt 0.013、2026-10-17）、小型の決定モデルの生の出力 | 中間の確率が出る | Platt / 等張を当てる(§7 本文) |
| 検証が要る | ホスト版 Jev / d1 など較正済みを謳うモデル | ほぼ 0 か 1 | 「1.0 と言った時の正答率」を測り、**act を許してよいか**を決める |

後者では `calibrate` は定数を出さず、閾値ごとの precision / recall と信頼度図だけを出す(`verify` 相当)。
act を許す判断は、その正答率が `thresholds.act` 以上であることを根拠にする。この時 spec に書く較正ブロックは
`method: identity`(定数なし、検証済みの印)で、鍵は同じく `(spec_hash, model)`。

### 7.2 較正で直るものと、文言で直すもの(実測)

Kev-4B に prompt-intent(5 択)を 62 件(英・日混在、自作ラベル)で聞いた(2026-10-16):

| | v1(選択肢の説明が短い) | v2(説明を具体化、other を「依頼ではないもの」に限定) |
|---|---|---|
| top-1 正答率(生) | 0.682 | **0.903** |
| precision@act / recall@act(生) | 1.000 / 0.156 | 1.000 / **0.732** |
| 誤りの行き先 | 21 件中 19 件が `other` | `other` への誤りは 0 |

v1 の誤りは**順位の誤り**(正解より `other` が上)で、これは bias で押し戻せるが、
その bias は「`other` を常に下げる」ので本当に other な入力を巻き込む。文言を直すと誤り自体が消える。

規則: **混同表で 1 つの選択肢に誤りが集まっていたら、較正の前に文言を直す。** 較正は
「順位は正しいが確信が足りない」(v2 の recall 0.732 の残り)にだけ使う。

較正の鍵は `(spec_hash, model)`。これで次が**構造的に**起きなくなる:

- 質問 A の較正を質問 B に流用する（hash が違う）
- モデルを替えて定数を据え置く（model が違う）
- プロンプトを直して定数を据え置く（hash が変わり較正が失効し、act が出なくなる）

## 8. 契約テスト

試作では**テストファイルを生成するのではなく `shikii check <spec>...` という検査コマンド**にした
(2026-10-16)。言語に依存せず、CI で回せて、spec を読む側と同じコードで検査できる。had-scorer の
`calibration_test.go` の一般化。検査する内容:

- 較正の `spec_hash` が現在のハッシュと一致する(文言を変えたのに定数を据え置いていない → stale)
- `method` が既知で鍵が揃っている(手書きの較正ブロックを弾く)
- ラベル(store + `eval.labeled` の raw 行)で、較正後の act 閾値での precision が `thresholds.act` 以上
- noul は正負が逆転していない

以下は当初案(テストの生成)。中身の対応は上と同じ。

- spec の `question` / `state.template` / `options` の逐語一致（較正を当てた文言から動いていない）
- 較正定数の境界: ラベル集合の「負例の上限」が `ask` 未満、「正例の下限」が `act` 以上、境界例が `act` 未満
- 単調性: 確率が上がれば帯が下がらない
- モデル名と定数の対（起動時に stderr に言うだけでなく、テストで縛る）
- `required` 欠落で `abstain` になる（負例）
- tainted で hosted に送らない（負例。実際にモック endpoint を立てて、呼ばれないことを確かめる）

テストは**生成してリポジトリに置く**。CI で落ちる形にする。

## 9. ドリフト監視と撤退

試作は `shikii drift <spec> --model M --window N`(2026-10-16)。直近 N 件のラベルで、較正時に記録した
Brier から +0.05 以上の悪化、または act での precision が `thresholds.act` 未満なら警告して exit 1。
`ask` 由来のラベルでの一致率(利用者がモデルの最上位を選んだ割合)も出す。

- (spec, model) ごとに、直近 N 件の `ask` 帯での一致率・`act` の取り消し率を出す
- 較正時の Brier から一定以上悪化したら warning。`act` を止めて `ask` に落とすかは spec の設定
- 一定期間の「当たり」（利用者が従った回数）が閾値を割った判定は「外せ」と言う。claude-jev が post-tool-use 判定を 0 件で外したのを自動化する
- 新しいモデルが出たら、同じラベル集合で較正して Brier を並べる。差し替えの判断材料にする

## 10. 検証計画（公開前に通すもの）

抽象が正しいかは、**既にある 2 つの判定を同じ spec で表現し直せるか**で決める。特例が要るなら抽象が間違っている。
既存コードを呼ぶのではなく、同じ判定を新しいフレームワークの上で作り直す。

1. claude-jev の `prompt-intent`（5 択 choice）を spec に書き直す
   - ホスト Jev と llama.cpp Kev-4B の両方で動かす
   - tainted セッションで hosted に行かないことを負例で確かめる
2. had-scorer の `similarity`（noul）を同じ spec に書く
   - `calibrate` が今の bias/T と同等の定数を出す
   - 生成された契約テストが、今の `calibration_test.go` と同じ 4 境界を縛る
   - わざと質問文を 1 語変えて、較正が失効し act が出なくなることを確かめる（RED）
3. 2 本で通ったら、記録 → ラベル → 再較正の輪を閉じる
   - `ask` で選んだ答えが次の `calibrate` に効くこと
4. ここまで来たら公開する価値がある

進捗(2026-10-16): 1〜3 まで通った。
- prompt-intent: v2 の文言で Kev-4B に vector 較正を書き戻し(62 件、CV precision 0.963)
- same-issue / same-inquiry(各 32 組。不具合報告の重複と、問い合わせの同一判定): Kev-4B は両方 platt(生 Brier 0.038 → CV 0.035、0.030 → 0.020)、
  OpenJev は両方 identity。2026-10-17 に題材を差し替えて測り直した(それ以前の はい/いいえ の数字は別の題材のもので、公開物には残さない)
- 輪: claude-jev の hook が `prompt-kind.yaml` で判定 → Stop で作業ツリーの変化から `edit` / `!edit` を付ける、が稼働。
  ラベルの出どころは「利用者が ask で選んだ答え」ではなく**結果**(変更したか)になった。部分ラベル `!x` はこのために足した
- score の帯は `thresholds.level: k` で `P(level ≥ k)`(同日追加)。同一の不具合の組で top 0.54 → P(≥3) 0.95(Ternary-Bonsai-27B、readout)。報告の precision も同じ事象で測る
- 目的の確認(同日): **他人が使えるフレームワーク**が目的で、same-issue や prompt-intent の精度は例にすぎない。
  それに合わせて、鍵も GPU も無しで一周する `scripts/demo.sh`(偽モデル `cmd/fake-systemone`)、OpenRouter の口、英語 README、CI を足した
- choice の順序バイアス測定は `shikii bias`(同日追加)。Kev-4B は flip 0/100、位置のずれ ≤ 0.024 で無視できる。
  Jev の jaggedness に載っている「選択肢順序の偏り」は、学習済みの決定モデルでは出ないことがある——測ってから補正を決める
- 普通の LLM の経路(2026-10-17): BonJev + Bonsai 2 27B を `--local` に向けただけで動いた。prompt-intent-v2 で top-1 0.968(Kev-4B 0.919)、
  はい/いいえ で Platt が初めて CV で選ばれた(差し替え後の same-issue では 0.049 → 0.019)。「較正が要るモデル」(§7.1)の最初の実例。`integrations/bonjev.md`
- 自前のアダプタ(2026-10-17): `cmd/readout`(`shikii-readout`)。普通の GGUF を System One にする。推論は持たず、後ろの llama-server の
  `/apply-template` + `/completion` で選択肢の文字の確率を読む。BonJev と同じ原理だが、fork への FFI も内蔵の較正も持たない。
  「フレームワークは推論を所有しない」(§3.2)は守ったまま、所有する範囲に「形式の変換」だけ足した形。数字は `integrations/readout.md`
- 記録の掃除(2026-10-17): `shikii gc`。消すのは「古くてラベルの無い記録」だけで、ラベル付きの記録とラベルは較正の材料として残す。
  既定は表示だけ(`--apply` で書き換え)、書き換えは一時ファイル + rename。「捨てる」操作なので変異 7 つで RED を確認した
- 正解の貯金と聞き直し(2026-10-17): 文言やモデルを変えても、人が付けた正解は本文と一緒に残り、`calibrate` が新しい (hash, model) に聞き直す(§6)。
  「少し変えるたびに人が判断し直すのでは」という指摘が発端。較正の鍵の厳しさはそのままに、戻す費用を人からモデルに移した
- 残り: `ask` 帯で利用者が選んだ答えを拾う口、等張回帰

## 11. 危ないところ

- **フレームワークはライブラリより失敗しやすい**。抽象が合わないと誰も使わない。§10 を飛ばさない
- **大きく作らない**。spec 形式 + `decide` + `calibrate` の 3 つが動くまで、他は作らない
- **市場が生まれて 1 か月**。System One 形式の上に薄く乗る。独自の呼び出し形式を作ると llama.cpp や OpenRouter の更新に置いていかれる
- **ラベルの偏り**。`ask` 帯からしかラベルが来ないと、確信の高い領域の較正が検証できない。`act` の取り消しと時間経過の弱いラベルで補う。それでも偏るので、較正結果に「ラベルの分布」を添える
- **弱いラベルの重み**。「取り消されなかった = 正解」は甘い。重みを下げる。重みは較正結果の信頼区間に反映する
- **OpenJev は非商用**。ローカル既定は Kev-4B か Clef（Apache 2.0）

## 12. 確かめていないこと

2026-10-16 時点で確かめたもの:

- llama.cpp 0.6.0（brew、build 11429）の `/v1/systemone` は **専用の決定モデル（Kev-4B）で動いた**。普通のチャット GGUF で動くかは未確認
- **日本語**: Kev-4B（英語のみと表記）は日本語の依頼文で `other` に確率が逃げやすく（v1 文言で top-1 0.68）、日本語の はい/いいえ の組でも
  正例 3 件が p≈0.6 前後に沈んだ。文言の具体化で大きく改善（0.90）するが、日本語で本気で使うなら OpenJev（日本語対応、非商用）か再評価が要る
- **位置バイアス**: Kev-4B で flip 0/100、位置のずれ ≤ 0.024。学習済みの決定モデルでは無視できる（`shikii bias` で測ってから決める）
- **弱いラベル + Platt**: 32 ペア（境界 8 件を w=0.5）で素直に当たった。逆転の判定からは弱いラベルを外す必要があった（§7）
- **較正済みモデルへの Platt**: 改善しない（CV で Brier 悪化）。identity と CV で比べる仕組みが要る（§7.1）

- **普通のチャット GGUF での `/v1/systemone`**: 本家 llama.cpp では通らない。Qwen3-8B Q4_K_M で `501 This model is not a decision model`。
  ロジット読み出しを System One 形式で出す BonJev を間に置けば通る(2026-10-17、Bonsai 2 27B で確認。`shikii` の変更なし)。
  自前の読み出し経路は本家 llama.cpp しか無い環境向けの保険になった
- **較正されていないモデル + Platt**: 効く。Bonsai 2 27B の same-issue 32 組で生 Brier 0.049 → CV 0.019。OpenJev(両方 identity)との対比が実機で出た
- **飽和した確率への Platt**: 素の Newton 法は発散する(2026-10-17、`shikii-readout` の Bonsai 2 で T=5e-7 まで飛び in-sample Brier 0.02 → 0.11)。
  CV の比較が identity に戻して事故は防いだが、`calib.Fit` を損失の下がる歩幅まで縮める直線探索付きに直した(RED: その 32 件で再現、GREEN: Brier 0.018)。
  文字の読み出しは確率が端に寄るので、この形は普通の LLM では常態になる
- **prompt の配置は較正の鍵の一部**: `shikii-readout` で state を選択肢の前から後ろに移しただけで、Ternary-Bonsai-27B の top-1 が
  0.919 → 0.968、日本語が 0.870 → 1.000 に動いた(2026-10-17)。同じ重みでも配置で順位が変わる。アダプタは配置名を応答の `model` に
  付けて(`+state-last`)、較正の鍵を配置ごとに分ける。順位の改善は較正では得られない(§7.2)ので、普通の GGUF では配置と文言を先に測る
- **Julia-1（144M、多言語）**: 使えない。prompt-intent-v2 で top-1 0.129（5 択の当て推量 0.2 未満）、はい/いいえ は逆転 22 件(当時の 32 組)。
  「日本語対応」の表記だけでは選べない。測って落とす

まだ確かめていないもの:

- GitLuci/local-typed-decisions 等の成熟度（スター・保守）
- 等張回帰が Platt に勝つ n（手元の 32〜66 では試していない）
- `!edit` の弱いラベルが数百件になったとき、`drift` がどれだけ誤報を出すか（結果ラベルの「変えなかっただけ」の割合）

## 13. 参照

### 一次情報・公式

- [Liquid AI: d1-ultra](https://www.liquid.ai/blog/d1-ultra)
- [ggml-org: New in llama.cpp: Decision Models](https://huggingface.co/blog/ggml-org/decision-models-in-llamacpp)
- [OpenRouter: openrouter-decisions skill](https://openrouter.ai/skills/openrouter-decisions)
- [OpenRouter: Jev Tutorial](https://openrouter.ai/docs/guides/community/jev-tutorial)

### 解説・まとめ

- [TypeSafe Jev project reference (gist)](https://gist.github.com/pjburnhill/adf8d28efcad9df037bfdece178ef965)
- [Firecrawl: What Is Jev?](https://www.firecrawl.dev/blog/what-is-jev)
- [benchlm: What Is Jev? and Its Limits](https://benchlm.ai/blog/posts/what-is-jev)
- [lalten.org: From Token Likelihoods to Choice Probabilities](https://lalten.org/drafts/llm_choice_models/)（温度 + 位置バイアスの較正の実例）

### 普通の GGUF で同じ機構を実装している OSS

- [GitLuci/local-typed-decisions](https://github.com/GitLuci/local-typed-decisions)
- [allcodernet/llama.cpp-decide](https://github.com/allcodernet/llama.cpp-decide)
- [NotXf1le/choosekit](https://github.com/NotXf1le/choosekit)
- [ChezRD/bonjev](https://github.com/ChezRD/bonjev) — Bonsai(PrismML の ternary Qwen)向け。`/v1/systemone` を出すので `shikii` からそのまま使えた(2026-10-17)

### 踏まえた手元の実装（参考のみ。流用しない）

- had-scorer（別プロジェクト、非公開）— noul 読み出し + Platt 較正 + 2 タスク分離、契約テストの原型、読み出し方式の最初の設計
- `~/dotfiles/claude-jev/README.md` — 閾値運用・taint 統制・撤退判断の実例
