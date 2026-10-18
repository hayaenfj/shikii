# 接続例: Claude Code の hook(`shikii hook`)— AI 自身の作業中の判断を測る

業務の判定(問い合わせの振り分けなど)はエージェントが MCP で呼ぶ([mcp.md](mcp.md))。これとは別に、**AI 自身が作業の最中にしている判断**を shikii に通すのがこの接続。判断の瞬間はツール呼び出しの直前・依頼の直後・ターンの終わりなので、エージェントが思い出して呼ぶ口ではなく、Claude Code の hook で必ず呼ぶ。

**Claude Code の hook だけ**。Cursor / OpenCode / Codex には `shikii init` で MCP と skill が入るが、作業中の判断を自動で測る口は無い(付けるなら下の「他のエージェントに付けるとき」)。帯(act / ask / discard / abstain)・線(較正)・receipt の言葉は [bands.md](../skills/shikii/references/bands.md)。

測るのは 4 つ(spec は [`claude-code/specs/`](claude-code/specs/)):

| spec | いつ | 問い | 正解の出どころ |
|---|---|---|---|
| `command-effect` | PreToolUse(Bash) | このコマンドは 読むだけ / プロジェクト内を変える / 外を変える / 消す・戻せない / 外に送る のどれか(複数当たるなら重い方) | 人が画面で答える |
| `edit-in-scope` | PreToolUse(Edit / Write / MultiEdit / NotebookEdit) | この編集は、人が最後に送った依頼の範囲か | 人が画面で答える |
| `needs-plan` | UserPromptSubmit | この依頼は、手を動かす前に進め方を人と合わせるべきか | 人が画面で答える |
| `prompt-kind` | UserPromptSubmit | この依頼で AI は結局何をするか(edit / explain / investigate / run / decide) | **結果から自動**: 次の依頼が来た時(無ければターンの終わり)に作業ツリーの中身が変わっていれば edit、変わっていなければ !edit(「edit ではなかった」の部分正解、重み 0.7) |

`status` の「1000 判定あたりの人の判断」が使うほど減り、act の判定に receipt(人が確かめた件数と精度)が付く。これが「AI の確かさが上がった」の数字になる。

## 入れる・外す

```sh
shikii up                    # 手元のモデル(:8080)。立っていないと hook は黙って何も記録しない
which shikii                 # Claude Code がそのまま `shikii` を呼ぶので、PATH に要る
shikii hook install          # 何をするかを表示するだけ
shikii hook install --yes    # 実行。新しいターミナルで claude を起こし直すと効く
shikii hook uninstall --yes  # 足した 3 つの hook だけ外す。記録と正解(~/.shikii/agent)は残る
```

`install` がすること(2 回目は何も足さない):

1. `~/.shikii/agent/` に基本構成(`decisions/` `wiki/` `.shikii/`)。4 つの spec と既製の wiki の知識を、**無いものだけ**写す(線の入った spec・人が直した spec は触らない)
2. `~/.claude/settings.json` の `hooks` に `PreToolUse`(matcher `Bash|Edit|Write|MultiEdit|NotebookEdit`)、`UserPromptSubmit`、`Stop` の 3 つ(command は `shikii hook <event>`、timeout は PreToolUse / UserPromptSubmit 10 秒、Stop 15 秒)。他の hook と鍵は残す。初めて settings.json に手を入れる時(install でも uninstall でも)、shikii の hook が入っていない直前の内容を `settings.json.bak-shikii` に控える(1 回だけ。元から settings.json が無ければ作らず、install が作った settings.json は控えない)

固定の規則(オプションは無い): 置き場は `~/.shikii/agent`(プロジェクトをまたいで 1 つ)、モデルは手元の `http://127.0.0.1:8080`(`shikii up`)、答え合わせの画面は `:8096`(プロジェクトの `serve` は 8095)。spec は全部 `send_to: [local]`、`pii: true` で、コマンドや依頼の文はこの機械の外に出ない。1 回の判定は手元のモデルで 0.1〜0.5 秒、hook 1 回(起動と記録を含む)で 0.6〜0.8 秒。ツール呼び出しごとにこれが足される。モデルが無ければ待たずに黙る。

答え合わせの画面:

```sh
cd ~/.shikii/agent && shikii serve --port 8096 --local http://127.0.0.1:8080
```

## 影の期間 → 帯で振る舞う

