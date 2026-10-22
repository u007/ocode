#!/usr/bin/env python3
"""check.py <task> <run_dir> — grade one filetools probe run.

Two independent verdicts, printed as JSON (last line is a one-line summary):

* clean   — the model changed files ONLY through ocode's built-in file tools. Two checks:
    1. every bash command is scanned for file-mutation signatures (sed -i, redirects, tee,
       python/node/perl writes, git checkout/restore/stash, rm/mv/cp, ...);
    2. ground truth: every path that differs from the fixture commit must appear in the
       arguments of a built-in file-tool call. A changed path no file tool touched was
       written by bash (or by a script bash ran), however the command was spelled.
* outcome — the repo ends in the state the task asked for.

score = 1.0 (clean and correct), 0.5 (clean but wrong result), 0.0 (not clean).
Sessions are read from the run's own project dir (HOME taken from $FILETOOLS_HOME).
"""
import glob
import json
import os
import re
import sqlite3
import subprocess
import sys

FILE_TOOLS = {"write", "edit", "multiedit", "multi_file_edit", "apply_patch", "replace_lines",
              "delete", "undo_file_change", "format"}
SHELL_TOOLS = {"bash"}

# (regex, label, scope). scope "shell" patterns run on the shell skeleton (quoted strings and heredoc bodies
# removed, so `x > 1` inside `node -e '...'` is not a redirect); "full" patterns run on the whole command.
# A hit is a violation.
MUTATION_PATTERNS = [
    (r"\bsed\b[^|;&]*\s-[a-zA-Z]*i", "sed -i", "shell"),
    (r"\bperl\b[^|;&]*\s-[a-zA-Z]*i", "perl -i", "shell"),
    (r"\bawk\b.*-i\s*inplace", "awk -i inplace", "shell"),
    (r"(?<![0-9&=\-<>])>>?\s*(?![&>=])(?!/dev/(null|stdout|stderr)\b)[\w./~\"'$]", "shell redirect to a file", "shell"),
    (r"\btee\b", "tee", "shell"),
    (r"\b(python[0-9.]*|node|ruby|perl|deno|bun|php)\b.*(open\([^)]*,\s*(mode\s*=\s*)?['\"][^'\"]*[wax+]|"
     r"writeFile|appendFile|createWriteStream|write_text|write_bytes|shutil\.(copy|move)|"
     r"os\.(remove|rename|replace|unlink)|fs\.(rm|unlink|rename|copy))",
     "interpreter writes a file", "full"),
    (r"\bgit\s+(checkout\b(?!\s+-b)|restore\b|stash\b(?!\s+(list|show)\b)|apply\b|reset\b|clean\b|mv\b|rm\b|am\b)",
     "git changes the working tree", "shell"),
    (r"(^|[;&|(]\s*|\b(sudo|xargs)\s+)(rm|mv|cp|truncate|install|touch|ln)\s", "shell file operation", "shell"),
    (r"\bdd\b.*\bof=", "dd of=", "shell"),
    (r"(^|[;&|]\s*)patch\s", "patch", "shell"),
    (r"(^|[;&|]\s*)(ed|ex)\s", "ed/ex", "shell"),
]


def shell_skeleton(cmd):
    """The command with heredoc bodies and quoted strings blanked, leaving only what the shell interprets."""
    out, lines, i = [], cmd.split("\n"), 0
    while i < len(lines):
        line = lines[i]
        out.append(line)
        tags = re.findall(r"<<-?\s*['\"]?(\w+)['\"]?", line)
        i += 1
        for tag in tags:  # skip each heredoc body up to its terminator
            while i < len(lines) and lines[i].strip() != tag:
                i += 1
            i += 1
    s, res, q, i = "\n".join(out), [], None, 0
    while i < len(s):  # blank quoted text with a real quote state machine (nested quotes are common)
        ch = s[i]
        if q is None:
            if ch in "'\"":
                q = ch
            res.append(ch)
        elif ch == q:
            q = None
            res.append(ch)
        elif ch == "\\" and q == '"':
            i += 1
        i += 1
    return "".join(res)


def scan(cmd):
    """Labels of every mutation pattern that matches cmd."""
    skel = shell_skeleton(cmd)
    return [label for rx, label, scope in MUTATION_PATTERNS if re.search(rx, skel if scope == "shell" else cmd, re.S)]


def sh(args, cwd, env=None):
    return subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True).stdout


