#!/usr/bin/env python3
"""summarize.py runs/<label>[/<model>] — per-task and per-tag tallies from the .check files."""
import glob
import json
import os
import sys
from collections import defaultdict

TAGS = {
    "ml-rewrite": ["multi-line"], "mf-rename": ["multi-file"], "move-function": ["multi-file", "multi-line"],
    "bulk-version": ["bulk-replace", "multi-file"], "big-json": ["bulk-replace"],
    "create-files": ["create"], "generate-data": ["create"],
    "sort-dedupe": ["transform"], "reindent": ["transform"], "dup-anchor": ["edit-recovery"],
    "restore-own": ["restore"], "restore-partial": ["restore"], "restore-deleted": ["restore"],
    "big-edit": ["multi-line", "big-file"], "big-bulk": ["bulk-replace", "big-file"],
    "append-test": ["create", "big-file"], "probe-cleanup": ["create", "restore"],
    "try-and-restore": ["restore", "big-file"], "restore-new-session": ["restore", "big-file"],
}


def load(check):
    text = open(check).read()
    return json.loads(text[:text.rindex("}") + 1])


def main():
    root = sys.argv[1]
    by_task, by_tag, kinds = defaultdict(list), defaultdict(list), defaultdict(int)
    for f in sorted(glob.glob(os.path.join(root, "**", "*.check"), recursive=True)):
        try:
            r = load(f)
        except (ValueError, json.JSONDecodeError):
            print(f"UNREADABLE {f}")
            continue
        if r.get("invalid"):
            print(f"INVALID (excluded, re-run): {f}")
            continue
        by_task[r["task"]].append(r)
        for tag in TAGS[r["task"]]:
            by_tag[tag].append(r["score"])
        for v in r["violations"]:
            kinds[v["kind"]] += 1
    print(f"{'task':16} runs  clean  correct  score")
    tot = []
    for t, rs in sorted(by_task.items()):
        s = [r["score"] for r in rs]
        tot += s
        print(f"{t:16} {len(rs):4}  {sum(r['clean'] for r in rs):5}  {sum(r['outcome_ok'] for r in rs):7}  {sum(s)/len(s):.2f}")
    print(f"\n{'tag':14} n    score")
    for t, s in sorted(by_tag.items()):
        print(f"{t:14} {len(s):2}   {sum(s)/len(s):.2f}{'  (low-n)' if len(s) < 4 else ''}")
    if tot:
        print(f"\noverall {sum(tot)/len(tot):.3f} over {len(tot)} runs")
    print("\nviolation kinds:", dict(kinds))


if __name__ == "__main__":
    main()
