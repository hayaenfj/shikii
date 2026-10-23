# 接続例: Claude Code と Codex の hook(`shikii hook`)— AI 自身の考え方を、考える前と考え終わった後の 2 点で測る

業務の判定(問い合わせの振り分けなど)はエージェントが MCP で呼ぶ([mcp.md](mcp.md))。これとは別に、**会話の記録・記憶の参照と、AI自身の考え方の判定**をshikiiへ接続するのがこのhook。測る瞬間は 2 つだけ: 人の依頼を受けて考える前と、考え終わってターンを閉じる時。Claude Code / Codex の hook が記録を開始する。新形式の着手前の判定は、その案内を受けた通常のエージェントが文脈を整理してから呼ぶ。

ツール呼び出しごと(Bash や Edit の直前)の hook は持たない。コマンド 1 つ 1 つの分類は AI の考え方ではなく作業の雑音で、1 依頼に何十回も走って依頼まわりの判定を沈める(実測: コマンドの判定 4606 件に対し依頼の判定 84 件)。許可の是非は Claude Code の permission と deny の規則が既に見ている。

**Claude Code と Codex の hook**(両方とも同じ形の hook を持つ。`install` は両方の設定に書く)。Cursor / OpenCode には `shikii init` で MCP と skill が入るが、考え方を自動で測る口は無い(付けるなら下の「他のエージェントに付けるとき」)。帯(act / ask / discard / abstain)・線(較正)・receipt の言葉は [bands.md](../skills/shikii/references/bands.md)。

測るのは 4 つ(spec は [`claude-code/specs/`](claude-code/specs/))。前の 2 つは見立て、後の 2 つは自己評価:

| spec | いつ | 問い | 記録・確認の担当 |
|---|---|---|---|
| `prompt-kind` | 考える前(UserPromptSubmit) | この依頼で AI は結局何をするか(edit / explain / investigate / run / decide) | **結果から自動**: 次の依頼が来た時(無ければターンの終わり)に作業ツリーの中身が変わっていれば edit、変わっていなければ !edit(「edit ではなかった」の部分正解、重み 0.7)。ターンの終わりの !edit は次の人の依頼まで持ち越し、裏の作業が後から変えたら(通知の後の Stop)同じ判定に edit を付け直す(新しい正解が勝つ) |
| `needs-plan` | 始める前（UserPromptSubmitから案内） | この依頼を受けたAIは、作業を始める前に人へ確かめるべきだったか（進め方や範囲で、人が決めることが残っていたか） | 通常のAIが合意・未決事項を原文付きで整理し、人が画面で答える |
| `changes-in-scope` | 考え終わった後(Stop) | このターンで変えたファイル(依頼の時と今の作業ツリーを比べた一覧)は、依頼の範囲の中だったか。何も変えていなければ聞かない | AIが根拠付きで仮採点し、人は抜き取りと保留を確認。正解と較正は別 |
| `done-as-asked` | 考え終わった後(Stop) | 依頼・変わったファイル・最後の返答を見て、頼まれたことを終えたか(人がもう一度言い直すことは残っていないか) | AIが根拠付きで仮採点し、人は抜き取りと保留を確認。正解と較正は別 |

依頼 1 回につき判定は 3〜4 件(ツール呼び出しの数に依らない)。`status` の「1000 判定あたりの人の判断」が使うほど減り、act の判定に receipt(人が確かめた件数と精度)が付く。これは判定モデルの運用の数字で、会話の記憶が役立った実績や訂正が減った割合とは分ける。

**会話も採点も線も、プロジェクトごと。** `shikii --init`で有効にした各プロジェクトの `.shikii/` に `decisions/` `wiki/` `guards/` `records/` `labels/` `sessions/` を置く。初期化が準備し、その後の記録はhookが行う。Git の中ではリポジトリのルート、外では cwd が仕事の単位。同名の別フォルダは混ぜず、同じ repo のサブディレクトリや symlink は同じ仕事になる。`.shikii/` の共有先へのリンクは使わない。hook は `/.shikii/` を Git のローカルな `info/exclude` に足し、私的な会話を `git add -A` に巻き込まない。状態取りでも `.shikii/` を除き、shikii 自身の記録をファイル変更の結果に数えない。