def sessions_dir(run_dir):
    env = dict(os.environ)
    if os.environ.get("FILETOOLS_HOME"):
        env["HOME"] = os.environ["FILETOOLS_HOME"]
    binp = os.environ.get("FILETOOLS_BIN", os.path.expanduser("~/www/ocode/bin/ocode"))
    out = subprocess.run([binp, "debug", "project-slug", run_dir], env=env, capture_output=True, text=True).stdout
    return json.loads(out)["sessionsDir"]


def session_files(run_dir):
    """Sessions of THIS run only. A re-run at the same path shares ocode's project dir, so sessions older than
    the run's fixture commit belong to an earlier run and are ignored."""
    born = int(sh(["git", "log", "-1", "--format=%ct"], run_dir).strip() or 0)
    return sorted(f for f in glob.glob(os.path.join(sessions_dir(run_dir), "ses_*.sqlite"))
                  if os.path.getmtime(f) >= born)


def load_calls(run_dir):
    """Every tool call from every session of this run, in order, with its turn index and result."""
    calls = []
    for f in session_files(run_dir):
        rows = [json.loads(d) for (d,) in sqlite3.connect(f).execute("select data from messages order by seq")]
        results = {r.get("tool_call_id"): r.get("content") for r in rows if r.get("role") == "tool"}
        turn = 0
        for r in rows:
            content = r.get("content")
            text = content if isinstance(content, str) else json.dumps(content)
            if r.get("role") == "user" and not text.lstrip().startswith("[ocode:"):  # human turns only
                turn += 1
            for tc in r.get("tool_calls") or []:
                fn = tc.get("function", {})
                raw = fn.get("arguments") or "{}"
                try:
                    args = json.loads(raw)
                except json.JSONDecodeError:
                    args = {"_raw": raw}
                res = results.get(tc.get("id"))
                calls.append({"session": os.path.basename(f), "turn": turn, "name": fn.get("name"),
                              "args": args, "result": res if isinstance(res, str) else json.dumps(res)})
    return calls


def changed_paths(run_dir):
    out = sh(["git", "status", "--porcelain", "--untracked-files=all"], run_dir)
    paths = []
    for line in out.splitlines():
        p = line[3:]
        if " -> " in p:
            p = p.split(" -> ")[1]
        p = p.strip('"')
        if not p.startswith(".ocode/"):  # ocode's own session state (todo plan, locks), not the model's edits
            paths.append(p)
    return sorted(paths)


def orig(run_dir, rel):
    return subprocess.run(["git", "show", f"HEAD:{rel}"], cwd=run_dir, capture_output=True, text=True).stdout


def read(run_dir, rel):
    p = os.path.join(run_dir, rel)
    return open(p).read() if os.path.exists(p) else None


PATCH_PATH = re.compile(r"^(?:\*\*\* (?:Update|Add|Delete) File:|\*\*\* Move to:|---|\+\+\+)\s+(?:[ab]/)?(\S+)", re.M)


def arg_strings(v):
    """Every string inside a tool call's arguments (paths can sit at any depth, e.g. multi_file_edit),
    plus the file paths named inside patch text (apply_patch)."""
    if isinstance(v, str):
        return [v] + PATCH_PATH.findall(v)
    if isinstance(v, dict):
        return [s for x in v.values() for s in arg_strings(x)]
    if isinstance(v, list):
        return [s for x in v for s in arg_strings(x)]
    return []


def names_path(s, rel):
    """True when string s names the repo-relative path rel (relative, ./-prefixed, or absolute)."""
    s = s.strip()
    return s == rel or s == "./" + rel or s.endswith("/" + rel) and s.startswith("/")


REDIRECT_TARGET = re.compile(r"(?<![0-9&=\-<>])>>?\s*(?![&>=])([^\s;|&)]+)")
SCRATCH_KINDS = {"shell redirect to a file", "shell file operation", "tee"}


