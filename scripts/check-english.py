#!/usr/bin/env python3
"""Check that what shikii ships is in English (run before pushing; CI runs it on every push).

Development may be written in Japanese first; everything pushed to the remote is English-first.
Japanese is allowed only in:
  - the Japanese side of a document pair (*.ja.md, docs/readme/README_ja.md, CHANGELOG_ja.md),
  - the Japanese UI dictionary (web/src/locales/ja.js), the built page (web/dist/), data under testdata/,
  - the wiki store written by v0.9.1 (internal/notebook/testdata/v091/, with the Japanese page labels of that version),
  - the language switcher line at the top of an English document ("**English** · [Japanese](<name>.ja.md)", with the Japanese word for Japanese),
  - files listed in scripts/japanese-allowlist.txt, each with the number of lines that contain Japanese
    (Japanese UI text such as display.ja and bi(en, ja), words matched against Japanese input,
    test inputs that exercise Japanese handling).
Any other file with Japanese fails with the offending lines. A limit is the exact count: an allowlisted
file with more Japanese lines fails, one with fewer fails until the limit is lowered, and a listed file
that no longer exists fails until its line is removed (so freed room cannot be filled with new Japanese).
Raise a limit only for text that has to stay Japanese.

Files are the ones Git tracks plus new files Git does not ignore (so a local run sees what CI will see
once they are added). "Japanese" is kana, kanji (including extension A and compatibility ideographs),
Japanese punctuation and brackets (U+3000-303F), full-width forms and half-width katakana (U+FF01-FF9F).
"""
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ALLOWLIST = os.path.join(ROOT, "scripts", "japanese-allowlist.txt")
JAPANESE = re.compile("[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\uff01-\uff9f]")
PAIRED = re.compile(r"(\.ja\.md$|^docs/readme/README_ja\.md$|^CHANGELOG_ja\.md$|^web/src/locales/ja\.js$|^web/dist/|^testdata/|^internal/notebook/testdata/v091/)")
BINARY = re.compile(r"\.(png|jpg|jpeg|gif|ico|svg|woff2?|gguf|bin)$")
# The language switcher at the top of an English document names the Japanese version in Japanese
SWITCHER = re.compile("^\\*\\*English\\*\\* \u00b7 \\[\u65e5\u672c\u8a9e\\]\\([^)]+\\)\\s*$")


def tracked(root):
    out = subprocess.run(["git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard"], capture_output=True, check=True).stdout
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
    files = sorted(set(tracked(root)))
    for path in files:
        if PAIRED.search(path) or BINARY.search(path):
            continue
        lines = japanese_lines(os.path.join(root, path))
        limit = limits.get(path)
        if limit is None:
            if not lines:
                continue
            print(f"check-english: {path}: Japanese in a file that ships in English ({len(lines)} lines):")
        elif len(lines) > limit:
            print(f"check-english: {path}: {len(lines)} lines with Japanese, more than the allowed {limit}:")
        elif len(lines) < limit:
            print(f"check-english: {path}: {len(lines)} lines with Japanese, fewer than the allowed {limit}; lower the limit to {len(lines)} in scripts/japanese-allowlist.txt" + (" (remove the line)" if not lines else ""))
            bad += 1
            continue
        else:
            continue
        bad += 1
        for i, text in lines[:20]:
            print(f"  {path}:{i}: {text.strip()[:160]}")
    present = set(files)
    for path in sorted(limits):
        if path not in present:
            print(f"check-english: {path} is in scripts/japanese-allowlist.txt but is not in the repository; remove the line")
            bad += 1
    if bad:
        print(f"check-english: FAIL ({bad} files). Translate them, or list text that must stay Japanese in scripts/japanese-allowlist.txt")
        return 1
    print(f"check-english: ok ({len(limits)} files may keep Japanese)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
