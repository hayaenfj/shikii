"""Generates the README's conversation-flow and structure figures in Japanese and English. No external fonts or network needed."""
from html import escape
from pathlib import Path
import unicodedata

ASSETS = Path(__file__).resolve().parent.parent / "docs" / "assets"
WIDTH = 1200
FONT = "-apple-system, BlinkMacSystemFont, 'Hiragino Sans', 'Noto Sans JP', 'Segoe UI', sans-serif"
COLORS = {"gold": "#f4c76a", "green": "#83d8b0", "blue": "#8db8f4"}


def width(text, size):
    """Text width with room for wrapping. Render and check again after making the image."""
    return sum(size * (1.05 if unicodedata.east_asian_width(c) in "WF" else .66) for c in text)


def wrap(text, size, limit):
    lines, line = [], ""
    words = list(text) if any(unicodedata.east_asian_width(c) in "WF" for c in text) else text.split(" ")
    joiner = "" if len(words) and any(unicodedata.east_asian_width(c) in "WF" for c in text) else " "
    for word in words:
        candidate = line + (joiner if line else "") + word
        if line and width(candidate, size) > limit:
            lines.append(line)
            line = word
        else:
            line = candidate
    if line:
        lines.append(line)
    return lines


class SVG:
    def __init__(self, lang, height, title, description):
        self.height = height
        self.out = [f'''<svg xmlns="http://www.w3.org/2000/svg" width="{WIDTH}" height="{height}" viewBox="0 0 {WIDTH} {height}" role="img" aria-labelledby="title desc" lang="{lang}" font-family="{FONT}">
<title id="title">{escape(title)}</title><desc id="desc">{escape(description)}</desc>
<defs>
  <linearGradient id="bg" x2="1" y2="1"><stop stop-color="#101b2d"/><stop offset="1" stop-color="#080f1b"/></linearGradient>
  <marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M 0 0 L 10 5 L 0 10" fill="none" stroke="#7d93af" stroke-width="1.5"/></marker>
</defs>
<rect width="{WIDTH}" height="{height}" rx="24" fill="url(#bg)"/>''']

    def rect(self, x, y, w, h, fill="#152236", stroke="#2b405b", radius=16):
        self.out.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{radius}" fill="{fill}" stroke="{stroke}"/>')

    def text(self, x, y, value, size=20, color="#dce6f5", weight=400):
        self.out.append(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}">{escape(value)}</text>')

    def paragraph(self, x, y, value, limit, size=20, color="#b3c3d9", leading=None):
        leading = leading or size * 1.5
        for line in wrap(value, size, limit):
            self.text(x, y, line, size, color)
            y += leading
        return y

    def arrow(self, path):
        self.out.append(f'<path d="{path}" fill="none" stroke="#7d93af" stroke-width="2" marker-end="url(#arrow)"/>')

    def save(self, path):
        path.write_text("\n".join(self.out + ["</svg>"]) + "\n", encoding="utf-8", newline="\n")