`~/.shikii/agent` は共通の型・道具・プロジェクトの索引だけを持つ。画面の :8096 はプロジェクトの入口を並べ、`/p/<実パスのハッシュ>/` から選んだ仕事の記録だけを読む。同じ URL 配下の API が採点・正解・教訓もその仕事へ保存し、成績を合算しない。hook が示す答え合わせのリンクも直接その仕事を開く。旧共通 store の会話や線は自動では引き継がない。

## 着手前の文脈を整理する

新形式の `needs-plan` は、今回の依頼・合意した方針・未決事項を入力にする。hookは現在の依頼と同じセッションの直前のやりとりを控え、通常のエージェントへ `shikii hook plan query / submit` を案内する。人は入力欄を埋めず、別のモデルも要らない。エージェントは原文の引用を添えて整理し、不明な項目は不明とする。画面も依頼・合意した方針・未決事項を分けて表示する。 目的と文脈を選ぶ作法はagent向けの英語skillが持ち、プログラムは同じセッションの直近4件の原文と、agentが指定した検索語に対応するwikiを返す。検索語を推測して方針を決める処理は持たない。wikiは出典と版を確認し、今回の合意とは区別して引用する。

合意には人の発言の根拠を求め、AIが提案しただけの案は合意にしない。引用が実在しても整理の意味は仮説であり、実行許可や人の正解にはしない。両項目が不明な場合は判定を見送る。線がない間は予想を記録するだけで、予想から確認の助言を出さない。モデル待ちに依頼が進んだ場合は古い結果を記録しない。同じ整理の再送は最初の判定を保持する。[JSONと根拠の規則](../skills/shikii/references/planning.md)。

既存のプロジェクトの質問は自動で書き換えない。新形式では合意・未決事項の入力が不足する過去の正解を流用せず、新しい入力への答え合わせを集める。

## 普段の会話を次の依頼に活かす

「まだ終わっていない」「そこは頼んでいない」と普段どおり伝えるだけで、元の依頼・AI の最後の返答・その後の人の発言を一組で残す。次に似た依頼が来ると、同じプロジェクトの題と適用条件が近い教訓を最大2件、着手前のAIへ短く引用する。教訓が無い話題は、未整理の似た会話を最大1件返す。画面での答え合わせや別のモデルは要らず、判定モデルの線が無い間やモデルが止まっている時も、この参照は働く。

記録するのは原文(依頼・返答・次の発言それぞれ最大 1500 バイト)で、訂正か追加依頼かを機械が確定したものではない。「追加で英訳して」も追加依頼として読める形で残し、発言が無いことは成功に数えない。会話の解釈を正解ラベルや精度の数字には入れない。引用より今回の依頼と規則が優先される。

記録は `.shikii/records/feedback/<プロジェクトの実パスのハッシュ>.jsonl`。通常の判定・正解とは別で、保存と検索は手元で行い、引用は利用中のコーディングエージェントの文脈に渡す。別の判定モデルや提案役には送らない。同名の別 repo の会話は混ぜず、同じ repo のサブディレクトリや symlink は同じ仕事として参照する。依頼と次の発言を結ぶのは、同じセッション・同じ仕事で返答した後だけ。通知、サブエージェント、返答前の追記は結ばない。読むのは末尾 4 MB のうち最近 256 件までで、依頼の語の重なりで探す。渡す記憶はJSONとして囲み、エスケープ後も全体1800バイトまで。同じセッションに同じ版を一時間は繰り返さず、更新された版はすぐ届ける。

0.4では、採点とwiki整理の未処理が2会話になった時、または1件が一日経った後の依頼で、短い振り返りの案内と出典IDだけを渡す。エージェントは `shikii_reflect` または `shikii reflect query / submit` で原文・採点対象・既存の教訓・人の訂正例を一度に確認し、一括保存する。原文を案内へ重ねない。再利用する教訓が無ければ出典IDだけで整理済みにできる。MCPがまだ更新されていないセッションにも、hookが実行ファイルの絶対パスでCLIを案内する。

