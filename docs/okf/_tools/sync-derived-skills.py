#!/usr/bin/env python3
"""Sync Kaizen derived skills into the //go:embed skills/ tree.

The per-model derived skills authored under `docs/okf/<stack>/derived/*.SKILL.md`
live OUTSIDE the `//go:embed all:skills` tree, so they do not ship in the ocode
binary. This tool copies each one into `skills/kaizen/<name>/SKILL.md`, where
`<name>` is the skill's frontmatter `name` (e.g. the conduct/tencent-hy3 skill
lands at `skills/kaizen/conduct-tuning-tencent-hy3/SKILL.md`).

The runtime gate (internal/skill/loader.go) admits these skills ONLY when the
active model matches their `tuned_for` AND their `stack` is active, so it is safe
to embed all of them — an ungated catalog never lists a Kaizen skill.

Behavior:
  * idempotent — re-running produces the same tree (byte-identical files are not
    rewritten);
  * generic over N stacks — copies EVERY `docs/okf/*/derived/*.SKILL.md`;
  * prunes stale `skills/kaizen/<name>/` dirs whose source no longer exists;
  * logs every write / skip / prune.

Usage:  python3 docs/okf/_tools/sync-derived-skills.py
Run from anywhere (paths are resolved relative to this file). Exits non-zero on a
malformed source skill (missing/blank frontmatter `name`) — fail fast, never
silently drop a skill.
"""
import pathlib
import shutil
import sys

OKF = pathlib.Path(__file__).resolve().parent.parent          # docs/okf
REPO = OKF.parent.parent                                       # repo root
KAIZEN_DIR = REPO / "skills" / "kaizen"


def read_name(src: pathlib.Path) -> str:
    """Return the frontmatter `name` of a SKILL.md, or fail loudly.

    Parsed line-by-line (split on the first ':' of each key line), mirroring the
    Go loader's `parseFrontmatter` — the derived skills' frontmatter values carry
    unquoted colons, so a strict YAML load would reject them.
    """
    lines = src.read_text(encoding="utf-8").splitlines()
    if not lines or lines[0].strip() != "---":
        sys.exit(f"ERROR: {src} has no frontmatter (expected leading '---')")
    for raw in lines[1:]:
        line = raw.strip()
        if line == "---":
            break
        key, sep, value = line.partition(":")
        if sep and key.strip().lower() == "name":
            name = value.strip().strip("\"'").strip()
            if name:
                return name
            sys.exit(f"ERROR: {src} frontmatter has a blank `name`")
    sys.exit(f"ERROR: {src} frontmatter is missing a `name` key")


def is_illustrative(src: pathlib.Path) -> bool:
    """True if the skill's frontmatter has `illustrative: true`.

    Illustrative skills are teaching placeholders (not a real closed-book eval),
    so they must NOT ship in the binary — embedding one would inject unvalidated
    guidance downstream for a matching model. Same line-by-line parse as read_name.
    """
    for raw in src.read_text(encoding="utf-8").splitlines()[1:]:
        line = raw.strip()
        if line == "---":
            break
        key, sep, value = line.partition(":")
        if sep and key.strip().lower() == "illustrative":
            return value.split("#", 1)[0].strip().strip("\"'").lower() == "true"
    return False


def main() -> None:
    sources = sorted(OKF.glob("*/derived/*.SKILL.md"))
    if not sources:
        print("sync-derived-skills: no derived skills found under docs/okf/*/derived/")

    wanted: set[str] = set()
    wrote = skipped = 0
    for src in sources:
        if is_illustrative(src):
            print(f"  skip (illustrative, not embedded): {src.relative_to(REPO)}")
            continue
        name = read_name(src)
        if name in wanted:
            sys.exit(f"ERROR: duplicate skill name {name!r} (second source: {src})")
        wanted.add(name)

        dest_dir = KAIZEN_DIR / name
        dest = dest_dir / "SKILL.md"
        new = src.read_text(encoding="utf-8")
        if dest.exists() and dest.read_text(encoding="utf-8") == new:
            print(f"  skip (unchanged): {src.relative_to(REPO)} -> {dest.relative_to(REPO)}")
            skipped += 1
            continue
        dest_dir.mkdir(parents=True, exist_ok=True)
        dest.write_text(new, encoding="utf-8")
        print(f"  wrote: {src.relative_to(REPO)} -> {dest.relative_to(REPO)}")
        wrote += 1

    # Prune stale kaizen dirs whose source skill no longer exists.
    pruned = 0
    if KAIZEN_DIR.exists():
        for child in sorted(KAIZEN_DIR.iterdir()):
            if child.is_dir() and child.name not in wanted:
                shutil.rmtree(child)
                print(f"  pruned (source gone): {child.relative_to(REPO)}")
                pruned += 1

    print(
        f"sync-derived-skills: {len(wanted)} skill(s) — "
        f"{wrote} written, {skipped} unchanged, {pruned} pruned."
    )


if __name__ == "__main__":
    main()
