#!/usr/bin/env python3
"""Regression tests for sync-derived-skills.py's prune guard.

Run:  python3 docs/okf/_tools/test_sync_derived_skills.py

Dependency-free (no pytest); exits non-zero on failure.

WHY THIS EXISTS
    The tool does `shutil.rmtree` on `skills/kaizen/<name>/` dirs whose derived source
    is gone. It used to delete every dir it did not recognise, which made it a silent
    data-loss hazard for any hand-authored Kaizen skill — one with no
    `docs/okf/*/derived/` source is invisible to the tool and was prune collateral on
    the next run. `tanstack-tuning-space-bunny-free` was the first such skill.

SAFETY
    Every case runs the tool against a THROWAWAY repo layout in a temp dir, never
    against this checkout: the tool resolves the repo root from `__file__`, so a copy
    placed at <tmp>/docs/okf/_tools/ retargets it harmlessly. The one real-tree check
    copies the actual tree and diffs the result, so the checkout is never handed to
    `shutil.rmtree`.
"""
import filecmp
import pathlib
import shutil
import subprocess
import sys
import tempfile

TOOLS = pathlib.Path(__file__).resolve().parent
REPO = TOOLS.parent.parent.parent
SCRIPT_NAME = "sync-derived-skills.py"

HAND_AUTHORED = "tanstack-tuning-space-bunny-free"
STALE = "stale-tuning-ghostmodel"
FRESH = "teststack-tuning-fake"

_failures = []


def check(cond: bool, msg: str) -> None:
    print(("  PASS  " if cond else "  FAIL  ") + msg)
    if not cond:
        _failures.append(msg)


def skill_md(name: str, body: str = "body\n") -> str:
    return f"---\nname: {name}\ndescription: d\n---\n{body}"


def stage(tmp: pathlib.Path) -> pathlib.Path:
    """Build a throwaway repo: one derived source, one hand-authored dir, one stale dir."""
    tools = tmp / "docs" / "okf" / "_tools"
    tools.mkdir(parents=True)
    shutil.copy2(TOOLS / SCRIPT_NAME, tools / SCRIPT_NAME)

    derived = tmp / "docs" / "okf" / "teststack" / "derived"
    derived.mkdir(parents=True)
    (derived / "teststack.fake.SKILL.md").write_text(skill_md(FRESH))

    k = tmp / "skills" / "kaizen"
    # Hand-authored, NO derived source: must survive.
    (k / HAND_AUTHORED).mkdir(parents=True)
    (k / HAND_AUTHORED / "SKILL.md").write_text(skill_md(HAND_AUTHORED, "hand written\n"))
    # Previously synced, source now gone: must still be pruned.
    (k / STALE).mkdir(parents=True)
    (k / STALE / "SKILL.md").write_text(skill_md(STALE, "previously synced\n"))
    return k


def run_tool(root: pathlib.Path) -> str:
    p = subprocess.run([sys.executable, str(root / "docs" / "okf" / "_tools" / SCRIPT_NAME)],
                       capture_output=True, text=True)
    if p.returncode != 0:
        print(p.stdout, p.stderr)
        raise SystemExit(f"tool exited {p.returncode}")
    return p.stdout


def test_prune_keeps_hand_authored() -> None:
    print("\n[1] a hand-authored dir with no derived source survives the prune")
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="kaizen-guard-"))
    try:
        k = stage(tmp)
        out = run_tool(tmp)
        check((k / HAND_AUTHORED).is_dir(),
              f"{HAND_AUTHORED} SURVIVED (was prune collateral)")
        check((k / HAND_AUTHORED / "SKILL.md").read_text() == skill_md(HAND_AUTHORED, "hand written\n"),
              "its content is byte-identical (not rewritten)")
        check("kept (hand-authored, not synced)" in out,
              "the prune log reports the preserve")
        check(not (k / STALE).exists(),
              f"{STALE} was PRUNED — the tool still retires its own orphans")
        check((k / FRESH / "SKILL.md").is_file(),
              "the derived source was written into the tree")
        out2 = run_tool(tmp)
        check("0 written" in out2, "re-run writes nothing (idempotent)")
        check("1 hand-authored kept" in out2, "re-run still preserves the hand-authored dir")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_guard_is_load_bearing() -> None:
    """Negative control: without the guard the dir IS pruned.

    Without this, a passing test 1 would not prove the guard is what saved the dir —
    it could pass for an unrelated reason and stop protecting anything later.
    """
    print("\n[2] NEGATIVE CONTROL — removing the guard brings the data loss back")
    unpatched = (TOOLS / SCRIPT_NAME).read_text().replace(f'    "{HAND_AUTHORED}",\n', "")
    check(unpatched != (TOOLS / SCRIPT_NAME).read_text(),
          "the HAND_AUTHORED entry was found and removed for the control")
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="kaizen-negctl-"))
    try:
        stage(tmp)  # builds the layout with the real tool...
        # ...then swap in the copy with the guard removed.
        (tmp / "docs" / "okf" / "_tools" / SCRIPT_NAME).write_text(unpatched)
        k = tmp / "skills" / "kaizen"
        run_tool(tmp)
        check(not (k / HAND_AUTHORED).exists(),
              "without the guard the hand-authored dir IS deleted (guard is load-bearing)")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_real_tree_is_a_noop() -> None:
    print("\n[3] the real tree is a no-op today (run against a COPY, never the checkout)")
    if not (REPO / "skills" / "kaizen").is_dir():
        check(False, "skills/kaizen not found — wrong REPO resolution?")
        return
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="kaizen-realdry-"))
    try:
        shutil.copytree(REPO / "docs" / "okf", tmp / "docs" / "okf", symlinks=True)
        shutil.copytree(REPO / "skills" / "kaizen", tmp / "skills" / "kaizen", symlinks=True)
        kdir = tmp / "skills" / "kaizen"
        before = {p.relative_to(tmp) for p in kdir.rglob("*")}
        out = run_tool(tmp)
        after = {p.relative_to(tmp) for p in kdir.rglob("*")}

        removed = sorted(str(x) for x in before - after)
        added = sorted(str(x) for x in after - before)
        changed = sorted(str(x) for x in after
                         if x.is_file() and x in before
                         and not filecmp.cmp(tmp / x, REPO / x, shallow=False))
        print("      " + out.strip().splitlines()[-1])
        check(not removed, f"nothing removed (got {removed or 'none'})")
        check(not added, f"nothing added (got {added or 'none'})")
        check(not changed, f"nothing rewritten (got {changed or 'none'})")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


test_prune_keeps_hand_authored()
test_guard_is_load_bearing()
test_real_tree_is_a_noop()

print("\nRESULT:", "ALL PASS" if not _failures else f"{len(_failures)} FAILURE(S)")
sys.exit(1 if _failures else 0)