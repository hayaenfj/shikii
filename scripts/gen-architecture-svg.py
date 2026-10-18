# shikii の構造の図(日本語と英語)。文字幅を見積もって枠を決める(はみ出し防止)。
import unicodedata, sys
W, H = 1500, 780
def est(text, size):
    w = 0.0
    for ch in text:
        if unicodedata.east_asian_width(ch) in ("W", "F"): w += size * 1.02
        elif ch == " ": w += size * 0.32
        elif ch.isupper() or ch.isdigit(): w += size * 0.72
        else: w += size * 0.63
    return w * 1.12  # 見積もり(等幅換算)に 12% の余裕: 手元の描画(rsvg)とブラウザ(Chrome/Safari の既定フォント)で同じ文を測ると、ブラウザが最大 10% 広かった(2026-10-18)。超える行には textLength を付けて枠内に収める  # ブラウザのフォントは手元の描画より広い。見積もりに余裕を持たせる
def esc(s): return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
STR = {
 "ja": dict(title="shikii の構造", sub="問いと正解は資産。モデルは使い捨て。知識は検証できる主張として wiki に溜まり、測るたびに自分で育つ。",
   human="人がすること（この 3 つだけ）", h1="意図を書く（spec と wiki の細則）", h2="抜き取りの正解に答える（迷い + act の 20 件に 1 件）", h3="主張の意味を確かめる（下書きを一般化し、確か / 却下を決める）",
   asset="資産（モデルを替えても残る）", a1="decisions/  問い", a1a="文言・選択肢・並べ方・配置・分解・文脈", a1b="ハッシュ = 聞き方 1 つにつき線 1 本",
   a2=".shikii/  正解の貯金", a2a="系譜つき: 人の正解 ＞ 機械の正解（審判・上の段）", a2b="本文と一緒に残り、文言やモデルが変わると聞き直す",
   a3="領収書", a3a="act の判定ごとに: どの線を、いつ、何件の正解で、", a3b="そのうち人の分、人が確かめた act の数と精度", a3c="「95% の確信」ではなく「人が検査した 48 件で精度 0.96」",
   loop="機械が育てる輪", n1=("decide","帯 act / ask / discard"), n2=("serve  答え合わせ","迷い → 人、抜き取り"), n3=("calibrate  線","ハッシュ × モデルごと、人の正解で"),
   n4=("tune  聞き方を測る","holdout + 符号検定 + 人の確認"), n5=("段階","最小 → 文脈 → 人"), n6=("judge  審判","人との一致率 ≥ act の時だけ"),
   loopnote="審判も迷えば人へ。act の判定の 20 件に 1 件は人が抜き取りで確かめる",
   wiki="検証できる wiki（知識）", w1="主張 = claim", w1a="+ 効く範囲 applies_to", w1b="+ 証拠 evidence（run ごと、人の正解の数つき）", w1c="+ 再検証の手順 retest（spec、候補 vs 候補）",
   w2="tune が勝者を下書きに起草", w3="別のモデルの run が証拠を足す", w4="wiki verify が今のモデルで測り直す", w5="独立したモデル 2 つが支持 → confirmable", w6="状態を変えるのは人だけ", w7="別の人が自分のモデルで回した結果も証拠になる",
   read="読む", draft="起草・証拠", check="確かめる", intent="意図・正解",
   models="モデル（使い捨て。替えたら calibrate が貯金を聞き直すだけ、人の手は 0）", m1="決定モデル（Kev-4B …）", m2="普通のチャット用モデル + shikii-readout（配置）", m3="ホスト版（鍵は環境変数だけ）", m4="審判は別の大きいモデル（同じ口）",
   mnote="共通の口 POST /v1/systemone。pii: true と本番を読んだ本文は手元のモデルだけ。",
   foot="docs/architecture.md の 4 つの柱: 検証できる wiki / 問いと正解は資産 / 信頼の負担（線・抜き取り・領収書・審判）/ 段階は制約付き最適化"),
 "en": dict(title="How shikii is built", sub="Questions and truths are the asset. Models are disposable. Knowledge accumulates as verifiable claims in a wiki and grows every time something is measured.",
   human="What people do (only these three)", h1="Write the intent (the spec and the wiki rules)", h2="Answer sampled truths (unsure ones + 1 in 20 acted)", h3="Check claims (generalize a draft, confirm or reject)",
   asset="Assets (survive a model swap)", a1="decisions/  the questions", a1a="wording, options, layout, steps, context", a1b="hash: one line per way of asking",
   a2=".shikii/  the bank of truths", a2a="human truths outrank machine truths", a2b="kept with the text; replayed on changes",
   a3="receipts", a3a="per act decision: which line, fitted when,", a3b="on how many truths; human checks; precision", a3c="not \"95% sure\" but \"0.96 on 48 checked by people\"",
   loop="The loop the machine grows", n1=("decide","bands act / ask / discard"), n2=("serve  answer page","unsure → person; audit"), n3=("calibrate  the line","per hash × model"),
   n4=("tune  measure wordings","holdout, sign test, a person"), n5=("ladder","minimal → context → a person"), n6=("judge","agrees with people ≥ act, else no"),
   loopnote="An unsure judge defers to a person. 1 in 20 acted decisions is audited by a person.",
   wiki="Verifiable wiki (knowledge)", w1="a claim", w1a="+ where it applies", w1b="+ evidence (per run, human-truth count)", w1c="+ retest recipe (spec, candidate vs other)",
   w2="tune drafts a claim per winner", w3="runs on other models add evidence", w4="wiki verify reruns on the current model", w5="2 independent models agree → confirmable", w6="only a person changes the status", w7="anyone's run on their model adds evidence",
   read="reads", draft="drafts", check="checks", intent="intent, truths",
   models="Models (disposable: swap one, calibrate replays the bank; no human work)", m1="decision models (Kev-4B …)", m2="plain chat models + shikii-readout (layout)", m3="hosted (key via environment only)", m4="the judge is another, larger model (same door)",
   mnote="One door: POST /v1/systemone. Texts marked pii: true or read from production never leave the machine.",
   foot="The four pillars in docs/architecture.md: verifiable wiki / questions and truths as assets / the burden of trust (lines, audit, receipts, judge) / the ladder as constrained optimization"),
}
def gen(lang, path):
    global W
    S = STR[lang]; out = []
    # 言語ごとの寸法: 英語は文が長いので小さい文字と狭い余白
    P = dict(ja=dict(AW=420, sub=12, pad=40, gap=50, wgap=60, wmin=430), en=dict(AW=400, sub=11, pad=28, gap=40, wgap=50, wmin=400))[lang]
    def text(x, y, s, cls="t", size=None, extra="", maxw=None):
        sz = f' font-size="{size}"' if size else ""
        fit = ""
        if maxw is not None:
            base = size or {"t":14,"h":20,"k":12.5,"ac":14,"gr":14,"bl":14}[cls]
            if est(s, base) > maxw:  # 最後の保険: 枠を超える行は枠内に収める(どの描画器でも溢れない)
                fit = f' textLength="{maxw:.0f}" lengthAdjust="spacingAndGlyphs"'
        out.append(f'<text x="{x}" y="{y}" class="{cls}"{sz}{fit} {extra}>{esc(s)}</text>')
    def pill(x, y, s, size=13, h=30):
        w = est(s, size) + 28
        out.append(f'<rect x="{x}" y="{y}" width="{w:.0f}" height="{h}" rx="{h/2:.0f}" ry="{h/2:.0f}" class="pill"/>'); text(x + 14, y + h/2 + size*0.36, s, "t", size); return w
    def node(x, y, title, sub, size=14, sub_size=None):
        sub_size = sub_size or P["sub"]
        w = max(est(title, size), est(sub, sub_size)) + P["pad"]
        out.append(f'<rect x="{x}" y="{y}" width="{w:.0f}" height="48" rx="12" ry="12" class="pill"/>'); text(x + 14, y + 19, title, "t", size); text(x + 14, y + 38, sub, "k", sub_size); return w
    # 先に横の配置を決めて、必要な幅にする(箱が右端を超えない)
    AX0, AW0 = 40, P["AW"]
    LX0 = AX0 + AW0 + 30
    nw0 = lambda t, u: max(est(t, 14), est(u, P["sub"])) + P["pad"]
    leftw0 = max(nw0(*S["n1"]), nw0(*S["n4"]), nw0(*S["n5"])); rightw0 = max(nw0(*S["n2"]), nw0(*S["n3"]), nw0(*S["n6"]))
    LW0 = 20 + leftw0 + P["gap"] + rightw0 + 20
    WX0 = int(LX0 + LW0 + P["wgap"])
    wneed = max(e for e in [est(S[k], 14 if k in ("w1","w2","w3","w4","w5") else 12.5) + 56 for k in ("w1","w1a","w1b","w1c","w2","w3","w4","w5","w6","w7")])
    WW0 = max(P["wmin"], min(wneed, 520))
    rowh = 60 + sum(est(S[k], 13) + 28 + 16 for k in ("h1","h2","h3")) + 40
    rowm = 60 + sum(est(S[k], 13) + 28 + 16 for k in ("m1","m2","m3","m4")) + 40
    W = int(max(1500, WX0 + WW0 + 40, rowh, rowm))
    out.append(f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {H}" width="{W}" height="{H}" font-family="-apple-system, 'Segoe UI', 'Hiragino Sans', 'Noto Sans JP', 'Helvetica Neue', Arial, sans-serif">
<defs>
<linearGradient id="bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#0f172a"/><stop offset="1" stop-color="#1e293b"/></linearGradient>
<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#94a3b8"/></marker>
<marker id="arrowAmber" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#fbbf24"/></marker>
<style>
.box {{ fill: #1e293b; stroke: #334155; stroke-width: 1.5; rx: 14; }}
.asset {{ fill: #172554; stroke: #3b82f6; }} .wiki {{ fill: #052e16; stroke: #22c55e; }} .human {{ fill: #3b2a05; stroke: #f59e0b; }} .model {{ fill: #1f2937; stroke: #64748b; stroke-dasharray: 6 4; }}
.t {{ fill: #e2e8f0; font-size: 14px; }} .h {{ fill: #f8fafc; font-size: 20px; font-weight: 700; }} .k {{ fill: #94a3b8; font-size: 12.5px; }}
.ac {{ fill: #fbbf24; font-size: 14px; font-weight: 700; }} .gr {{ fill: #86efac; font-size: 14px; font-weight: 700; }} .bl {{ fill: #93c5fd; font-size: 14px; font-weight: 700; }}
.flow {{ stroke: #94a3b8; stroke-width: 2; fill: none; marker-end: url(#arrow); }} .flowA {{ stroke: #fbbf24; stroke-width: 2.5; fill: none; marker-end: url(#arrowAmber); }}
.pill {{ fill: #0f172a; stroke: #475569; stroke-width: 1; }}
</style></defs>
<rect width="{W}" height="{H}" fill="url(#bg)"/>''')
    text(40, 48, S["title"], "h", 26); text(40, 74, S["sub"], "k", 14)
    out.append(f'<rect x="40" y="100" width="{W-80}" height="92" class="box human"/>'); text(60, 130, S["human"], "ac", 15)
    x = 60
    for k in ("h1","h2","h3"): x += pill(x, 146, S[k]) + 16
    AX, AW = 40, P["AW"]
    out.append(f'<rect x="{AX}" y="232" width="{AW}" height="320" class="box asset"/>'); text(60, 262, S["asset"], "bl", 15)
    rows = [(296,"a1","t"),(316,"a1a","k"),(334,"a1b","k"),(370,"a2","t"),(390,"a2a","k"),(408,"a2b","k"),(444,"a3","t"),(464,"a3a","k"),(482,"a3b","k"),(518,"a3c","k")]
    for y,k,c in rows:
        x0 = 60 if c=="t" else 76
        text(x0, y, S[k], c, 12 if (c=="k" and est(S[k],12.5) > AW-50) else None, 'font-style="italic"' if k=="a3c" else "", maxw=AX+AW-16-x0)
    LX = AX + AW + 30
    NL = LX + 20
    nw = lambda t, u: max(est(t, 14), est(u, P["sub"])) + P["pad"]
    leftw = max(nw(*S["n1"]), nw(*S["n4"]), nw(*S["n5"])); rightw = max(nw(*S["n2"]), nw(*S["n3"]), nw(*S["n6"]))
    NR = NL + leftw + P["gap"]
    LW = (NR + rightw + 20) - LX
    out.append(f'<rect x="{LX}" y="232" width="{LW:.0f}" height="320" class="box"/>'); text(LX+20, 262, S["loop"], "t", 15, 'font-weight="700"')
    w1 = node(NL, 282, *S["n1"]); w2 = node(NR, 282, *S["n2"]); w3 = node(NR, 362, *S["n3"]); w4 = node(NL, 362, *S["n4"]); w5 = node(NL, 442, *S["n5"]); w6 = node(NR, 442, *S["n6"])
    cl, cr = NL + 60, NR + 60
    out.append(f'<path d="M {NL+w1:.0f} 306 L {NR-2} 306" class="flow"/>'); out.append(f'<path d="M {cr} 330 L {cr} 360" class="flow"/>')
    out.append(f'<path d="M {NR-2} 386 L {NL+w4+2:.0f} 386" class="flow"/>')
    out.append(f'<path d="M {NL+w5:.0f} 466 L {NL+w5:.0f} 466" class="flow" style="display:none"/>'); out.append(f'<path d="M {cl} 362 L {cl} 332" class="flow"/>')
    out.append(f'<path d="M {cl} 410 L {cl} 440" class="flow"/>'); out.append(f'<path d="M {cr} 410 L {cr} 440" class="flow"/>')
    out.append(f'<path d="M {NR-2} 466 L {NL+w5+2:.0f} 466" class="flow"/>')
    text(NL, 532, S["loopnote"], "k", 11.5, maxw=LX+LW-16-NL)
    WX = int(LX + LW + P["wgap"]); WW = (W-40) - WX
    out.append(f'<rect x="{WX}" y="232" width="{WW}" height="320" class="box wiki"/>'); text(WX+20, 262, S["wiki"], "gr", 15)
    for y,k,c in [(296,"w1","t"),(316,"w1a","k"),(334,"w1b","k"),(352,"w1c","k"),(388,"w2","t"),(410,"w3","t"),(432,"w4","t"),(468,"w5","gr"),(490,"w6","k"),(518,"w7","k")]:
        base = 14 if c!="k" else 12.5
        size = None
        for cand in (12.5, 12, 11.5, 11):
            if est(S[k], base) > WW-44 and est(S[k], cand) <= WW-44: size = cand; break
        if est(S[k], base) > WW-44 and size is None: size = 11
        x0 = WX+36 if (c=="k" and y<360) else WX+20
        text(x0, y, S[k], c, size, 'font-style="italic"' if k=="w7" else "", maxw=WX+WW-16-x0)
    out.append(f'<path d="M {AX+AW} 392 L {LX-2} 392" class="flowA"/>'); text(AX+AW+4, 382, S["read"], "k", 10.5)
    out.append(f'<path d="M {LX+LW} 392 L {WX-2} 392" class="flowA"/>'); text(LX+LW+2, 382, S["draft"], "k", 10.5)
    out.append(f'<path d="M {WX+180} 232 C {WX+180} 206, {WX} 206, {WX-15} 192" class="flowA"/>'); text(WX+60, 218, S["check"], "k", 11)
    out.append('<path d="M 235 232 C 235 206, 220 206, 228 192" class="flowA"/>'); text(246, 218, S["intent"], "k", 11)
    out.append(f'<rect x="40" y="592" width="{W-80}" height="120" class="box model"/>'); text(60, 622, S["models"], "k", 15, 'fill="#cbd5e1" font-weight="700"')
    x = 60
    for k in ("m1","m2","m3","m4"): x += pill(x, 642, S[k], 13, 32) + 16
    text(60, 700, S["mnote"], "k", maxw=W-120)
    out.append(f'<path d="M {cl} 592 L {cl} 554" class="flow"/>'); out.append(f'<path d="M {cr} 592 L {cr} 554" class="flow"/>')
    text(40, 760, S["foot"], "k", 12, maxw=W-80)
    out.append("</svg>")
    open(path,"w",encoding="utf-8").write("\n".join(out)+"\n")
import os
ASSETS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "docs", "assets")
gen("ja", os.path.join(ASSETS, "shikii-architecture.svg"))
gen("en", os.path.join(ASSETS, "shikii-architecture-en.svg"))
print("written ja + en")
