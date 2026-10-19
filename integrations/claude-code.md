# 接続例: Claude Code と Codex の hook(`shikii hook`)— AI 自身の考え方を、考える前と考え終わった後の 2 点で測る

業務の判定(問い合わせの振り分けなど)はエージェントが MCP で呼ぶ([mcp.md](mcp.md))。これとは別に、**AI 自身の考え方**を shikii に通すのがこの接続。測る瞬間は 2 つだけ: 人の依頼を受けて考える前と、考え終わってターンを閉じる時。エージェントが思い出して呼ぶ口ではなく、Claude Code / Codex の hook で必ず呼ぶ。

ツール呼び出しごと(Bash や Edit の直前)の hook は持たない。コマンド 1 つ 1 つの分類は AI の考え方ではなく作業の雑音で、1 依頼に何十回も走って依頼まわりの判定を沈める(実測: コマンドの判定 4606 件に対し依頼の判定 84 件)。許可の是非は Claude Code の permission と deny の規則が既に見ている。

**Claude Code と Codex の hook**(両方とも同じ形の hook を持つ。`install` は両方の設定に書く)。Cursor / OpenCode には `shikii init` で MCP と skill が入るが、考え方を自動で測る口は無い(付けるなら下の「他のエージェントに付けるとき」)。帯(act / ask / discard / abstain)・線(較正)・receipt の言葉は [bands.md](../skills/shikii/references/bands.md)。

測るのは 4 つ(spec は [`claude-code/specs/`](claude-code/specs/))。前の 2 つは見立て、後の 2 つは自己評価:

| spec | いつ | 問い | 正解の出どころ |
|---|---|---|---|
| `prompt-kind` | 考える前(UserPromptSubmit) | この依頼で AI は結局何をするか(edit / explain / investigate / run / decide) | **結果から自動**: 次の依頼が来た時(無ければターンの終わり)に作業ツリーの中身が変わっていれば edit、変わっていなければ !edit(「edit ではなかった」の部分正解、重み 0.7) |
| `needs-plan` | 考える前(UserPromptSubmit) | この依頼は、手を動かす前に進め方を人と合わせるべきか | 人が画面で答える |
| `changes-in-scope` | 考え終わった後(Stop) | このターンで変えたファイル(依頼の時と今の作業ツリーを比べた一覧)は、依頼の範囲の中だったか。何も変えていなければ聞かない | 人が画面で答える |
| `done-as-asked` | 考え終わった後(Stop) | 依頼・変わったファイル・最後の返答を見て、頼まれたことを終えたか(人がもう一度言い直すことは残っていないか) | 人が画面で答える |

依頼 1 回につき判定は 3〜4 件(ツール呼び出しの数に依らない)。`status` の「1000 判定あたりの人の判断」が使うほど減り、act の判定に receipt(人が確かめた件数と精度)が付く。これが「AI の確かさが上がった」の数字になる。

**置き場は 1 つ、記録には仕事の印。** `~/.shikii/agent` はプロジェクトをまたいで 1 つで、線も spec ごとに 1 本(問いは AI の考え方についてのもので、どの repo でも同じ基準で正解が付く。プロジェクトごとに分けると小さい repo では線が一生引けない)。その代わり、記録には `project`(cwd の repo のルートのフォルダ名。git の外なら cwd の名)が付き、画面は 2 つ以上の仕事があれば「仕事ごと」の絞り込みを出し、カードに印を見せる。`status` は `by project: haya1=12 okurun-ai=3` を出す。入力ではないので、モデルへの問いもハッシュも変わらない。

## 入れる・外す

```sh
shikii up                    # 手元のモデル(:8080)。立っていないと hook は黙って何も記録しない
which shikii                 # Claude Code がそのまま `shikii` を呼ぶので、PATH に要る
shikii hook install          # 何をするかを表示するだけ
shikii hook install --yes    # 実行。新しいターミナルで claude を起こし直すと効く
shikii hook uninstall --yes  # 足した 2 つの hook だけ外す。記録と正解(~/.shikii/agent)は残る
```