生成領域は `.shikii/wiki/lessons/<仕事のハッシュ>/`。`index.md` は索引、`log.md` は追記の履歴、`revisions/` は前の版。原資料は `records/wiki-sources/<仕事のハッシュ>/` に不変の形で残す。教訓の `id` を渡すと更新でき、`replaces` で古い教訓を置き換える。出典が消えた・変わった教訓は参照から外す。教訓は常に仮説で、正解・線・`patterns/` の状態は変えない。

`query` で教訓や出典を読み、`lint` で出典・版・リンクを点検する。Stop でも一日一回だけ点検して索引のずれを修復する。既存の教訓への統合・更新を優先する(件数の固定の上限は無い。0.5.0 で撤廃)。自動の記憶では整理済みの原文を教訓へ重ねない。引用は今使っているエージェントに渡し、新たなモデルには送らない。「知識」画面は、選んだ仕事の教訓と原文を表示する。

## 届いた記憶と訂正の観察

実際に届けた引用と版を依頼IDに結び、普段のreflectで使った判断と確認結果を保存する。記憶の役立ち方は helpful / unused / harmful / unknown。0.4.1の訂正の追跡は、教訓の `correction: {source_id, quote, action}` に指定した人の返答の原文と次の行動を付け、次に届いた時の `correction_result` で followed / repeated / not_applicable / unknown を記録する。訂正という解釈は仮説のままで、原文が存在することから正解や実行許可を作らない。

訂正に沿ったことと役立ったことは別の観察。成功や沈黙だけで改善にせず、不明と未評価を残す。知識画面の数は直近の配送の窓の実績で、記憶なしとの改善率ではない。追加のモデル・人の入力は要らず、既存の `reflect query / submit` とMCP・HTTPで扱う。[JSONと根拠の規則](../skills/shikii/references/review.md)。訂正の追跡は0.4.1で利用できる。

