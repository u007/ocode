#!/usr/bin/env python3
"""testdoctor — stack-agnostic metrics for pruning a test suite safely.

  timing   per-test wall time from `go test -json` output or JUnit XML files
  smells   static bloat scan of test files (any language)
  plan     run each unit in isolation with coverage -> keep/drop plan
  verify   re-measure coverage after pruning, diff against the plan, run gates

A "unit" is whatever you can run alone: a test file (default), or a name from
--unit-list. Coverage formats: goprofile, lcov, clover (PHP), jacoco (Java).
Presets: go, vitest, jest, bun, pytest, phpunit. Anything else: --cmd/--format.
Everything is stdlib; run with `python3 -I`.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import shlex
import subprocess
import sys
import tempfile
import time
import xml.etree.ElementTree as ET
from collections import defaultdict
from pathlib import Path


def die(msg: str, code: int = 2) -> None:
    print(f"testdoctor: {msg}", file=sys.stderr)
    sys.exit(code)


def write_json(path: Path, data) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True))
    print(f"wrote {path}")


# ---------------------------------------------------------------- timing ---

def timing_go_json(text: str, subtests: bool) -> tuple[list, list]:
    tests, pkgs = {}, {}
    for line in text.splitlines():
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        act, pkg = ev.get("Action"), ev.get("Package", "")
        if act not in ("pass", "fail", "skip"):
            continue
        if "Test" in ev:
            if "/" in ev["Test"] and not subtests:
                continue
            tests[(pkg, ev["Test"])] = {"suite": pkg, "test": ev["Test"], "status": act,
                                        "elapsed": ev.get("Elapsed", 0.0)}
        elif pkg:
            pkgs[pkg] = {"suite": pkg, "status": act, "elapsed": ev.get("Elapsed", 0.0)}
    return list(tests.values()), list(pkgs.values())


def timing_junit(paths: list[Path]) -> tuple[list, list]:
    tests, suites = [], []
    for p in paths:
        root = ET.parse(p).getroot()
        for s in root.iter("testsuite"):
            name = s.get("name", p.name)
            suites.append({"suite": name, "status": "fail" if int(s.get("failures", 0)) + int(s.get("errors", 0)) else "pass",
                           "elapsed": float(s.get("time", 0) or 0)})
            for c in s.iter("testcase"):
                status = "pass"
                if c.find("failure") is not None or c.find("error") is not None:
                    status = "fail"
                elif c.find("skipped") is not None:
                    status = "skip"
                tests.append({"suite": c.get("classname") or name, "test": c.get("name"),
                              "status": status, "elapsed": float(c.get("time", 0) or 0)})
    return tests, suites


def cmd_timing(args) -> None:
    if args.go:
        cmd = ["go", "test", "-json", "-count=1"] + args.inputs
        print("$", " ".join(cmd), file=sys.stderr)
        text = subprocess.run(cmd, text=True, capture_output=True).stdout
        tests, suites = timing_go_json(text, args.subtests)
    else:
        paths = [p for g in args.inputs for p in Path(".").glob(g)]
        if not paths:
            die("no JUnit XML files matched (or pass --go for go test -json)")
        tests, suites = timing_junit(paths)
    tests.sort(key=lambda r: -r["elapsed"])
    suites.sort(key=lambda r: -r["elapsed"])
    write_json(Path(args.out_dir) / "timing.json", {"tests": tests, "suites": suites})
    print(f"\n{len(tests)} tests, {sum(r['elapsed'] for r in tests):.1f}s summed test time")
    print("\nslowest suites:")
    for r in suites[: args.top]:
        print(f"  {r['elapsed']:8.2f}s  {r['status']:4}  {r['suite']}")
    print("\nslowest tests:")
    for r in tests[: args.top]:
        print(f"  {r['elapsed']:8.2f}s  {r['status']:4}  {r['suite']}  {r['test']}")
    failed = [r for r in tests if r["status"] == "fail"]
    if failed:
        print(f"\n{len(failed)} FAILING tests — suite is not green, fix before pruning:")
        for r in failed[:50]:
            print(f"  {r['suite']}  {r['test']}")


# ---------------------------------------------------------------- smells ---

TEST_GLOBS = ["*_test.go", "*.test.*", "*.spec.*", "test_*.py", "*_test.py", "*Test.php",
              "*Test.java", "*Tests.cs", "*_spec.rb", "*_test.rb", "*_test.ex*", "*.test.dart"]
TEST_START = re.compile(
    r"^\s*(func Test\w+\(|(?:it|test)(?:\.\w+)?\(|def test_\w+|"
    r"(?:public )?function test\w+|@Test\b|#\[test\]|\[(?:Fact|Theory|Test)\]|"
    r"(?:it|test) ['\"])")
SLEEP = re.compile(r"\btime\.Sleep\(|\bsleep\(|\busleep\(|\bThread\.sleep\(|\btime\.sleep\(|"
                   r"\bsetTimeout\(|\bTask\.Delay\(|\bawait new Promise\(")
ASSERT = re.compile(r"\bassert|\bexpect\(|\bt\.(Error|Fatal|Fail)|\brequire\.|\bshould\b|"
                    r"\bAssert\.|\$this->assert|\(t[,)]|\.toBe|\bsnapshot", re.I)


def normalize(body: str) -> str:
    body = re.sub(r"//.*|#.*", "", body)
    body = re.sub(r"\"[^\"\n]*\"|'[^'\n]*'|`[^`]*`", '"S"', body)
    body = re.sub(r"\b\d+\b", "N", body)
    return re.sub(r"\s+", " ", body).strip()


def test_blocks(src: str) -> list[tuple[int, int, str]]:
    """Split a file into test blocks: from one test-start line to the next."""
    lines = src.splitlines()
    starts = [i for i, l in enumerate(lines) if TEST_START.match(l)]
    blocks = []
    for k, s in enumerate(starts):
        e = starts[k + 1] if k + 1 < len(starts) else len(lines)
        blocks.append((s + 1, e, "\n".join(lines[s:e])))
    return blocks


def cmd_smells(args) -> None:
    files = []
    for d in args.dirs or ["."]:
        for g in TEST_GLOBS:
            files += [p for p in Path(d).rglob(g)
                      if not any(x in p.parts for x in ("node_modules", ".worktrees", "vendor", "target", "dist"))]
    files = sorted(set(files))
    shapes, sleeps, big, no_assert = defaultdict(list), [], [], []
    per_dir = defaultdict(lambda: [0, 0, 0])
    for f in files:
        src = f.read_text(errors="replace")
        blocks = test_blocks(src)
        d = per_dir[str(f.parent)]
        d[0] += 1
        d[1] += len(blocks)
        d[2] += src.count("\n")
        for s, e, body in blocks:
            loc = f"{f}:{s}"
            # hash the body without its first (name-bearing) line
            shapes[hashlib.sha1(normalize(body.split("\n", 1)[-1]).encode()).hexdigest()].append(loc)
            if e - s >= args.max_lines:
                big.append({"lines": e - s, "loc": loc})
            if SLEEP.search(body):
                sleeps.append(loc)
            if not ASSERT.search(body):
                no_assert.append(loc)
    dups = [v for v in shapes.values() if len(v) > 1]
    report = {"files": len(files), "same_shape_groups": dups, "sleep": sleeps,
              "oversized": sorted(big, key=lambda r: -r["lines"]), "no_assertion": no_assert,
              "dirs": sorted([{"dir": k, "files": v[0], "tests": v[1], "loc": v[2]} for k, v in per_dir.items()],
                             key=lambda r: -r["loc"])}
    write_json(Path(args.out_dir) / "smells.json", report)
    print(f"\n{len(files)} test files, {sum(v[1] for v in per_dir.values())} tests")
    print(f"{len(dups)} same-shape groups ({sum(len(v) - 1 for v in dups)} extra copies) — "
          "merge into table/parameterised tests or drop true duplicates")
    print(f"{len(sleeps)} tests sleep — poll with a deadline or use a fake clock")
    print(f"{len(big)} tests >= {args.max_lines} lines; {len(no_assert)} tests with no visible assertion")
    print("\nheaviest directories by test LOC:")
    for r in report["dirs"][: args.top]:
        print(f"  {r['loc']:7d} loc {r['tests']:5d} tests {r['files']:4d} files  {r['dir']}")


# ------------------------------------------------------------- coverage ---

def parse_goprofile(p: Path) -> set:
    cov = set()
    if p.exists():
        for line in p.read_text().splitlines()[1:]:
            block, _, count = line.rpartition(" ")
            if count.strip() not in ("", "0"):
                cov.add(block)
    return cov


def parse_lcov(p: Path) -> set:
    cov, sf = set(), ""
    if p.exists():
        for line in p.read_text(errors="replace").splitlines():
            if line.startswith("SF:"):
                sf = line[3:]
            elif line.startswith("DA:"):
                ln, hits = line[3:].split(",")[:2]
                if hits.strip() != "0":
                    cov.add(f"{sf}:{ln}")
            elif line.startswith("BRDA:"):
                ln, blk, br, taken = line[5:].split(",")[:4]
                if taken not in ("-", "0"):
                    cov.add(f"{sf}:{ln}b{blk}.{br}")
    return cov


def parse_clover(p: Path) -> set:
    cov = set()
    if p.exists():
        for f in ET.parse(p).getroot().iter("file"):
            for l in f.iter("line"):
                if int(l.get("count", 0)) > 0:
                    cov.add(f"{f.get('name')}:{l.get('num')}")
    return cov


def parse_jacoco(p: Path) -> set:
    cov = set()
    if p.exists():
        for pk in ET.parse(p).getroot().iter("package"):
            for sf in pk.iter("sourcefile"):
                for l in sf.iter("line"):
                    if int(l.get("ci", 0)) > 0:
                        cov.add(f"{pk.get('name')}/{sf.get('name')}:{l.get('nr')}")
                    if int(l.get("cb", 0)) > 0:
                        cov.add(f"{pk.get('name')}/{sf.get('name')}:{l.get('nr')}b")
    return cov


def cov_path(name: str, covdir: Path, cwd: str) -> Path:
    """Absolute as given; else under {covdir} if present there, else under --cwd
    (runners like Maven write to a fixed project-relative path)."""
    c = Path(name)
    if c.is_absolute():
        return c
    return covdir / c if (covdir / c).exists() else Path(cwd) / c


FORMATS = {"goprofile": parse_goprofile, "lcov": parse_lcov, "clover": parse_clover, "jacoco": parse_jacoco}

# Preset: unit glob, isolation command, full-suite command, coverage format,
# coverage file (relative to {covdir}), extra gates. {unit} {covdir} {root}
# are substituted. Go is special-cased in select_units (units = files, run by
# -run regex over the Test funcs they contain).
PRESETS = {
    "go": dict(glob="*_test.go", format="goprofile",
               cmd="go test -count=1 -covermode=set -coverprofile={covdir}/cov.out -run {unit} {root}",
               full="go test -count=1 -covermode=set -coverprofile={covdir}/cov.out {root}",
               cov="cov.out", gates=["go test -count=1 -race {root}"]),
    "vitest": dict(glob="*.test.*", format="lcov", cov="lcov.info",
                   cmd="npx vitest run {unit} --coverage.enabled --coverage.reporter=lcov --coverage.reportsDirectory={covdir}",
                   full="npx vitest run --coverage.enabled --coverage.reporter=lcov --coverage.reportsDirectory={covdir}"),
    "jest": dict(glob="*.test.*", format="lcov", cov="lcov.info",
                 cmd="npx jest {unit} --coverage --coverageReporters=lcov --coverageDirectory={covdir}",
                 full="npx jest --coverage --coverageReporters=lcov --coverageDirectory={covdir}"),
    "bun": dict(glob="*.test.*", format="lcov", cov="lcov.info",
                cmd="bun test {unit} --coverage --coverage-reporter=lcov --coverage-dir={covdir}",
                full="bun test --coverage --coverage-reporter=lcov --coverage-dir={covdir}"),
    "pytest": dict(glob="test_*.py", format="lcov", cov="lcov.info",
                   cmd="python -m pytest -q {unit} --cov --cov-report=lcov:{covdir}/lcov.info",
                   full="python -m pytest -q --cov --cov-report=lcov:{covdir}/lcov.info"),
    "phpunit": dict(glob="*Test.php", format="clover", cov="clover.xml",
                    cmd="vendor/bin/phpunit {unit} --coverage-clover {covdir}/clover.xml",
                    full="vendor/bin/phpunit --coverage-clover {covdir}/clover.xml"),
}


def select_units(args, root: Path) -> dict[str, str]:
    """unit label -> value substituted for {unit}."""
    if args.unit_list:
        return {l: l for l in Path(args.unit_list).read_text().split("\n") if l.strip()}
    files = sorted(p for p in root.rglob(args.glob)
                   if not any(x in p.parts for x in ("node_modules", "vendor", ".worktrees")))
    if args.preset == "go":
        units = {}
        for f in files:
            names = re.findall(r"^func (Test\w+)\(", f.read_text(errors="replace"), re.M)
            names = [n for n in names if n != "TestMain"]
            if not names:
                continue
            if args.granularity == "test":
                for n in names:
                    units[f"{f.relative_to(root)}:{n}"] = f"'^{n}$'"
            else:
                units[str(f.relative_to(root))] = "'^(" + "|".join(names) + ")$'"
        if not units:
            die(f"no Test funcs under {root}")
        return units
    return {str(p.relative_to(root)): str(p.relative_to(args.cwd)) for p in files}


def sub(cmd: str, **kw) -> list[str]:
    return shlex.split(cmd.format(**kw))


def plan_from_sets(units: dict[str, set], times: dict[str, float]) -> dict:
    """Greedy set cover: units with unique blocks are essential; then add the
    unit with the most new blocks until the baseline is reached."""
    owners = defaultdict(int)
    for s in units.values():
        for b in s:
            owners[b] += 1
    essential = {u for u, s in units.items() if any(owners[b] == 1 for b in s)}
    covered = set().union(*(units[u] for u in essential)) if essential else set()
    keep, rest = set(essential), [u for u in units if u not in essential]
    while rest:
        best = max(rest, key=lambda u: (len(units[u] - covered), -times.get(u, 0)))
        if not units[best] - covered:
            break
        keep.add(best)
        covered |= units[best]
        rest.remove(best)
    drop = []
    for u in rest:
        overlap = sorted(((len(units[u] & units[k]), k) for k in keep), reverse=True)
        drop.append({"unit": u, "blocks": len(units[u]), "seconds": round(times.get(u, 0), 2),
                     "covered_by": [k for n, k in overlap[:3] if n]})
    drop.sort(key=lambda d: -d["seconds"])
    return {"baseline_blocks": len(set().union(*units.values())) if units else 0,
            "keep": sorted(keep), "essential": sorted(essential), "drop_candidates": drop,
            "empty_units": sorted(u for u, s in units.items() if not s),
            "unit_blocks": {u: len(s) for u, s in units.items()},
            "unit_seconds": {u: round(t, 2) for u, t in times.items()}}


def cmd_plan(args) -> None:
    preset = PRESETS.get(args.preset, {})
    if not preset and not (args.cmd and args.format and args.full_cmd):
        die("unknown preset; use --preset or all of --cmd/--full-cmd/--format (see -h)")
    args.glob = args.glob or preset.get("glob", "*")
    fmt = args.format or preset["format"]
    cov_name = args.cov_file or preset.get("cov", "")
    cmd_t = args.cmd or preset["cmd"]
    full_t = args.full_cmd or preset["full"]
    gates = args.gate or preset.get("gates", [])
    root = Path(args.cwd, args.root)  # globbing happens from --cwd, like the commands
    units = select_units(args, root)
    if not units:
        die(f"no units matched {args.glob} under {root}")
    sets, times, failures = {}, {}, []
    for i, (label, val) in enumerate(units.items(), 1):
        covdir = Path(tempfile.mkdtemp(prefix="testdoctor-"))
        cmd = sub(cmd_t, unit=val, covdir=covdir, root=args.root)
        t0 = time.time()
        r = subprocess.run(cmd, cwd=args.cwd, text=True, capture_output=True)
        times[label] = time.time() - t0
        if r.returncode != 0:
            failures.append(label)
        sets[label] = FORMATS[fmt](cov_path(cov_name, covdir, args.cwd))
        print(f"[{i}/{len(units)}] {label}: {len(sets[label])} blocks {times[label]:.1f}s"
              + ("  FAIL" if r.returncode else ""), file=sys.stderr)
    if not any(sets.values()):
        print((r.stdout + r.stderr)[-1500:], file=sys.stderr)
        die("every unit produced zero coverage — coverage tooling missing or wrong --cov-file? (last output above)")
    plan = plan_from_sets(sets, times)
    plan.update({"preset": args.preset, "root": args.root, "cwd": args.cwd, "format": fmt,
                 "cov_file": cov_name, "full_cmd": full_t, "gates": gates,
                 "failed_units": failures, "baseline_set": sorted(set().union(*sets.values()))})
    tag = re.sub(r"[^A-Za-z0-9]+", "_", f"{args.preset}_{root}").strip("_")
    write_json(Path(args.out_dir) / f"plan-{tag}.json", plan)
    n = len(units)
    saved = sum(times[d["unit"]] for d in plan["drop_candidates"])
    print(f"\n{n} units, baseline {plan['baseline_blocks']} covered blocks")
    print(f"keep {len(plan['keep'])} ({len(plan['essential'])} essential), "
          f"drop candidates {len(plan['drop_candidates'])} (~{saved:.0f}s isolated, incl. process overhead)")
    if plan["empty_units"]:
        print(f"{len(plan['empty_units'])} units cover nothing (smoke/external?): " + ", ".join(plan["empty_units"][:10]))
    if failures:
        print(f"{len(failures)} units FAIL alone (order-dependent?): " + ", ".join(failures[:10]))
    print("\ndrop candidates (slowest first):")
    for d in plan["drop_candidates"][:40]:
        print(f"  {d['seconds']:7.2f}s {d['blocks']:5d} blk  {d['unit']}  <= {', '.join(d['covered_by'][:2])}")


def cmd_verify(args) -> None:
    plan = json.loads(Path(args.plan).read_text())
    covdir = Path(tempfile.mkdtemp(prefix="testdoctor-"))
    cmds = [plan["full_cmd"]] + plan["gates"]
    now = None
    for i, t in enumerate(cmds):
        cmd = sub(t, unit="", covdir=covdir, root=plan["root"])
        print("$", " ".join(cmd), file=sys.stderr)
        r = subprocess.run(cmd, cwd=plan["cwd"], text=True, capture_output=True)
        if r.returncode != 0:
            print((r.stdout + r.stderr)[-3000:], file=sys.stderr)
            die(f"command failed after pruning: {t}", code=1)
        if i == 0:
            now = FORMATS[plan["format"]](cov_path(plan["cov_file"], covdir, plan["cwd"]))
    baseline = set(plan["baseline_set"])
    lost = sorted(baseline - now)
    print(f"\nbaseline {len(baseline)} blocks, now {len(now)} (+{len(now - baseline)} new), lost {len(lost)}")
    for b in lost[:60]:
        print(f"  LOST {b}")
    if lost:
        die(f"{len(lost)} baseline blocks lost — restore the units named in covered_by or add targeted tests", code=1)
    print("OK: no coverage lost; suite and gates green")


# ------------------------------------------------------------------ main ---

def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--out-dir", default=".test-doctor")
    sp = ap.add_subparsers(dest="cmd", required=True)

    s = sp.add_parser("timing", help="per-test time: --go <pkgs> or JUnit XML globs")
    s.add_argument("inputs", nargs="+", help="go packages (with --go) or JUnit XML globs")
    s.add_argument("--go", action="store_true", help="run `go test -json` on inputs")
    s.add_argument("--subtests", action="store_true")
    s.add_argument("--top", type=int, default=30)
    s.set_defaults(fn=cmd_timing)

    s = sp.add_parser("smells", help="static bloat scan (any language)")
    s.add_argument("dirs", nargs="*")
    s.add_argument("--max-lines", type=int, default=120)
    s.add_argument("--top", type=int, default=20)
    s.set_defaults(fn=cmd_smells)

    s = sp.add_parser("plan", help="isolate each unit with coverage; emit keep/drop plan",
                      epilog="custom stack example: --cmd 'mvn -q -Dtest={unit} test' "
                             "--full-cmd 'mvn -q test' --format jacoco --cov-file target/site/jacoco/jacoco.xml "
                             "--unit-list tests.txt   ({covdir} is a fresh temp dir per run)")
    s.add_argument("root", help="directory (or Go package path) holding the units")
    s.add_argument("--preset", choices=sorted(PRESETS), default="")
    s.add_argument("--granularity", choices=["file", "test"], default="file", help="go only")
    s.add_argument("--glob", default="", help="unit file glob (overrides preset)")
    s.add_argument("--unit-list", default="", help="file with one unit name per line (test names, classes...)")
    s.add_argument("--cmd", default="", help="isolation command template with {unit} {covdir} {root}")
    s.add_argument("--full-cmd", default="", help="full-suite coverage command for verify")
    s.add_argument("--format", choices=sorted(FORMATS), default="")
    s.add_argument("--cov-file", default="", help="coverage file: under {covdir}, else relative to --cwd, or absolute")
    s.add_argument("--gate", action="append", help="extra verify command (repeatable)")
    s.add_argument("--cwd", default=".", help="working directory for commands")
    s.set_defaults(fn=cmd_plan)

    s = sp.add_parser("verify", help="re-measure coverage, diff against plan baseline, run gates")
    s.add_argument("plan")
    s.set_defaults(fn=cmd_verify)

    args = ap.parse_args()
    args.fn(args)


if __name__ == "__main__":
    main()
