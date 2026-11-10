# shikii(閾) — 詳しい使い方(日本語)

shikii は、エージェントをあなたのプロジェクトに合わせて育てる道具です。そのプロジェクトで言われたこと・決めたこと・確かめたことを `.shikii/` に貯め、次の仕事の前にエージェントへ渡します。機械で確かめられる訂正は止める検査にし、作業の前と後の手順を確かめ、作業の跡を作業記録に残します。正解が貯まれば、判断を人に聞かずに任せてよい線を引きます。名前は閾値の「閾」で、コマンドの名前も `shikii`。

この手引きは、短い[日本語の README](readme/README_ja.md) で省いたことを全部書きます。全コマンド、HTTP API、モデルのつなぎ方、業務の判定の作り方です。英語の手引きは [guide.md](guide.md)、設計は [decision-ops-framework.ja.md](decision-ops-framework.ja.md)、構造の説明は [architecture.ja.md](architecture.ja.md)。モジュールは `github.com/hayaenfj/shikii`、ライセンスは Apache-2.0 OR MIT(どちらかを選択。[`LICENSE`](../LICENSE))。

- [インストール・アップデート](#インストールアップデート)
- [プロジェクトごとの有効化](#プロジェクトごとの有効化)
- [普段の会話で使う](#普段の会話で使う)
- [止める検査](#止める検査)
- [作業の前と後の手順](#作業の前と後の手順)
- [作業記録](#作業記録)
- [確認結果を原資料にし、知識を検索・見直す](#確認結果を原資料にし知識を検索見直す)
- [画面と棚卸し](#画面と棚卸し)
- [判定モデル](#判定モデル)
- [業務の判定を任せる](#業務の判定を任せる)
- [コマンドの一覧](#コマンドの一覧)
- [serve の HTTP API](#serve-の-http-api)
- [構成](#構成)

## 知識を準備し、考えた内容を記録する（0.19.0）

0.30.1では、MCPのツールを使う前に`shikii_wiki`の説明で共通手順を全文読みます。同じreceiptの検索では固定案内を以前の配送へ結ぶ場合があり、以前の応答が無ければ同じreceiptとqueryに`reasoning:{depth:focused}`を付けて全文を戻します。条件・出典の版・原文回復、エラー状態や追加の内容を保ち、AIへ完全な結果を1回渡す手順は[MCPの知識配送](mcp-knowledge.ja.md)を参照してください。

依頼本文`task`と話者・不足状態を示す`task_context`は応答上限でも保持し、収まらない時は保存前にエラーを返します。同じレシート内で配送済みの本文や抜粋を再利用する時は、変更のない要約・適用条件などを`metadata_from`で以前の配送番号`delivery`へ結び付けます。参照化の処理では未分類・再確認・反例・不足状態を残し、条件が変われば通常どおり配送します。応答全体の予算処理で本文や説明が省略された時は未読・未解決として扱い、不足案内と同じ版の取得先を確認します。全文と完全な説明の回復には、十分な予算または個別の同版問い合わせを使います。追加のモデルや設定は不要です。[参照の対応と上限](../skills/shikii/references/recording.md#bounded-reading-and-memory-upgrades)を確認してください。

`shikii work prepare`へ検索語`query`と現在のhookの`session_id`・`turn_id`を渡します。返された会話・知識・未処理の証拠・`how_to_record`を読み、`work record`へレシート`receipt`と判断・観察・出典付きの記事を渡します。出典のID・記事の版・原資料の時刻はshikiiが保持します。差し込み先が曖昧なら候補の本文を読んで判断します。`complete`・`errors`・`pending`と振り返りの残件を確認し、失敗分は同じレシートで再送します。

hookが無い時は`query`とエージェント自身が書いた`request`を使えますが、人の合意としては扱えません。MCPは`shikii_wiki`のprepare/recordと`work`オブジェクト、HTTPは同じ本文を`POST /v1/wiki/notebook`へ渡します。従来の個別操作も使えます。更新後はセッション・画面・MCPを再起動し、同じwikiや進行中のセッションを書くプロセスを0.27.0以上へ揃えてください。条件に合う手順結果の選択には0.25.0以上を使います。


## 現在の知識・反例・手順の見直し

エージェントは、出典を読んだ上で本文の引用へ`passages`を付け、現在使う内容・過去の経緯・再確認する内容・反例の候補を分けられます。引用に条件と理由を残し、同じ記事の版へ結び付けます。`work prepare`と`consult`はこの位置付けを`orientation`として先に返し、未分類の範囲や前提ファイルの変化も示します。新しい日付というだけで過去の条件を上書きしません。知識の画面からも引用と条件を一緒に編集できます。

`consult`は、分類した反例と出典付きの失敗結果を、通常の記事候補とは別に探します。失敗ごとに今回の案との関連を調べるため、新しい無関係な失敗が以前の反例を隠しません。返すのは反例の候補と当時の条件・出典で、案への賛否はエージェントが判断します。

失敗と修正のある実行結果を保存すると、`work record`の`procedure_reviews`と通常の手入れへ、元の手順の原文候補・修正候補・証拠を返します。エージェントが現在の条件と照合し、`notes`で記事を直すと、返答に最新版のレビューが含まれます。保存した変更を確認し、その最新の`ref`で`maintenance:[{ref,reason}]`を保存して見直しを完了します。記事の修正だけでは完了せず、古い`ref`は拒否されます。変更不要の場合は元の`ref`で確認した理由を残します。同じ証拠で見直しを重ねず、新しい結果では再開します。人の追加採点や成功の自動ラベルは作りません。

本文の引用は最大8件で、一意に一致し、互いに重ならない必要があります。引用は600バイト以内、条件と理由は各400バイト以内です。省略は保持、空配列は消去です。本文を変えて引用が一致しなくなる場合は、同じ版の確認付きで引用も一緒に直します。詳しい形は[記録の手順](../skills/shikii/references/recording.md)を参照してください。この保存形式を使う前に、同じwikiを書く全プロセスを対応する本体へ更新して再起動してください。以前の本体は新しい分類を検証できず保存を拒否しますが、出典と履歴は残ります。

## インストール・アップデート

インストールは[日本語 README](readme/README_ja.md#始め方)の curl の 1 行。ソースから入れる場合は clone した shikii のフォルダで `sh install.sh`。curl の 1 行は最新の版を取ってくるので Go は要らない。ソースから入れる時は Go 1.27 以上と Git が要る。

```sh
shikii update
```

GitHub Releases の最新安定版から、利用中の OS・CPU 向けの本体と readout を取得する。Go・curl・GitHub CLI は不要。SHA256SUMS と添付のハッシュ、起動する本体の版を確認し、実行中の shikii の実体と同じフォルダへ更新する。PATH の別の場所へ入れ直すことはない。現在の版と同じか古い配布物には置き換えない。

本体のライセンスと第三者の通知は、実行ファイルの隣の `shikii-licenses/` へ残す。配布物の欠落やハッシュ不一致では置き換えず、書き込み途中の失敗では本体と readout を元へ戻す。同時更新は拒否する。保存先の書き込み権限が無い場合は、その旨を表示する。

この実行ファイルを使う hook が導入済みなら、新しい本体から hook と作法を更新する。導入していない hook は追加しない。hookを使わずMCPやskillだけで利用する場合は、更新後に初期化済みのプロジェクトで`shikii --init`を再実行するとskillを更新できる。既存データを保持し、hookは有効にしない。本体の更新後、導入済みhookの再反映だけに失敗した場合は本体の更新済みを明示し、そのhookの再反映を`shikii hook install --yes`で再試行できる。起動中の画面・MCP は停止しないため、再起動して新版へ切り替える。更新した skill と hook の案内文は新しいエージェントのセッションで使う。既存プロジェクトの質問文と較正は保持し、新しく初期化するプロジェクトには新版の既製プロンプトを用意する。

**0.16.0への更新:** 同じwikiを使うすべての機械を更新し、エージェントのセッション・画面・MCPを再起動する。旧版は新しい判断理由・前提の情報を持つ記事を変更済みと判断し、検索から外し、wikiへの保存も拒否する。保存済みの記事は壊れない。

**0.8 系から 0.9 系(0.9.0 以上)への更新:** 0.9.0 で種類と場面を付けた記事は、0.8 系の本体からは「変更あり」に見え、0.8 系は wiki への保存を全部断る(記事は壊れない)。更新したら、開いているエージェントのセッション(とその MCP)を開き直す。同じ wiki を使う機械は全部 0.9.0 以上にする(どれかの機械で 0.9.2 を使うなら全部 0.9.2 以上。0.9.1 以前は、0.9.2 が書いた記事(決まった語が英語)を「変更あり」と見なす)。更新の最後にもこの注意が出る。0.9.2 から shikii は AGENTS.md に書かない。アップデートは、前の版が登録済みの各プロジェクトの AGENTS.md に書いた shikii の節だけを外し、ファイルの残りは残す(区切りの行だけで囲まれた節がちょうど 1 つある時だけ。repo の外を指すリンクや、区切りの行が 1 つ・3 つ以上の時は触らずに理由を出す。CRLF のファイルは CRLF のまま)。

**0.6.0 以前からの更新は、初期化済みの各プロジェクトの `.shikii/` の中を全部消して初期化し直す**(プロジェクトの決定)。以前の会話・教訓・仮採点・正解・較正・作業記録は消え、既製の問い・wiki の雛形・プロジェクトの登録ファイルは作り直される。残したいものは先に控える。0.7.0 以降からの更新は全部残す。0.6.0 から更新する時は、消去は古い本体の最後の手順の中で走り、その表示は出ない(0.6.0 が出力を捨てる)。先に `--yes` 無しの `shikii hook install` で消す対象を確かめる(作業記録がある時は件数も出る)。

## プロジェクトごとの有効化

インストール後、利用するプロジェクトで `shikii --init`(`shikii init` も同じ)を一度実行する。エージェントに「このプロジェクトで shikii を使いたい」と頼んでもよい。既製の問い(hook の 4 問・wiki の 2 問・配送の 1 問 lesson-applies)・wiki・止める検査と作業記録の置き場・MCP・作法と画面への入口を揃え、最後に `.shikii/project.json` を書く。hook はこのファイルがある場所だけで動き、未初期化の場所には保存先も Git の除外も作らない。サブフォルダからも同じプロジェクトルートを使い、再初期化しても既存の設定・会話・正解を保持する。`shikii doctor` で状態を確かめる。

`.shikii/` の中身:

| 場所 | 中身 | コミット |
|---|---|---|
| `decisions/` | 決定モデルへの問い | する |
| `wiki/` | 教訓・条件・以前の版 | する |
| `guards/` | 人の訂正から作った止める検査 | する |
| `labels/` | 人と結果が付けた正解(モデルを替えても残る資産) | する |
| `records/` | 会話・判定・採点・配送・観察・検査の結果。判定のたびに増える | 任意(`shikii gc` が古い正解なしの記録を消す) |
| `sessions/` | セッションごとの控え(届けた教訓、伝えた知らせ、変わったファイル) | しない |
| `handoff/` | 作業記録(監査の報告、リリースの手順、差分、画像) | 残したい物だけ(`shikii gc` は触らない) |

初期化は `.shikii/` を Git の未追跡ファイルから除く(私的な会話が入るため)。コミットするものは、除外を外して自分で足す。

**フォルダを移す・名前を変える時:** 同じディスクの中で移すなら、直す物はありません(0.9.7)。`.shikii/` の中の記録は、置き場を最初に開いた時の名前のままで、shikii が移したり名前を変えたりしません。なので wiki・原資料・記録はフォルダと一緒に移り、次の依頼の hook が新しいパスでプロジェクトを自分でつなぎ直して、1 回だけ知らせます。写し(`cp -r`)は乗っ取らず、記録は元に入り続けます。別のディスクへ移した時と、アーカイブや別の機械から持ち込んだ `.shikii` は、shikii から写しと見分けられないので自分ではつなぎ直さず、hook が「ここでは shikii は動いていない」と知らせます。`shikii --init` すると、wiki と記録を持った別のプロジェクトになります。初期化した時と同じパスにある `.shikii` は、どこから来た物でもそのプロジェクトです。0.9.7 に上げる時、この機械に登録済みのプロジェクトには全部、置き場の id を書くので、上げた後に移した置き場はつなぎ直ります。上げる前に移した置き場は、自分ではつなぎ直しません(0.9.6 の登録では、移したのか写したのかを見分けられないため)。その時は hook が知らせ、そこで `shikii --init` すると、wiki と記録はそのまま使えます。今の置き場の横に別の名前の wiki の置き場が残る時は `shikii wiki lint` が `other-path-store` と出し、この置き場が知らないパスで控えた整理待ちの原資料は `source-from-other-path` と出します(まだ要る時は控え直してください)。機械を全部 0.9.7 にし、エージェントのセッションも開き直してください。古い shikii がプロジェクトを登録し直すと、hook が知らせて、全部の shikii を上げるよう頼みます。エージェント自身の記録(Claude Code の `~/.claude/projects/`、Codex の `[projects]`)はパスで呼ばれ、shikii は移しません。作法の [references/wiki.md](../skills/shikii/references/wiki.md) の「Moving or renaming the project folder」を見てください。

答え合わせの画面そのもの(`web/`、Svelte + Tailwind CSS)を直すには Node 24 が要る(`make web`)。使うだけなら要らない: 組んだ画面はコミットされ、実行ファイルに埋め込まれている。

## 普段の会話で使う

Claude Code と Codex は、依頼の前・作業の後・セッションの始め(要約の直後にも)の hook(Claude Code はサブエージェントを起こす直前と始まる時、ファイルを変える・コマンドを流す直前にも。計 6 つ、Codex は 3 つ)で会話の記録・記憶の参照・既製の問いの判定を行う。依頼・返答・次の人の発言を読み、利用中のエージェントが同じ話題の知識へ統合する。検索用の短い要約と、背景・判断理由・未確定点を残す本文を分け、関連ページを結ぶ。`shikii wiki query lesson:<ページID>` で全文を読め、話題の検索では本文も対象になる。エージェントは、本人の訂正や決めたことのほか、自分で新しく作った・変えた決まり・テスト・手順(コマンドやスクリプト)も、頼まれなくてもそのターンのうちに wiki のやり方かルールの記事に書く(近い記事があれば足すか直す)。[エージェント向けの編集手順](../skills/shikii/references/wiki.md)を参照。

同じプロジェクトの似た依頼へ最大 2 件、1800 バイト以内の短い引用を届け、同じ版は同じセッションへ一時間繰り返さない。配送は振り返りの評価で育つ(0.12): ある入り口(依頼、またはその直前に届けたコマンドやファイル)で 2 回「使われなかった」・0 回「役に立った」の版は、その入り口では届けない(別の入り口と、記事の新しい版はもう一度試す)。配送ごとに入り口を記録する。出す教訓について(依頼の時と道具の直前。サブエージェントには聞かない)、判定モデルに既製の問い lesson-applies「この依頼で、このコマンド・ファイルの直前に、この教訓は当てはまるか」を聞く(0.12)。線が引けるまでは記録するだけ、引けたら信頼できる「違う」の時だけその教訓を飛ばして次の候補を見る。モデルの失敗・停止は何も変えない。正解は振り返りの評価(役に立った = はい、使われなかった = いいえ)から付くので、人は答えない(wiki の同じ話題の判定と同じ)。経緯(`history`)の記事は自動では届けない(なぜそうなったかの話なので、探した時に読む)。Claude Code のサブエージェントにも同じ形で届ける。親が Agent ツールに渡した仕事の文を、ツールの直前(`PreToolUse` の Agent / Task だけ)で控え、サブエージェントが始まる時(`SubagentStart`)にその文で教訓を選ぶ。控えが無い時は、親の依頼では選ばず、場面が「いつも」のこのプロジェクトで決められたルールだけを届ける(親の依頼で選ぶと、子の仕事に関係ない教訓が届くため)。親の控えは、使った子への仕事の控えを外すほかは変えず、配送は別の ID で記録する。どちらも Codex には無い event。Claude Code がファイルを書き換える直前(Edit・Write・MultiEdit・NotebookEdit)とコマンドを流す直前(Bash)(`PreToolUse`)には、題・教訓・当てはまる場面(applies_to)か本文のコード部分(`...`・``` の囲み。本文の平文は数えない)にそのファイルかコマンドを書いた教訓を最大 1 件届ける。ファイルはパスの末尾(`cmd/shikii/version.go`)か名前(`version.go`。`myversion.go` や別のフォルダの `version.go` は別のファイル)で、コマンドはプログラムと最初の語(`git commit`・`gh release`・`make test`。`shikii` と `npm` は 3 語目まで見るので `shikii hook plan` は `shikii hook install` ではない。`git -c k=v` のようなオプションは飛ばし、引用符の中の語と、最初の `<<` から後(ヒアドキュメントの本文とその後に続く物)はコマンドと見ない)で照らす。同じ版を同じセッションへ一時間繰り返さない記録は依頼の時と同じで、セッションの最初の依頼の前には届けず、モデルに聞くのは出す教訓についての lesson-applies だけで、文脈を足すだけで、拒否を出すのは有効な `forbidden-commands` の検査がコマンドを断る時だけ(許可と確認は出さない)。この配送も振り返りで同じく評価し、`shikii status` に別の行で数える。Codex には足さない。会話の整理に別のモデルは起こさない。判定用モデルが使えなくても、会話の記録と参照は働く。ツールを止める仕組みでも、実行許可を出す仕組みでもない(止めるのは次の「止める検査」と「作業の前と後の手順」だけで、手順はターンの終わりに、止める検査はターンの終わりか、`forbidden-commands` はコマンドの直前に直させる)。[hook の詳しい説明](../integrations/claude-code.ja.md)。

組み込みの問い(「始める前に確認してほしかったか」「AI が変えたファイルは、頼んだことに必要な物だけか」「頼まれたことを終えたか」)にあなたが答えた内容も、線を待たずにエージェントへ届く。画面(または抜き取りの確かめ、手で付けた答え)で答えた前の依頼と新しい依頼が似ている時、その前の依頼と一緒に、英語の案内文で答えの関係を伝える。画面で使った問いと答えの日本語は、表示ラベルとして区別して保持する。理由の原文は翻訳しない。一緒に、あなたが選び分けた 2 つの答え、つまりエージェント自身の答えとその理由(着手前の整理か仮の採点。0.11 の決まりで突き合わせた物だけ)と判定モデルの答えも届くので、エージェントは自分の考え方がこのプロジェクトでどう判断されたかを学べる。結果やエージェントが付けた正解は引用しない。似ているかの基準は過去の会話と同じで、新しい依頼の語の半分以上(2 語以上)が前の依頼にあること。教訓と同じ 2 件・1800 バイトの枠を分け合い、合うあなたの答えがあれば 1 件分はそれに取っておき、残りを教訓が使い、教訓が使わなかった枠は 2 件目の答えに回す。届けるのはあなた自身の依頼だけで、サブエージェントには届けない。

届いた教訓が行動を変えたかは、振り返りで観察する(helpful / unused / harmful / unknown)。訂正が届いた時は、それに沿ったか・再発したか(`correction_result`: followed / repeated / not_applicable / unknown)も残す。成功・追加依頼・沈黙だけで改善にしない。数は `shikii status` の `memory:` の行と画面に出る。届けたあなたの答えも同じく評価し、その下の行(「of them your past answers」)に数える。

## 止める検査

機械で確かめられる訂正は「止める検査」にする(`.shikii/guards/<id>.yaml`)。種類は `paired-files`(片方が変わったらもう片方も)、`forbidden-words`(足した行に使わない語)、`commit-body`(そのターンのコミットの本文に含む語)、`forbidden-commands`(流そうとしているコマンドが当たってはいけない正規表現。Claude Code の Bash の `PreToolUse` で流す直前に断る。`example` に止めるべきコマンドを書き、`enable` がそれを止めて見せる。Stop ではコマンドを見ない、0.11)の 4 つ。Stop で有効な検査を変わったファイル・足した行・依頼の時の HEAD からのコミットに当て、引っかかれば `decision: block` と理由を返してエージェントに直させる。止めた後の Stop では止めず、直ったかを `records/guards.jsonl` に記録する。`forbidden-commands` の検査は、Claude Code がコマンドを流す直前(Bash の `PreToolUse`、0.11)に動く。形に当たるコマンドを理由付きで断り(`permissionDecision: deny`)、エージェントはターンの終わりではなくその場で直したコマンドを流す。この止めはそのターンの記録に `tool: true` で残り、検査が通る Stop が直ったことを記録する。ファイルの変更は起きる前には判定しない。`forbidden-words` は今までどおり Stop だけで動く(足す行を直前に見る検査は 0.11 で作ったが、監査が 3 回続けて行を見逃す・除外したファイルを断る道を見つけたので外した)。

```sh
shikii guard add < guard.json       # 草稿(無効)。JSON: id, kind, title, reason, source, pairs/words/except/allow/required
shikii guard enable <id>            # 負例で止まる(RED)・正例で通る(GREEN)を見せてから有効にする
shikii guard disable <id>           # 無効に戻す(ファイルは残る)
shikii guard list                   # 状態と最後の結果
shikii guard try                    # 今の作業ツリーに当てて見せる(コミットは見ない)
```

草稿はエージェントが置き、有効にするのは人(作法は [skill の guards.md](../skills/shikii/references/guards.md))。`shikii status` の `guards:` の行に有効な数・止めたターン・直った数が出る。

## 作業の前と後の手順

毎回のターンで、整理・推論の見直し・振り返りを組み込みの規則で確かめる。いつも有効で、外せない(`shikii guard list` に `built in (always enabled)` として出る)。`CLAUDE.md` や `AGENTS.md` の無いプロジェクトでも効く。

- **作業の前の整理(plan-before-work)**: ホストのhook識別子と話題の語でprepareを一度呼び、今回の依頼と開始を保存します。別の開始提出は不要です。具体的な未決の選択には人の回答が必要で、準備を合意にはしません。詳しい整理には既存のhook plan query/submitも使えます。
- **推論の見直し(reasoning-before-end)**: 依頼のhookが必須の状態を保存し、`work prepare`が今回の条件と過去の経験を返す。prepareが開始を保存し、通常は終了時のresultで見直しを閉じます。重要な判断の変更には、明示的なstart/update/finishも使えます。根拠は依頼・供給した記事・観察の参照へ結び付く。新しい証拠を保存した後は、古い終了記録の再送では通らない。wiki訂正の失敗や未配置が残れば、再準備で消さずに解消する。未保存の項目ごとに元の参照を保ち、別の保存成功で消さない。容量不足では必要な作業を残して古いレシートを外し、再準備で復旧する。終了時の判断はagent原資料にも残し、次の仕事で読めるようにする。方針の維持や証拠不足も認め、新しい発見や記事は毎回要求しない。Stopは保存結果と終了時の出典版を確認する。全ツールの実行前の再検討や、判断の質を保証するものではない。追加のモデル・設定・人の採点は不要。[入力と上限](../skills/shikii/references/recording.md#required-review-while-doing-the-work)を参照する。
- **終える前の振り返り(reflect-before-end)**: prepareに含まれる未処理の原文と配送済みの記憶を読み、必要な採点・記事更新・評価を終了時のwork recordへまとめます。別のreflect query/submitは詳しい回復に使い、提示済みの資料を繰り返し取得しません。

```sh
shikii reflect query
shikii reflect submit < reflection.json
```

`reflection.json` は取得した対象に対する `reviews`・`wiki`・`memory`・`maintenance` を含む JSON。`complete` と各項目の `error` を確認し、失敗分だけ直して再送する。[入力例と作法](../skills/shikii/references/review.md)、整理の形は [planning.md](../skills/shikii/references/planning.md)。

完了の判定より先に、Stopが整理と振り返りの保存結果を確かめる。欠けた処理は具体的なコマンドとしてエージェントへ戻す。次の終了時に再検査し、すべて済んでいれば完了を判定する。残件が減れば残りだけを戻し、減らなければ `continue:false` と `stopReason` で未完了を明示して自動継続を止める。確認や復旧状態の保存に失敗した時も未完了とする。裏の作業の完了通知ではこの確認をリセットしない。次の依頼が来ても、必要な処理が未完了なら完了の判定を作らない。結果は `records/guards.jsonl` に残り、`shikii status` の `rituals:` と画面のAIの様子に出る。最後の返答では本人が頼んだ作業の結果を伝える。

**最後の返事を人の言葉で書く(reply-language、0.11)**: Claude Code の `language` が、その言葉の文字で書く言葉(日本語・中国語・韓国語・ロシア語・ウクライナ語)の時、Stop は最後の返事も確かめる。決まった数え方で決める: コードのブロック、行の中のコード、URL、パスや識別子に見える語(`/`・`_`・数字・語の中の点・camelCase などを含む語)を除き、残りの文字(その言葉の文字とラテン文字の合計)が 60 字に満たなければ短い返事として確かめない。60 字以上なら、その言葉の文字が 20% 以上で通る。英語の語を交ぜた日本語の返事は十分に上回り、英語で書いた返事はほぼ 0% になる。通らない時は、人の言葉で書き直すようエージェントに頼む。同じ残件の確認を使い、未対応なら未完了と表示して自動継続を止める。ラテン文字で書く言葉(と `language` の無い時)は確かめず、設定は無い(そうした言葉が設定されている時だけ働く)。`rituals:` の行に `reply-language`(確かめた数・通った数・止めてから書き直した数)として数え、画面の AI の様子にも出る。

作業規則の無いプロジェクトでも作法が届くよう、hook はセッションに 1 回、何か伝えるターンに一言を添える(報告と作業ファイルは作業記録へ、本人が言った決まりや訂正はすぐ wiki の教訓へ)。サブエージェントには、教訓が届かない時も報告の書き先を伝える。

## 作業記録

書き換えながら進める作業ファイル(監査の報告、リリースの手順と記録、差分、画像)の置き場は `.shikii/handoff/<仕事>/`。repo の直下に散らかさず、セッションが終わっても残る。人は画面の「知識 → 作業記録」で読む(文字は文字のまま、画像は画像で出し、HTML は描かない)。

```sh
shikii handoff list                                    # 一覧(新しい順)
shikii handoff read audit-0.9/report.md                # 読む(文字は 4 MiB まで)
shikii handoff write audit-0.9/report.md < report.md   # 置き換える(1 回 1 MiB までの UTF-8)
shikii handoff append audit-0.9/log.md < more.md       # 足す
```

MCP では `shikii_wiki` の `handoff_list`・`handoff_read`・`handoff_write`・`handoff_append`。パスは `.shikii/handoff/` からの相対(`.shikii/handoff/` 付きでもよい)。`..`・絶対パス・隠し名・制御文字・ファイルやフォルダへのリンク・ハードリンクは断り、置き場の外には出ない。置き換えは毎回別の名の一時ファイルを経て rename する(書き換えの途中で落ちて残った一時ファイルは、次にそのフォルダへ書く時に 1 日より古いものを片付ける)。

次の仕事に効く教訓は wiki に、確かめた時点の証拠の要点は原資料(`shikii wiki capture`。`reference` に作業記録のパスを書ける)に、変わり続けるファイルは作業記録に、と分ける。

## 確認結果を原資料にし、知識を検索・見直す

会話のほか、エージェントが実際に確認したコード・テスト・Releaseなども原資料にできる。以下の操作は通常のエージェントが行い、人が毎回整理する必要はない。

```sh
shikii wiki capture < observation.json
shikii wiki query "返金 適用条件"
shikii wiki query "返金 適用条件" continue:CURSOR
shikii wiki query "lesson:<ページID>"
shikii wiki query "<出典ID>" "<別の出典ID>"
shikii wiki query "lesson:<ページID>@<版>"
shikii wiki ingest < article.json
shikii wiki classify <ページID> <種類> <場面>
shikii wiki maintain < maintenance.json
shikii wiki lint
```

`observation.json`には`kind`（`human`・`agent`・`code`・`test`・`release`）、`title`・`content`・`reference`・`version`・`observed_at`（RFC3339）を書く。原資料に独自の上限は無く、JSON全体が置き場の1行の上限(4MiB)までなら控えられる。確認した抜粋や結果と、その対象の版を残す。`reference`は参照先の説明で、URL取得やコマンド実行は行わない。返されたIDを記事の`sources`へ使う。同じ観察は重複登録せず、変わった内容や版は別の原資料になる。

検索は題・適用条件・要約・本文を調べ、一致した欄と語、候補の版を返す。候補だけでは意味の関連性を確定できないため、本文と必要な原資料を確認する。`CURSOR`は直前の応答の`next_cursor`に置き換え、同じ検索語または出典IDで続きを取得する。末尾へ着いたことと、必要な知識を見つけたことは別。応答が大きければ分割されるが、記事数や出典数で探索を打ち切らない。同じプロジェクト・同じ版の実際に読んだ内容は作業中に再利用する。

`article.json`の短い`lesson`と詳しい`body`を分け、背景・根拠・適用条件・未確定点を既存記事へ統合する。`related`で関連をつなぎ、意味の編集には`change_reason`を残す。利用できる長い会話原文は引用時に別のハッシュとともに保持し、元の判定記録を消した後も参照できる。原資料や原文が欠けている時は、その限界を残す。

着手前の `hook plan query` は、検索上位の教訓に加え、その記事が `related` で直接結ぶ有効な `criteria` を最大2件返す（0.14）。要約と適用条件は末尾を切らず、種類と場面も渡す。`pages` 未指定なら判断基準を先に、本文が丸ごと収まる記事を最大2件・本文合計8000バイト以内で `article:<id>:<revision>` として返す。入らない本文は既存の `pages` 指定（最大2件）や `wiki query` で読める。応答の `wiki_context` は全記事の一覧を繰り返さず `available_count` を返すが、判定記録の `knowledge_context.available` には全版を保存する。関連候補・本文取得・実際の引用を区別し、関連や過去の判断を現在の許可とは扱わない。

### 外部の担当割り当てとつなぐ

短期間だけ担当者を起動するホストは、`work prepare`と`work record`に任意の`assignment`を渡せる。実行ID・共通グループ・役割・工程・目的・問い・条件・実装版を保つ。準備の`teamwork`には、他の担当が実際に読んだ版からの変更、以前の調査と進行中の調査、未確認の問いと検証方法、手順を見直すための失敗の記録が入る。各報告には条件・出典・未統合／統合済み／取り消しの実装状態を残す。担当者の起動・中止・統合はホストが管理する。

入力と上限は[担当割り当ての契約](../skills/shikii/references/recording.md#external-assignments)を参照する。観察を`event`で記録し、裏付けのある手順の訂正や検証結果を、従来の版を確認するnotesで同じ知識へ戻す。報告だけで、人の正解・許可・検証済みの成功にはしない。`evidence_scope`は原報告の自動配送を範囲内に留め、ホストが`shareable`を付けた要約は同じプロジェクトの別の範囲にも渡せる。同じ置き場へアクセスできる担当者は原資料を検索できるため、これらの欄はアクセス制御ではない。

`assignment`を指定しなければ、既存のCLI・MCP・hookは従来の経路を使い、担当割り当ての記録も作らない。chitenへの依存、新しいサービス・モデル・設定項目は増えない。追加するAI向けの案内は英語とし、利用者の依頼と原資料は元の言語、報告本文は依頼の言語で保つ。

部分保存の後もイベントは元のreceiptで扱う。以前の本体が書いた未完了イベントは、変更前の入力を一度再送してから失敗したnotesを訂正する。再開前に担当割り当てを書く全プロセスを0.24.0以上へ更新する。再送と移行の条件は記録の手順を参照する。

chitenの対応した担当報告では、確認した記事と判断に使った知識、手順の実行結果を区別して既存のdiscoveriesとoutcomesへ戻せます。実利用と各結果を別々の原資料へ保存し、記事の訂正や同一再送で同じ実行を重ねません。報告に対応したchitenを別途使う任意連携で、shikiiの通常利用には不要です。

### 作業途中の知識と結果をつなぐ（0.20.0）

着手時のprepareは、全文が入らない記事にも今回の依頼に関わる原文の抜粋と位置・版を返します。条件や例外が未解決の時だけ全文を読みます。同じreceiptへquery、proposal、conditionsのいずれかを渡すと、追加検索・作業案の照合・手順の再利用を行い、以前の保存と記事参照を保ちます。検索の続きはcursorを使います。追加で返した本文・要点・抜粋は同じknowledge参照で引用でき、前の引用も残します。未取得の本文は引用できず、合意には人の原文が必要です。照合・再利用の依頼欄は保存済みの依頼も含め16000バイトまでで、超える時は個別のwiki操作へ明示的に範囲を絞った依頼を渡します。

recordのdiscoveriesは記事のrefと状況・判断・証拠を受け、通常の発見の保存へつなぎます。公開済みの同一再送は記事の変更・削除・置換後も復旧でき、拒否が確定した入力は保存枠を使いません。outcomesはhowtoのref、conditions・action・failure・correction・resultと観察のsourcesを受けます。reuseは過去の条件・失敗・修正・結果と出典を返し、手順の版の変化を示します。利用中のエージェントが現在条件と比べ、証拠に沿ってhowtoの変更を保存します。成功の自動ラベルや人の正解にはしません。

0.25.0では、再利用で返す手順結果は最大2件のまま、現在条件の内容語、依頼と報告された行動・失敗との一致を優先し、同点は新しい順にします。同程度に条件が合う失敗は後の結果と並べて保持します。文脈が無い時は従来の新しい順です。候補の選択なので、元の条件・出典・手順の版の変更を確認して使います。

操作例・固定の上限・再送と版の境界は[記録の手順](../skills/shikii/references/recording.md)を参照してください。これらは0.20.0から使えます。使う前に同じ記録へ書く全プロセスを更新してください。旧版は新しい文脈と結果の索引を保持できません。

### プロジェクトの経験を作業中に使う（0.18.0）

作業案を決める時は、`shikii wiki consult` に依頼と進め方案をJSONで渡します。

```json
{"request":"新しい組織の請求書を振り分ける","proposal":"振り分けのたびに入力フォームを追加する"}
```

関係する記事の条件・理由・本文・出典と関連記事が返ります。エージェントがそれを読み、案を支持する理由、過去の反例、例外、不明な点をまとめます。検索一致やリンク自体を賛成と数えません。既存の判定モデルが使える時は適用の判断も添えます。過去の選択は今回の許可にはなりません。

繰り返す仕事では、`shikii wiki reuse` に依頼と現在の条件を渡します。

```json
{"request":"次の版を準備する","conditions":"LinuxとmacOS。前の版からインストーラーを変更した"}
```

やり方の記事、判断理由、検証の原資料、出典が参照する作業記録から、エージェントが今回の手順を組み立てます。前提の変化と成功の証拠不足を明示し、各段階の出典を残します。現在の作業記録は未検証の補足資料として区別し、コマンドは自動実行しません。

どちらもMCPの`shikii_wiki`（operationは`consult`か`reuse`）と`POST /v1/wiki/notebook`から使えます。文字列は各16000バイト、CLIのJSON全体はエスケープを含め1MiBまでです。補助の適用モデルへ全文を渡せる上限（依頼1500バイト・作業案と現在条件の合計欄400バイト）を超える場合は、条件を切らずに補助判断を見送り、model_noticeで知らせます。エージェントへは全文を返します。話題の候補は3件、関連記事を含め全6件・2段まで、本文の合計は16000バイトです。長い本文は分類した引用を優先し、未分類の原文を最大3箇所まで加えて位置付きで返します。引用と未分類の抜粋を合わせて各記事2400バイト以内とし、不明な条件がある時だけ版付きの本文を読み直します。手順ごとの原資料は最大3件で抜粋は各1200バイト、現在の作業記録は各600バイトです。作業記録は出典が`.shikii/handoff/`を参照する物だけを既存の読取制限で読みます。URLや別のパスは取得しません。

手動検索で役立った記事が当時届いていなければ、エージェントは通常の`discoveries`へ診断の`case_id`を付けます。同じプロジェクトの完全な診断と読んだ版を確認し、当時の未配信理由を`maintenance`の`missed-delivery`へ戻します。そこで診断・比較・部分編集、または変更不要の記録へ進みます。届いた記事やセッション内で配信を控えた記事は見逃しにせず、履歴不足を現在の知識で埋めません。同じ証拠は確認後に繰り返さず、新しい証拠で再確認します。検索候補の改善と、次の実作業で役立ったかは別に確かめます。人の追加記入や正解ラベルは作りません。


### 経験をつなぎ、検証と手順へ育てる（0.22.0）

記事の`learning`には、複数の経験を結んだ見立て、小さく確かめる方法と観察結果、再利用する作業の手順を残せます。`connections`は別記事の版・正確な引用・条件・つなぐ理由・支持か反例かを持ち、元の出典も引き継ぎます。`probe`は問い・確かめ方・見直す条件と出典付きの結果、`practice`は`howto`の条件・手順・避け方・確かめ方を持ちます。見立てや結果はエージェントの報告であり、人の正解や成功ラベルにはしません。

CLI・MCP・HTTPの`ingest`/`patch`と`work record`のnotesで保存・改訂します。`prepare`/`consult`/`reuse`の`learning_context`を読み、接続先の版や前提が変わった時は条件を再確認します。本文とは別に各記事2000バイトまでをその場で返し、入らなければ`omitted`と版付きの取得先を返します。同じ作業記録へ版付きの`lesson:<id>@<revision>`を明示して追加取得すると、保存上限12KiBまでの学びを重複なく全文で返します。`reuse`は収まる場合にその記事版の作業の手順も返します。知識画面でも問い・観察結果・手順を読めます。共有wikiの書き手は全員更新してから使ってください。

`shikii wiki organize`へ`organization:{reason,sources,targets,moves}`を渡すと、元記事の版と移す原文を確かめ、分割・統合の案を返します。この操作では保存しません。出典と分類した引用を保つ新記事の作成案を先に保存し、返された版を照合した後、`requires`付きの編集案を元記事へ適用します。元記事や行き先が変わった場合は拒否します。途中で失敗した時は保存済みの結果を読み直して案を作り直します。元記事の履歴と、行き先へたどれる参照は残ります。全体を一度に保存する処理ではありません。元記事4件・新記事4件・移動8箇所まで、organize・ingest・patchのCLI入力は1MiBです。詳しい入力例と上限は[エージェントの手順](../skills/shikii/references/learning.md)にあります。

0.22.0の学びを保存する前に、同じwikiを書く全環境で`shikii update`を実行し、エージェントのセッション・画面・MCPを再起動してください。旧版は学び付きの記事を読めません。0.21.0からの更新では既存データを保持します。更新した本体を使うhookがあればskillも更新し、hookが無い場合は前述の`shikii --init`を再実行します。起動中のプロセスは再起動しません。旧版へ戻す可能性がある場合は、更新前の`.shikii/`を控え、本体とデータを一緒に戻せるようにしてください。

### 0.27.0：見立てを次の判断へつなぐ

判断を変え得る未確認事項は、`work record`のtrialへ課題・問い・期待する観察・方法・条件・結果で変わる判断・確認の負担・出典を保存します。実際の確認後、その試行へ観察と出典を結び付けます。期待と異なる結果は、途中か終了時の判断へ引用して戻します。再準備や保存中断でも元の計画・結果を保ち、別の結果で置き換えません。

現在の出典が有効な未実行の試行から、小さい確認を候補として返します。エージェントは`reasoning.next_trial`で別の有効な試行を選べます。`comparison`は同じ課題の保存済み実行2件を読み、同じ測定項目の前後と差を返します。検索候補を比べる`wiki compare`とは区別します。実際に確認するのはホストで、計画の保存だけで実行したり、報告した効果が正しいと確定したりはしません。

`reasoning.depth`のbrief/focused/deepで、今回の判断に読む量を合わせます。briefの広い検索は正確な抜粋を返し、版を固定した明示読取では全文を回復できます。判断が未解決、出典の前提が変更、保存や食い違いの確認が未完了なら深く照合します。必須の保存と振り返りは省きません。[試行の入力と復旧](../skills/shikii/references/recording.md#expectations-execution-and-the-next-check-0270)を参照してください。

有効なファイル・本文のguardは、Claude CodeのBashの`PreToolUse`で観測できた`git push`と変更を伴う`gh release`の直前にも照合します。通常のRTK経由も含め、実物の不足を直してから再送します。公開は他の処理を終えた後の別のツール呼び出しにし、変更後の実物を確認できない複合コマンドは拒否します。dry-run/helpと単独の読取専用コマンドはこの追加の対象外です。Codexでは現在、このコマンド用hookを登録せず終了時の確認を使います。OpenCodeは自動hookが無く、MCPとskillを使います。範囲を絞った解析のため、内部を読めないshell展開・heredocの後ろ・別の公開経路を完全に覆うものではありません。

### 記事の種類と場面

0.9.0 から、教訓を持つ記事には `kind`(種類)と `scene`(場面)が要る。無い保存は断られ、断りの文に使える値が出る。

| kind | 画面の名前 | 何を書くか |
| --- | --- | --- |
| `rule` | このプロジェクトで決められたルール | いつも守ること |
| `howto` | やり方 | 手順と進め方 |
| `criteria` | 判断の基準 | どちらにするかの決め方 |
| `fact` | 前提と事実 | 今そうなっていること |
| `history` | 経緯 | なぜそうなったか |

`scene` はその仕事の言葉で「〜する時」と書く 1 行(48 文字まで)。「コミットする時」「請求書を出す時」「患者に説明する時」のように、開発の言葉に限らない。振り返りの `place` は今ある場面と記事の数(`scenes`)を返すので、近い場面があれば同じ言い方を使う(似た言い方で場面が散らない)。どの依頼にも効く決まりは「いつも」にする。付けるのはエージェントで、人が選ぶことはない。

0.9.0 より前に作った記事には種類も場面も無い。依頼前の hook が、そうした記事を 1 回に 5 本ずつ挙げてエージェントに付けさせ(`shikii wiki classify <ページID> <種類> <場面>`、MCP は `shikii_wiki` の `operation: "classify"`)、付けないまま終えようとすると Stop が復旧を求める(作法の検査 `wiki-kinds`)。保存によって残件が減れば残りを再び求め、減らない時や検査・復旧状態の保存に失敗した時は未完了を明示して自動継続を止める。教訓の文は変えず、新しい版として残る。付ける間に同じ記事が別のエージェントに直されたら、classify は断り、読み直して付け直す(直しを古い教訓で戻さない)。wiki に読めない記事や手で直された記事がある間は保存が全部断られるので、hook は付ける記事を頼まず、`shikii wiki lint` で確かめるよう 1 行だけ伝える。`wiki query` の返事は残りの数と次の 5 本から始まり、`wiki ingest`・`wiki classify`・`reflect submit` の返事にも残りが載る。hook の無いエージェント(hook を信頼する前の Codex、OpenCode)には、MCP の案内と作法(skill)が同じことを伝える。作法は `~/.claude/skills/shikii` と、Codex が読む `~/.agents/skills/shikii` の両方に置く。shikii は AGENTS.md に書かない。

`wiki query`の一覧や`reflect query`には最大2記事の`maintenance`対象がある。本文・原資料・関連ページを確認し、必要な編集を保存した後、最新の`page_id`・`revision`・`key`と確認理由`reason`を`maintenance.json`へ渡す。振り返りの`maintenance`配列へまとめてもよい。記事や関連する版が変われば古い見直し結果は拒否し、未変更でも30日後に再確認する。本文やリンクがないことは確認のきっかけであり、不要な文章や関係を追加する指示ではない。 同じ版・同じ入り口で「使われなかった」が2件以上あり「役に立った」が0件、または「判断を妨げた」評価がある記事も、理由と判断の証拠付きで優先して戻す。例は3件まで、総件数も添える。エージェントは普段の振り返りで出典と照らし、必要な時だけ適用条件や本文を直す。未使用だけで記事が誤りとは決めず、変更不要の見直しも記録できる。同じ証拠を毎回出さず、新しい該当評価が加われば再確認する。不明・未評価・古い版の評価は悪い評価として扱わず、人の追加記入や採点は不要。

着手前の`hook plan query`は`pages`で必要な本文を選べる。判断には候補・読んだ記事・引用した版・当時の利用可能な版と取得量が残る。過去の比較は記録された版だけで行い、後から更新されたwikiや解決結果を混ぜない。保存・検索・引用の件数と、判断に役立ったという観察は分ける。詳しい契約は[wikiの作法](../skills/shikii/references/wiki.md)を参照する。

明示的な検索は全記事を対象にし、`search.total`(有効な一致記事数)と `search.candidates` を返す。応答が大きければ JSON のデータ量で分割し、同じ検索語や出典 ID と、CLI の `continue:CURSOR` または MCP・HTTP の `cursor` で続きを読む。プロジェクト・条件・結果が変わったカーソルは拒否する。1 件だけ大きい会話の原資料は単独で返し、その長い原文(`originals`)が返事 1 回に入らない時は、`original_part`(`id`・`from`・`to`・`bytes`・`json`)で分けて返す。`json` を順につなげると、その会話の原文の一覧の JSON に戻る。返事 1 回に入らない控えた原資料の本文は、`observation_part`(`id`・`from`・`to`・`bytes`)を付けて分けて返し、つなげると本文に戻る。語の一致による検索なので話題が違えば検索語を見直す。

### 今あるメモリを wiki へ移す

エージェントがすでにメモリを持っている(Claude Code の自動メモリのフォルダ: `MEMORY.md` と 1 事実 1 ファイル、Codex のメモ)なら、「メモリを shikii の wiki へ移して」の一言で移せます。skill の [import-memory.md](../skills/shikii/references/import-memory.md) が手順です: 各ファイルを原資料として残し(`shikii wiki capture`)、規則ごとに適用条件・種類・場面付きの教訓を 1 本ずつ下書きし(言われた決まりは「このプロジェクトで決められたルール」、手順は「やり方」、事実は「前提と事実」など。場面は `place` が返す今ある場面を優先して使う)、振り返りの `place` で既存の記事に合流させて重複を作らず、メモリのファイルは本文をそのまま残し、先頭近くに wiki の記事を指す 1 行を足すだけにします(`MEMORY.md` も今の行を残して記事の id を書き足します)。どの依頼にも効く規則(見える文の言語、使わない語、push する時)は、wiki が依頼との近さで教訓を届けるため、索引にも 1 行ずつ残します。消す・空にする・書き換えるものはありません(古いファイルを消すのは本人です)。手順は `shikii --init` が置く skill の一部なので、初期化したどのプロジェクトでも使えます。依頼前の hook も、そのフォルダ(と Codex の共通の `~/.codex/memories` のうちこのプロジェクトを名指しするファイル)に `.shikii/wiki <記事の id>` の指し先が無いファイルが残っていれば、セッションに 1 回気付いて、今の依頼の後に移すようエージェントへ伝えます。全プロジェクト共通のファイルは本文を残し、指す行を足すだけです。

## 画面と棚卸し

答え合わせの画面はプロジェクトごとに 1 つ。最初の依頼の hook が、そのプロジェクトの画面が立っていなければ `shikii serve --store <プロジェクト>/.shikii --port 0` を起こし(空いているポートを取る)、ブラウザで開く。ポートと pid は `~/.shikii/agent/pages/<プロジェクトの id>.json` に残り、同じプロジェクトの複数のエージェントは 1 つの画面を共有する。画面の場所は `shikii page`(`page: http://127.0.0.1:<port>/ (port …, pid …, up)`。立っていなければ起こして待つ)。画面は `127.0.0.1`・`localhost`・`::1` の名前で開く(他の Host は 403 で断る)。

画面は「答える」「知識」「AI の様子」の 3 つで、上の切替で選ぶ。初めて開く時は、人を待つ判定があれば「答える」、無ければ「知識」から始まり、選んだ画面は次も開く。

「答える」は、人が答える問い(依頼の種類、始める前に確認してほしかったか、AI が変えたファイルは頼んだことに必要な物だけか、頼まれたことを終えたか)の待ちを 1 列にまとめ、1 枚ずつ出す。エージェントも同じ問いに答える物(始める前に確認してほしかったかは着手前の整理、範囲と終えたかは振り返りの仮の答え)は、エージェントの答えが判定モデルの答えと分かれた時と、エージェントが決めきれなかった時だけ人を待つ。その札には、判定モデルの答え(自信つき)とエージェントの答え(理由つき)が並び、答えのボタンでどちらかを選ぶ。2 つが合えば、判定モデルが迷っていても誰にも聞かない。合った答えは機械の正解(出どころ `agreed`)として残り、人の答えには数えないので、線を引くには今までどおり人の正解が要る。分かれた物を出す数に上限は無い。札の先頭とボタンの上にその問いの文、中に今回の依頼・最後の返答・変えたファイル・その後の人の返事、下に貼り付いた答えのボタン。キーでも答えられる(1〜9 で選ぶ、Enter で AI と同じ、→ でスキップ)。待っている数は「答える」の横に出る。AI だけが答える問い(wiki の矛盾・差し込み先)と、結果から正解が付く記録は出ない。聞き方は、エージェントが新しい聞き方に変えた時(訳付きで「このままでよい / 元に戻す」)と、意味が同じか決めきれなかった時(訳と違いの見立て付きで「意味は同じ / 違う」)だけここに出る。hook の「あとで答える」の URL で来た時は、その判定が先頭に出る。

「AI の様子」は、普段は見なくてよい物の置き場。問いごとの学習の進みが 1 行ずつ並び、押すとその問いの欄(進み具合、最後の自動較正、まだ答えていない分・結果で確定する分・答えた分の列、答えの直し、聞き方の候補)が出る。届けた教訓と効いたか、作業の前と後の手順、止める検査と最後の結果、使っている判定モデルと人を待つ判定の割合もここにある。

知識画面で記事を開き、**編集**で題・要点・適用条件・背景・種類・場面を変えられます。**削除**は確認後、通常の一覧・検索・自動配信から記事を外します。出典・関連する参照・過去の版は残り、データの完全消去ではありません。`shikii wiki query lesson:<id>`や`lesson:<id>@<revision>`で記録を明示的に読むことはできます。削除済みの記事は、同じ題の新規草稿を含め、通常の取り込みでは復活しません。記事を削除した後は、動いているエージェントも0.15.0以降に揃えてください。編集画面を開いている間に記事が更新されると、保存時に競合を知らせ、入力を残します。仲間の変更と比較して案を確認し保存するか、必要な文をコピーして最新の記事を開き直してください。変更時は既存の出典に加え、送信した編集内容も原資料として保存します。判断を測るための正解にはしません。

「知識」は「教訓」と「作業記録」の 2 つのタブ。教訓のタブの一番上には「このプロジェクトで決められたルール」の欄があり、人や AI が決めたルールが、場面の絞り込みと関係なく全部並ぶ(埋もれないように)。「いつも」のルールが先頭、次に新しい順で、最近 7 日に増えた・変わったルールには「新」のラベルが付く。その下で、左で場面を選び、真ん中に種類ごとの題、題を押すと右に本文・当てはまる時・背景・関連・出典が出る。場面を選ばない時は種類ごとに役立った回数の多い順に 4 本ずつ出し、残りは「ほか N 本を見る」で開く。種類と場面の無い記事は「未分類」にまとまり、エージェントが依頼のたびに減らす。上の「あなたに見てほしいもの」には、機械が決められない例外だけが出る: 害があった教訓、繰り返された訂正、有効にする前の止める検査。人が記事を 1 本ずつ確かめる作りではない。検索・見直し・振り返りの数、前からの記事の木、以前の主張は、畳んだ「記事の詳しい記録」の中にある。

### 棚卸し: `shikii status`

```sh
bin/shikii status --store .shikii .shikii/decisions/prompt-kind.yaml
# prompt-kind: records=1 labels=1
#   by source: outcome=1   by truth: !edit=1
#   jev-1.14.0 @ 68ed82631e63f2a2   1  collecting (min_n 50)
```

spec ごとに記録とラベルの数、ラベルの出どころ(ask / outcome / file)と中身の偏り、(model, spec_hash) ごとの、線を引く時に数える正解の数(入力ごとに最新、人と結果の正解だけ。画面と同じ数)と状態
(`calibrated: vector` / `collecting (min_n …)` / `only one answer so far`(片方の答えしか無く線を引けない) / `tried: the answers could not be separated`(試したが分けられない。正解が増えたら再び試す) / `stopped: the probabilities do not tell the answers apart (AUC …)`(線が 1 つも無い hook の問いで、見分ける力が足りず止めた。依頼の 20 回に 1 回だけ聞き、聞き方が変わるか、その 1 回に別のモデルが答えるとまた試す) / `enough truths: the line is drawn automatically`(自動で引く途中) / `stale hash`)を出す。運用で最初に見る画面。結果から正解を付ける問いには `outcome truths checked by a person: …` の行が出て、結果の答えごとに、画面で人が確かめた数(一致が決まらない間、正解が 50 件以下の答えは全部、多い答えは 10 件に 1 件を出す)、合った数、線を引くのに使うかを言う。

`person's work` の行は、今の規則で人が答える判定(結果が答えず、エージェントも答えていない判定の迷い、線のある自動判定の抜き取り、エージェントの答えが判定モデルと分かれた物と決めきれなかった物)の数を、全体と直近7日の1000判定あたりで示し、正解を人と結果のどちらが決めたかを分けて数える。`rungs` の `left to a person` は迷った数で、結果が答える判定の迷いは人を待たない。人の手間を減らす変更の効果は、この行の前後で比べる。

`memory:`(届けた教訓と評価)、`guards:`(有効な数・止めたターン・直った数)、`rituals:`(作業の前と後の手順)、`page:`(画面の URL)の行も出る。

線が新しく引けた時と、正解が `eval.min_n` に届いたのに線を引けない時(答えが片方だけ、試して答えを分けられなかった、または問いを止めた)は、Stop の hook がターンの終わりに本人へ一言知らせる(Claude Code では端末に出る)。問いごとに、状態・文言・モデルが変わった時だけ 1 回で、画面の「AI の様子」にも同じ理由が出る。

## 判定モデル

### 推奨のモデル(TypeSafe の Jev)

```sh
export TYPESAFE_API_KEY=...       # 鍵は環境変数だけから読む。shikii は Keychain を読まない
shikii doctor                     # hosted に「key present」と出れば、hook・既製の問い・pii: false の判定は Jev へ
```

推奨は TypeSafe の Jev。鍵があれば hook と既製の問いは Jev へ送り、手元のモデルは起こさない(`shikii up` も断る。取得するものは無い)。鍵を置いたまま手元のモデルを使うなら `SHIKII_MODEL=local`、Jev に固定するなら `SHIKII_MODEL=hosted`(CLI・MCP・`serve`・hook で同じ)。`pii: true` の判定は鍵があっても手元にしか送らないので、鍵を置いたまま手元のモデルで答えさせるには `SHIKII_MODEL=local shikii up` で立てる。

### 鍵を置けない時の手元のモデル(llama.cpp)

```sh
brew install llama.cpp            # Homebrewの公式formula
llama-server --version
shikii up                         # llama-server を :8086 に、shikii-readout を :8080 に立てる(初回は約 7.6 GB を取得)
```

基本モデルは `prism-ml/Ternary-Bonsai-27B-gguf` の `Ternary-Bonsai-27B-Q2_g64.gguf`(普通のチャット用モデル。`shikii-readout` が選択肢の文字の確率を読み、決定モデルの速さで答える)。導入は[Homebrewのformula](https://formulae.brew.sh/formula/llama.cpp)、他のOSは[公式の手順](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md)を参照する。`shikii up` と既製hookも、鍵が無ければこのモデルを使う。動作中のメモリは約 10 GB で、普通の PC では重いことがある。OpenJev のような学習済みの決定モデルは `shikii up --hf <repo:quant>`(readout はその答えをそのまま通す)。Kev-4B は使えない。

モデル名は応答の `model`が較正の鍵になる。`--model` で送った名前ではない。

### 接続先

業務の `decide`・MCP・`serve` は、System One形式の `POST /v1/systemone` を使う。OpenAI互換のチャットAPIと同じ意味ではない。CLIで指定するURLにパスがあればそれを使い、パスが無ければ `/v1/systemone` を付ける。応答には問いごとの `answers` と確率が必要。`shikii-readout` はllama.cppの `/apply-template`・`/tokenize`・`/completion` を使う変換役で、任意のチャットAPIに対応する変換役ではない。

| 接続先 | 条件 | URLの例 |
|---|---|---|
| Jev(推奨) | `TYPESAFE_API_KEY`、外部送信を許可するspec(`pii: false`) | 指定不要(`--hosted https://api.typesafe.ai` が既定) |
| llama.cpp | 鍵を置けない時。通常のGGUF(手元の標準はTernary-Bonsai-27B)を `shikii-readout` 越しに。OpenJevなどの決定モデルはそのまま通す | `--local http://127.0.0.1:8080`(`shikii up` が立てる readout の口) |
| Ollama | v0.35.0以上、System One対応の決定モデル。今回の実機推論は未検証 | `--local http://127.0.0.1:11434 --model nimble` |
| LM Studio | Jev互換のSystem One対応API、読み込み済みの決定モデル。今回の実機推論は未検証 | `--local http://127.0.0.1:1234 --model <読み込み済みのモデルID>` |

参照: [OllamaのSystem One](https://docs.ollama.com/api/systemone)、[LM StudioのSystem One](https://lmstudio.ai/docs/developer/jev-compat/systemone)、[TypeSafeのAPI](https://docs.typesafe.ai/introduction/quickstart)。公式仕様とクライアントを照合した表で、接続先の実機推論の成功を保証する表ではない。ローカルのクライアントはAuthorizationを付けないため、認証を必須にした構成は別途確認が必要。

既製のhookは、鍵があれば Jev、無ければ `127.0.0.1:8080` の固定構成で、OllamaやLM Studioへ自動で切り替わらない。会話の整理と参照は判定用モデルが止まっていても動く。OllamaはローカルURL経由でクラウドモデルも呼べるため、手元だけで扱う判定にはローカルのモデルを選ぶ。[Ollamaのクラウドモデルの説明](https://docs.ollama.com/capabilities/decision)。

### `--hosted` と `--local` の例

`--hosted` は URL にパスがあればそのまま使う。鍵は環境変数 `DECISIONS_API_KEY`(または `TYPESAFE_API_KEY`)。

| 先 | 例 |
|---|---|
| TypeSafe Jev | `--hosted https://api.typesafe.ai --model jev-latest` |
| OpenRouter Decisions API | `--hosted https://openrouter.ai/api/alpha/decisions --model typesafe/jev-1.13` |
| llama.cpp(ローカル) | `--local http://127.0.0.1:8080` |
| **`shikii-readout`**(普通の GGUF。後ろは任意の llama-server) | `--local http://127.0.0.1:8090`([`integrations/readout.ja.md`](../integrations/readout.ja.md)) |
| BonJev(同じ原理の外部実装。Bonsai 専用) | `--local http://127.0.0.1:8083`([`integrations/bonjev.ja.md`](../integrations/bonjev.ja.md)) |
| 偽モデル | `--local http://127.0.0.1:8099`(`make fake`) |

### 普通の GGUF を本家 llama.cpp だけで決定モデルにする: `shikii-readout`

**fork も Rust も学習も要らない。** `brew install llama.cpp` の llama-server に好きな GGUF を載せ、前にアダプタを 1 つ置けば、
`shikii` からは OpenJev や Jev と同じ決定モデルに見える。

本家 llama.cpp の `/v1/systemone` は学習済みの決定モデル(OpenJev / Clef …)しか受けない。普通のチャット用 GGUF を使うには、
選択肢に A, B, … を割り当てて 1 トークンだけ回し、その文字の確率を読めばよい(had-scorer と同じ原理)。それを System One 形式で出すのが
`shikii-readout`。推論は持たず、後ろの llama-server(本家でも PrismML の fork でも)に HTTP で聞くだけ。

```sh
llama-server -m <any>.gguf --port 8086 --alias my-model      # 本家でも fork でも
bin/shikii-readout --upstream http://127.0.0.1:8086 --port 8090
bin/shikii decide .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8090 -i message='README の誤字を直して'
```

- 起動時に `/tokenize` で全ラベルが 1 トークンかを確かめ、違えば起動しない
- `/apply-template` でそのモデルの chat template を使い、Qwen3 系の `<think>` は `enable_thinking: false` で潰す(受けない upstream には送り直す)
- 返す確率は**素の softmax**。較正はしない。応答の `model` は llama-server の `--alias` で決まり、それが較正の鍵になる
- 上位 `--n-probs` に入らなかった選択肢は床の確率(1e-6)を得る(0 にすると較正の log が壊れる)
- state は prompt の最後に置き、spec ごとの接頭辞(質問・選択肢)を先に 1 回流してチェックポイントを作る。再帰層を持つ Qwen3.5/3.8 系
  (Bonsai の土台)はこれが無いと `cache_prompt` が効かない

詳しくは [`integrations/readout.ja.md`](../integrations/readout.ja.md)。

## 業務の判定を任せる

自分の仕事の判断(「この問い合わせは前のと同じ案件か」など)を、正解を貯めながら少しずつ任せる。判定のファイル(spec)に質問・選択肢・入力・しきい値を書き、答えを付けていくと、ある自信より上なら当たると言える線が引ける。線が引けるまでは `act` は出ない。

```sh
go build -o bin/shikii ./cmd/shikii

# 以下の例で使う問いを、このプロジェクトへコピーする
bin/shikii new prompt-intent --from examples/prompt-intent.yaml
bin/shikii new same-issue --from decisions/same-issue.yaml

# 仕様のハッシュ(較正の鍵)
bin/shikii hash .shikii/decisions/same-issue.yaml

# 判定。ホスト版は TYPESAFE_API_KEY があれば使う。--tainted なら hosted に送らない
TYPESAFE_API_KEY=... bin/shikii decide .shikii/decisions/prompt-intent.yaml -i message='README の誤字を直して'
bin/shikii decide .shikii/decisions/same-issue.yaml --local http://127.0.0.1:8080 --tainted \
  -i report1='ログイン画面でパスワードを 3 回間違えると 500 エラーになる' -i report2='パスワード誤入力の回数制限に達した直後にサーバーエラー(500)'

# 判定は ./.shikii/records/ に本文ごと残る。ask で選んだ答えをラベルにする
bin/shikii label <trace_id> change_request --source ask
bin/shikii label --last --spec-id prompt-intent question --source ask   # 直近の記録に付ける(trace 不要)

# 較正。noul は identity と Platt、choice/score は identity と vector(選択肢ごとの bias + 温度)を 5-fold CV で比べて勝った方を書く。
# ファイルのラベルと store のラベル((spec_hash, model) が一致するもの)を合わせて使う
bin/shikii calibrate .shikii/decisions/same-issue.yaml --model 'ternary-bonsai-27b-q2g64+state-last'   # 応答の model(鍵があれば Jev の名前)
```

### 判定の定義ファイル(spec)の書き方

`shikii new <id>` が対話(またはフラグ `--type --question --option id=説明 --input name --pii true|false --send-to local`)で作る。項目の意味と書き方の全部は [skills/shikii/references/spec.md](../skills/shikii/references/spec.md)(`shikii init` が `~/.claude/skills/shikii/references/spec.md` にも置く)。

正解が `eval.min_n`(spec に必ず書く。`shikii new` の既定は 50)に足りないうちは `calibrate --write` も `tune` も書かない。手持ちが少ないうちに試すなら、spec の `eval.min_n` を手持ちの件数に下げる。`--force` で書いた線は `check` が「calibrate なら書かなかった線」として落とすので、試しの道には使わない。

### 2 分で一周(鍵も GPU も無し)

```sh
scripts/demo.sh                  # 偽モデル(cmd/fake-systemone)で decide → label → calibrate → check → drift → status
scripts/demo.sh --overconfident  # 確率を端に寄せた偽モデル。較正が効く様子を見る
```

### 契約の検査と劣化の監視

```sh
# CI で回す。較正の spec_hash が今の文言と合うか、ラベル上で act での precision が act 以上か。違反で exit 1
bin/shikii check .shikii/decisions/*.yaml

# 直近 N 件のラベルで、較正時の Brier から +0.05 以上悪化 / precision が act 未満なら警告して exit 1
bin/shikii drift .shikii/decisions/prompt-intent.yaml --model jev-1.14.0 --window 50
```

較正の方法は 3 つ。どれも `calibrate --write` が生成し、鍵は (spec_hash, model)。

| method | 対象 | 中身 |
|---|---|---|
| `platt` | noul | σ((Δ−bias)/T) |
| `vector` | choice / score | softmax((log p_k + b_k)/T)。選択肢ごとの癖(短い state で other に逃げる等)を直す |
| `identity` | どれでも | 定数なし。生の分布で act での precision が足りると検証したことを示す値(Jev のような較正済みモデル向け) |

`calibrate --write` は `calibration` キーだけを差し替え、spec のコメントと文言はそのまま残す。同じラベル付きファイルで再実行すると、store に残った同じ入力のラベルを再利用してモデルを呼ばないので、モデルを止めたままでも較正・check・drift が回る。較正が要るかはモデルの触れ込みではなく、(質問, モデル) ごとの交差検証で決まる。モデル別の数字は [`docs/model-comparison.ja.md`](model-comparison.ja.md)、測った数字は [`docs/bench.ja.md`](bench.ja.md)。

### score の帯: `thresholds.level`

順序尺度では隣り合うレベルに確率が割れるので、最上位の確率で帯を決めると実際より弱く見える。たとえば「直接関連」と「強く関連」に確率が半々に割れると、最上位は 0.5 前後で ask 止まりになるが、「直接関連以上」の確率は高い。
`thresholds.level: k` を置くと、帯は **P(level ≥ k)** で決まる(top と期待値 score はそのまま出す)。
較正の鍵には入らない(閾値と同じ扱い)。報告の precision も「P(level ≥ k) と正解レベル ≥ k」で測る。

### choice の順序バイアス: `shikii bias`

```sh
bin/shikii bias .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8080 --rows 20 --permutations 5   # state は calibrate と同じ貯金(--store の正解の本文、または --labeled)
```

選択肢の並びを入れ替えて同じ state を聞き直し、最上位が変わる率(flip rate)と、位置ごとの確率のずれ
(spec の並びで聞いた時との差)を出す。flip rate > 0.10 か位置のずれの幅 > 0.10 で警告・exit 1。
生成しないモデルなので順列を回すのは安い。選択肢は spec に書いた並びのまま送る(`systemone.OrderedCriteria`。Go の map だと鍵がソートされてしまう)。

### 掃除: `shikii gc`

記録(`records/`)は判定のたびに増える。ラベルは較正の材料なので捨てないが、ラベルの付かなかった古い記録は要らない。

```sh
bin/shikii gc --store .shikii                 # 表示だけ(既定: 30d より古くラベルの無い記録が候補)
# prompt-kind: would remove 120 unlabeled record(s) older than 30d; keep 48 labeled, 15 recent, 0 unparsable
# dry run: nothing was changed. Re-run with --apply to remove them.
bin/shikii gc --store .shikii --apply         # 実際に消す
bin/shikii gc --older-than 1w .shikii/decisions/prompt-intent-v2.yaml       # spec と年齢を指定(d / w / h。0 なら年齢を問わない)
```

消すのは「`--older-than` より古く、ラベルが 1 つも付いていない記録」だけ。ラベル付きの記録・ラベル・JSON として読めない行は年齢に関係なく残す。
書き換えは同じディレクトリに書いてから rename で置き換えるので、途中で止まっても元のファイルは壊れない。

`make status` / `make drift` / `make check` / `make gc` / `make install` / `make server` も用意した(`Makefile`)。

## コマンドの一覧

`shikii --help` と同じ並び。フラグの全部は `shikii <コマンド> --help`。

| コマンド | すること |
|---|---|
| `shikii init`(`shikii --init`) | このプロジェクトを有効にする(既製の問い・wiki・止める検査と作業記録の置き場・MCP・作法・画面への入口)。2 回目も安全 |
| `shikii new <id>` | 質問に答えるだけで `.shikii/decisions/<id>.yaml` を作る(`--from` で見本を写す) |
| `shikii up` | 手元のモデルを起こす(llama-server と、普通の GGUF なら `shikii-readout`)。鍵がある時は起こさない |
| `shikii serve` | HTTP の口と答え合わせの画面 |
| `shikii mcp` | 同じ中身を stdio の MCP サーバーとして出す(`shikii_specs`・`shikii_decide`・`shikii_label`・`shikii_status`・`shikii_tune`・`shikii_wiki`・`shikii_review`・`shikii_reflect`) |
| `shikii doctor` | 鍵・手元のモデル・置き場の状態と、hook と Codex の MCP の登録が動かす shikii の実行ファイルが今もあって動かせるかを確かめる(無い時は直し方を出す)。確かめるのは、shikii が書く形ちょうどの hook(単引用符のパスか、パスに使う文字だけの裸のパスの後に ` hook <出来事>`、本体の名が `shikii`)だけ。ほかの書き方で shikii を動かす command(`~`・`$HOME`・引用符・別の command の後につないだ物・出来事の後ろに語が続く物)は、手で書かれていて確かめていないと言い、`hook install` も `uninstall` も command ごと触らない |
| `shikii update` | 最新の安定版へ本体と readout を更新する |
| `shikii hook install` / `uninstall` | Claude Code と Codex の hook を入れる・外す(`--yes` 無しは表示だけ) |
| `shikii uninstall` | shikii がエージェントに入れた物(hook・MCP の項目・エージェントの作法)を外し、画面を止める。データ(`.shikii/`・`~/.shikii/agent`)と実行ファイルは残し、実行ファイルを消す `rm` のコマンドを出す(`--yes` 無しは表示だけ) |
| `shikii hook plan query` / `submit` | 作業の前の整理(エージェントが使う) |
| `shikii hook variants submit` | 聞き方の候補を渡す(エージェントが使う) |
| `shikii hook variants check` | 上回った候補の意味が同じかと(英語以外の設定なら)両方の訳を渡す(エージェントが使う。同じなら採用、人は元に戻せる) |
| `shikii decide <spec>` | 判定して帯を返す |
| `shikii label <trace_id> <truth>` | 正解を付ける(`--last` で直近の記録に) |
| `shikii calibrate <spec>` | 正解で線を引く(`--write` で spec に書く) |
| `shikii status` | 記録と正解の数、線の状態、教訓・止める検査・手順・画面の行 |
| `shikii page` | このプロジェクトの画面の URL(立っていなければ起こす) |
| `shikii tune <spec>` | 聞き方の候補を正解の貯金で採点する(採用は意味が同じと確かめてから: エージェントの `hook variants check`、画面、`--write`) |
| `shikii import <past.csv> --spec <spec>` | 手元の過去の表を正解の貯金(JSON)にする |
| `shikii judge <spec> --local <URL>` | 別の大きいモデルを審判にし、人の正解と十分に一致すれば正解の無い記録に重み付きの正解を付ける |
| `shikii wiki query` / `consult` / `reuse` / `organize` / `reconcile` / `patch` / `diagnose` / `compare` / `capture` / `ingest` / `classify` / `maintain` / `lint` / `verify` | wiki の検索・作業案の照合・手順の再利用・原資料・登録・種類と場面を付ける・見直し・点検・主張の再検証 |
| `shikii handoff list` / `read` / `write` / `append` | 作業記録の読み書き |
| `shikii guard list` / `add` / `enable` / `disable` / `try` | 止める検査 |
| `shikii review query` / `submit` | 会話と結果からの仮採点(振り返りでまとめて使うことが多い) |
| `shikii reflect query` / `submit` | 終える前の振り返り |
| `shikii check <spec>...` | CI 向け: 文言が変わった線・手書きの線・精度不足の線を落とす |
| `shikii drift <spec> --model M` | 直近の正解で線の劣化を見る |
| `shikii bias <spec>` | 選択肢の並びで答えが変わるかを測る |
| `shikii gc` | 古くて正解の無い記録の掃除(`--apply` で消す) |
| `shikii hash <spec>` | 較正の鍵になる spec のハッシュ |
| `shikii version` | 版を表示する |

## serve の HTTP API

本文は JSON。`-H 'Content-Type: application/json'` が無いと 415 で蹴る。`spec` は `.shikii/decisions/<id>.yaml` の id。

| 口 | 体 | 返すもの |
|---|---|---|
| `POST /v1/decide` | `{"spec","inputs":{…},"tainted":false}` | 判定(band、top、p、trace_id、receipt)。入力に `request` がある時は、あれば `similar_decisions`(似た依頼への本人の答え。エージェントと判定モデルの答え付き)と `lessons`(いちばん近い wiki の教訓)も付く(0.11) |
| `POST /v1/label` | `{"spec","trace_id","truth","weight":1,"source":"ui"}` | 付けた正解(文脈の段を通った判定は 2 段に付く) |
| `GET /v1/queue?spec=<id>` | | 未回答(人待ちの記録と、結果待ちの記録。各項目に `waits_for_person`。やり直した元は出ない) |
| `GET /v1/answered?spec=<id>&limit=30` | | 回答済み(人の正解だけ) |
| `POST /v1/redo` | `{"spec","trace_id"}` | 今の聞き方と線で同じ本文を再判定し、前と今を並べて返す(新しい記録、人の答えは写る) |
| `GET /v1/status` | | spec ごとの状態(線の有無、正解の数、人の言葉の質問) |
| `GET /v1/tune?spec=<id>` · `POST /v1/tune/run` · `POST /v1/tune/variant` · `POST /v1/tune/adopt` | `{"spec","propose"}` · `{"spec","name","question","options"}` · `{"spec","run","meaning_same":true}` | 聞き方の改善の最新と今の候補・裏で回す・人が書いた候補を足す(検証してから。採用はしない)・採否 |
| `POST /v1/tune/review` | `{"spec","run","keep":true}` | エージェントが意味を確かめて自動で採用した聞き方を、人が「このままでよい」(true)か「元に戻す」(false。採用した版は `.reverted` に名を変えて残す) |
| `POST /v1/tune/sync` | `{"spec","propose"}` | その場で聞き方の改善を回して結果を返す(MCP の `shikii_tune` が使う。採用はしない) |
| `GET /v1/wiki` · `POST /v1/wiki/claim` | | wiki の主張の一覧 · 主張の状態を変える(promote / delete。変えるのは人だけ) |
| `POST /v1/wiki/notebook` | `operation: prepare / record / query / consult / reuse / organize / reconcile / ingest / patch / diagnose / compare / capture / classify / maintain / lint / handoff_list / handoff_read / handoff_write / handoff_append` | プロジェクトのwiki。原資料と記事の検索・登録・見直し・点検。教訓は仮説。handoff_* は作業記録の読み書き(MCP と同じ操作。画面そのものは読むだけ) |
| `POST /v1/wiki/article` | `action: edit / delete`、`id`、`revision`。editは`title`、`lesson`、`applies_to`、`body`、`kind`、`scene` | このプロジェクトの記事を編集・削除する。古い版は409。削除後は検索・配信から外し、出典と履歴を残す。 |
| `GET /v1/handoff/file?path=` | `path`(.shikii/handoff/ からの相対) | 作業記録の 1 件。画像は画像、文字と HTML は text/plain、それ以外は保存。置き場の外は 400 |
| `POST /v1/review` | `operation: query / submit` | 会話と結果から仮採点。原文・対象・訂正例を取得し、根拠付きで保存。人の確認は抜き取りと保留 |
| `POST /v1/reflect` | `operation: query / submit`、`reviews` / `wiki` / `memory` / `maintenance` | 採点・wiki整理・届いた記憶と訂正の観察、記事の見直しをまとめて確認・保存(query は整理待ちの会話を全部並べ(古い 2 件は原文、続く 40 件までは頭だけ、残りは more_pending の件数)、原資料と見直しの対象は 2 件ずつ見せ、submit は会話・項目・見直しの数を限らない。既存の記事に足す時、元の出典は引き継がれる)。`complete`と各`error`を確認する |
| `GET /v1/specs` | | 読み込んでいる spec の一覧 |

判断の未回答・回答済み一覧は、続きがあれば `next_cursor` を返します。同じ取得先・問いに `cursor=<next_cursor>` と `limit`（最大1000件）を渡して読み進めます。不正な位置は400、履歴や回答が変わった場合は409を返すため、最初から読み直してください。画面は続きも自動取得し、既存の「さらに表示」で順に表示します。

```sh
curl -s localhost:8095/v1/decide -H 'Content-Type: application/json' -d '{"spec":"same-issue","inputs":{"report1":"…","report2":"…"}}'
curl -s localhost:8095/v1/label  -H 'Content-Type: application/json' -d '{"spec":"same-issue","trace_id":"<decide の返事の trace_id>","truth":"yes"}'
```

## 構成

| 場所 | 役割 |
|---|---|
| `internal/spec` | Decision Spec の読み書き。較正の鍵 `Hash()`(質問・テンプレ・選択肢から。閾値・policy は含めない) |
| `internal/systemone` | System One 形式(`POST /v1/systemone`)の薄いクライアント。Jev / llama.cpp はベース URL の差だけ |
| `internal/decide` | 仕様 + 入力 → 帯。required 欠落は abstain、tainted は hosted を外す、**較正が無い判定は act を出さない** |
| `internal/calib` | Platt 較正(`calib.go`)、vector scaling(`vector.go`)、Brier / ECE / 信頼度図、AUC と 95% 区間(答えを見分ける力) |
| `cmd/shikii` | CLI |
| `cmd/shikii-readout` | **普通の GGUF を System One にするアダプタ** `shikii-readout`。後ろの llama-server の `/apply-template` と `/completion` で選択肢の文字の確率を読む。推論も較正も持たない |
| `cmd/fake-systemone` | 鍵も GPU も無しで輪を回すための偽モデル |
| `decisions/*.yaml` | spec の見本(この repo。自分のプロジェクトでは `.shikii/decisions/` に写す) |
| `testdata/` | 作り物のラベル付きデータ(依頼文 62 件、不具合報告と問い合わせの各 32 組)と、CI 用の較正の裏付け(`store/labels/`) |

## まだ無いもの

- 等張回帰
- 鍵の取り出し(環境変数で渡す。このプロセスはキーチェーンに触らない)

## 作業中に見つけた知識を次へつなぐ

エージェントが自分で検索して役立った記事も、次の検索へ生かせます。wikiの検索は、実際に返した記事と版を結び付けた参照記録を返します。その記事によって判断が変わった時に、エージェントが状況・判断・証拠を普段の振り返りへ添えます。読むだけでは役立ったと数えません。記録は同じ版の検索と既存の配送判定へ使われ、編集・置換・削除後の版へ評価を引き継ぎません。参照記録は最新64件、役立った発見は最新128件をプロジェクトごとに保持します。

記事には、条件、選んだ方法、見送った案と理由、確認した結果を短く残せます。エージェントが出典から書き、分からない理由は不明とします。着手前や教訓の配送で一緒に届き、知識画面でも読んだり編集したりできます。

特定のプロジェクトファイルが前提になる知識には、最大4ファイルを結び付けられます。shikiiはハッシュを控え、変更・消失・読取不能があれば記事を再確認の対象へ戻します。記事と次の配送にも確認が必要なことを示します。変更だけで規則を無効にしたり削除したりせず、エージェントが証拠を確かめて更新します。

着手前の整理と保留の振り返りを、一度の取得・提出にまとめられます。判定対象も届いた記憶もない相づちには、無関係な判定例や記事を付けません。保存失敗は明示し、再送して完了を確認します。

APIの追加: `wiki query`は`lookup:{id,query,pages:[{id,revision}]}`を返します。`reflect submit`は`discoveries:[{lookup_id,page_id,revision,context,decision,evidence}]`を受け取ります（各説明は1000バイトまで）。記事の草稿に`rationale`（1600バイトまで）と`watch`（相対パス、通常ファイル最大4件・各1 MiBまで）を指定できます。隠しパス、リンク、プロジェクト外のパスは拒否します。省略時は既存値を保持し、空の値で消します。`maintenance.changed_files`と記事の`recheck`に前提の変化が入り、確認後に`watch`を再提出すると基準を更新します。これらはエージェントの観察であり、人の正解や許可ではありません。`hook plan submit`には通常の振り返り提出と同じ形の`reflection`を含められます（全体で1 MiBまで）。入れ子の保存結果も確認します。

## 知識の一部を直し、必要な場面で見つかるか確かめる

エージェントは、記事全体を送り直さず、必要な文章だけを修正できます。記事とその版を読み、置き換える文章・修正後の文章・出典・変更理由を渡します。その間に別の編集が入った場合や、対象の文章が複数箇所にある場合は保存を拒否します。他の節、出典、判断理由、前提として確認するファイルは保たれます。

必要な記事が届かなかったときは、当時の記録から理由を調べられます。依頼に一致しなかった、候補の上限から外れた、最近すでに届いていた、過去の評価で抑えられた、判定モデルが対象外と判断した、といった理由を区別します。記録のない過去の理由は不明として扱います。

保存前に、修正案を最大8件の過去の場面で試せます。当時の依頼、記事の版、検索に使われた文脈を固定し、候補の変化と、別の記事が候補から外れるなどの影響も表示します。比較では保存しません。候補に入ることは実際の配信や有用性を意味せず、セッション内の抑制、過去の評価、判定モデルは再実行しません。利用者が例題を準備したり、比較結果を採点したりする必要はありません。

CLIは標準入力のJSONで操作します。`wiki patch`は`{id,revision,sources,change_reason,edits:[{field,before,after}]}`、`wiki diagnose`は`{id,case_id}`（両方省略可）、`wiki compare`は`{patch,cases}`（cases省略可、重複なしの記録IDを最大8件）です。MCPでは同名のoperationを使い、patchは`patch`欄に入れます。他の操作は同じ欄を使います。部分編集は完全一致の文章置換で、1回最大16箇所です。詳しくは[エージェントの手順](../skills/shikii/references/wiki.md#precise-edits-delivery-diagnosis-and-comparison)を参照してください。診断記録はプロジェクト内に最大32件・4MiBを保持し、不完全な記録や読めない過去は比較できないと明示します。これらは0.17.0で利用できます。

部分編集は、記事ファイルの一度の置換で確定します。入力検査や版の競合では記事を変えません。確定後に索引・ログ・出典の処理済み登録が失敗した場合は、保存済みの記事と復旧の警告を返します。古い部分編集を再送せず、その版を読み直して警告の手順に従ってください。手動検索で役立ったという評価は記事の版に結び付くため、比較する修正案には引き継ぎません。

### 変更を受け渡し、並行した仕事を比べる（0.23.0）

作業準備の`collective`に、すでに供給した記事の変更、条件と出典を保った見立ての対照、今回確かめられそうなwikiの問いを返します。変更前後の版を読み、確認だけを`reviewed:["changeN"]`で記録します。判断への実利用は別に記録します。初回の`knowledgeN`を確認すると、同じセッションの次の依頼で選ばれた記事に限って比較基準を引き継げます。同じ出典を共有していることを示し、異なる出典IDだけで独立の裏付けとはしません。検証結果は条件と出典を添えて同じ問いへ戻します。

追加情報全体で4KiB、変更2件・対照1組・検証機会1件までです。省略した内容には版付きの取得先を残し、既存の学びは重ねず参照します。CLI・MCP・HTTPで同じ操作を使えます。`wiki reconcile`へ`{"patch":{...}}`を渡すと、元/現在/提案を保存せず比較し、保存できる案だけに現在版を条件とするpatchを返します。同じ箇所や構造化した情報の競合は明示して残します。案を確認して通常のpatchで保存し、さらに版が変わっていれば拒否します。知識画面でも古い版への保存に失敗した後、入力を残して仲間の変更と比較できます。詳細は[エージェントの手順](../skills/shikii/references/learning.md#work-together-on-the-same-knowledge)にあります。

0.22.0からの更新ではプロジェクトのデータを保持します。新しい作業contextを使う前に、同じ記録を書く全プロセスを0.23.0へ揃え、セッション・画面・MCPを再起動してください。旧版の書き手は新しいcontextを保持できません。旧版へ戻す可能性がある場合は、更新前の`.shikii/`を控え、本体とデータを一緒に戻せるようにしてください。

### 記録の負担を減らす

通常は記録用の2呼び出しで進めます。prepareが開始を保存し、終了時のrecordへ
result:{changed,checked?,evidence?,remaining?}と必要な記事更新・振り返りをまとめます。
確認結果は一度だけ保存して参照し、新しい知識が無ければwiki本文を作りません。
配置より先に元の報告を保存し、saved-pendingは整理待ちを示します。保存済みの報告を
再送せず、失敗した参照だけ回復できます。範囲や完了など内部評価を人の採点待ちに
せず、人にしか決められない具体的な選択を尋ねます。

prepareのmax_bytesで応答量を制限できます。省略時は同じ版の取得先を残し、
文の区切りを保つ引用と同じreceiptの本文・同一抜粋の再利用で再読を減らします。
同じ案内文は、応答内の`reading_notices`へ原文のまままとめ、各`reading_notice`から
参照します。本文・引用・省略の状態・同じ版の取得先は保持し、実際の応答全体が
小さくなる場合だけ共用します。
抜粋の再利用だけで全文を取得した扱いにはしません。記憶の未使用・
不明に説明文を作る必要はありません。記憶の方針が変わる更新ではUpgrading memory…
と進捗を出し、導入先のエージェントが出典・好み・履歴を保って少しずつ確認し、
中断から再開します。通常は1依頼につき最大2記事の1組だけ確認し、続きは次の依頼で
再開します。記憶の更新を完了する明示依頼では残りの組も続けられます。
関連記事の保存内容・条件・出典・修正理由がすべて完全に同一で版の履歴だけが
違う場合、確認済みの記事への再確認の連鎖を省きます。編集した記事自身、
新しい証拠、前提の変化、30日後の確認は対象です。比較情報のない古い確認結果は、
実際の再確認まで従来の動作を保ちます。確認の送信時には、関連記事の現行版を含む
最新のrevisionとkeyが必要です。
[記録の契約](../skills/shikii/references/recording.md#ordinary-work-takes-two-calls)を参照してください。