`install` がすること(2 回目は何も足さない):

1. `~/.shikii/agent/` に基本構成(`decisions/` `wiki/` `records/` `labels/`。store そのもの)。4 つの spec と既製の wiki の知識を、**無いものだけ**写す(線の入った spec・人が直した spec は触らない)
2. `~/.claude/settings.json`(Claude Code)と `~/.codex/hooks.json`(Codex)の `hooks` に `UserPromptSubmit` と `Stop` の 2 つ(command は `shikii hook <event>`、timeout は UserPromptSubmit 10 秒、Stop 15 秒。matcher は無い)。古い版が足した `PreToolUse` の shikii の hook は外す(他の hook と鍵は残す)。Codex は新しい hook を一度人に確かめさせる: Codex の中で `/hooks` を開いて trust する(しないと hook は飛ばされる)。初めて settings.json に手を入れる時(install でも uninstall でも)、shikii の hook が入っていない直前の内容を `settings.json.bak-shikii` に控える(1 回だけ。元から settings.json が無ければ作らず、install が作った settings.json は控えない)

固定の規則(オプションは無い): 置き場は `~/.shikii/agent`(プロジェクトをまたいで 1 つ。これが store で、中に decisions/ wiki/ records/ labels/ sessions/)、モデルは手元の `http://127.0.0.1:8080`(`shikii up`)、答え合わせの画面は `:8096`(プロジェクトの `serve` は 8095)。spec は全部 `send_to: [local]`、`pii: true` で、依頼・返答・ファイル名はこの機械の外に出ない。1 回の判定は手元のモデルで 0.1〜0.5 秒(2 つの問いは並行)、hook 1 回(起動と記録を含む)で 0.6〜1 秒。これが依頼の直後とターンの終わりに 1 回ずつ足されるだけで、ツール呼び出しには何も足されない。モデルが無ければ待たずに黙る。

答え合わせの画面:

```sh
shikii serve --store ~/.shikii/agent --port 8096 --local http://127.0.0.1:8080
```

## 影の期間 → 帯で振る舞う

shikii は線(較正)が無いうちは `act` を出さない。hook はそれをそのまま「線が無いうちは口を出さない」にしている。ツールは一切止めず、記録だけが溜まる。入れた日から出るのは、ターンの終わりの「答え待ち N 件 → URL」の 1 行だけ(下)。

**聞き方を試すのも機械**(wiki の lint に当たる、ゆっくりした周期): spec に `variants:`(人が書いた候補の言い方)があり、人の正解が `eval.min_n` 以上で、前回の自動 tune から正解が 50 件増えていれば、Stop が `shikii tune`(`--write` 無し)を切り離した別プロセスで回し、結果を `wiki/runs/` と `hook.log` の `auto-tune …` の行に残す。候補が無い spec では何もしない(試す物が無い)。採用は人が意味を確かめてから(画面か `tune --write`)。前回の数は `tuned-<spec>` に控える。

線を引くのは機械: 人の正解(入力ごとに 1 件。同じ依頼文を 50 回答えても 1 件)が `eval.min_n`(50)に届くと、画面(`serve`)で答えた直後はその serve が、そうでなければ次のターンの終わりに hook が(4 spec とも。prompt-kind の結果の正解は画面を通らないのでここでしか引けない)、線を引いて spec に書く。hook は `shikii calibrate --write` を切り離した別プロセスで起こす(文言を変えた後の聞き直しが長くても hook の timeout に掛からない。同じ spec は 10 分に 1 回まで)。結果は `hook.log` の `auto-calibrate …` の行。そこから hook が振る舞いを変える。

