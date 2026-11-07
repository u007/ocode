#!/usr/bin/env python3
"""Merge harvested corpus shards into DIR/corpus.jsonl and DIR/actors.jsonl.

merge --dir DIR   reads DIR/harvest/*.jsonl (artifacts) and DIR/harvest/*.actors.jsonl,
                  validates every line against the contract schema, dedupes artifacts by
                  url and by normalised title, drops artifacts whose actors are unknown,
                  and writes the two merged files plus DIR/corpus_log.jsonl.
"""

import argparse
import json
import re
import sys
from pathlib import Path

ARTIFACT_KEYS = {"id", "title", "actor_ids", "date", "url", "text"}
ACTOR_KEYS = {"id", "name", "url"}
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
SLUG_RE = re.compile(r"^[a-z0-9][a-z0-9-]*$")


def read_jsonl(path: Path):
    with path.open() as fh:
        for n, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                yield n, json.loads(line)
            except json.JSONDecodeError as exc:
                yield n, exc


def norm_title(title: str) -> str:
    return re.sub(r"[^a-z0-9]+", " ", title.lower()).strip()


def norm_url(url) -> str:
    if not isinstance(url, str):
        return ""
    u = url.strip().lower().rstrip("/")
    u = re.sub(r"^https?://(www\.)?", "", u)
    return u


def merge(dir_path: Path) -> int:
    harvest = dir_path / "harvest"
    if not harvest.is_dir():
        sys.exit(f"error: {harvest} does not exist")
    log_path = dir_path / "corpus_log.jsonl"
    log = log_path.open("w")

    def drop(stage, item, reason):
        rec = {"stage": stage, "item": item, "reason": reason}
        log.write(json.dumps(rec) + "\n")
        print(f"drop [{stage}] {item}: {reason}", file=sys.stderr)

    actors: dict[str, dict] = {}
    for path in sorted(harvest.glob("*.actors.jsonl")):
        for n, rec in read_jsonl(path):
            where = f"{path.name}:{n}"
            if isinstance(rec, Exception):
                drop("actors", where, f"invalid json: {rec}")
                continue
            if set(rec) != ACTOR_KEYS:
                drop("actors", where, f"keys {sorted(rec)} != {sorted(ACTOR_KEYS)}")
                continue
            if not SLUG_RE.match(rec["id"]):
                drop("actors", where, f"bad slug {rec['id']!r}")
                continue
            if rec["id"] in actors and actors[rec["id"]]["name"] != rec["name"]:
                drop("actors", where, f"id {rec['id']} already used for {actors[rec['id']]['name']!r}")
                continue
            actors[rec["id"]] = rec

    artifacts: dict[str, dict] = {}
    seen_url: dict[str, str] = {}
    seen_title: dict[str, str] = {}
    for path in sorted(harvest.glob("*.jsonl")):
        if path.name.endswith(".actors.jsonl"):
            continue
        for n, rec in read_jsonl(path):
            where = f"{path.name}:{n}"
            if isinstance(rec, Exception):
                drop("artifacts", where, f"invalid json: {rec}")
                continue
            if set(rec) != ARTIFACT_KEYS:
                drop("artifacts", where, f"keys {sorted(rec)} != {sorted(ARTIFACT_KEYS)}")
                continue
            if not SLUG_RE.match(rec["id"]):
                drop("artifacts", where, f"bad slug {rec['id']!r}")
                continue
            if rec["date"] is not None and not DATE_RE.match(str(rec["date"])):
                drop("artifacts", where, f"bad date {rec['date']!r}")
                continue
            if not isinstance(rec["actor_ids"], list) or not rec["actor_ids"]:
                drop("artifacts", where, "actor_ids empty")
                continue
            unknown = [a for a in rec["actor_ids"] if a not in actors]
            if unknown:
                drop("artifacts", where, f"unknown actors {unknown}")
                continue
            words = len(rec["text"].split())
            if words < 40:
                drop("artifacts", where, f"text too short ({words} words)")
                continue
            if rec["id"] in artifacts:
                drop("artifacts", where, f"duplicate id {rec['id']} (kept {artifacts[rec['id']]['_where']})")
                continue
            u = norm_url(rec["url"])
            if u and u in seen_url:
                drop("artifacts", where, f"duplicate url of {seen_url[u]}")
                continue
            t = norm_title(rec["title"])
            if t in seen_title:
                drop("artifacts", where, f"duplicate title of {seen_title[t]}")
                continue
            if u:
                seen_url[u] = rec["id"]
            seen_title[t] = rec["id"]
            rec["_where"] = where
            artifacts[rec["id"]] = rec

    used = {a for rec in artifacts.values() for a in rec["actor_ids"]}
    for aid in sorted(set(actors) - used):
        drop("actors", aid, "no artifacts reference it")

    with (dir_path / "corpus.jsonl").open("w") as fh:
        for aid in sorted(artifacts):
            rec = dict(artifacts[aid])
            rec.pop("_where")
            rec["actor_ids"] = sorted(set(rec["actor_ids"]))
            fh.write(json.dumps(rec, ensure_ascii=False) + "\n")
    with (dir_path / "actors.jsonl").open("w") as fh:
        for aid in sorted(used):
            fh.write(json.dumps(actors[aid], ensure_ascii=False) + "\n")
    log.close()

    dated = sum(1 for r in artifacts.values() if r["date"])
    multi = sum(1 for a in used if sum(a in r["actor_ids"] for r in artifacts.values()) > 1)
    print(f"artifacts: {len(artifacts)}  actors: {len(used)}  dated: {dated}  "
          f"actors with >1 artifact: {multi}  log: {log_path}")
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("merge", help="merge DIR/harvest/*.jsonl into DIR/corpus.jsonl + DIR/actors.jsonl")
    p.add_argument("--dir", required=True, type=Path)
    args = parser.parse_args()
    if args.cmd == "merge":
        sys.exit(merge(args.dir))


if __name__ == "__main__":
    main()