COPY = {
    "ja": {
        "hero_title": "毎回のターンで、プロジェクトの知識が育つ。",
        "hero_sub": "作業の前に読み、作業中は跡を残し、終える前に振り返る。手順を飛ばすと止まります。",
        "cards": [
            ("作業の前に読む", "会話を整理し、wikiを探す", "似た依頼の教訓が届く"),
            ("作業中は跡を残す", "報告・記録・画像は作業記録へ", "repoには成果だけ"),
            ("終える前に振り返る", "学んだことを教訓にする", "止める検査が訂正を確かめる"),
        ],
        "return": "次の依頼で届く",
        "hero_foot": "機械で確かめられる訂正は止める検査に。正解は結果から付き、人は抜き取りと保留を見ます。",
        "arch_title": "記憶は、初期化したプロジェクトに残る",
        "arch_sub": "エージェントが育てる知識と、判定を任せるための正解と線を分けて持ちます。",
        "people_title": "人は意図と訂正を伝え、必要な確認をする",
        "people_body": "普段の会話に加え、採点の抜き取り・保留や、業務の正解・聞き方の意味を確認します。",
        "project": "このプロジェクトの .shikii/",
        "memory_title": "エージェントの仕事を育てる",
        "memory_rows": [
            ("原資料と版を残す", "records/ · 会話・判定・確認結果"),
            ("教訓に統合して届ける", "wiki/ · 本文・出典・効いたかの振り返り"),
            ("訂正を止める検査にする", "guards/ · 有効にするのは人"),
            ("作業の跡を残す", "handoff/ · 報告・手順・画像(画面で読む)"),
        ],
        "decision_title": "業務の判定を任せる",
        "decision_rows": [
            ("問いと選択肢を決める", "decisions/ · 意図を定義する"),
            ("モデルに判定を聞く", "act / ask / discard / abstain"),
            ("人や結果から正解を得る", "labels/ · 仮採点と区別する"),
            ("線を引き、聞き方を測る", "calibrate / tune · 意味は人が確認"),
        ],
        "memory_foot": "教訓は仮説。評価はAIの観察。",
        "decision_foot": "会話の仮採点だけでは線を引かない。",
        "shared_title": "共通の入口: ~/.shikii/agent",
        "shared_body": "型・道具・プロジェクトの索引。会話・正解・線は各プロジェクトへ保存。",
        "arch_foot": "会話の整理は利用中のエージェント。既製の問いの判定はJev(鍵が無い時は手元のモデル)が担当します。",
    },
    "en": {
        "hero_title": "Every turn adds to what the project knows.",
        "hero_sub": "Read first, keep a trail, reflect at the end. A skipped step stops the turn.",
        "cards": [
            ("Read before work", "Search the wiki first", "Similar lessons arrive"),
            ("Keep a trail", "Reports go to work records", "Not into the repository"),
            ("Reflect at the end", "Save what was learned", "Guards check corrections"),
        ],
        "return": "Delivered with the next request",
        "hero_foot": "Checkable corrections become guards. Truths come from results; people check samples and undecided cases.",
        "arch_title": "Memory stays with the initialized project",
        "arch_sub": "What the agent learns is kept apart from the truths and lines used to delegate decisions.",
        "people_title": "People provide intent, corrections and the checks that need them",
        "people_body": "Keep working as usual. Check sampled assessments, undecided cases, business truths and wording meaning.",
        "project": "This project's .shikii/",
        "memory_title": "Grow the agent's work",
        "memory_rows": [
            ("Keep sources and revisions", "records/ · conversations, decisions, results"),
            ("Merge into lessons and deliver them", "wiki/ · bodies, sources, whether they helped"),
            ("Turn corrections into guards", "guards/ · a person enables them"),
            ("Leave a trail of the work", "handoff/ · reports, steps, screenshots (on the page)"),
        ],
        "decision_title": "Delegate a repeated decision",
        "decision_rows": [
            ("Define the question and options", "decisions/ · express the intent"),
            ("Ask a model for a decision", "act / ask / discard / abstain"),
            ("Get truths from people or outcomes", "labels/ · separate from provisional scores"),
            ("Calibrate and measure wording", "calibrate / tune · people check meaning"),
        ],
        "memory_foot": "Lessons are hypotheses. Assessments are AI observations.",
        "decision_foot": "Conversation assessments alone cannot calibrate a line.",
        "shared_title": "Shared entry point: ~/.shikii/agent",
        "shared_body": "Templates, tools and project index. Conversations, truths and calibration belong to each project.",
        "arch_foot": "The active agent organizes conversations. Jev (or a local model without a key) answers the built-in questions.",
    },
}