shikii は線(較正)が無いうちは `act` を出さない。hook はそれをそのまま「線が無いうちは口を出さない」にしている。ツールは一切止めず、記録だけが溜まる。入れた日から出るのは、ターンの終わりの「答え待ち N 件 → URL」の 1 行だけ(下)。

線を引くのは機械: 人の正解(入力ごとに 1 件。同じコマンドを 50 回答えても 1 件)が `eval.min_n`(50)に届くと、画面(`serve`)で答えた直後はその serve が、そうでなければ次のターンの終わりに hook が(4 spec とも。prompt-kind の結果の正解は画面を通らないのでここでしか引けない)、線を引いて spec に書く。hook は `shikii calibrate --write` を切り離した別プロセスで起こす(文言を変えた後の聞き直しが長くても hook の timeout に掛からない。同じ spec は 10 分に 1 回まで)。結果は `hook.log` の `auto-calibrate …` の行。そこから hook が振る舞いを変える。

| spec | 判定 | hook が出すもの |
|---|---|---|
| どれでも | 線が無い(影) | 何も出さない(記録だけ) |
| command-effect | act で `read` / `change_project` | 何も出さない(黙って通す) |
| command-effect | act で `change_outside` / `destroy` / `send_outside` | 確認を出す(`permissionDecision: ask`。理由は `shikii: this command looks like destroy (p=0.91; line on 61 truths, human-checked precision 0.96)`)。`--dangerously-skip-permissions` の時は確認を出さず端末に同じ 1 行 |
| command-effect | ask / discard だが、重い 3 択(change_outside / destroy / send_outside)の確率の和が act 以上(例: destroy 0.45 + send_outside 0.40) | 確認を出す(理由は `shikii: this command looks like one of change_outside / destroy / send_outside (together p=0.85…)`)。どれであっても止めて人に聞く判断は同じなので、割れを「迷い」で終わらせない |
| command-effect | ask / discard(線はあるが迷った) | 端末に 1 行 `shikii: unsure (command-effect destroy p=0.62). Answer later: http://127.0.0.1:8096/?spec=command-effect&trace=…` |
| edit-in-scope | discard(確信して「範囲外」) | エージェントに文脈 + 端末に 1 行 `shikii: this edit looks outside what the user asked for (edit-in-scope p(yes)=0.12…). If it is a needed step, say why; if not, do not make it` |
| needs-plan | act | エージェントに文脈 `shikii: this request looks like one to agree on an approach with the user before changing anything (needs-plan p=0.88…). Consider EnterPlanMode or a one-line confirmation first` |
| needs-plan / edit-in-scope | ask | 端末に 1 行(URL つき) |
| prompt-kind | 何でも | 何も出さない(結果の正解のためにツリーのハッシュを控える) |
| (Stop) | 線が無くても | 新しく答え待ちになった判定があれば 1 行(件数と URL)。無ければ黙る |

**deny は出さない**。止めるかどうかは人。hook が出すのは「黙る / 確認を出す / 注意の文」の 3 つだけ。

文は英語。`~/.claude/settings.json` の `language` が日本語なら、同じ文を日本語で併記する。他の言語は文は英語のままで、記録の `display` に spec の `display.<lang>` の質問と答えの名前(あれば)と言語だけが入る。例: `shikii: this command looks like destroy (p=0.91) / shikii: このコマンドは「消す・上書き・戻せない変更」に見える(p=0.91)`。記録(`records/<spec>.jsonl`)にも `display` として言語・質問・答え・出した文が残るので、「何と言われて何をしたか」を後から辿れる。英語の設定なら英語だけで、`display` は付かない(spec に `display.<lang>` が無く出した文も無い時も付かない)。

ターンの終わり(Stop)には、そのセッションの判定で人の答えを待つもの(迷った判定と、act の 20 件に 1 件の抜き取り。線が無い間は、生の確率が spec の `thresholds.ask` 以上の判定が「迷った」になり、それ未満の discard は記録だけ)が新しく増えていれば 1 行だけ出す: `shikii: 3 of this session's decisions wait for your answer: http://127.0.0.1:8096/`。画面が立っていなければ URL の代わりに起動コマンド。同じ数を毎ターン繰り返さない。URL の `?spec=<id>&trace=<trace_id>` で開くと、その 1 件が先頭に来る(答え済み・列に無い trace は元の順のまま)。

## 失敗しても止めない

モデルが無い・5 秒で返らない・store に書けない → hook は exit 0 で何も出さず、理由を `~/.shikii/agent/.shikii/hook.log` に 1 行書く。hook が作業を止めることはない。Claude Code 側で `shikii` が見つからない(PATH)時は hook.log も書かれないので、`which shikii` と Claude Code の `--debug` で見る。