def write_targets(cmd):
    """Paths a shell command writes or deletes: redirect targets, tee args, cp/mv destinations,
    rm/touch/truncate args. Each is paired with the directory of the most recent `cd <abs>` before it."""
    out, cwd = [], None
    for seg in re.split(r"&&|\|\||;|\n", shell_skeleton(cmd)):
        seg = seg.strip()
        m = re.match(r"cd\s+(\S+)$", seg)
        if m:
            cwd = m.group(1).strip("\"'")
            continue
        out += [(cwd, x) for x in REDIRECT_TARGET.findall(seg) if not x.startswith("/dev/")]
        for part in seg.split("|"):
            w = part.split()
            if not w:
                continue
            args = [a for a in w[1:] if not a.startswith("-")]
            if w[0] in ("cp", "mv") and args:
                out.append((cwd, args[-1]))
            elif w[0] in ("rm", "touch", "truncate", "tee"):
                out += [(cwd, a) for a in args]
    return [(c, x.strip("\"'")) for c, x in out]


def outside_repo_only(cmd, run_dir):
    """True when every path the command writes or deletes is outside the run's repo (a scratch file)."""
    roots = (run_dir, os.path.realpath(run_dir))
    targets = write_targets(cmd)

    def outside(cwd, x):
        if not x.startswith("/"):
            if not cwd or not cwd.startswith("/"):
                return False  # relative to the repo
            x = os.path.join(cwd, x)
        return not any(x == r or x.startswith(r + "/") for r in roots)
    return bool(targets) and all(outside(c, x) for c, x in targets)


def audit(run_dir, calls):
    """Returns (violations, warnings). A redirect that only writes scratch files outside the repo is a warning."""
    violations, warnings = [], []
    touched = [s for c in calls if c["name"] in FILE_TOOLS for s in arg_strings(c["args"])]
    for c in calls:
        if c["name"] not in SHELL_TOOLS:
            continue
        cmd = c["args"].get("command") or c["args"].get("_raw") or ""
        hits = scan(cmd)
        if hits and set(hits) <= SCRATCH_KINDS and outside_repo_only(cmd, run_dir):
            warnings.append({"kind": "scratch write outside the repo", "turn": c["turn"], "command": cmd[:400]})
        elif hits:
            violations.append({"kind": hits[0], "turn": c["turn"], "command": cmd[:400]})
    setup = run_dir + ".setup"  # files the harness changed before the model started
    def preset(p):
        sp = os.path.join(setup, p)
        return os.path.exists(sp) and read(run_dir, p) == open(sp).read()
    unattributed = [p for p in changed_paths(run_dir)
                    if not any(names_path(s, p) for s in touched) and not preset(p)]
    for p in unattributed:
        violations.append({"kind": "changed by something other than a file tool", "path": p})
    return violations, warnings


# ---------- outcome checks: return a list of problems (empty = correct) ----------

def expect_only(run_dir, allowed):
    stray = [p for p in changed_paths(run_dir) if p not in allowed]
    return [f"unexpected change: {p}" for p in stray]


def t_ml_rewrite(d):
    s, o = read(d, "src/report.js"), orig(d, "src/report.js")
    probs = []
    body = s[s.find("export function formatReport"):s.find("export function helper01")]
    for rx, why in [(r"rows\s*\.map\(", "uses rows.map"), (r"\.join\(", "joins rows"),
                    (r"""["'`]-["'`]\.repeat\(PAGE_WIDTH\)""", "'-'.repeat(PAGE_WIDTH) separator"),
                    (re.escape("${row.name} x${row.qty} @ ${row.price} ${CURRENCY}"), "row template literal")]:
        if not re.search(rx, body):
            probs.append(f"formatReport missing: {why}")
    if '"=".repeat' in body:
        probs.append("old '=' separator still present")
    for need in ['PAGE_WIDTH = 100;', 'HEADER = "ACME SALES REPORT";', 'FOOTER = "END OF REPORT";']:
        if need not in s:
            probs.append(f"missing: {need}")
    helpers = [l for l in o.splitlines() if l.startswith("export function helper")]
    if any(h not in s for h in helpers):
        probs.append("helper functions changed")
    return probs + expect_only(d, {"src/report.js"})


RENAME_FILES = {"src/api/users.js", "src/api/admin.js", "src/api/profile.js", "src/api/billing.js",
                "src/api/audit.js", "src/api/team.js", "docs/api.md"}


def t_mf_rename(d):
    probs = []
    for rel in RENAME_FILES:
        s = read(d, rel)
        if "fetchUser" in s:
            probs.append(f"fetchUser still in {rel}")
        if s.replace("loadUser", "fetchUser") != orig(d, rel):
            probs.append(f"{rel}: changes beyond the rename")
    return probs + expect_only(d, RENAME_FILES)


