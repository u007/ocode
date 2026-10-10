#!/usr/bin/env python3
"""Audit Kaizen derived/promoted skills against the closed-book scorecards.

Run:  python3 docs/okf/_tools/audit-derived-skills.py [--quiet] [--json]

WHY THIS EXISTS
    The corpus rule (HOW-TO-EVALUATE.md step 4) is: derive a corrective skill for
    every tag below the scorecard's threshold. Nothing enforced it, so the two
    failure modes are both silent:

      UNDER-DERIVED — a model scored below threshold on a tag and nobody ever wrote
        the skill, so the model keeps that blind spot forever and no test fails.
      OVER-DERIVED  — a skill ships for a model that aced the stack, which spends
        prompt budget restating things the model already knows and dilutes the
        corrections that matter.

    HAND_AUTHORED names are a declared exception, imported from sync-derived-skills.py
    so there is exactly ONE place to register them (a second constant would drift, and
    the drift direction is "the prune guard forgets a skill" = silent deletion).

SCOPE / EXIT CODES
    0 = no violations (exceptions and unassessable rows are reported, not failed)
    1 = at least one violation
    2 = the audit could not run (unparseable scorecard, missing corpus)

    Unparseable input is exit 2, never 0: a checker that silently reads zero tags
    would report "no violations" forever, which is worse than no checker.
"""
import argparse
import importlib.util
import json
import pathlib
import re
import sys

TOOLS = pathlib.Path(__file__).resolve().parent
OKF = TOOLS.parent                           # docs/okf
REPO = TOOLS.parent.parent.parent             # repo root
KAIZEN_DIR = REPO / "skills" / "kaizen"

# Re-run artifacts, not baselines. A `.with-skill.md` scorecard deliberately has NO
# below-threshold tag (that is the point — the skill fixed them), so mistaking one for
# a baseline INVERTS the conclusion and reports the skill as unwarranted.
NON_BASELINE_SUFFIXES = (".with-skill", ".rerun")


def load_sync_tool():
    """Import sync-derived-skills.py so HAND_AUTHORED and is_illustrative have ONE
    definition. Re-implementing either here is how this audit first produced a FALSE
    POSITIVE: it compared an illustrative skill's `illustrative: true   # comment` line
    to "true" without stripping the comment, so a skill that is deliberately not
    shipped was reported as "never synced"."""
    spec = importlib.util.spec_from_file_location(
        "_sync_derived", TOOLS / "sync-derived-skills.py")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def frontmatter(p: pathlib.Path) -> dict:
    """Frontmatter key/value pairs, or {} for a missing/malformed file.

    Tolerating a missing file matters: threshold_for() probes `<stack>/meta.yaml` as a
    fallback, and not every stack directory is guaranteed to carry one.
    """
    try:
        text = p.read_text(encoding="utf-8")
    except OSError:
        return {}
    if not text.startswith("---"):
        return {}
    # Drop the opening delimiter BEFORE splitting, else "\n---" matches the opening
    # one and every key is lost (the audit then reads zero scorecards).
    body = text[len("---"):].split("\n---", 1)[0]
    return dict(re.findall(r"^([A-Za-z_]+):\s*(.+?)\s*$", body, re.M))


def name_of(p: pathlib.Path) -> str:
    return frontmatter(p).get("name", "").strip().strip("\"'")


def threshold_for(stack: str, fm: dict) -> float:
    """Scorecard frontmatter wins (it is per-model), then meta.yaml, then 0.85."""
    for v in (fm.get("threshold"), frontmatter(OKF / stack / "meta.yaml").get("threshold")):
        if v:
            try:
                return float(v)
            except ValueError:
                pass
    return 0.85


def weak_tags(p: pathlib.Path) -> dict:
    """Return {tag: subscore} for tags scoring below `threshold`.

    Scans the whole `| tag | subscore |` table rather than trusting the `action`
    column, so a hand-edited action cell cannot hide a below-threshold tag.
    """
    th = threshold_for(p.parent.parent.name, frontmatter(p))
    out = {}
    for line in p.read_text(encoding="utf-8").splitlines():
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) < 2 or cells[0] in ("tag", "") or set(cells[0]) <= {"-", ":"}:
            continue
        try:
            score = float(cells[1])
        except ValueError:
            continue
        if score < th:
            out[cells[0]] = score
    return out


def scorecards():
    """(stack, model_id, path) for BASELINE scorecards only."""
    for p in sorted(OKF.glob("*/scores/*.md")):
        fm = frontmatter(p)
        mid = fm.get("model_id")
        if not mid:                      # README.md and friends are not scorecards
            continue
        if p.stem.endswith(NON_BASELINE_SUFFIXES):
            continue
        if p.stem != mid.replace("/", "__"):
            print(f"WARNING: {p.relative_to(REPO)} — filename != model_id "
                  f"'{mid}' flattened; treating as baseline anyway", file=sys.stderr)
        yield p.parent.parent.name, mid, p


