#!/usr/bin/env python3
"""Temporal holdout for the alien-discovery scores.

Contract: skills/alien-discovery/references/pipeline-contract.md (alien_holdout.py section).

Old artifacts are dated before --cutoff, new artifacts on or after it. Targets are the
distinct k-subsets of each new artifact's atoms. A target is strictly novel when no old
artifact contains the whole subset. A target is reachable when all its atoms appear in some
old artifact; an unreachable target can never be in the pool. The pool is the sets present in
coherence.json, availability.json and candidates.json whose atoms are all old-artifact atoms,
re-ranked with the z-scores and fusion from alien_score. Recovered targets are those in the
top --top of the re-ranked pool. Headline chance = top / pool_size x targets_in_pool.
"""
from __future__ import annotations

import argparse
import itertools
import sys
from collections import Counter
from datetime import date
from pathlib import Path

from alien_score import (
    StageError,
    key_of,
    load_json,
    load_jsonl,
    need,
    parse_key,
    rank_pool,
    say,
    write_json,
)


def _ratio(recovered: int, chance: float):
    return (recovered / chance) if chance > 0 else None


def _block(recovered: list, top_used: int, pool_size: int, in_pool: int, total: int) -> dict:
    """Headline chance and ratio use targets in the pool; the all-targets figures are secondary."""
    chance = top_used / pool_size * in_pool
    chance_all = top_used / pool_size * total
    return {
        "targets": total,
        "targets_in_pool": in_pool,
        "recovered": len(recovered),
        "chance": chance,
        "ratio": _ratio(len(recovered), chance),
        "chance_all_targets": chance_all,
        "ratio_all_targets": _ratio(len(recovered), chance_all),
    }


def _bad_atom(aid, c):
    raise StageError(f"vocab.json artifact_atoms[{aid}] has non-integer cluster id {c!r}")


def load_split(d: Path, cutoff: date, k: int) -> dict:
    """Dated split of the corpus, the atoms of every artifact, and the distinct k-subset targets."""
    corpus = load_jsonl(d / "corpus.jsonl")
    artifact_atoms = need(load_json(d / "vocab.json"), "artifact_atoms", "vocab.json")
    dates: dict = {}
    undated: list = []
    for rec in corpus:
        aid = need(rec, "id", "corpus.jsonl")
        raw = need(rec, "date", f"corpus.jsonl artifact {aid}")
        if raw is None:
            undated.append(aid)
            continue
        try:
            dates[aid] = date.fromisoformat(raw)
        except (TypeError, ValueError) as e:
            raise StageError(f"corpus.jsonl artifact {aid}: date {raw!r} is not YYYY-MM-DD") from e
    if undated:
        say(f"holdout: {len(undated)} undated artifacts excluded from both sides: {', '.join(sorted(undated))}")
    if not dates:
        raise StageError("no artifact in corpus.jsonl has a date; the holdout needs dated artifacts")
    old = sorted(a for a, dt in dates.items() if dt < cutoff)
    new = sorted(a for a, dt in dates.items() if dt >= cutoff)
    if not old:
        raise StageError(f"no dated artifact before cutoff {cutoff.isoformat()}")
    if not new:
        raise StageError(f"no dated artifact on or after cutoff {cutoff.isoformat()}")

    atoms: dict = {}
    for aid in sorted(dates):
        if aid not in artifact_atoms:
            say(f"holdout: artifact {aid} has no entry in vocab.json artifact_atoms; treated as having no atoms")
            atoms[aid] = frozenset()
            continue
        atoms[aid] = frozenset(
            int(c) if isinstance(c, int) and not isinstance(c, bool) else _bad_atom(aid, c)
            for c in artifact_atoms[aid]
        )
    old_sets = [atoms[a] for a in old]
    old_union = frozenset().union(*old_sets)

    owners: dict = {}
    for aid in new:
        ats = sorted(atoms[aid])
        if len(ats) < k:
            say(f"holdout: new artifact {aid} has {len(ats)} atoms, fewer than k={k}; no targets from it")
            continue
        for t in itertools.combinations(ats, k):
            owners.setdefault(t, set()).add(aid)
    all_targets = sorted(owners)
    reachable = [t for t in all_targets if set(t) <= old_union]
    return {
        "old": old,
        "new": new,
        "undated": sorted(undated),
        "old_sets": old_sets,
        "old_union": old_union,
        "all_targets": all_targets,
        "reachable": reachable,
        "novel": {t: not any(set(t) <= s for s in old_sets) for t in all_targets},
    }