def hero(lang, path):
    s = COPY[lang]
    svg = SVG(lang, 630 if lang == "en" else 570, s["hero_title"], s["hero_sub"])
    svg.text(48, 64, "shikii", 30, "#f4c76a", 700)
    svg.text(48, 126, s["hero_title"], 38, "#f0f5fc", 700)
    svg.paragraph(48, 170, s["hero_sub"], 1104, 21)
    for i, (title, a, b) in enumerate(s["cards"]):
        x = 48 + i * 384
        accent = COLORS[["gold", "green", "blue"][i]]
        svg.rect(x, 216, 336, 260 if lang == "en" else 216)
        svg.text(x + 24, 259, f"0{i + 1}", 20, accent, 700)
        svg.text(x + 24, 304, title, 24, "#f0f5fc", 700)
        y = svg.paragraph(x + 24, 345, a, 288, 19)
        svg.paragraph(x + 24, y + 9, b, 288, 18, "#a8bad3")
        if i < 2:
            svg.arrow(f"M {x + 342} 324 H {x + 376}")
    bottom, return_y, footer_y = (484, 530, 608) if lang == "en" else (440, 478, 548)
    svg.arrow(f"M 984 {bottom} V {return_y} H 216 V {bottom}")  # back to 01 (before the work) on the next request
    svg.text(236, return_y + 30, s["return"], 18, COLORS["green"])
    svg.text(48, footer_y, s["hero_foot"], 19, "#a8bad3")
    svg.save(path)


def architecture(lang, path):
    s = COPY[lang]
    en = lang == "en"
    svg = SVG(lang, 1150 if en else 970, s["arch_title"], s["arch_sub"])
    project_y, card_y, row_y, row_gap = (310, 374, 458, 90) if en else (282, 346, 430, 66)
    card_h, project_h, shared_y = (494, 590, 930) if en else (390, 480, 794)
    svg.text(40, 58, s["arch_title"], 32, "#f0f5fc", 700)
    svg.paragraph(40, 98, s["arch_sub"], 1120, 19)
    svg.rect(40, 142, 1120, 134 if en else 108, "#28241b", "#6d5631")
    svg.text(64, 181, s["people_title"], 23, COLORS["gold"], 700)
    svg.paragraph(64, 215, s["people_body"], 1072, 18)
    svg.rect(40, project_y, 1120, project_h, "#0e1a2a", "#4d6d96")
    svg.text(64, project_y + 40, s["project"], 23, "#e3edfa", 700)
    for i, kind in enumerate(["memory", "decision"]):
        x = 64 + i * 560
        accent = COLORS["green" if i == 0 else "blue"]
        svg.rect(x, card_y, 512, card_h)
        svg.text(x + 22, card_y + 40, s[kind + "_title"], 22, accent, 700)
        for row, (title, body) in enumerate(s[kind + "_rows"]):
            y = row_y + row * row_gap
            svg.text(x + 22, y, f"{row + 1}.", 19, accent, 700)
            svg.text(x + 54, y, title, 20, "#e3edfa", 600)
            svg.paragraph(x + 54, y + 26, body, 434, 18)
        svg.paragraph(x + 22, card_y + card_h - (42 if en else 25), s[kind + "_foot"], 468, 18, accent)
    svg.rect(40, shared_y, 1120, 120 if en else 100)
    svg.text(64, shared_y + 37, s["shared_title"], 22, "#d2e1f5", 700)
    svg.paragraph(64, shared_y + 72, s["shared_body"], 1072, 18)
    svg.paragraph(40, 1094 if en else 937, s["arch_foot"], 1120, 18)
    svg.save(path)


def generate(directory=ASSETS):
    directory.mkdir(parents=True, exist_ok=True)
    hero("ja", directory / "shikii-conversation-ja.svg")
    hero("en", directory / "shikii-conversation.svg")
    architecture("ja", directory / "shikii-architecture.svg")
    architecture("en", directory / "shikii-architecture-en.svg")


if __name__ == "__main__":
    generate()
    print("Generated 4 figures (conversation flow and structure, Japanese and English)")