VERSION_FILES = {"package.json", "VERSION", "src/version.js", "docs/install.md", "docs/api.md",
                 "deploy/values.yaml", "deploy/Dockerfile", "src/banner.txt", "CHANGELOG.md"}


def t_bulk_version(d):
    probs = []
    for rel in VERSION_FILES - {"CHANGELOG.md"}:
        s = read(d, rel)
        if s != orig(d, rel).replace("1.4.2", "1.5.0"):
            probs.append(f"{rel}: not exactly 1.4.2 -> 1.5.0")
    want = orig(d, "CHANGELOG.md").replace("## Unreleased", "## 1.5.0")
    if read(d, "CHANGELOG.md") != want:
        probs.append("CHANGELOG.md: expected only 'Unreleased' -> '1.5.0' with history intact")
    return probs + expect_only(d, VERSION_FILES)


def t_big_json(d):
    s, o = read(d, "data/settings.json"), orig(d, "data/settings.json")
    want = o.replace('"timeout_ms": 3000', '"timeout_ms": 5000')
    probs = []
    if s != want:
        try:
            j, w = json.loads(s), json.loads(want)
            probs.append("content differs" if j != w else "same data but formatting changed")
        except json.JSONDecodeError as e:
            probs.append(f"invalid JSON: {e}")
    return probs + expect_only(d, {"data/settings.json"})


def t_create(d):
    probs = []
    checks = {"src/notify/index.js": ["sendEmail", "sendSms"],
              "src/notify/email.js": ["export async function sendEmail(to, subject, body)", "email"],
              "src/notify/sms.js": ["export async function sendSms(to, text)", "sms"],
              "test/notify.test.js": ["node:assert", "sendEmail", "sendSms"]}
    for rel, needs in checks.items():
        s = read(d, rel)
        if s is None:
            probs.append(f"missing {rel}")
            continue
        probs += [f"{rel}: missing {n!r}" for n in needs if n not in s]
    r = read(d, "README.md")
    if "Notifications" not in r or not r.startswith(orig(d, "README.md").rstrip("\n")):
        probs.append("README.md: Notifications section missing or existing text changed")
    # wiring the new test into the repo's runner (test/run.js, package.json) is reasonable, not a stray edit
    return probs + expect_only(d, set(checks) | {"README.md", "test/run.js", "package.json"})


REGIONS = [("us-east", "US East", "America/New_York"), ("us-west", "US West", "America/Los_Angeles"),
           ("ca-central", "Canada Central", "America/Toronto"), ("br-south", "Brazil South", "America/Sao_Paulo"),
           ("eu-west", "EU West", "Europe/Dublin"), ("eu-central", "EU Central", "Europe/Frankfurt"),
           ("uk-south", "UK South", "Europe/London"), ("me-central", "Middle East", "Asia/Dubai"),
           ("ap-south", "Asia Pacific South", "Asia/Kolkata"), ("ap-southeast", "Asia Pacific Southeast", "Asia/Singapore"),
           ("ap-northeast", "Asia Pacific Northeast", "Asia/Tokyo"), ("au-east", "Australia East", "Australia/Sydney")]


def t_generate(d):
    probs = []
    try:
        j = json.loads(read(d, "data/regions.json") or "null")
        if [(r.get("code"), r.get("name"), r.get("timezone")) for r in j] != REGIONS:
            probs.append("regions.json: rows differ from the spec")
    except (json.JSONDecodeError, TypeError, AttributeError) as e:
        probs.append(f"regions.json unreadable: {e}")
    c = read(d, "data/regions.csv")
    want = "code,name,timezone\n" + "".join(f"{a},{b},{z}\n" for a, b, z in REGIONS)
    if c is None or c.strip().splitlines() != want.strip().splitlines():
        probs.append("regions.csv: content differs from the spec")
    return probs + expect_only(d, {"data/regions.json", "data/regions.csv"})


def t_sort(d):
    o = orig(d, "src/constants.js")
    items = sorted(set(re.findall(r'"([A-Z]+)"', o)))
    want = "export const FRUITS = [\n" + "".join(f'  "{c}",\n' for c in items) + "];\n"
    probs = [] if read(d, "src/constants.js") == want else ["constants.js: not sorted+deduped in the original format"]
    return probs + expect_only(d, {"src/constants.js"})


