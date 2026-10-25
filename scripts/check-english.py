#!/usr/bin/env python3
"""Check that what shikii ships is in English (run before pushing; CI runs it on every push).

Development may be written in Japanese first; everything pushed to the remote is English-first.
Japanese is allowed only in:
  - the Japanese side of a document pair (*.ja.md, docs/readme/README_ja.md, CHANGELOG_ja.md),
  - the Japanese UI dictionary (web/src/locales/ja.js), the built page (web/dist/), data under testdata/,
  - the language switcher line at the top of an English document ("**English** · [Japanese](<name>.ja.md)", with the Japanese word for Japanese),
  - files listed in scripts/japanese-allowlist.txt, each with the most lines that may contain Japanese
    (Japanese UI text such as display.ja and bi(en, ja), words matched against Japanese input,
    test inputs that exercise Japanese handling).
Any other tracked file with Japanese, or an allowlisted file with more Japanese lines than its limit,
fails with the offending lines. Lower a limit when Japanese is removed; raise one only for text
that has to stay Japanese.
"""
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ALLOWLIST = os.path.join(ROOT, "scripts", "japanese-allowlist.txt")
JAPANESE = re.compile("[\u3040-\u30ff\u4e00-\u9fff]")
PAIRED = re.compile(r"(\.ja\.md$|^docs/readme/README_ja\.md$|^CHANGELOG_ja\.md$|^web/src/locales/ja\.js$|^web/dist/|^testdata/)")
BINARY = re.compile(r"\.(png|jpg|jpeg|gif|ico|svg|woff2?|gguf|bin)$")
# The language switcher at the top of an English document names the Japanese version in Japanese
SWITCHER = re.compile("^\\*\\*English\\*\\* \u00b7 \\[\u65e5\u672c\u8a9e\\]\\([^)]+\\)\\s*$")


def tracked(root):
    out = subprocess.run(["git", "-C", root, "ls-files", "-z"], capture_output=True, check=True).stdout
    return [p for p in out.decode().split("\0") if p]


def japanese_lines(path):
    try:
        with open(path, encoding="utf-8") as f:
            return [(i, line.rstrip("\n")) for i, line in enumerate(f, 1) if JAPANESE.search(line) and not SWITCHER.match(line)]
    except (UnicodeDecodeError, FileNotFoundError, IsADirectoryError):
        return []


def load_allowlist(path):
    limits = {}
    with open(path, encoding="utf-8") as f:
        for n, line in enumerate(f, 1):
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            count, _, name = line.partition(" ")
            if not count.isdigit() or not name:
                sys.exit(f"check-english: {path}:{n}: expected '<lines> <path>'")
            limits[name.strip()] = int(count)
    return limits


def main(root=ROOT, allowlist=ALLOWLIST):
    limits = load_allowlist(allowlist)
    bad = 0
    for path in tracked(root):
        if PAIRED.search(path) or BINARY.search(path):
            continue
        lines = japanese_lines(os.path.join(root, path))
        if not lines:
            continue
        limit = limits.get(path)
        if limit is None:
            print(f"check-english: {path}: Japanese in a file that ships in English ({len(lines)} lines):")
        elif len(lines) > limit:
            print(f"check-english: {path}: {len(lines)} lines with Japanese, more than the allowed {limit}:")
        else:
            continue
        bad += 1
        for i, text in lines[:20]:
            print(f"  {path}:{i}: {text.strip()[:160]}")
    if bad:
        print(f"check-english: FAIL ({bad} files). Translate them, or list text that must stay Japanese in scripts/japanese-allowlist.txt")
        return 1
    print(f"check-english: ok ({len(limits)} files may keep Japanese)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