## 入れる・外す

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
shikii hook install          # 何をするかを表示するだけ(install.sh が --yes で流すのと同じ)
shikii hook install --yes    # 実行。新しいターミナルで claude を起こし直すと効く
shikii hook uninstall --yes  # 足した hook(Claude Code 3 つ、Codex 2 つ)だけ外す。各プロジェクトの記録と正解(.shikii/)は残る
which shikii                 # Claude Code がそのまま `shikii` を呼ぶので、PATH に要る
```

**インストール後、利用するプロジェクトで一度初期化する。** `shikii --init` を実行するか、エージェントに「このプロジェクトでshikiiを使いたい」と頼む。初期化は既製の4問・wiki・MCP・作法と画面への入口を揃え、最後に `.shikii/project.json` を保存する。印が無い・別のプロジェクトの印・壊れた印ではhookは動かず、保存先・Gitの除外・モデルの起動にも触れない。初期化失敗では有効にせず、再実行しても既存データを保持する。 鍵が無く、モデル(:8080)が応えなければ、依頼の直後の hook が `shikii up` を裏で起こす(初回はモデルを取ってくるので、その間の数ターンは記録が付かないだけ。llama-server が無ければ hook.log に入れ方を 1 行)。答え合わせの画面(:8096)は、ターンの終わりに答え待ちが出た時に立っていなければ hook が `shikii serve` を裏で起こす。起こす権利は guard(`starting-model` / `starting-page`)の flock で 1 つの hook だけが取り、前に起こした子が生きている間(長いモデルの取得中も)と 10 分の中は起こさない。出力は `~/.shikii/agent/up.log` と `serve.log`。

`install` がすること(2 回目は何も足さない):

1. `~/.shikii/agent/` に共通の型と既製の wiki を置く。各仕事の `.shikii/` は`shikii --init`（`shikii init`も同じ）が準備し、4 つの spec を埋め込みの原本から**無いものだけ**写す。他の仕事の線は写さず、既にその仕事で育てた spec は触らない。エージェントの作法(skill)を `~/.claude/skills/shikii` に置き直す(`shikii init` と同じ物。これで業務の判定の立ち上げもエージェントが進められる)。llama-server があればモデルを裏で起こす
2. `~/.claude/settings.json`(Claude Code)と `~/.codex/hooks.json`(Codex)の `hooks` に `UserPromptSubmit` と `Stop` の 2 つ、Claude Code にはさらに `SubagentStart`(0.7.3。サブエージェントが始まる時に、親の依頼に近い教訓を同じ形で届ける。Codex には無い event なので足さない)(command は **実行ファイルの絶対パス** + ` hook <event>`。エージェントの shell の PATH に `shikii` が無くても動く。実行ファイルを動かしたら `shikii hook install --yes` を流し直すと古いパスの handler が外れて新しいものが入る。timeout は UserPromptSubmit 10 秒、Stop 15 秒。matcher は無い)。古い版が足した `PreToolUse` の shikii の hook は外す(他の hook と鍵は残す)。Codex は新しい hook を一度人に確かめさせる: Codex の中で `/hooks` を開いて trust する(しないと hook は飛ばされる)。初めて settings.json に手を入れる時(install でも uninstall でも)、shikii の hook が入っていない直前の内容を `settings.json.bak-shikii` に控える(1 回だけ。元から settings.json が無ければ作らず、install が作った settings.json は控えない)

固定の規則(オプションは無い): 置き場は各仕事の `.shikii/`(decisions/ wiki/ guards/ records/ labels/ sessions/)、モデルは推奨が TypeSafe の Jev(環境変数 `TYPESAFE_API_KEY` がある時。`SHIKII_MODEL=local` なら手元)、鍵が無ければ手元の `http://127.0.0.1:8080`(`shikii up`)、答え合わせの画面は `:8096`(プロジェクトの `serve` は 8095)。既製の spec は全部 `send_to: [hosted, local]`、`pii: false` で、鍵があれば依頼・返答・wiki の教訓が TypeSafe へ送られる(2026-10-23 の決定。手元の記録にも残る)。1 回の判定は Jev で約 0.2 秒、手元のモデルで 0.1〜0.6 秒(2 つの問いは並行)、hook 1 回(起動と記録を含む)で 0.6〜1 秒。これが依頼の直後とターンの終わりに 1 回ずつ足されるだけで、ツール呼び出しには何も足されない。モデルが無ければ待たずに黙る。

答え合わせの画面は hook が立てる。手で立てるなら:

```sh
shikii serve --store ~/.shikii/agent --port 8096 --local http://127.0.0.1:8080
```

## 影の期間 → 帯で振る舞う

shikii は線(較正)が無いうちは `act` を出さない。判定に基づく助言も線が引けてから出す。過去の会話は正誤を判定せず引用として返せるので、線が無い間から着手前に届く。ツールは一切止めない。ターンの終わりには「答え待ち N 件 → URL」が 1 行出る(下)。

**聞き方を試すのも機械**(wiki の lint に当たる、ゆっくりした周期): spec に `variants:`(人が書いた候補の言い方)があり、人の正解が `eval.min_n` 以上で、前回の自動 tune から正解が 50 件増えていれば、Stop が `shikii tune`(`--write` 無し)を切り離した別プロセスで回し、結果を `wiki/runs/` と `hook.log` の `auto-tune …` の行に残す。候補が無い spec では何もしない(試す物が無い)。採用は人が意味を確かめてから(画面か `tune --write`)。前回の数は `tuned-<spec>` に控える。候補は画面の「聞き方」の欄で人が自分の言葉で書ける(短い名前と質問の言い換え。多択なら選択肢の説明も「id: 説明」の行で)。送ると `POST /v1/tune/variant` が spec として検証してから `variants:` に足す(採点も採用もしない)。YAML を開かなくても、人が書く → 機械が 50 件ごとに測る → 人が採用する、が画面で回る。

