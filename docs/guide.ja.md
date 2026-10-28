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

## インストール・アップデート

インストールは[日本語 README](readme/README_ja.md#始め方)の curl の 1 行。ソースから入れる場合は clone した shikii のフォルダで `sh install.sh`。curl の 1 行は最新の版を取ってくるので Go は要らない。ソースから入れる時は Go 1.27 以上と Git が要る。

```sh
shikii update
```

GitHub Releases の最新安定版から、利用中の OS・CPU 向けの本体と readout を取得する。Go・curl・GitHub CLI は不要。SHA256SUMS と添付のハッシュ、起動する本体の版を確認し、実行中の shikii の実体と同じフォルダへ更新する。PATH の別の場所へ入れ直すことはない。現在の版と同じか古い配布物には置き換えない。

本体のライセンスと第三者の通知は、実行ファイルの隣の `shikii-licenses/` へ残す。配布物の欠落やハッシュ不一致では置き換えず、書き込み途中の失敗では本体と readout を元へ戻す。同時更新は拒否する。保存先の書き込み権限が無い場合は、その旨を表示する。

この実行ファイルを使う hook が導入済みなら、新しい本体から hook と作法を更新する。導入していない hook は追加しない。この反映だけに失敗した場合は本体の更新済みを明示し、`shikii hook install --yes` で再試行できる。起動中の画面・MCP は停止しないため、再起動して新版へ切り替える。更新した skill と hook の案内文は新しいエージェントのセッションで使う。既存プロジェクトの質問文と較正は保持し、新しく初期化するプロジェクトには新版の既製プロンプトを用意する。

**0.8 系から 0.9 系(0.9.0 以上)への更新:** 0.9.0 で種類と場面を付けた記事は、0.8 系の本体からは「変更あり」に見え、0.8 系は wiki への保存を全部断る(記事は壊れない)。更新したら、開いているエージェントのセッション(とその MCP)を開き直す。同じ wiki を使う機械は全部 0.9.0 以上にする(どれかの機械で 0.9.2 を使うなら全部 0.9.2 以上。0.9.1 以前は、0.9.2 が書いた記事(決まった語が英語)を「変更あり」と見なす)。更新の最後にもこの注意が出る。0.9.2 から shikii は AGENTS.md に書かない。アップデートは、前の版が登録済みの各プロジェクトの AGENTS.md に書いた shikii の節だけを外し、ファイルの残りは残す(印だけの行で囲まれた節がちょうど 1 つある時だけ。repo の外を指すリンクや、印が 1 つ・3 つ以上の時は触らずに理由を出す。CRLF のファイルは CRLF のまま)。

**0.6.0 以前からの更新は、初期化済みの各プロジェクトの `.shikii/` の中を全部消して初期化し直す**(プロジェクトの決定)。以前の会話・教訓・仮採点・正解・較正・作業記録は消え、既製の問い・wiki の雛形・プロジェクトの印は作り直される。残したいものは先に控える。0.7.0 以降からの更新は全部残す。0.6.0 から更新する時は、消去は古い本体の最後の手順の中で走り、その表示は出ない(0.6.0 が出力を捨てる)。先に `--yes` 無しの `shikii hook install` で消す対象を確かめる(作業記録がある時は件数も出る)。

## プロジェクトごとの有効化

インストール後、利用するプロジェクトで `shikii --init`(`shikii init` も同じ)を一度実行する。エージェントに「このプロジェクトで shikii を使いたい」と頼んでもよい。既製の問い(hook の 4 問と wiki の 2 問)・wiki・止める検査と作業記録の置き場・MCP・作法と画面への入口を揃え、最後に `.shikii/project.json` を書く。hook はこの印がある場所だけで動き、未初期化の場所には保存先も Git の除外も作らない。サブフォルダからも同じプロジェクトルートを使い、再初期化しても既存の設定・会話・正解を保持する。`shikii doctor` で状態を確かめる。

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

Claude Code と Codex は、依頼の前と作業の後の hook(Claude Code はサブエージェントを起こす直前と始まる時にも。計 4 つ、Codex は 2 つ)で会話の記録・記憶の参照・既製の問いの判定を行う。依頼・返答・次の人の発言を読み、利用中のエージェントが同じ話題の知識へ統合する。検索用の短い要約と、背景・判断理由・未確定点を残す本文を分け、関連ページを結ぶ。`shikii wiki query lesson:<ページID>` で全文を読め、話題の検索では本文も対象になる。エージェントは、本人の訂正や決めたことのほか、自分で新しく作った・変えた決まり・テスト・手順(コマンドやスクリプト)も、頼まれなくてもそのターンのうちに wiki のやり方かルールの記事に書く(近い記事があれば足すか直す)。[エージェント向けの編集手順](../skills/shikii/references/wiki.md)を参照。

同じプロジェクトの似た依頼へ最大 2 件、1800 バイト以内の短い引用を届け、同じ版は同じセッションへ一時間繰り返さない。経緯(`history`)の記事は自動では届けない(なぜそうなったかの話なので、探した時に読む)。Claude Code のサブエージェントにも同じ形で届ける。親が Agent ツールに渡した仕事の文を、ツールの直前(`PreToolUse` の Agent / Task だけ)で控え、サブエージェントが始まる時(`SubagentStart`)にその文で教訓を選ぶ。控えが無い時は、親の依頼では選ばず、場面が「いつも」のこのプロジェクトで決められたルールだけを届ける(親の依頼で選ぶと、子の仕事に関係ない教訓が届くため)。親の控えは、使った子への仕事の控えを外すほかは変えず、配送は別の ID で記録する。どちらも Codex には無い event。会話の整理に別のモデルは起こさない。判定用モデルが使えなくても、会話の記録と参照は働く。ツールを止める仕組みでも、実行許可を出す仕組みでもない(止めるのは次の「止める検査」と「作業の前と後の手順」だけで、どちらもターンの終わりに直させるだけ)。[hook の詳しい説明](../integrations/claude-code.ja.md)。

届いた教訓が行動を変えたかは、振り返りで観察する(helpful / unused / harmful / unknown)。訂正が届いた時は、それに沿ったか・再発したか(`correction_result`: followed / repeated / not_applicable / unknown)も残す。成功・追加依頼・沈黙だけで改善にしない。数は `shikii status` の `memory:` の行と画面に出る。

## 止める検査

機械で確かめられる訂正は「止める検査」にする(`.shikii/guards/<id>.yaml`)。種類は `paired-files`(片方が変わったらもう片方も)、`forbidden-words`(足した行に使わない語)、`commit-body`(そのターンのコミットの本文に含む語)の 3 つ。Stop で有効な検査を変わったファイル・足した行・依頼の時の HEAD からのコミットに当て、引っかかれば `decision: block` と理由を返してエージェントに直させる。止めた後の Stop では止めず、直ったかを `records/guards.jsonl` に記録する。

```sh
shikii guard add < guard.json       # 草稿(無効)。JSON: id, kind, title, reason, source, pairs/words/except/allow/required
shikii guard enable <id>            # 負例で止まる(RED)・正例で通る(GREEN)を見せてから有効にする
shikii guard disable <id>           # 無効に戻す(ファイルは残る)
shikii guard list                   # 状態と最後の結果
shikii guard try                    # 今の作業ツリーに当てて見せる(コミットは見ない)
```

草稿はエージェントが置き、有効にするのは人(作法は [skill の guards.md](../skills/shikii/references/guards.md))。`shikii status` の `guards:` の行に有効な数・止めたターン・直った数が出る。

## 作業の前と後の手順

毎回のターンで、2 つの手順を組み込みの規則で確かめる。いつも有効で、外せない(`shikii guard list` に `built in (always enabled)` として出る)。`CLAUDE.md` や `AGENTS.md` の無いプロジェクトでも効く。

- **作業の前の整理(plan-before-work)**: 依頼の hook が整理を頼んだターンで、ファイルを変える前に `shikii hook plan query` で今回と直近の会話を読み、エージェントが選んだ話題の語で wiki を探し、合意したことと未決のことを `shikii hook plan submit` で保存する。検索語の無い query には「wiki はまだ探していない」と返る。判定モデルが止まっていても整理は保存される。
- **終える前の振り返り(reflect-before-end)**: 依頼の hook が振り返る会話を挙げたターンで、終える前に `shikii reflect query` で原文・届いた記憶・教訓・人の訂正を読み、採点(reviews)・wiki・届いた教訓の評価(memory)を `shikii reflect submit` で保存する。

```sh
shikii reflect query
shikii reflect submit < reflection.json
```

`reflection.json` は取得した対象に対する `reviews`・`wiki`・`memory`・`maintenance` を含む JSON。`complete` と各項目の `error` を確認し、失敗分だけ直して再送する。[入力例と作法](../skills/shikii/references/review.md)、整理の形は [planning.md](../skills/shikii/references/planning.md)。

手順が欠けたまま終えようとすると、Stop が一度だけターンを止め、打つべきコマンドを伝える。二度目は止めない(裏の作業の完了通知の後も)が、欠けたことは `plan-before-work` / `reflect-before-end` として `records/guards.jsonl` に残る。`shikii status` の `rituals:` の行(頼んだ数のうち済んだ数、止められてから済んだ数)と画面の AI の様子に出る。止めた時の文は、最後の返答で作法の報告ではなく本人が頼んだ作業の結果を伝えるよう、エージェントに頼む。

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

0.9.0 より前に作った記事には種類も場面も無い。依頼前の hook が、そうした記事を 1 回に 5 本ずつ挙げてエージェントに付けさせ(`shikii wiki classify <ページID> <種類> <場面>`、MCP は `shikii_wiki` の `operation: "classify"`)、付けないまま終えようとすると Stop が一度だけ止める(作法の検査 `wiki-kinds`)。教訓の文は変えず、新しい版として残る。付ける間に同じ記事が別のエージェントに直されたら、classify は断り、読み直して付け直す(直しを古い教訓で戻さない)。wiki に読めない記事や手で直された記事がある間は保存が全部断られるので、hook は付ける記事を頼まず、`shikii wiki lint` で確かめるよう 1 行だけ伝える。`wiki query` の返事は残りの数と次の 5 本から始まり、`wiki ingest`・`wiki classify`・`reflect submit` の返事にも残りが載る。hook の無いエージェント(hook を信頼する前の Codex、OpenCode)には、MCP の案内と作法(skill)が同じことを伝える。作法は `~/.claude/skills/shikii` と、Codex が読む `~/.agents/skills/shikii` の両方に置く。shikii は AGENTS.md に書かない。

`wiki query`の一覧や`reflect query`には最大2記事の`maintenance`対象がある。本文・原資料・関連ページを確認し、必要な編集を保存した後、最新の`page_id`・`revision`・`key`と確認理由`reason`を`maintenance.json`へ渡す。振り返りの`maintenance`配列へまとめてもよい。記事や関連する版が変われば古い見直し結果は拒否し、未変更でも30日後に再確認する。本文やリンクがないことは確認のきっかけであり、不要な文章や関係を追加する指示ではない。

着手前の`hook plan query`は`pages`で必要な本文を選べる。判断には候補・読んだ記事・引用した版・当時の利用可能な版と取得量が残る。過去の比較は記録された版だけで行い、後から更新されたwikiや解決結果を混ぜない。保存・検索・引用の件数と、判断に役立ったという観察は分ける。詳しい契約は[wikiの作法](../skills/shikii/references/wiki.md)を参照する。

明示的な検索は全記事を対象にし、`search.total`(有効な一致記事数)と `search.candidates` を返す。応答が大きければ JSON のデータ量で分割し、同じ検索語や出典 ID と、CLI の `continue:CURSOR` または MCP・HTTP の `cursor` で続きを読む。プロジェクト・条件・結果が変わったカーソルは拒否する。1 件だけ大きい会話の原資料は単独で返し、その長い原文(`originals`)が返事 1 回に入らない時は、`original_part`(`id`・`from`・`to`・`bytes`・`json`)で分けて返す。`json` を順につなげると、その会話の原文の一覧の JSON に戻る。返事 1 回に入らない控えた原資料の本文は、`observation_part`(`id`・`from`・`to`・`bytes`)を付けて分けて返し、つなげると本文に戻る。語の一致による検索なので話題が違えば検索語を見直す。

### 今あるメモリを wiki へ移す

エージェントがすでにメモリを持っている(Claude Code の自動メモリのフォルダ: `MEMORY.md` と 1 事実 1 ファイル、Codex のメモ)なら、「メモリを shikii の wiki へ移して」の一言で移せます。skill の [import-memory.md](../skills/shikii/references/import-memory.md) が手順です: 各ファイルを原資料として残し(`shikii wiki capture`)、規則ごとに適用条件・種類・場面付きの教訓を 1 本ずつ下書きし(言われた決まりは「このプロジェクトで決められたルール」、手順は「やり方」、事実は「前提と事実」など。場面は `place` が返す今ある場面を優先して使う)、振り返りの `place` で既存の記事に合流させて重複を作らず、メモリのファイルは本文をそのまま残し、先頭近くに wiki の記事を指す 1 行を足すだけにします(`MEMORY.md` も今の行を残して記事の id を書き足します)。どの依頼にも効く規則(見える文の言語、使わない語、push する時)は、wiki が依頼との近さで教訓を届けるため、索引にも 1 行ずつ残します。消す・空にする・書き換えるものはありません(古いファイルを消すのは本人です)。手順は `shikii --init` が置く skill の一部なので、初期化したどのプロジェクトでも使えます。依頼前の hook も、そのフォルダ(と Codex の共通の `~/.codex/memories` のうちこのプロジェクトを名指しするファイル)に `.shikii/wiki <記事の id>` の指し先が無いファイルが残っていれば、セッションに 1 回気付いて、今の依頼の後に移すようエージェントへ伝えます。全プロジェクト共通のファイルは本文を残し、指す行を足すだけです。

## 画面と棚卸し

答え合わせの画面はプロジェクトごとに 1 つ。最初の依頼の hook が、そのプロジェクトの画面が立っていなければ `shikii serve --store <プロジェクト>/.shikii --port 0` を起こし(空いているポートを取る)、ブラウザで開く。ポートと pid は `~/.shikii/agent/pages/<プロジェクトの id>.json` に残り、同じプロジェクトの複数のエージェントは 1 つの画面を共有する。画面の場所は `shikii page`(`page: http://127.0.0.1:<port>/ (port …, pid …, up)`。立っていなければ起こして待つ)。画面は `127.0.0.1`・`localhost`・`::1` の名前で開く(他の Host は 403 で断る)。

画面は「答える」「知識」「AI の様子」の 3 つで、上の切替で選ぶ。初めて開く時は、人を待つ判定があれば「答える」、無ければ「知識」から始まり、選んだ画面は次も開く。

「答える」は、人が答える問い(依頼の種類、始める前に確認してほしかったか、AI が変えたファイルは頼んだことに必要な物だけか、頼まれたことを終えたか)の待ちを 1 列にまとめ、1 枚ずつ出す。札の先頭とボタンの上にその問いの文、中に今回の依頼・最後の返答・変えたファイル・その後の人の返事、下に貼り付いた答えのボタン。キーでも答えられる(1〜9 で選ぶ、Enter で AI と同じ、→ でスキップ)。待っている数は「答える」の横に出る。AI だけが答える問い(wiki の矛盾・差し込み先)と、結果から正解が付く記録は出ない。聞き方は、エージェントが新しい聞き方に変えた時(訳付きで「このままでよい / 元に戻す」)と、意味が同じか決めきれなかった時(訳と違いの見立て付きで「意味は同じ / 違う」)だけここに出る。hook の「あとで答える」の URL で来た時は、その判定が先頭に出る。

「AI の様子」は、普段は見なくてよい物の置き場。問いごとの学習の進みが 1 行ずつ並び、押すとその問いの欄(進み具合、最後の自動較正、まだ答えていない分・結果で確定する分・答えた分の列、答えの直し、聞き方の候補)が出る。届けた教訓と効いたか、作業の前と後の手順、止める検査と最後の結果、使っている判定モデルと人を待つ判定の割合もここにある。

「知識」は「教訓」と「作業記録」の 2 つのタブ。教訓のタブの一番上には「このプロジェクトで決められたルール」の欄があり、人や AI が決めたルールが、場面の絞り込みと関係なく全部並ぶ(埋もれないように)。「いつも」のルールが先頭、次に新しい順で、最近 7 日に増えた・変わったルールには「新」の印が付く。その下で、左で場面を選び、真ん中に種類ごとの題、題を押すと右に本文・当てはまる時・背景・関連・出典が出る。場面を選ばない時は種類ごとに役立った回数の多い順に 4 本ずつ出し、残りは「ほか N 本を見る」で開く。種類と場面の無い記事は「未分類」にまとまり、エージェントが依頼のたびに減らす。上の「あなたに見てほしいもの」には、機械が決められない例外だけが出る: 害があった教訓、繰り返された訂正、有効にする前の止める検査。人が記事を 1 本ずつ確かめる作りではない。検索・見直し・振り返りの数、前からの記事の木、以前の主張は、畳んだ「記事の詳しい記録」の中にある。

### 棚卸し: `shikii status`

```sh
bin/shikii status --store .shikii .shikii/decisions/prompt-kind.yaml
# prompt-kind: records=1 labels=1
#   by source: outcome=1   by truth: !edit=1
#   jev-1.14.0 @ 68ed82631e63f2a2   1  collecting (min_n 50)
```

spec ごとに記録とラベルの数、ラベルの出どころ(ask / outcome / file)と中身の偏り、(model, spec_hash) ごとの、線を引く時に数える正解の数(入力ごとに最新、人と結果の正解だけ。画面と同じ数)と状態
(`calibrated: vector` / `collecting (min_n …)` / `only one answer so far`(片方の答えしか無く線を引けない) / `tried: the answers could not be separated`(試したが分けられない。正解が増えたら再び試す) / `stopped: the probabilities do not tell the answers apart (AUC …)`(線が 1 つも無い hook の問いで、見分ける力が足りず止めた。依頼の 20 回に 1 回だけ聞き、聞き方が変わるか、その 1 回に別のモデルが答えるとまた試す) / `enough truths: the line is drawn automatically`(自動で引く途中) / `stale hash`)を出す。運用で最初に見る画面。結果から正解を付ける問いには `outcome truths checked by a person: …` の行が出て、画面で人が確かめた数(一致が決まらない間は 10 件に 1 件を出す)、合った数、線を引くのに使うかを言う。

`person's work` の行は、今の規則で人が答える判定(結果が答えない判定の迷い、線のある自動判定の抜き取り、AIの仮採点の抜き取りと保留)の数を、全体と直近7日の1000判定あたりで示し、正解を人と結果のどちらが決めたかを分けて数える。`rungs` の `left to a person` は迷った数で、結果が答える判定の迷いは人を待たない。人の手間を減らす変更の効果は、この行の前後で比べる。

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
| `identity` | どれでも | 定数なし。生の分布で act での precision が足りると検証した印(Jev のような較正済みモデル向け) |

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
| `shikii doctor` | 鍵・手元のモデル・置き場の状態と、hook と Codex の MCP の登録が動かす shikii の実行ファイルが今もあって動かせるかを確かめる(無い時は直し方を出す)。確かめるのは shikii が書く形の hook(単引用符のパスか裸のパスで、本体の名が `shikii`)だけ。相対のパス(`~/…`・`$HOME/…`)の shikii の hook は確かめていないと言い、`hook install` が絶対パスに書き直す。ほかの書き方(二重引用符・`\`・出来事の後ろに語が続く)の物は確かめていないと言い、`hook install` も `uninstall` も触らない |
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
| `shikii wiki query` / `capture` / `ingest` / `classify` / `maintain` / `lint` / `verify` | wiki の検索・原資料・登録・種類と場面を付ける・見直し・点検・主張の再検証 |
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
| `POST /v1/decide` | `{"spec","inputs":{…},"tainted":false}` | 判定(band、top、p、trace_id、receipt) |
| `POST /v1/label` | `{"spec","trace_id","truth","weight":1,"source":"ui"}` | 付けた正解(文脈の段を通った判定は 2 段に付く) |
| `GET /v1/queue?spec=<id>` | | 未回答(人待ちの記録と、結果待ちの記録。各項目に `waits_for_person`。やり直した元は出ない) |
| `GET /v1/answered?spec=<id>&limit=30` | | 回答済み(人の正解だけ) |
| `POST /v1/redo` | `{"spec","trace_id"}` | 今の聞き方と線で同じ本文を再判定し、前と今を並べて返す(新しい記録、人の答えは写る) |
| `GET /v1/status` | | spec ごとの状態(線の有無、正解の数、人の言葉の質問) |
| `GET /v1/tune?spec=<id>` · `POST /v1/tune/run` · `POST /v1/tune/variant` · `POST /v1/tune/adopt` | `{"spec","propose"}` · `{"spec","name","question","options"}` · `{"spec","run","meaning_same":true}` | 聞き方の改善の最新と今の候補・裏で回す・人が書いた候補を足す(検証してから。採用はしない)・採否 |
| `POST /v1/tune/review` | `{"spec","run","keep":true}` | エージェントが意味を確かめて自動で採用した聞き方を、人が「このままでよい」(true)か「元に戻す」(false。採用した版は `.reverted` に名を変えて残す) |
| `POST /v1/tune/sync` | `{"spec","propose"}` | その場で聞き方の改善を回して結果を返す(MCP の `shikii_tune` が使う。採用はしない) |
| `GET /v1/wiki` · `POST /v1/wiki/claim` | | wiki の主張の一覧 · 主張の状態を変える(promote / delete。変えるのは人だけ) |
| `POST /v1/wiki/notebook` | `operation: query / ingest / capture / maintain / lint / handoff_list / handoff_read / handoff_write / handoff_append` | プロジェクトのwiki。原資料と記事の検索・登録・見直し・点検。教訓は仮説。handoff_* は作業記録の読み書き(MCP と同じ操作。画面そのものは読むだけ) |
| `GET /v1/handoff/file?path=` | `path`(.shikii/handoff/ からの相対) | 作業記録の 1 件。画像は画像、文字と HTML は text/plain、それ以外は保存。置き場の外は 400 |
| `POST /v1/review` | `operation: query / submit` | 会話と結果から仮採点。原文・対象・訂正例を取得し、根拠付きで保存。人の確認は抜き取りと保留 |
| `POST /v1/reflect` | `operation: query / submit`、`reviews` / `wiki` / `memory` / `maintenance` | 採点・wiki整理・届いた記憶と訂正の観察、記事の見直しをまとめて確認・保存(query は古い原資料と見直しの対象を 2 件ずつ見せ、submit は会話・項目・見直しの数を限らない。既存の記事に足す時、元の出典は引き継がれる)。`complete`と各`error`を確認する |
| `GET /v1/specs` | | 読み込んでいる spec の一覧 |

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