def derived_skills(is_illustrative) -> dict:
    """{stack: {model_id: [paths]}} from docs/okf/<stack>/derived/.

    Illustrative skills are teaching placeholders that must never ship, so they are
    excluded here for the same reason sync-derived-skills.py skips them — otherwise the
    audit reports "never synced" for a skill that is correctly absent.
    """
    out = {}
    for p in sorted(OKF.glob("*/derived/*.SKILL.md")):
        if is_illustrative(p):
            continue
        fm = frontmatter(p)
        out.setdefault(p.parent.parent.name, {}).setdefault(fm.get("tuned_for", "?"), []).append(p)
    return out


def has_with_skill_evidence(stack: str, model: str) -> bool:
    return any(p.stem.endswith(".with-skill")
               for p in (OKF / stack / "scores").glob(f"{model.replace('/', '__')}.with-skill.md"))


def audit():
    sync = load_sync_tool()
    hand = sync.HAND_AUTHORED
    derived = derived_skills(sync.is_illustrative)
    promoted = {d.name for d in KAIZEN_DIR.iterdir() if d.is_dir()} if KAIZEN_DIR.exists() else set()

    under, over, not_promoted, excused, evidenced, unassessable = [], [], [], [], 0, []

    for stack, model, card in scorecards():
        weak = weak_tags(card)
        skills = derived.get(stack, {}).get(model, [])
        names = [name_of(s) for s in skills]

        if not skills and not promoted.intersection({n for n in names}):
            if weak:
                under.append({"stack": stack, "model": model, "weak": weak,
                              "card": str(card.relative_to(REPO))})
            continue

        if names and not weak:
            row = {"stack": stack, "model": model, "skills": names,
                   "card": str(card.relative_to(REPO))}
            (excused if all(n in hand for n in names) else over).append(row)
            continue

        for n in names:
            if n in promoted:
                if has_with_skill_evidence(stack, model):
                    evidenced += 1
            else:
                not_promoted.append({"stack": stack, "model": model, "skill": n})
            if n not in promoted and n not in hand:
                unassessable.append(n)

    return {
        "under_derived": under, "over_derived": over, "not_promoted": not_promoted,
        "excused": excused, "with_skill_evidence": evidenced,
        "unassessable": sorted(set(unassessable)), "hand_authored": sorted(hand),
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--quiet", action="store_true", help="violations only")
    ap.add_argument("--json", action="store_true", help="machine-readable report")
    args = ap.parse_args()

    n_cards = sum(1 for _ in scorecards())
    if n_cards == 0:
        print("ERROR: no baseline scorecards found — corpus layout changed?", file=sys.stderr)
        return 2

    rep = audit()
    violations = len(rep["under_derived"]) + len(rep["over_derived"]) + len(rep["not_promoted"])

    if args.json:
        print(json.dumps(rep, indent=2))
        return 1 if violations else 0

    print(f"Kaizen skill audit — {n_cards} baseline scorecards\n")

    def section(title, rows, fmt):
        if not rows:
            return
        print(f"{title} ({len(rows)}):")
        for r in rows:
            print("   " + fmt(r))
        print()

    section("UNDER-DERIVED — below threshold with no skill (a silent blind spot)",
            rep["under_derived"],
            lambda r: f"{r['stack']}/{r['model']}: weak {sorted(r['weak'])} "
                      f"({', '.join(f'{k} {v}' for k, v in sorted(r['weak'].items()))})")
    section("OVER-DERIVED — skill ships for a model that aced the stack",
            rep["over_derived"],
            lambda r: f"{r['stack']}/{r['model']}: {r['skills']}")
    section("NOT-PROMOTED — derived skill never synced into skills/kaizen/",
            rep["not_promoted"],
            lambda r: f"{r['stack']}/{r['model']}: {r['skill']}")
    section("EXCUSED — hand-registered exception (declared in HAND_AUTHORED)",
            rep["excused"],
            lambda r: f"{r['stack']}/{r['model']}: {r['skills']}")

    if not args.quiet:
        print(f"with-skill re-benchmark evidence on file: {rep['with_skill_evidence']} skill(s)")
        print(f"HAND_AUTHORED registry: {rep['hand_authored'] or '(empty)'}")
        if rep["unassessable"]:
            print(f"unassessable (no scorecard for their tuned_for): {rep['unassessable']}")

    print(f"\nviolations: {violations}")
    return 1 if violations else 0


if __name__ == "__main__":
    sys.exit(main())