線を引くのは機械: 人の正解(入力ごとに 1 件。同じ依頼文を 50 回答えても 1 件)が `eval.min_n`(50)に届くと、画面(`serve`)で答えた直後はその serve が、そうでなければ次のターンの終わりに hook が(4 spec とも。prompt-kind の結果の正解は画面を通らないのでここでしか引けない)、線を引いて spec に書く。hook は `shikii calibrate --write` を切り離した別プロセスで起こす(文言を変えた後の聞き直しが長くても hook の timeout に掛からない。同じ spec は 10 分に 1 回まで)。結果は `hook.log` の `auto-calibrate …` の行。そこから hook が振る舞いを変える。

| spec | 判定 | hook が出すもの |
|---|---|---|
| どれでも | 線が無い(影) | 判定からの助言は出さない(過去の会話の引用は別) |
| needs-plan | act | エージェントに文脈 `shikii: this request looks like one to agree on an approach with the user before changing anything (needs-plan p=0.88; line on 61 truths, human-checked precision 0.96). Consider EnterPlanMode or a one-line confirmation first` |
| needs-plan | discard(合わせなくてよい) | 何も出さない |
| changes-in-scope | discard(確信して「範囲の外」) | 端末の人に 1 行 `shikii: this turn's changes look outside what you asked for (changes-in-scope p(yes)=0.12…). Check the changed files before going on` |
| done-as-asked | discard(確信して「終えていない」) | 端末の人に 1 行 `shikii: this turn looks unfinished against what you asked (done-as-asked p(yes)=0.15…). Something may be left to ask for again` |
| changes-in-scope / done-as-asked | act(範囲の中 / 終えた) | 何も出さない |
| needs-plan | ask(線はあるが迷った) | 端末に 1 行 `shikii: unsure (done-as-asked yes p=0.62). Answer later: http://127.0.0.1:8096/?spec=done-as-asked&trace=…` |
| changes-in-scope / done-as-asked | ask | 次の会話で AI の仮採点へ回す。人は抜き取りと保留を確認 |
| prompt-kind | 何でも | 何も出さない(結果の正解のためにツリーのハッシュを控える) |
| (Stop) | 線が無くても | 新しく答え待ちになった判定があれば 1 行(件数と URL)。無ければ黙る |

**ツールは止めない。許可の判定(allow / ask / deny)も出さない。** 考える前の問い(needs-plan)はエージェントへの文脈になり、考え終わった後の問いは端末の人への文になる: Stop で文脈を返すと会話が勝手に続いてしまうので、続けるかどうか(「まだ終わってないよ」)は人が決める。hook が出すのは「黙る / エージェントに文脈 / 人に 1 行 / 止める検査に引っかかった時だけ Stop を止めて理由(decision: block)」の 4 つ。

文は英語。`~/.claude/settings.json` の `language` が日本語なら、同じ文を日本語で併記する。他の言語は文は英語のままで、記録の `display` に spec の `display.<lang>` の質問と答えの名前(あれば)と言語だけが入る。例: `shikii: this turn looks unfinished against what you asked (done-as-asked p(yes)=0.15)` の下に `shikii: このターンは頼んだことを終えていないように見える(done-as-asked p(はい)=0.15)`。記録(`records/<spec>.jsonl`)にも `display` として言語・質問・答え・出した文が残るので、「何と言われて何をしたか」を後から辿れる。はい / いいえの問い(noul)の答えの名前は p(yes) が 0.5 未満なら「いいえ」側(終えていない、範囲の外)。英語の設定なら英語だけで、`display` は付かない(spec に `display.<lang>` が無く出した文も無い時も付かない)。

ターンの終わり(Stop)には、そのセッションの判定で人の答えを待つもの(迷った判定と、act の 20 件に 1 件の抜き取り。線が無い間は、生の確率が spec の `thresholds.ask` 以上の判定が「迷った」になり、それ未満の discard は記録だけ)が新しく増えていれば 1 行だけ出す: `shikii: 3 of this session's decisions wait for your answer: http://127.0.0.1:8096/`。画面が立っていなければ hook が裏で起こし、行は常にこの URL。同じ数を毎ターン繰り返さない。URL の `?spec=<id>&trace=<trace_id>` で開くと、その 1 件が先頭に来る(答え済み・列に無い trace は元の順のまま)。

## 長い返答を画面で読む

