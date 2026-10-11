#!/usr/bin/env python3
"""Regression tests for audit-derived-skills.py.

Run:  python3 docs/okf/_tools/test_audit_derived_skills.py

Dependency-free (no pytest); exits non-zero on failure.

Every case builds a synthetic corpus in a temp dir and runs a COPY of the audit tool
against it (the tool resolves OKF/REPO from __file__, so a copy retargets it). Nothing
here reads or writes the real corpus.

Each test below pins a bug this audit actually had, or a trap in the corpus layout that
would make it silently report "no violations":
  * the `.with-skill` inversion — a re-run scorecard has NO weak tag by construction, so
    treating it as a baseline reports a shipped skill as unwarranted;
  * `illustrative: true   # comment` — comparing the raw line to "true" made a
    deliberately-unshipped skill look "never synced";
  * the zero-scorecard guard — a path off-by-one made the audit read 0 cards and exit 2,
    which is the only reason that bug was caught instead of reporting a clean corpus.
"""
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile

TOOLS = pathlib.Path(__file__).resolve().parent
TOOL = "audit-derived-skills.py"

_failures = []


def check(cond, msg):
    print(("  PASS  " if cond else "  FAIL  ") + msg)
    if not cond:
        _failures.append(msg)


def card(model_id, tags, threshold=0.75):
    """A scorecard whose per-tag table lists {tag: subscore}."""
    rows = "\n".join(f"| {t} | {v} | 4 | ok | omit (strong) |" for t, v in tags.items())
    return (f"---\nmodel_id: {model_id}\nthreshold: {threshold}\n---\n"
            f"# Scorecard\n\n| tag | subscore | n | trust | action |\n"
            f"|-----|---------:|--:|-------|--------|\n{rows}\n")


def derived(name, tuned_for, stack="teststack", illustrative=False):
    ill = "illustrative: true   # teaching placeholder, never shipped\n" if illustrative else ""
    return (f"---\nname: {name}\ntuned_for: {tuned_for}\nstack: {stack}\n{ill}---\n# body\n")


def build(tmp, cards=(), deriveds=(), promoted=()):
    """Materialise a synthetic corpus; returns the audit-tool path to run."""
    (tmp / "docs/okf/_tools").mkdir(parents=True, exist_ok=True)
    shutil.copy2(TOOLS / TOOL, tmp / "docs/okf/_tools" / TOOL)
    shutil.copy2(TOOLS / "sync-derived-skills.py", tmp / "docs/okf/_tools/sync-derived-skills.py")
    for stack, model_id, tags, th in cards:
        d = tmp / "docs/okf" / stack / "scores"
        d.mkdir(parents=True, exist_ok=True)
        (d / f"{model_id.replace('/', '__')}.md").write_text(card(model_id, tags, th))
    for stack, name, tuned_for, ill in deriveds:
        d = tmp / "docs/okf" / stack / "derived"
        d.mkdir(parents=True, exist_ok=True)
        (d / f"{stack}.{tuned_for.replace('/', '__')}.SKILL.md").write_text(
            derived(name, tuned_for, stack, ill))
    for p in promoted:
        (tmp / "skills/kaizen" / p).mkdir(parents=True, exist_ok=True)
        (tmp / "skills/kaizen" / p / "SKILL.md").write_text(derived(p, p))
    return tmp / "docs/okf/_tools" / TOOL


def run(tool, *args):
    p = subprocess.run([sys.executable, str(tool), "--json", *args],
                       capture_output=True, text=True)
    try:
        return p.returncode, json.loads(p.stdout)
    except json.JSONDecodeError:
        return p.returncode, {"_stdout": p.stdout, "_stderr": p.stderr}


def sandbox():
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="kaizen-audit-"))
    return tmp, build(tmp)


