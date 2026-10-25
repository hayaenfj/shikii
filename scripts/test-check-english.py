#!/usr/bin/env python3
"""Tests for scripts/check-english.py (make check-english runs them first).

Each case builds a small Git repository in a temporary folder and runs the check on it.
Japanese characters are written as \\u escapes so this file itself stays English.
"""
import contextlib
import importlib.util
import io
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_english", os.path.join(HERE, "check-english.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

KANA = "\u30c6\u30b9\u30c8"            # katakana
HALF_KANA = "\uff83\uff7d\uff84"       # half-width katakana
BRACKETS = "\u300c\u300d\u3002"        # Japanese brackets and full stop
FULL_WIDTH = "\uff01\uff08\uff09"      # full-width exclamation mark and parentheses
EXT_A = "\u3400"                       # CJK extension A


def run(files, allowlist="", untracked=None, ignore=""):
    """files are committed, untracked are left as new files. Returns (exit code, output)."""
    with tempfile.TemporaryDirectory() as root:
        subprocess.run(["git", "init", "-q", root], check=True)
        for name, text in files.items():
            os.makedirs(os.path.dirname(os.path.join(root, name)) or root, exist_ok=True)
            with open(os.path.join(root, name), "w", encoding="utf-8") as f:
                f.write(text)
        if ignore:
            with open(os.path.join(root, ".gitignore"), "w", encoding="utf-8") as f:
                f.write(ignore)
        subprocess.run(["git", "-C", root, "add", "-A"], check=True)
        for name, text in (untracked or {}).items():
            with open(os.path.join(root, name), "w", encoding="utf-8") as f:
                f.write(text)
        allow = os.path.join(root, ".allow")
        with open(allow, "w", encoding="utf-8") as f:
            f.write(allowlist)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = check.main(root=root, allowlist=allow)
        return code, out.getvalue()


CASES = [
    # (name, expected exit code, kwargs)
    ("English only passes", 0, dict(files={"a.go": "hello\n"})),
    ("kana fails", 1, dict(files={"a.go": KANA + "\n"})),
    ("half-width katakana fails", 1, dict(files={"a.go": "x := \"" + HALF_KANA + "\"\n"})),
    ("Japanese brackets and full stop fail", 1, dict(files={"a.go": "x := \"" + BRACKETS + "\"\n"})),
    ("full-width symbols fail", 1, dict(files={"a.go": "x := \"" + FULL_WIDTH + "\"\n"})),
    ("CJK extension A fails", 1, dict(files={"a.go": EXT_A + "\n"})),
    ("an exact limit passes", 0, dict(files={"a.go": KANA + "\n" + KANA + "\n"}, allowlist="2 a.go\n")),
    ("more lines than the limit fail", 1, dict(files={"a.go": KANA + "\n" + KANA + "\n"}, allowlist="1 a.go\n")),
    ("a limit larger than the count fails", 1, dict(files={"a.go": KANA + "\n"}, allowlist="2 a.go\n")),
    ("a listed file with no Japanese fails", 1, dict(files={"a.go": "hello\n"}, allowlist="1 a.go\n")),
    ("a listed file that does not exist fails", 1, dict(files={"a.go": "hello\n"}, allowlist="1 gone.go\n")),
    ("a new file Git does not know yet is checked", 1, dict(files={"a.go": "hello\n"}, untracked={"b.go": KANA + "\n"})),
    ("an ignored file is not checked", 0, dict(files={"a.go": "hello\n"}, untracked={"b.log": KANA + "\n"}, ignore="*.log\n")),
    ("the Japanese side of a pair passes", 0, dict(files={"a.ja.md": KANA + "\n"})),
]


def main():
    failed = 0
    for name, want, kwargs in CASES:
        code, out = run(**kwargs)
        if code != want:
            failed += 1
            print(f"test-check-english: FAIL {name}: exit {code}, want {want}\n{out}")
    if failed:
        print(f"test-check-english: FAIL ({failed} of {len(CASES)})")
        return 1
    print(f"test-check-english: ok ({len(CASES)} cases)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
