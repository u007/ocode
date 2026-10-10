#!/usr/bin/env python3
"""Build bench/judge/cases.jsonl from the repo's existing judge fixtures plus simulated.yaml.

    python3 build_cases.py          # needs PyYAML (pip install pyyaml)

Sources:
  internal/agent/testdata/permission_judge_eval/must_ask.yaml    -> permission, expect=ask
  internal/agent/testdata/permission_judge_eval/should_allow.yaml -> permission, expect=allow
  bench/judge/simulated.yaml                                      -> simulated cases

The fixtures carry the author's home path. It is replaced with a neutral
/work/project so the committed file leaks no local path.
"""
import json
import os
import yaml

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
FIXTURES = os.path.join(REPO, "internal", "agent", "testdata", "permission_judge_eval")
OUT = os.path.join(HERE, "cases.jsonl")
PRIVATE_PREFIX = "/Users/james"
NEUTRAL = "/home/user"


def clean(s):
    return (s or "").replace(PRIVATE_PREFIX, NEUTRAL)


def perm_case(c, expect, source, wd):
    return {
        "id": c["id"],
        "judge": "permission",
        "source": source,
        "expect": expect,
        "concern": c.get("concern", ""),
        "state": {
            "tool": "bash",
            "command": clean(c["command"]),
            "working_directory": wd,
            "platform": c.get("platform", "linux"),
        },
    }


def main():
    cases = []
    for fname, expect in (("must_ask.yaml", "ask"), ("should_allow.yaml", "allow")):
        with open(os.path.join(FIXTURES, fname)) as f:
            doc = yaml.safe_load(f)
        wd = NEUTRAL  # fixture paths are replaced; keep one neutral root for all cases
        for c in doc["cases"]:
            cases.append(perm_case(c, expect, f"fixture:{fname}", wd))

    with open(os.path.join(HERE, "simulated.yaml")) as f:
        sim = yaml.safe_load(f)
    wd = sim["working_directory"]
    for c in sim["permission"]:
        cases.append(perm_case(c, c["expect"], "simulated", wd))
    for c in sim["relevance"]:
        cases.append({
            "id": c["id"],
            "judge": "relevance",
            "source": "simulated",
            "expect": c["expect"],
            "concern": "",
            "state": {"query": c["query"]},
            "document": c["doc"],
        })

    with open(OUT, "w") as f:
        for c in cases:
            f.write(json.dumps(c, ensure_ascii=False) + "\n")
    counts = {}
    for c in cases:
        k = (c["judge"], c["source"], c["expect"])
        counts[k] = counts.get(k, 0) + 1
    print(f"wrote {len(cases)} cases to {OUT}")
    for k in sorted(counts):
        print("  ", k, counts[k])


if __name__ == "__main__":
    main()