| spec | 判定 | hook が出すもの |
|---|---|---|
| どれでも | 線が無い(影) | 何も出さない(記録だけ) |
| needs-plan | act | エージェントに文脈 `shikii: this request looks like one to agree on an approach with the user before changing anything (needs-plan p=0.88; line on 61 truths, human-checked precision 0.96). Consider EnterPlanMode or a one-line confirmation first` |
| needs-plan | discard(合わせなくてよい) | 何も出さない |
| changes-in-scope | discard(確信して「範囲の外」) | 端末の人に 1 行 `shikii: this turn's changes look outside what you asked for (changes-in-scope p(yes)=0.12…). Check the changed files before going on` |
| done-as-asked | discard(確信して「終えていない」) | 端末の人に 1 行 `shikii: this turn looks unfinished against what you asked (done-as-asked p(yes)=0.15…). Something may be left to ask for again` |
| changes-in-scope / done-as-asked | act(範囲の中 / 終えた) | 何も出さない |
| どれでも | ask(線はあるが迷った) | 端末に 1 行 `shikii: unsure (done-as-asked yes p=0.62). Answer later: http://127.0.0.1:8096/?spec=done-as-asked&trace=…` |
| prompt-kind | 何でも | 何も出さない(結果の正解のためにツリーのハッシュを控える) |
| (Stop) | 線が無くても | 新しく答え待ちになった判定があれば 1 行(件数と URL)。無ければ黙る |

**ツールは止めない。許可の判定(allow / ask / deny)も出さない。** 考える前の問い(needs-plan)はエージェントへの文脈になり、考え終わった後の問いは端末の人への文になる: Stop で文脈を返すと会話が勝手に続いてしまうので、続けるかどうか(「まだ終わってないよ」)は人が決める。hook が出すのは「黙る / エージェントに文脈 / 人に 1 行」の 3 つだけ。

文は英語。`~/.claude/settings.json` の `language` が日本語なら、同じ文を日本語で併記する。他の言語は文は英語のままで、記録の `display` に spec の `display.<lang>` の質問と答えの名前(あれば)と言語だけが入る。例: `shikii: this turn looks unfinished against what you asked (done-as-asked p(yes)=0.15)` の下に `shikii: このターンは頼んだことを終えていないように見える(done-as-asked p(はい)=0.15)`。記録(`records/<spec>.jsonl`)にも `display` として言語・質問・答え・出した文が残るので、「何と言われて何をしたか」を後から辿れる。はい / いいえの問い(noul)の答えの名前は p(yes) が 0.5 未満なら「いいえ」側(終えていない、範囲の外)。英語の設定なら英語だけで、`display` は付かない(spec に `display.<lang>` が無く出した文も無い時も付かない)。

ターンの終わり(Stop)には、そのセッションの判定で人の答えを待つもの(迷った判定と、act の 20 件に 1 件の抜き取り。線が無い間は、生の確率が spec の `thresholds.ask` 以上の判定が「迷った」になり、それ未満の discard は記録だけ)が新しく増えていれば 1 行だけ出す: `shikii: 3 of this session's decisions wait for your answer: http://127.0.0.1:8096/`。画面が立っていなければ URL の代わりに起動コマンド。同じ数を毎ターン繰り返さない。URL の `?spec=<id>&trace=<trace_id>` で開くと、その 1 件が先頭に来る(答え済み・列に無い trace は元の順のまま)。

## 失敗しても止めない

モデルが無い・5 秒で返らない・store に書けない → hook は exit 0 で何も出さず、理由を `~/.shikii/agent/hook.log` に 1 行書く。hook が作業を止めることはない。Claude Code 側で `shikii` が見つからない(PATH)時は hook.log も書かれないので、`which shikii` と Claude Code の `--debug` で見る。

## 中の形

