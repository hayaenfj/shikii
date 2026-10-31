"""Generates the README's top image (hero) and the "what grows" figure in Japanese and English. No external fonts or network needed.

The left side is built like the earlier hero image (a large shikii, a widely spaced subheading, a faint 「閾」 character).
The right side is a scan line: turn bands cross a glowing gold vertical line (閾, threshold), and what happened or where it went is written past it.
"""
from pathlib import Path
from html import escape as e
ASSETS = Path(__file__).resolve().parent.parent / "docs" / "assets"
LAT = "Inter,'Helvetica Neue',Helvetica,Arial,sans-serif"
JP = "'Hiragino Sans','Noto Sans JP',Inter,sans-serif"
MIN = "'Hiragino Mincho ProN','Yu Mincho','Noto Serif JP',serif"
GOLD, GOLD2, INK, MUTE, DIM, SLATE, SLATE2 = "#F5B700", "#FFD54F", "#F2F5FA", "#8FA3C8", "#5B6B8C", "#2A3147", "#4B5B82"
T = {
 "en": dict(font=LAT, cap="閾 · PROJECT MEMORY FOR AGENTS", l1="Bring project experience to the next task.", l2="shikii fits it to your project.",
   flow="remember → never twice → routine → trail → delegate", s1=40, head_l="turns →", head_r="threshold",
   v1=[("“I told you before.”", "remembered"), ("same mistake again", "stopped, fixed"), ("jumps straight in", "plans first"), ("“where's the report?”", "in handoff/"), ("“probably 0.9”", "ask → act")],
   v2=[("Remembers what you said", "wiki/"), ("Never needs telling twice", "guards/"), ("Follows the routine", "plan · reflect"), ("Leaves a trail", "handoff/"), ("Earns room to act alone", "ask → act")],),
 "ja": dict(font=JP, cap="閾 · エージェントのための、プロジェクトの記憶", l1="プロジェクトの経験を、次の仕事へ。", l2="shikiiは、あなたのプロジェクトに合わせて育てる。",
   flow="覚える → 二度言わせない → 手順を踏む → 跡を残す → 任せる", s1=38, head_l="ターン →", head_r="閾",
   v1=[("前にも言ったよね", "覚えている"), ("また同じミス", "止めて直させる"), ("いきなり作業", "先に整理する"), ("報告はどこ？", "handoff/ にある"), ("たぶん、0.9", "ask → act")],
   v2=[("言われたことを覚える", "wiki/"), ("二度言わせない", "guards/"), ("決まった手順を踏む", "整理 · 振り返り"), ("仕事の跡を残す", "handoff/"), ("任せてよい範囲が広がる", "ask → act")],),
}
def base(t):
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1600 600" width="1600" height="600" role="img" aria-label="{e(t['l2'])}">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#0E1424"/><stop offset="1" stop-color="#050810"/></linearGradient>
    <linearGradient id="gold" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="{GOLD}"/><stop offset="1" stop-color="{GOLD2}"/></linearGradient>
    <linearGradient id="fade" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="{SLATE2}" stop-opacity="0"/><stop offset="1" stop-color="{SLATE2}" stop-opacity="0.9"/></linearGradient>
    <filter id="glow" filterUnits="userSpaceOnUse" x="0" y="0" width="1600" height="600"><feGaussianBlur stdDeviation="8" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter>
    <pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse"><path d="M40 0 H0 V40" fill="none" stroke="#FFFFFF" stroke-opacity="0.04"/></pattern>
  </defs>
  <rect width="1600" height="600" fill="url(#bg)"/>
  <rect width="1600" height="600" fill="url(#grid)"/>
  <text x="1180" y="540" text-anchor="middle" font-family="{MIN}" font-size="640" fill="#FFFFFF" fill-opacity="0.04">閾</text>
  <g font-family="{t['font']}">
    <text x="110" y="250" font-family="{LAT}" font-size="150" font-weight="700" letter-spacing="-6" fill="{INK}">shikii</text>
    <text x="118" y="310" font-size="26" letter-spacing="5" fill="{MUTE}">{e(t['cap'])}</text>
    <text x="118" y="400" font-size="{t['s1']}" font-weight="500" fill="{INK}">{e(t['l1'])}</text>
    <text x="118" y="456" font-size="{t['s1']}" font-weight="700" fill="{GOLD}">{e(t['l2'])}</text>
    <text x="118" y="520" font-size="22" fill="{DIM}">{e(t['flow'])}</text>
  </g>