## 中の形

```
UserPromptSubmit ─ shikii hook user-prompt-submit ─ 前の依頼の pending に結果の正解(今のツリーと比べる)
                                                     decide prompt-kind, needs-plan
                                                     └ sessions/<id>.json に request と作業ツリーのハッシュを控える
PreToolUse ─────── shikii hook pre-tool-use ──────── Bash → decide command-effect / Edit… → decide edit-in-scope(request は控えから)
Stop ───────────── shikii hook stop ──────────────── 残った pending に結果の正解。4 spec の正解が min_n に届いていれば線を引く
                                                     └ last_assistant_message を控える(needs-plan の文脈の段に使う)。答え待ちの要約を 1 行
```

- transcript は読まない(形が文書化されていない)。セッションごとの控えは `~/.shikii/agent/.shikii/sessions/<session_id>.json`(request、previous、pending、asked。7 日で消える)。Claude Code は hook を並列に走らせるので、控えは flock の中で 読む → 変える → 書く(tmp に書いて rename)。錠は短く持ち(モデルの呼び出しは錠の外)、2 秒待っても取れなければ錠なしで進む
- 作業ツリーのハッシュは、仮の index に `git add -A`(.gitignore は尊重、未追跡も入る)して `git write-tree` した tree の id。HEAD や index は入れないので、commit / stash / checkout だけでは変わらず、ファイルの中身が変わった時だけ変わる。新しい blob は仮の objects に書いて消すので、未追跡ファイル(`.env` など)の中身が repo の `.git/objects` に残らない。全体で 3 秒を超えたら(.gitignore の無い巨大な node_modules など)正解を付けない。限界: 並走する別のエージェントやフォーマッタの変更も「変わった」に数える
- 短い返事(40 字未満。「ok」「yes go ahead」)は依頼を置き換えず前の依頼に添える(edit-in-scope が「ok」を依頼として読まないように)
- Claude Code 側の契約(公式の hooks reference、2026-10-18): hook は stdin に `session_id` `cwd` `permission_mode` `tool_name` `tool_input` `prompt` `stop_hook_active` `last_assistant_message` を受け、stdout の `hookSpecificOutput.permissionDecision`(allow / ask / deny)と `additionalContext`、トップレベルの `systemMessage` を返せる。人が許可を拒んだことを知る event は無いので、command-effect の正解は画面からだけ

## claude-jev から移る人へ

1. 先に claude-jev を置いた場所の `uninstall.mjs`(例: `node ~/dotfiles/claude-jev/uninstall.mjs --yes`)で jev を外す(両方入っていると同じ event で 2 回判定される)
2. Node は要らない。置き場は `~/.claude/jev/` → `~/.shikii/agent/`。古い記録(`~/.claude/jev/decisions-store`)は引き継がない(spec の文言が違うので線の材料にならない)
3. 固定の閾値(0.75 で進め、0.60 で確認)は無い。線が引けるまで何もせず、引けたら帯で振る舞う
4. prompt-intent → prompt-kind(同じ 5 択)。モデルの振り分け(jev-router)は無い

## 他のエージェントに付けるとき

必要なのは 3 つだけ:

1. 判定したい場所で `shikii decide <spec> --json -i key=value` を呼び、`band` と `trace_id` を受け取る
2. 結果が分かる場所(終了時・取り消し時・利用者が選んだ時)で `shikii label <trace_id> <truth> --source outcome` を呼ぶ。「それではなかった」しか分からなければ `!option`、弱ければ `--weight 0.7`
3. 送ってはいけない文脈(個人情報・本番データ)では `--tainted` を付ける。spec の `send_to` に `local` が無ければ abstain になる

## skill(作法)と MCP(口)と hook の使い分け

- **hook**(この文書): AI 自身の動きを、常に自動で測る。人も Claude も意識しない
- **MCP**(`shikii mcp`、[mcp.md](mcp.md)): 業務の判定を、エージェントやプログラムが「今この判定が要る」と自分で呼ぶ口
- **skill**(`skills/shikii/SKILL.md`): shikii を使って仕事をする時の作法。いつ判定を呼ぶか、帯にどう従うか、正解と線と状態は人のもの、tune の採用は人の「意味は同じ」の後、本文を外に出さない判定。CI は `scripts/check-skill.sh` で前書きと参照の実在を見る