判定モデルへの入力は従来どおり 1,500 バイトまでとし、人が画面で読む原文は `records/text/<trace_id>.json` に別に保存する。原文は欄ごとに 64 KB までで、UTF-8 の文字を割らない。画面の未回答・回答済みの両方と `shikii review query` はこの原文を読める。hook の引用は引き続き短く渡し、長い原文をそのままモデルや hook の文脈へ足さない。

原文は元の入力のハッシュと冒頭に結び付け、判定記録・正解・較正を書き換えない。原文の保存上限や読み出せない場合は画面に示す。以前の版の記録は原文が残っていないことがあるため、保存上限付近の欄にその旨を示す。元の会話に保存済みの冒頭と一致する原文がある時は、元の判定を変えずに画面用の原文として補える。

## AI の仮採点と抜き打ち確認

Stop の範囲・完了の判定は全件を人への答え待ちにせず、次の人の返事と同じターンの判定を結び付ける。UserPromptSubmitは2会話ずつ振り返りをまとめ、`shikii_reflect` または `shikii reflect query / submit` で原文と作業結果から仮採点し、根拠不足は `unknown` にする。通知・別の仕事・古い Stop の扱いは会話記録と同じ。

仮採点は `records/reviews/<spec>.jsonl` に原資料と根拠付きで残し、正解や較正へは混ぜない。人は実行・見送り・迷いの帯によらず無作為の抜き取りと保留を確認する。最初と不一致が出た種類は 5 件に 1 件、直近の無作為の確認が 20 件続けて一致したら 20 件に 1 件。保留の確認は無作為の成績とは分ける。人の訂正は次の採点役へ返す。仮採点を保存した後で人の答えに合わせて書き換えることはできない。

画面は人の返事を示し、AI の答えを選択肢で強調しない。AI の採点と根拠は「くわしく」で開いて読める。件数と人との一致、無作為の確認の一致、抜き取りの頻度も表示する。仮採点前は全件を人に求めず、過去の版で記録した判定の扱いは維持する。採点とwiki整理の案内は一つで、原文はqueryで一度読む。保存の一部が失敗した場合はcomplete:falseと項目ごとのerrorを返し、成功分を保持して再送できる。既存の MCP セッションでも hook が示す絶対パスの CLI が使える。

## 失敗しても止めない

モデルが無い・5 秒で返らない・store に書けない → hook は exit 0 で何も出さず、理由を `~/.shikii/agent/hook.log` に 1 行書く。hook が作業を止めることはない。Claude Code 側で `shikii` が見つからない(PATH)時は hook.log も書かれないので、`which shikii` と Claude Code の `--debug` で見る。

## 訂正を止める検査にする(0.7.3)

repo と CLAUDE.md を読めば分かる教訓は行動を変えなかった(2026-10-23、5 課題 × 2 条件で 4 同点)が、知り得ない決まりは届けば 6 回中 6 回守られ、止める検査は 6 回中 6 回直させた(数字はガイド)。機械で確かめられる訂正は `.shikii/guards/<id>.yaml` の「止める検査」にする。種類は `paired-files`(片方が変わったらもう片方も)、`forbidden-words`(足した行に使わない語)、`commit-body`(そのターンのコミットの本文に含む語)の 3 つで、新しいオプションは無い。Stop で、依頼の時の一覧との差(変わったファイル)、依頼の時の中身との差(足した行)、依頼の時に控えた HEAD からのコミットに当て、引っかかれば `decision: block` と理由を返す。`stop_hook_active` の Stop では止めず、直ったかを `records/guards.jsonl` に記録する(Claude Code の hooks reference によれば連続 8 回で block を無視するので、1 回で止めるのをやめる)。一覧や HEAD が取れない時は通す。

草稿はエージェントが `shikii guard add`(JSON を標準入力)で無効のまま置き、人が `shikii guard enable <id>` で有効にする。enable はその種類の負例で実際に止まる(RED)・正例で通る(GREEN)のを見せてから `enabled: true` にし、止まらなければ有効にしない。`shikii guard try` は今の作業ツリーに当てて見せる(コミットは見ない)。数は `shikii status` の guards の行。作法は [skill の guards.md](../skills/shikii/references/guards.md)。