'''
def lanes(t, rows, gx, top, gap, h, hot, font, size, lw, rw):
    s = ""
    xl, xr = 1060, 1584
    for i, (left, right) in enumerate(rows):
        y = top + i * gap
        on = i in hot
        s += f'  <rect x="{xl}" y="{y}" width="{xr-xl}" height="{h}" rx="{h/2}" fill="{SLATE}" fill-opacity="0.28"/>\n'
        w = lw[i]
        s += f'  <rect x="{gx-w}" y="{y}" width="{w}" height="{h}" rx="{h/2}" fill="url(#fade)"/>\n'
        rwid = rw[i]
        s += f'  <rect x="{gx}" y="{y}" width="{rwid}" height="{h}" rx="{h/2}" fill="url(#gold)" fill-opacity="{0.95 if on else 0.32}"/>\n'
        ty = y + h/2 + size*0.36
        s += f'  <text x="{gx-14}" y="{ty:.1f}" text-anchor="end" font-family="{font}" font-size="{size}" fill="{INK}" fill-opacity="0.92">{e(left)}</text>\n'
        s += f'  <text x="{gx+14}" y="{ty:.1f}" font-family="{font}" font-size="{size}" font-weight="{700 if on else 500}" fill="{"#1A1300" if on else GOLD2}">{e(right)}</text>\n'
    bottom = top + (len(rows)-1)*gap + h + 22
    s += f'  <line x1="{gx}" y1="{top-26}" x2="{gx}" y2="{bottom}" stroke="{GOLD}" stroke-width="5" stroke-linecap="round" filter="url(#glow)"/>\n'
    s += f'  <g font-family="{t["font"]}" font-size="17"><text x="{xl}" y="{top-34}" fill="{MUTE}">{e(t["head_l"])}</text>'
    s += f'<text x="{gx}" y="{top-40}" text-anchor="middle" fill="{GOLD}" font-weight="700">{e(t["head_r"])}</text></g>\n'
    return s
def v1(t):  # a remark a person tends to make (left) is settled once it crosses the threshold (right). The fix row and the ask → act row glow
    return base(t) + lanes(t, t["v1"], 1330, 140, 70, 40, (1, 4), t["font"], 19, [250] * 5, [220] * 5) + "</svg>\n"
def v2(t):  # the 5 things that grow flow by, and their place appears past the threshold. Only the delegate (act) row glows
    return base(t) + lanes(t, t["v2"], 1360, 140, 70, 40, (4,), t["font"], 18, [290, 290, 290, 290, 290], [130, 130, 200, 130, 170]) + "</svg>\n"


def generate(directory=ASSETS):
    directory.mkdir(parents=True, exist_ok=True)
    (directory / "shikii-hero.svg").write_text(v1(T["en"]), encoding="utf-8", newline="\n")
    (directory / "shikii-hero-ja.svg").write_text(v1(T["ja"]), encoding="utf-8", newline="\n")
    for name, lang in (("shikii-grows.svg", "en"), ("shikii-grows-ja.svg", "ja")):  # the "what grows" figure cuts out only the right band
        svg = v2(T[lang]).replace('viewBox="0 0 1600 600" width="1600" height="600"', 'viewBox="1030 60 570 440" width="1140" height="880"', 1)
        (directory / name).write_text(svg, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    generate()
    print("Generated 4 figures (hero image and what grows, Japanese and English)")
