# 正解・較正・審判の約束

- 正解の出どころ(source): `ui` / `manual` / `ask` / `audit` / `outcome` は人か結果。`context`(上の段の答え)と `judge`(審判)は機械。`replay` / `file` は確率を取り直しただけで正解は人のもの
- 線を書く条件: 人の正解が `min_n` 以上。機械の正解だけでは書かれない。`check` は人の正解だけでの act の精度も見る
- 聞き直し(replay): 文言やモデルが変わっても、本文の残っている正解は calibrate が今のハッシュで聞き直す。人は要らない
- 審判(`shikii judge`): 別の大きいモデルに同じ spec を聞く。先に人の正解との一致率を測り、act に届く審判だけが、答えの無い記録に重み=一致率の正解を付ける。審判も迷えば人へ。審判は人の正解を置き換えない(人は審判を抜き取りで検査する)
- tune の採用: holdout が現状を上回り、片側の符号検定で p ≤ 0.05、さらに人が「意味は同じ」と確かめた時だけ。あなたが `--write` を打つのは、人がその候補を読んで了承した後
- wiki(`.shikii/wiki/`): `wiki/specs/<id>.md` の Rules はその判定の細則(人が書く)。`wiki/patterns/` は主張(claim / applies_to / evidence / retest)。tune の勝者は `wiki/patterns/drafts/` に下書きで入る。状態を変えるのは人だけ。`shikii wiki verify` で今のモデルで再検証して証拠を足せる
