#!/usr/bin/env python3
"""Verify every `file.go:NNN` code anchor in the DB-environment-template docs.

Prints the ACTUAL source line for each cited anchor so a human (or a diff)
can confirm the anchor points at what the prose claims. An anchor whose
target line is blank, or which does not exist, is flagged.

Usage:  python3 scripts/check_doc_anchors.py [doc.md ...]
"""

import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Anchor file -> real path. The docs cite a bare file name; the repo has one
# of each, so resolve by searching the tree for a unique match.
CANDIDATE_DIRS = ["internal", "web/src", "cmd", "pkg"]

# Basename -> relative path (resolved once, then cached).
RESOLVED = {}


def resolve(basename):
    if basename in RESOLVED:
        return RESOLVED[basename]
    hits = []
    for d in CANDIDATE_DIRS:
        for dirpath, _dirnames, filenames in os.walk(os.path.join(ROOT, d)):
            for fn in filenames:
                if fn == basename:
                    hits.append(os.path.relpath(os.path.join(dirpath, fn), ROOT))
    if not hits:
        RESOLVED[basename] = None
        return None
    # Prefer the non-test file when both exist.
    hits.sort(key=lambda p: (p.endswith("_test.go"), len(p)))
    RESOLVED[basename] = hits[0]
    return hits[0]


def main(paths):
    if not paths:
        paths = [
            "docs/concepts/db-environment-templates.md",
            "docs/concepts/db-connector.md",
            "docs/concepts/sqlite-browser.md",
        ]
    pattern = re.compile(r"([A-Za-z0-9_/.-]+\.(?:go|tsx|ts)):(\d+)")
    bad = 0
    total = 0
    for doc in paths:
        full = os.path.join(ROOT, doc)
        if not os.path.exists(full):
            print(f"!! {doc}: missing")
            bad += 1
            continue
        with open(full, encoding="utf-8") as fh:
            for lineno, line in enumerate(fh, 1):
                for m in pattern.finditer(line):
                    raw, num = m.group(1), int(m.group(2))
                    total += 1
                    base = os.path.basename(raw)
                    rel = resolve(base)
                    if rel is None:
                        print(f"!! {doc}:{lineno}: {raw}:{num} -> file not found")
                        bad += 1
                        continue
                    src = os.path.join(ROOT, rel)
                    with open(src, encoding="utf-8") as sf:
                        lines = sf.read().split("\n")
                    if num < 1 or num > len(lines):
                        print(f"!! {doc}:{lineno}: {raw}:{num} -> out of range ({rel} has {len(lines)} lines)")
                        bad += 1
                        continue
                    target = lines[num - 1]
                    flag = ""
                    if not target.strip():
                        flag = "  <== BLANK LINE"
                        bad += 1
                    print(f"{rel}:{num}{flag}")
                    print(f"    {target.strip()[:140]}")
    print(f"\nchecked {total} anchors, {bad} suspicious")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
