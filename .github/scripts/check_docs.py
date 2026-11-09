#!/usr/bin/env python3
"""Check local Markdown link targets without network or adjacent repositories."""

from __future__ import annotations

import argparse
from collections import Counter
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[2]
LINK = re.compile(r"!?\[[^\]\n]*\]\(\s*(?:<([^>]+)>|([^\s)]+))(?:\s+[^)]*)?\)")
REFERENCE = re.compile(r"^\s{0,3}\[[^\]\n]+\]:\s*<?([^\s>]+)>?", re.MULTILINE)


def prose(text: str) -> str:
    """Ignore fenced examples while retaining newlines for useful diagnostics."""
    return re.sub(
        r"(?ms)^\s{0,3}(`{3,}|~{3,})[^\n]*\n.*?^\s{0,3}\1\s*$",
        lambda match: "\n" * match.group().count("\n"),
        text,
    )


def anchors(path: Path) -> set[str]:
    text = prose(path.read_text())
    result = set(re.findall(r'(?:id|name)=[\"\']([^\"\']+)[\"\']', text))
    counts: Counter[str] = Counter()
    for match in re.finditer(r"(?m)^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$", text):
        title = re.sub(r"<[^>]+>", "", match.group(1)).lower()
        title = re.sub(r"\[([^]]+)\]\([^)]*\)", r"\1", title)
        slug = re.sub(r"[^\w\- ]", "", title).replace(" ", "-")
        count = counts[slug]
        counts[slug] += 1
        result.add(f"{slug}-{count}" if count else slug)
    return result


def check(path: Path) -> list[str]:
    text = prose(path.read_text())
    links = [(m.start(), m.group(1) or m.group(2)) for m in LINK.finditer(text)]
    links += [(m.start(), m.group(1)) for m in REFERENCE.finditer(text)]
    errors = []
    for offset, link in links:
        parsed = urlsplit(link)
        if parsed.scheme or parsed.netloc:
            continue
        target = (path.parent / unquote(parsed.path)).resolve() if parsed.path else path
        if not target.is_relative_to(ROOT):
            # Hindsight and agent source trees have independent lifecycles.
            continue
        problem = None
        if not target.exists():
            problem = "target does not exist"
        elif parsed.fragment and target.is_file() and target.suffix.lower() == ".md":
            if unquote(parsed.fragment) not in anchors(target):
                problem = "section does not exist"
        if problem:
            line = text.count("\n", 0, offset) + 1
            errors.append(f"{path.relative_to(ROOT)}:{line}: {link}: {problem}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("files", nargs="*")
    args = parser.parse_args()
    files = args.files or subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "*.md"],
        cwd=ROOT,
    ).decode().split("\0")
    paths = sorted({(ROOT / name).resolve() for name in files if name})
    errors = []
    for path in paths:
        if path.is_file() and path.suffix.lower() == ".md":
            errors.extend(check(path))
    if errors:
        print("\n".join(errors))
        return 1
    print("Local Markdown links and section anchors passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
