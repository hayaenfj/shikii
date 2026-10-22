# 正解・較正・審判の約束

- 通常のエージェントの仮採点は `shikii review`（MCP は `shikii_review`）。会話を受けて範囲と完了を採点し、根拠不足は保留にする。`records/reviews/` に保存し、下の正解ラベル・較正には混ぜない。人の無作為の確認と保留の確認を分け、訂正を次の採点に戻す。手順は [review.md](review.md)

- 正解の出どころ(source): `ui` / `manual` / `ask` / `audit` / `outcome` は人か結果。`context`(上の段の答え)と `judge`(審判)は機械。`replay` / `file` は確率を取り直しただけで正解は人のもの
- hook の結果の正解(source `outcome`、AIの解釈は使わない): prompt-kind は依頼の間に作業ツリーが変わったか。changes-in-scope は変えたファイルが次の人の依頼の間に元へ戻されたら no(重み1)、次の次の依頼まで残れば yes(重み0.5)。done-as-asked は次の人の発言が同じ依頼の言い直しなら no(重み1)、はっきり別の依頼なら yes(重み0.5)、短い返事と中間は付けない。結果の付く判定は迷っても人を待たない
- 線が引けない時: 正解が `min_n` に届いて自動較正が線を書けなかったら、hook がエージェントに聞き方の候補を頼む(`shikii hook variants submit`、最大3件)。同じ文言とモデルの自動較正は正解が20件増えるまで繰り返さない。上回った候補は Stop で人に一度だけ伝わり、採用は人が意味を確かめてから
- 線を書く条件: 人の正解が `min_n` 以上。機械の正解だけでは書かれない。`check` は人の正解だけでの act の精度も見る
- 聞き直し(replay): 文言やモデルが変わっても、本文の残っている正解は calibrate が今のハッシュで聞き直す。人は要らない
- 審判(`shikii judge`): 別の大きいモデルに同じ spec を聞く。先に人の正解との一致率を測り、act に届く審判だけが、答えの無い記録に重み=一致率の正解を付ける。審判も迷えば人へ。審判は人の正解を置き換えない(人は審判を抜き取りで検査する)
- tune の採用: holdout が現状を上回り、片側の符号検定で p ≤ 0.05、さらに人が「意味は同じ」と確かめた時だけ。あなたが `--write` を打つのは、人がその候補を読んで了承した後
- wiki(`.shikii/wiki/`): `wiki/specs/<id>.md` の Rules はその判定の細則(人が書く)。`wiki/patterns/` は主張(claim / applies_to / evidence / retest)。tune の勝者は `wiki/patterns/drafts/` に下書きで入る。状態を変えるのは人だけ。`shikii wiki verify` で今のモデルで再検証して証拠を足せる