def cmd_targets(args) -> None:
    d = Path(args.dir)
    cutoff = _parse_cutoff(args.cutoff)
    if args.k < 2:
        raise StageError(f"--k must be at least 2, got {args.k}")
    split = load_split(d, cutoff, args.k)
    unreachable = len(split["all_targets"]) - len(split["reachable"])
    write_json(
        d / "holdout_targets.json",
        {
            "cutoff": cutoff.isoformat(),
            "k": args.k,
            "unreachable": unreachable,
            "targets": [list(t) for t in split["reachable"]],
        },
    )
    print(
        f"targets cutoff={cutoff.isoformat()} k={args.k}: {len(split['all_targets'])} distinct targets, "
        f"{len(split['reachable'])} reachable (written), {unreachable} unreachable"
    )
    say(f"holdout: wrote {d / 'holdout_targets.json'}")


def _parse_cutoff(text: str) -> date:
    try:
        return date.fromisoformat(text)
    except ValueError as e:
        raise StageError(f"--cutoff must be YYYY-MM-DD, got {text!r}") from e


def cmd_run(args) -> None:
    d = Path(args.dir)
    cutoff = _parse_cutoff(args.cutoff)
    if args.k < 2:
        raise StageError(f"--k must be at least 2, got {args.k}")
    betas = list(dict.fromkeys(args.betas))
    for b in betas:
        if not 0.0 <= b <= 1.0:
            raise StageError(f"beta must be in [0, 1], got {b}")

    split = load_split(d, cutoff, args.k)
    all_targets = split["all_targets"]
    reachable_set = set(split["reachable"])
    novel = split["novel"]
    n_novel = sum(novel.values())

    coh = need(load_json(d / "coherence.json"), "sets", "coherence.json")
    avail = need(load_json(d / "availability.json"), "sets", "availability.json")
    cand_sets = need(load_json(d / "candidates.json"), "sets", "candidates.json")
    coh_log = {rec["set"]: rec["reason"] for rec in load_jsonl(d / "coherence_log.jsonl")}
    sources = {
        "coherence": set(coh),
        "availability": set(avail),
        "candidates": {key_of(s) for s in cand_sets},
    }
    # Same pool as alien_score select (three-way intersection), then restricted to old-artifact atoms.
    pool_full_set = set.intersection(*sources.values())
    pool_keys = [parse_key(k) for k in sorted(pool_full_set, key=parse_key) if set(parse_key(k)) <= split["old_union"]]
    outside = len(pool_full_set) - len(pool_keys)
    scored = sources["coherence"] | sources["availability"]
    skipped = []
    for key in sorted(scored - pool_full_set, key=parse_key):
        missing = [name for name, keys in sources.items() if key not in keys]
        skipped.append({"set": key, "reason": "missing from " + ", ".join(missing)})
    never_scored = len(sources["candidates"] - scored)
    for rec in skipped:
        say(f"holdout: set {rec['set']} skipped: {rec['reason']}")
    if not pool_keys:
        raise StageError("holdout pool is empty: no scored set lies entirely in old-artifact atoms")

    # Exactly one status per target. Only "in_pool" targets can be recovered.
    status: dict = {}
    for t in all_targets:
        key = key_of(t)
        if t not in reachable_set:
            status[t] = "unreachable"
        elif key not in sources["candidates"]:
            status[t] = "excluded_by_candidates"
        elif key not in sources["coherence"]:
            status[t] = "dropped_in_coherence" if key in coh_log else "not_in_coherence"
        elif key not in sources["availability"]:
            status[t] = "not_in_availability"
        else:
            status[t] = "in_pool"
    pool_set = set(pool_keys)
    if {t for t in all_targets if status[t] == "in_pool"} != {t for t in all_targets if t in pool_set}:
        raise StageError("internal error: target status disagrees with the pool membership")
    status_counts = Counter(status.values())
    all_in = [t for t in all_targets if status[t] == "in_pool"]
    novel_in = [t for t in all_in if novel[t]]

    pool_size = len(pool_keys)
    top_used = min(args.top, pool_size)
    if args.top > pool_size:
        say(f"holdout: --top {args.top} exceeds pool size {pool_size}; using {top_used}")
    ranked = rank_pool(
        pool_keys,
        [float(need(coh[key_of(t)], "score", "coherence.json")) for t in pool_keys],
        [float(need(avail[key_of(t)], "availability", "availability.json")) for t in pool_keys],
        betas,
    )

    per_beta = {}
    for b in betas:
        top_set = {e["set"] for e in ranked[b][:top_used]}
        rec_all = [t for t in all_in if t in top_set]
        rec_novel = [t for t in novel_in if t in top_set]
        per_beta[str(b)] = {
            "pool_size": pool_size,
            "top_used": top_used,
            "all": _block(rec_all, top_used, pool_size, len(all_in), len(all_targets)),
            "strictly_novel": _block(rec_novel, top_used, pool_size, len(novel_in), n_novel),
            "recovered": {
                "all": [key_of(t) for t in rec_all],
                "strictly_novel": [key_of(t) for t in rec_novel],
            },
        }

    write_json(
        d / "holdout.json",
        {
            "cutoff": cutoff.isoformat(),
            "k": args.k,
            "top": args.top,
            "top_used": top_used,
            "old_artifacts": split["old"],
            "new_artifacts": split["new"],
            "undated_artifacts": split["undated"],
            "targets": {
                "total": len(all_targets),
                "unreachable": status_counts["unreachable"],
                "in_pool": len(all_in),
                "strictly_novel": n_novel,
                "strictly_novel_in_pool": len(novel_in),
                "status_counts": {name: status_counts[name] for name in sorted(status_counts)},
            },
            "pool": {
                "size": pool_size,
                "counts": {
                    "coherence": len(sources["coherence"]),
                    "availability": len(sources["availability"]),
                    "candidates": len(sources["candidates"]),
                    "intersection": len(pool_full_set),
                },
                "excluded_outside_old_atoms": outside,
                "candidates_never_scored": never_scored,
                "skipped": skipped,
            },
            "betas": per_beta,
        },
    )

    counts = ", ".join(f"{name} {status_counts[name]}" for name in sorted(status_counts))
    print(f"holdout cutoff={cutoff.isoformat()} k={args.k} pool={pool_size} top={top_used}")
    print(f"targets: {len(all_targets)} total ({counts}); in pool {len(all_in)}, strictly novel in pool {len(novel_in)}")
    print("beta    | all in pool: recovered chance ratio | strictly novel in pool: recovered chance ratio")
    for b in betas:
        blk = per_beta[str(b)]
        a, nv = blk["all"], blk["strictly_novel"]
        print(
            f"{b:<7} | {a['targets_in_pool']:>12} {a['recovered']:>9} {a['chance']:>6.2f} "
            f"{_fmt_ratio(a['ratio']):>5} | {nv['targets_in_pool']:>22} {nv['recovered']:>9} "
            f"{nv['chance']:>6.2f} {_fmt_ratio(nv['ratio']):>5}"
        )
    say(f"holdout: wrote {d / 'holdout.json'}")


def _fmt_ratio(r) -> str:
    return "n/a" if r is None else f"{r:.2f}"


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="Temporal holdout for the alien-discovery scores.")
    sub = p.add_subparsers(dest="cmd", required=True)

    st = sub.add_parser("targets", help="write holdout_targets.json: reachable k-subset targets to score")
    st.add_argument("--dir", required=True)
    st.add_argument("--cutoff", required=True, help="YYYY-MM-DD; before = old, on or after = new")
    st.add_argument("--k", type=int, default=3)
    st.set_defaults(func=cmd_targets)

    sp = sub.add_parser("run", help="re-rank the old-atom pool and report recovery ratios")
    sp.add_argument("--dir", required=True)
    sp.add_argument("--cutoff", required=True, help="YYYY-MM-DD; before = old, on or after = new")
    sp.add_argument("--k", type=int, default=3)
    sp.add_argument("--top", type=int, default=300)
    sp.add_argument("--betas", type=float, nargs="+", default=[0.0, 0.7])
    sp.set_defaults(func=cmd_run)
    return p


def main(argv=None) -> None:
    args = build_parser().parse_args(argv)
    try:
        args.func(args)
    except StageError as e:
        say(f"error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