```
UserPromptSubmit ─ shikii hook user-prompt-submit ─ 前の依頼の pending に結果の正解(今のツリーと比べる)
                                                     decide prompt-kind, needs-plan(並行)
                                                     └ sessions/<id>.json に request と作業ツリーのハッシュ、<id>.files に作業ツリーの一覧を控える
Stop ───────────── shikii hook stop ──────────────── 残った pending に結果の正解
                                                     依頼の時の一覧と今の一覧を比べて「変わったファイル」→ decide changes-in-scope, done-as-asked(並行。同じ依頼で 1 回だけ)
                                                     └ last_assistant_message を控える(needs-plan の文脈の段に使う)。答え待ちの要約を 1 行。4 spec の正解が min_n に届いていれば線を引く
```

- transcript は読まない(形が文書化されていない)。セッションごとの控えは `~/.shikii/agent/sessions/<session_id>.json`(request、previous、pending、asked、judged。7 日で消える)と `<session_id>.files`(依頼の時の `git ls-files --stage` の一覧。4 MB まで)。Claude Code は hook を並列に走らせる(Stop と次の依頼が重なる)ので、控えは flock の中で 読む → 変える → 書く(tmp に書いて rename)。錠は短く持ち(モデルの呼び出しは錠の外)、2 秒待っても取れなければ錠なしで進む
- 作業ツリーの状態は、仮の index に `git add -A`(.gitignore は尊重、未追跡も入る)して `git write-tree` した tree の id(ハッシュ)と、その index の一覧(ファイルごとの blob の id)。HEAD や index は入れないので、commit / stash / checkout だけでは変わらず、ファイルの中身が変わった時だけ変わる。新しい blob は仮の objects に書いて消すので、未追跡ファイル(`.env` など)の中身が repo の `.git/objects` に残らない(一覧にも中身は入らない。path と id だけ)。全体で 3 秒を超えたら(.gitignore の無い巨大な node_modules など)正解を付けず、変わったファイルも分からない扱い。git の外では prompt-kind の正解と changes-in-scope は無く、done-as-asked の files は `(unknown: not a git working tree)`。限界: 並走する別のエージェントやフォーマッタの変更も「変わった」に数える
- 変わったファイルの一覧は問いに最大 30 件(残りは `... and N more`)。何も変えていなければ changes-in-scope は聞かず、done-as-asked の files は `(none)`
- 短い返事(40 字未満。「ok」「yes go ahead」)は依頼を置き換えず前の依頼に添える(ターンの終わりの問いが「ok」を依頼として読まないように)。短い返事でも新しい依頼として judged は戻るので、「ok」の後のターンもまた評価される
- Claude Code 側の契約(公式の hooks reference、2026-10-18): hook は stdin に `session_id` `cwd` `prompt` `stop_hook_active` `last_assistant_message` を受け、stdout の `hookSpecificOutput.additionalContext`(UserPromptSubmit)とトップレベルの `systemMessage` を返せる。Stop で `hookSpecificOutput` を返すと会話が続くので返さない
- Codex 側の契約(learn.chatgpt.com/docs/hooks、2026-10-19): 同じ event 名と stdin の鍵(`Stop` にも `last_assistant_message` と `stop_hook_active`)に、Codex だけの `turn_id` と `model` が足される。Stop の出力は JSON が必須(shikii は常に JSON)。shikii は Codex と Claude Code を見分けない(両方同じ 2 つの問い)

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

- **hook**(この文書): AI 自身の考え方を、依頼の直後とターンの終わりに常に自動で測る。人も Claude も意識しない
- **MCP**(`shikii mcp`、[mcp.md](mcp.md)): 業務の判定を、エージェントやプログラムが「今この判定が要る」と自分で呼ぶ口
- **skill**(`skills/shikii/SKILL.md`): shikii を使って仕事をする時の作法。いつ判定を呼ぶか、帯にどう従うか、正解と線と状態は人のもの、tune の採用は人の「意味は同じ」の後、本文を外に出さない判定。CI は `scripts/check-skill.sh` で前書きと参照の実在を見る
