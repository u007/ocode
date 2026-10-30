#!/usr/bin/env python3
"""Tally a sweep: python3 summarize.py runs/<label>

Reads runs/<label>/<model>/<task>.<rep>.check (last line `SCORE n/m PASS|FAIL`) and .meta, then prints
  * per task: runs passed / run, mean check fraction, mean seconds
  * per tag: mean check fraction over the tasks carrying the tag (the Kaizen subscore axis)
A run with no .check or no SCORE line counts as 0 (the model produced nothing checkable).
"""
import collections
import pathlib
import re
import sys

TAGS = {
    "attendance-status": ["formulas", "lookup", "dates-times"],
    "attendance-summary": ["formulas", "summary"],
    "expense-summary": ["formulas", "lookup", "summary"],
    "expense-pie": ["charts"],
    "budget-bar": ["charts"],
    "sales-pivot": ["summary", "formulas"],
    "sales-top3": ["summary", "formulas"],
    "messy-clean": ["cleanup", "dates-times"],
    "audit-variance": ["formulas", "formatting"],
    "ledger-format": ["formatting"],
    "orders-lookup": ["formulas", "lookup", "dates-times"],
    "expense-daily": ["formulas", "summary", "dates-times"],
    "log-append": ["preserve"],
}


def main(root):
    per_task = collections.defaultdict(list)
    for chk in sorted(pathlib.Path(root).rglob("*.check")):
        task = chk.name.split(".")[0]
        last = chk.read_text().strip().splitlines()[-1:] or [""]
        m = re.match(r"SCORE (\d+)/(\d+) (PASS|FAIL)", last[0])
        frac, ok = (int(m[1]) / int(m[2]), m[3] == "PASS") if m else (0.0, False)
        meta = chk.with_suffix(".meta")
        secs = int(re.search(r"secs=(\d+)", meta.read_text())[1]) if meta.exists() and "secs=" in meta.read_text() else 0
        per_task[task].append((frac, ok, secs))
    if not per_task:
        sys.exit(f"no *.check files under {root}")
    print(f"{'task':<20} {'pass':>5} {'mean':>6} {'secs':>6}")
    for t, runs in per_task.items():
        print(f"{t:<20} {sum(o for _, o, _ in runs)}/{len(runs):<3} {sum(f for f, _, _ in runs) / len(runs):6.2f} {sum(s for *_, s in runs) / len(runs):6.0f}")
    tag = collections.defaultdict(list)
    for t, runs in per_task.items():
        for g in TAGS.get(t, []):
            tag[g] += [f for f, _, _ in runs]
    print(f"\n{'tag':<14} {'subscore':>8} {'n':>3}")
    for g, v in sorted(tag.items()):
        print(f"{g:<14} {sum(v) / len(v):8.2f} {len(v):>3}")
    allruns = [f for r in per_task.values() for f, _, _ in r]
    full = [o for r in per_task.values() for _, o, _ in r]
    print(f"\noverall: {sum(allruns) / len(allruns):.2f} mean check fraction, {sum(full)}/{len(full)} runs fully passed")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "runs")