def test_under_derived():
    print("\n[1] a weak tag with no skill is reported (the silent blind spot)")
    tmp, tool = sandbox()
    try:
        build(tmp, [("teststack", "weakmodel", {"rsc": 0.58, "hooks": 0.94}, 0.75)], [])
        rc, rep = run(tool)
        under = rep.get("under_derived", [])
        check(rc == 1, "exit 1 (a violation is not silently clean)")
        check(len(under) == 1, f"one under-derived row (got {len(under)})")
        check(under and set(under[0]["weak"]) == {"rsc"},
              "only the BELOW-threshold tag is listed, not the strong one")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_over_derived_and_excused():
    print("\n[2] a skill for an aced model is flagged UNLESS hand-registered")
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="kaizen-audit-"))
    try:
        tool = build(tmp, [("teststack", "acemodel", {"hooks": 0.99}, 0.75)],
                     [("teststack", "acemodel-tuning", "acemodel", False)],
                     ["acemodel-tuning"])
        rc, rep = run(tool)
        check(len(rep.get("over_derived", [])) == 1,
              "unregistered over-derivation is flagged")

        # Same corpus, but the name is in HAND_AUTHORED.
        src = (tmp / "docs/okf/_tools/sync-derived-skills.py").read_text()
        (tmp / "docs/okf/_tools/sync-derived-skills.py").write_text(
            src.replace("HAND_AUTHORED = frozenset({",
                        'HAND_AUTHORED = frozenset({\n    "acemodel-tuning",'))
        rc, rep = run(tmp / "docs/okf/_tools" / TOOL)
        check(len(rep.get("over_derived", [])) == 0, "hand-registered -> no violation")
        check(len(rep.get("excused", [])) == 1, "and it is reported as EXCUSED, not hidden")
        check(rc == 0, "excused alone exits 0")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_with_skill_is_not_a_baseline():
    print("\n[3] a .with-skill re-run is NOT a baseline (the inversion trap)")
    tmp, tool = sandbox()
    try:
        build(tmp, [("teststack", "m", {"rsc": 0.58}, 0.75)],
              [("teststack", "teststack-tuning-m", "m", False)], ["teststack-tuning-m"])
        # The re-run: same model, no weak tags, because the skill worked.
        (tmp / "docs/okf/teststack/scores/m.with-skill.md").write_text(
            card("m", {"rsc": 0.95}, 0.75))
        rc, rep = run(tool)
        check(len(rep.get("over_derived", [])) == 0,
              "the re-run scorecard did NOT flip the verdict to over-derived")
        check(len(rep.get("under_derived", [])) == 0,
              "the baseline still shows the tag as covered")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_illustrative_is_excluded():
    print("\n[4] an `illustrative: true  # comment` skill is excluded (false-positive bug)")
    tmp, tool = sandbox()
    try:
        build(tmp, [("teststack", "m", {"rsc": 0.58}, 0.75)],
              [("teststack", "teststack-tuning-m", "m", True)], [])
        rc, rep = run(tool)
        check(len(rep.get("not_promoted", [])) == 0,
              "a deliberately-unshipped illustrative skill is NOT reported as unsynced")
        check(len(rep.get("under_derived", [])) == 1,
              "and the model is correctly reported as under-derived instead")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_zero_corpus_is_an_error():
    print("\n[5] an empty/mis-rooted corpus exits 2, never 0")
    tmp, tool = sandbox()
    try:
        for f in (tmp / "docs/okf/teststack/scores").glob("*.md"):
            f.unlink()
        rc, _ = run(tool)
        check(rc == 2, f"exit 2 when no baseline scorecards parse (got {rc})")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_per_model_threshold():
    print("\n[6] the threshold is per-model, not a global constant")
    tmp, tool = sandbox()
    try:
        # 0.80: a 0.78 tag is a violation; under a global 0.75 it would not be.
        build(tmp, [("teststack", "strict", {"a": 0.78}, 0.80)], [])
        rc, rep = run(tool)
        check(len(rep.get("under_derived", [])) == 1,
              "0.78 < threshold 0.80 -> violation")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


test_under_derived()
test_over_derived_and_excused()
test_with_skill_is_not_a_baseline()
test_illustrative_is_excluded()
test_zero_corpus_is_an_error()
test_per_model_threshold()

print("\nRESULT:", "ALL PASS" if not _failures else f"{len(_failures)} FAILURE(S)")
sys.exit(1 if _failures else 0)