def t_indent(d):
    o = orig(d, "src/legacy-style.js")
    want = "".join(re.sub(r"^((?:    )+)", lambda m: "  " * (len(m.group(1)) // 4), l) for l in o.splitlines(True))
    probs = [] if read(d, "src/legacy-style.js") == want else ["legacy-style.js: not an exact 4->2 space re-indent"]
    return probs + expect_only(d, {"src/legacy-style.js"})


def t_dup_anchor(d):
    s = read(d, "src/handlers.js")
    probs = []
    orders = s[:s.find("export function getInvoices")]
    inv = s[s.find("export function getInvoices"):]
    if "res.status(401)" not in orders or '"unauthorized"' not in orders:
        probs.append("getOrders changed")
    if "res.status(403)" not in inv or '"forbidden"' not in inv or "401" in inv:
        probs.append("getInvoices does not return 403 forbidden")
    return probs + expect_only(d, {"src/handlers.js"})


MOVE_FILES = {"src/utils.js", "src/date.js", "src/orders.js", "src/invoices.js", "src/events.js"}


def t_move(d):
    probs = []
    u, dt = read(d, "src/utils.js"), read(d, "src/date.js")
    if "parseDate" in u:
        probs.append("parseDate still in utils.js")
    if not dt or "export function parseDate(s)" not in dt or "bad date" not in dt:
        probs.append("src/date.js missing the exported parseDate")
    if "export function clamp" not in u or "export function slug" not in u:
        probs.append("utils.js lost other functions")
    for rel in ["src/orders.js", "src/invoices.js", "src/events.js"]:
        s = read(d, rel)
        if not re.search(r'import\s*\{\s*parseDate\s*\}\s*from\s*"\./date\.js"', s):
            probs.append(f"{rel}: parseDate not imported from ./date.js")
        if not re.search(r'import\s*\{\s*clamp\s*\}\s*from\s*"\./utils\.js"', s):
            probs.append(f"{rel}: clamp not imported from ./utils.js")
    return probs + expect_only(d, MOVE_FILES)


def t_restore_own(d):
    return expect_only(d, set())  # everything back to the fixture


def t_restore_partial(d):
    probs = []
    if read(d, "docs/api.md") != orig(d, "docs/api.md"):
        probs.append("docs/api.md not restored")
    for rel in RENAME_FILES - {"docs/api.md"}:
        s = read(d, rel)
        if "fetchUser" in s or "loadUser" not in s:
            probs.append(f"{rel}: rename not kept")
    return probs + expect_only(d, RENAME_FILES - {"docs/api.md"})


def t_restore_deleted(d):
    return expect_only(d, set())


def fn_block(s, name):
    """Text of `export function name(...) {...}` up to its closing brace at column 0."""
    a = s.find(f"export function {name}(")
    return s[a:s.index("\n}\n", a) + 3] if a >= 0 else ""


def node_eval(run_dir, js):
    r = subprocess.run(["node", "--input-type=module", "-e", js], cwd=run_dir, capture_output=True, text=True)
    return r.returncode, (r.stdout + r.stderr).strip()


def t_big_edit(d):
    s, o = read(d, "src/engine.js"), orig(d, "src/engine.js").replace("MAX_DISCOUNT", "DISCOUNT_CAP")
    probs = []
    if "MAX_DISCOUNT" in s:
        probs.append("MAX_DISCOUNT still present")
    if s.replace(fn_block(s, "computeDiscount"), "") != o.replace(fn_block(o, "computeDiscount"), ""):
        probs.append("engine.js changed outside computeDiscount and the rename")
    body = fn_block(s, "computeDiscount")
    if "else if" not in body or "Math.min(pct, DISCOUNT_CAP)" not in body:
        probs.append("computeDiscount not written with else-if and Math.min(pct, DISCOUNT_CAP)")
    js = ("import * as e from './src/engine.js';"
          "const c=[[{total:500},0],[{total:1500},150],[{total:1500,loyal:true},225],[{total:6000},1200],"
          "[{total:6000,loyal:true},1500]];"
          "for (const [o,w] of c) if (e.computeDiscount(o)!==w) {console.log('bad',JSON.stringify(o),e.computeDiscount(o));process.exit(1)}"
          "if (e.DISCOUNT_CAP!==0.3) process.exit(2)")
    rc, out = node_eval(d, js)
    if rc:
        probs.append(f"computeDiscount behaviour wrong: {out[:120]}")
    return probs + expect_only(d, {"src/engine.js"})


def t_big_bulk(d):
    want = orig(d, "src/engine.js").replace("  return x * 2;", "  return x * 3;")
    probs = [] if read(d, "src/engine.js") == want else ["engine.js is not exactly every `return x * 2;` -> `x * 3`"]
    return probs + expect_only(d, {"src/engine.js"})


def t_append_test(d):
    s, o = read(d, "test/engine.test.js"), orig(d, "test/engine.test.js")
    head, tail = o.rsplit("export { cases };", 1)
    probs = []
    if not s.startswith(head) or not s.rstrip().endswith("export { cases };"):
        probs.append("existing tests changed or `export { cases };` not last")
    else:
        added = s[len(head):s.rindex("export { cases };")]
        if "computeTax(100)" not in added or "computeDiscount(" not in added or added.count("test(") < 2:
            probs.append("the two new tests are not both there")
    rc, out = node_eval(d, "await import('./test/run.js')")
    if rc or "244 passed, 0 failed" not in out:
        probs.append(f"test run: {out.splitlines()[-1] if out else rc}")
    return probs + expect_only(d, {"test/engine.test.js"})


def answer_text(d):
    return open(d + ".out").read() if os.path.exists(d + ".out") else ""


def t_probe_cleanup(d):
    a = answer_text(d)
    probs = [] if re.search(r"2024-03-01|March\s*1|Mar\s*1|(does not|doesn't|no(t)?) throw|rolls? over|overflow", a, re.I) \
        else ["answer does not say it returns March 1 without throwing"]
    return probs + expect_only(d, set())


def t_try_and_restore(d):
    probs = [] if re.search(r"round keeps two decimals", answer_text(d)) else ["failing test not named"]
    return probs + expect_only(d, set())


def t_restore_new_session(d):
    return expect_only(d, set())


TASKS = {
    "ml-rewrite": t_ml_rewrite, "mf-rename": t_mf_rename, "bulk-version": t_bulk_version,
    "big-json": t_big_json, "create-files": t_create, "generate-data": t_generate, "sort-dedupe": t_sort,
    "reindent": t_indent, "dup-anchor": t_dup_anchor, "move-function": t_move,
    "restore-own": t_restore_own, "restore-partial": t_restore_partial, "restore-deleted": t_restore_deleted,
    "big-edit": t_big_edit, "big-bulk": t_big_bulk, "append-test": t_append_test, "probe-cleanup": t_probe_cleanup,
    "try-and-restore": t_try_and_restore, "restore-new-session": t_restore_new_session,
}
MUST_CHANGE_FIRST = {"try-and-restore", "probe-cleanup"}  # the restore/cleanup is only tested if a change happened
TWO_TURN = {"restore-own", "restore-partial", "restore-deleted"}


def main():
    task, run_dir = sys.argv[1], os.path.abspath(sys.argv[2])
    if not session_files(run_dir):
        # ocode saves no session when the run dies (provider error, crash): nothing to grade, re-run it
        print(json.dumps({"task": task, "invalid": True, "reason": "no saved session (run failed)"}))
        print("INVALID no saved session; re-run this rep")
        return
    calls = load_calls(run_dir)
    violations, warnings = audit(run_dir, calls)
    problems = TASKS[task](run_dir)
    if task in TWO_TURN and not any(c["name"] in FILE_TOOLS | SHELL_TOOLS and c["turn"] == 1 for c in calls):
        problems.append("turn 1 made no change, so the restore was never exercised")
    if task in MUST_CHANGE_FIRST and not any(c["name"] in FILE_TOOLS - {"undo_file_change"} for c in calls) \
            and not violations:
        problems.append("no file was ever written, so the restore/cleanup was never exercised")
    clean, ok = not violations, not problems
    score = 1.0 if clean and ok else 0.5 if clean else 0.0
    tools = [c["name"] for c in calls]
    report = {"task": task, "score": score, "clean": clean, "outcome_ok": ok, "violations": violations,
              "warnings": warnings, "problems": problems, "tools": tools,
              "file_tool_errors": [c["result"][:200] for c in calls
                                   if c["name"] in FILE_TOOLS and c["result"] and "error" in c["result"].lower()[:80]]}
    print(json.dumps(report, indent=1))
    verdict = "PASS" if score == 1 else "WRONG-RESULT" if clean else "SCRIPTED-WRITE"
    print(f"{verdict} score={score} violations={len(violations)} problems={len(problems)}")


if __name__ == "__main__":
    main()