## 中の形

```
UserPromptSubmit ─ shikii hook user-prompt-submit ─ 前の依頼の pending に結果の正解(今のツリーと比べる)
                                                     直前の返答と今回の人の発言を一組で残す。同じ仕事の似た依頼の会話を引用
                                                     decide prompt-kind。新形式のneeds-planは原文付きの文脈整理をエージェントへ案内
                                                     └ sessions/<id>.json に request と作業ツリーのハッシュ、<id>.files に作業ツリーの一覧を控える
SubagentStart ──── shikii hook subagent-start ───── 親の依頼に近い教訓をサブエージェントの文脈へ(親の控えは変えず、配送は別の ID で記録。Claude Code だけ)
Stop ───────────── shikii hook stop ──────────────── 残った pending に結果の正解(変わっていれば edit で片付け、変わっていなければ !edit を付けて持ち越す)
                                                     依頼の時の一覧と今の一覧を比べて「変わったファイル」→ decide changes-in-scope, done-as-asked(並行。同じ依頼で 1 回だけ)
                                                     └ last_assistant_message を控える(次の人の発言との対応・needs-plan の文脈)。答え待ちの要約を 1 行。4 spec の正解が min_n に届いていれば線を引く
```

- transcript は読まない(形が文書化されていない)。セッションごとの控えは `.shikii/sessions/<session_id>.json`(request、previous、pending、asked、judged。7 日で消える)と `<session_id>.files`(依頼の時の `git ls-files --stage` の一覧。4 MB まで)。Claude Code は hook を並列に走らせる(Stop と次の依頼が重なる)ので、控えは flock の中で 読む → 変える → 書く(tmp に書いて rename)。錠は短く持ち(モデルの呼び出しは錠の外)、2 秒待っても取れなければ錠なしで進む
- 作業ツリーの状態は、repo のルートで(サブディレクトリから起こしたセッションでも repo 全体を見る)、仮の index に `git add -A`(.gitignore は尊重、未追跡も入るが `.shikii/` は除外)して `git write-tree` した tree の id(ハッシュ)と、その index の一覧(ファイルごとの blob の id)。HEAD や index は入れないので、commit / stash / checkout だけでは変わらず、ファイルの中身が変わった時だけ変わる。新しい blob は仮の objects に書いて消すので、未追跡ファイル(`.env` など)の中身が repo の `.git/objects` に残らない(一覧にも中身は入らない。path と id だけ)。全体で 3 秒を超えたら(.gitignore の無い巨大な node_modules など)正解を付けず、変わったファイルも分からない扱い。一覧(ファイルごとの mode と blob の id。chmod だけの変更も数える)は 4 MB まで読んで止める。git の外では prompt-kind の正解と changes-in-scope は無く、done-as-asked の files は `(unknown: no git listing for this turn)`。限界: 並走する別のエージェントやフォーマッタの変更も「変わった」に数える。`git add` は repo の clean filter(Git LFS など)を通常どおり走らせる(普通の `git add` と同じ副作用。filter を持つ repo では知っておく)
- 変わったファイルの一覧は問いに最大 30 件(残りは `... and N more`)。何も変えていなければ changes-in-scope は聞かず、done-as-asked の files は `(none)`
- 短い返事(40 字未満。「ok」「yes go ahead」)は依頼を置き換えず前の依頼に添える(ターンの終わりの問いが「ok」を依頼として読まないように)。短い返事でも新しいターンなので judged は戻り、「ok」の後のターンもまた評価される(依頼の文は「前の依頼 / ok」で別の入力)
- 裏の作業の完了通知(Claude Code がサブエージェントや背景のコマンドの終わりを「ユーザーの発言」として流す `<task-notification>` / `[SYSTEM NOTIFICATION - NOT USER INPUT]`)は依頼ではない: 考える前の 2 問は聞かず、依頼・一覧・pending も置き換えない(実測で記録の半分近くがこれだった)。通知の後に作業が続いたら、そのターンの終わりで元の依頼に対して「範囲か・終えたか」をもう一度判定する(本人の選択: 長い裏作業の最後の状態を測る。1 つの依頼に通知の数だけ判定が残る)
- 重なり: Stop(依頼 A)がモデル待ちの間に次の依頼 B が控えを書いても、Stop A の印(judged、previous、返答の控え)は A の控えにしか当たらない(依頼ごとの識別子も比べるため、同じ文を繰り返した依頼も区別する)。pending は自分が見た trace だけ settle、一覧は先頭行の依頼のハッシュが合う時だけ比べる。`agent_id` の付いた UserPromptSubmit(サブエージェント等)は人の依頼ではないので控えを変えない
- hook.log の 1 行は 400 バイトで切る(モデルの誤りの文に応答本文が付いてくることがある)
- Claude Code 側の契約(公式の hooks reference、2026-10-18): hook は stdin に `session_id` `cwd` `prompt` `stop_hook_active` `last_assistant_message` を受け、stdout の `hookSpecificOutput.additionalContext`(UserPromptSubmit)とトップレベルの `systemMessage` を返せる。Stop で `hookSpecificOutput` を返すと会話が続くので返さない
- Codex 側の契約(learn.chatgpt.com/docs/hooks、2026-10-19): 同じ event 名と stdin の鍵(`Stop` にも `last_assistant_message` と `stop_hook_active`)に、Codex だけの `turn_id` と `model` が足される。Stop の出力は JSON か無出力(shikii は出す時は JSON、出さない時は無出力)。shikii は Codex と Claude Code を見分けない(両方同じ 2 つの問い)

## claude-jev から移る人へ

1. 先に claude-jev を置いた場所の `uninstall.mjs`(例: `node ~/dotfiles/claude-jev/uninstall.mjs --yes`)で jev を外す(両方入っていると同じ event で 2 回判定される)
2. Node は要らない。置き場は `~/.claude/jev/` → `~/.shikii/agent/`。古い記録(`~/.claude/jev/decisions-store`)は引き継がない(spec の文言が違うので線の材料にならない)
3. 固定の閾値(0.75 で進め、0.60 で確認)は無い。線が引けるまで何もせず、引けたら帯で振る舞う
4. prompt-intent → prompt-kind(同じ 5 択)。コマンドの分類(jev の PreToolUse)は無い: 測るのは依頼の見立てと終わりの自己評価だけ。モデルの振り分け(jev-router)も無い

shikii の前の版(2026-10-18)の hook から移る人: `shikii hook install --yes` をもう一度流すと PreToolUse の shikii の hook が settings から外れ、新しい 2 つの spec が写される。`~/.shikii/agent/decisions/command-effect.yaml` と `edit-in-scope.yaml` とその記録は消さない(要らなければ手で消すか別の場所に移す)。

## 他のエージェントに付けるとき

必要なのは 3 つだけ:

1. 判定したい場所で `shikii decide <spec> --json -i key=value` を呼び、`band` と `trace_id` を受け取る
2. 結果が分かる場所(終了時・取り消し時・利用者が選んだ時)で `shikii label <trace_id> <truth> --source outcome` を呼ぶ。「それではなかった」しか分からなければ `!option`、弱ければ `--weight 0.7`
3. 送ってはいけない文脈(個人情報・本番データ)では `--tainted` を付ける。spec の `send_to` に `local` が無ければ abstain になる

## skill(作法)と MCP(口)と hook の使い分け

- **hook**(この文書): AI 自身の考え方を、依頼の直後とターンの終わりに記録する。新形式の着手前の判定は、通常のエージェントが原文付きの文脈を整理して呼ぶ
- **MCP**(`shikii mcp`、[mcp.md](mcp.md)): 業務の判定を、エージェントやプログラムが「今この判定が要る」と自分で呼ぶ口
- **skill**(`skills/shikii/SKILL.md`): shikii を使って仕事をする時の作法。いつ判定を呼ぶか、帯にどう従うか、正解と線と状態は人のもの、tune の採用は人の「意味は同じ」の後、本文を外に出さない判定。CI は `scripts/check-skill.sh` で前書きと参照の実在を見る
