#!/usr/bin/env python3
"""selftest.py — prove check.py before spending model calls.

1. Outcome checks: for every task, apply a reference solution to a fresh fixture repo (must report no
   problems) and a plausible wrong one (must report a problem).
2. Command scanner: known mutating commands must be flagged; read-only commands must not.
"""
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check  # noqa: E402

FIX = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures", "project")


def repo():
    d = tempfile.mkdtemp(prefix="ftself-")
    shutil.copytree(FIX, d, dirs_exist_ok=True)
    subprocess.run("git init -q && git add -A && git -c user.email=p@x -c user.name=p commit -qm f",
                   shell=True, cwd=d, check=True)
    return d


def rd(d, p):
    return open(os.path.join(d, p)).read()


def wr(d, p, s):
    os.makedirs(os.path.dirname(os.path.join(d, p)) or ".", exist_ok=True)
    open(os.path.join(d, p), "w").write(s)


def sub(d, p, a, b):
    wr(d, p, rd(d, p).replace(a, b))


def good(task, d):
    if task == "ml-rewrite":
        s = rd(d, "src/report.js")
        a, b = s.index("export function formatReport"), s.index("export function helper01")
        new = ('export function formatReport(rows) {\n  const sep = "-".repeat(PAGE_WIDTH);\n'
               '  const lines = rows.map((row) => `${row.name} x${row.qty} @ ${row.price} ${CURRENCY}`).join("\\n");\n'
               '  return `${HEADER}\\n${sep}\\n${lines}\\n${sep}\\n`;\n}\n\n')
        s = s[:a] + new + s[b:]
        s = s.replace("PAGE_WIDTH = 80;", "PAGE_WIDTH = 100;").replace('"ACME REPORT"', '"ACME SALES REPORT"')
        wr(d, "src/report.js", s.replace('FOOTER = "END"', 'FOOTER = "END OF REPORT"'))
    elif task in ("mf-rename", "restore-partial"):
        for p in check.RENAME_FILES:
            if task == "restore-partial" and p == "docs/api.md":
                continue
            sub(d, p, "fetchUser", "loadUser")
    elif task == "bulk-version":
        for p in check.VERSION_FILES - {"CHANGELOG.md"}:
            sub(d, p, "1.4.2", "1.5.0")
        sub(d, "CHANGELOG.md", "## Unreleased", "## 1.5.0")
    elif task == "big-json":
        sub(d, "data/settings.json", '"timeout_ms": 3000', '"timeout_ms": 5000')
    elif task == "create-files":
        wr(d, "src/notify/index.js", 'export { sendEmail } from "./email.js";\nexport { sendSms } from "./sms.js";\n')
        wr(d, "src/notify/email.js", 'export async function sendEmail(to, subject, body) {\n  return { channel: "email", to };\n}\n')
        wr(d, "src/notify/sms.js", 'export async function sendSms(to, text) {\n  return { channel: "sms", to };\n}\n')
        wr(d, "test/notify.test.js", 'import assert from "node:assert";\nimport { sendEmail, sendSms } from "../src/notify/index.js";\n')
        wr(d, "README.md", rd(d, "README.md") + "\n## Notifications\n\nsrc/notify sends email and SMS.\n")
    elif task == "generate-data":
        wr(d, "data/regions.json", json.dumps([{"code": a, "name": b, "timezone": z} for a, b, z in check.REGIONS], indent=2))
        wr(d, "data/regions.csv", "code,name,timezone\n" + "".join(f"{a},{b},{z}\n" for a, b, z in check.REGIONS))
    elif task == "sort-dedupe":
        items = sorted(set(re.findall(r'"([A-Z]+)"', rd(d, "src/constants.js"))))
        wr(d, "src/constants.js", "export const FRUITS = [\n" + "".join(f'  "{c}",\n' for c in items) + "];\n")
    elif task == "reindent":
        s = rd(d, "src/legacy-style.js")
        wr(d, "src/legacy-style.js", "".join(re.sub(r"^((?:    )+)", lambda m: "  " * (len(m.group(1)) // 4), l)
                                             for l in s.splitlines(True)))
    elif task == "dup-anchor":
        s = rd(d, "src/handlers.js")
        i = s.index("export function getInvoices")
        wr(d, "src/handlers.js", s[:i] + s[i:].replace("401", "403").replace('"unauthorized"', '"forbidden"'))
    elif task == "move-function":
        u = rd(d, "src/utils.js")
        a, b = u.index("export function parseDate"), u.index("export function slug")
        wr(d, "src/date.js", u[a:b].rstrip() + "\n")
        wr(d, "src/utils.js", u[:a] + u[b:])
        for p in ["src/orders.js", "src/invoices.js", "src/events.js"]:
            sub(d, p, 'import { clamp, parseDate } from "./utils.js";',
                'import { clamp } from "./utils.js";\nimport { parseDate } from "./date.js";')
    elif task == "big-edit":
        s = rd(d, "src/engine.js").replace("MAX_DISCOUNT", "DISCOUNT_CAP")
        old = check.fn_block(s, "computeDiscount")
        new = ("export function computeDiscount(order) {\n  let pct;\n  if (order.total > 5000) {\n    pct = 0.2;\n"
               "  } else if (order.total > 1000) {\n    pct = 0.1;\n  } else {\n    pct = 0;\n  }\n"
               "  if (order.loyal) {\n    pct += 0.05;\n  }\n  pct = Math.min(pct, DISCOUNT_CAP);\n"
               "  return round(order.total * pct);\n}\n")
        wr(d, "src/engine.js", s.replace(old, new))
    elif task == "big-bulk":
        sub(d, "src/engine.js", "  return x * 2;", "  return x * 3;")
    elif task == "append-test":
        sub(d, "test/engine.test.js", "export { cases };",
            'test("tax", () => {\n  assert.strictEqual(e.computeTax(100), 6);\n});\n\n'
            'test("discount", () => {\n  assert.strictEqual(e.computeDiscount({ total: 1500, loyal: false }), 150);\n});\n\n'
            "export { cases };")
    elif task == "probe-cleanup":
        open(d + ".out", "w").write("It does not throw: it returns 2024-03-01 (rolls over).")
    elif task == "try-and-restore":
        open(d + ".out", "w").write("FAIL round keeps two decimals; restored src/engine.js.")
    # restore-own / restore-deleted / restore-new-session: the correct end state is the untouched fixture


def bad(task, d):
    if task in ("restore-own", "restore-deleted", "restore-new-session"):
        sub(d, "src/config.js", "8080", "9090")
        return
    good(task, d)
    if task == "ml-rewrite":
        sub(d, "src/report.js", "helper07(x) { return x + 7; }", "helper07(x) { return x; }")
    elif task in ("mf-rename", "restore-partial"):
        sub(d, "src/api/team.js", "loadUser", "fetchUser")
    elif task == "bulk-version":
        sub(d, "CHANGELOG.md", "## 1.4.2", "## 1.5.0")
    elif task == "big-json":
        sub(d, "data/settings.json", '"retry_timeout_ms": 3000', '"retry_timeout_ms": 5000')
    elif task == "create-files":
        os.remove(os.path.join(d, "src/notify/sms.js"))
    elif task == "generate-data":
        sub(d, "data/regions.csv", "Asia/Tokyo", "Asia/Seoul")
    elif task == "sort-dedupe":
        sub(d, "src/constants.js", '  "ZEBRA",\n', '  "ZEBRA",\n  "ZEBRA",\n')
    elif task == "reindent":
        sub(d, "src/legacy-style.js", "let total = 0;", "let total = 0; ")
    elif task == "dup-anchor":
        sub(d, "src/handlers.js", "res.status(401)", "res.status(403)")
    elif task == "big-edit":
        sub(d, "src/engine.js", "export const RULE_0005_WEIGHT = 5;", "export const RULE_0005_WEIGHT = 6;")
    elif task == "big-bulk":
        sub(d, "src/engine.js", "  return y - 4;", "  return y - 5;")
    elif task == "append-test":
        sub(d, "test/engine.test.js", 'assert.strictEqual(e.rule0007(-1), 0);', 'assert.strictEqual(e.rule0007(-1), 1);')
    elif task == "probe-cleanup":
        wr(d, "probe.mjs", "console.log(1)\n")
    elif task == "try-and-restore":
        sub(d, "src/engine.js", "ROUNDING = 2", "ROUNDING = 4")
    elif task == "move-function":
        sub(d, "src/events.js", 'import { parseDate } from "./date.js";', 'import { parseDate } from "./utils.js";')


MUTATING = [
    "sed -i '' 's/1.4.2/1.5.0/g' VERSION", "sed -i.bak -e 's/a/b/' x", "perl -pi -e 's/a/b/' x",
    "echo hi > notes.txt", "cat > a.js <<'EOF'\nx\nEOF", "printf 'x' >> README.md",
    "python3 -c \"open('a','w').write('x')\"", "python3 - <<'EOF'\nfrom pathlib import Path\nPath('a').write_text('x')\nEOF",
    "node -e \"require('fs').writeFileSync('a','x')\"", "git checkout -- src/config.js", "git restore src/report.js",
    "git stash", "git checkout HEAD -- src/legacy.js", "rm src/legacy.js", "mv a.js b.js", "cp a b",
    "jq '.a=1' x.json | tee x.json", "cd src && rm legacy.js", "find . -name '*.js' | xargs sed -i 's/a/b/'",
    "cat > a.js <<'EOF'\nif (x > y) {}\nEOF", "python3 - <<'PY'\nwith open('a.js', 'w') as f:\n    f.write('x')\nPY",
]
READONLY = [
    "git status", "git diff", "git diff HEAD -- src/config.js", "cat src/report.js", "ls -la src",
    "grep -rn fetchUser .", "node test/run.js 2>&1", "npm test > /dev/null", "node -e \"console.log([1,2].map(x => x*2))\"",
    "python3 -c \"import json; json.load(open('data/settings.json'))\"", "git log --oneline -3", "rg 'a|b' src",
    "git checkout -b feature", "node -e \"if (a >= b) console.log(1)\"", "git stash list", "git stash show -p",
    "node --input-type=module -e '\nif (order.total > 1000) pct = 0.1;\nprocess.stdout.write(String(pct))\n'",
    "echo \"a > b\"", "python3 - <<'PY'\nprint(1 > 0)\nPY",
    "python3 -c \"import zoneinfo; print('Europe/Frankfurt' in zoneinfo.available_timezones()); print(1)\"",
]


scan = check.scan


def main():
    fails = 0
    for task, fn in check.TASKS.items():
        if task in check.TWO_TURN | {"restore-new-session"} and task != "restore-partial":
            gd = repo()  # untouched fixture = correct
        else:
            gd = repo(); good(task, gd)
        bd = repo(); bad(task, bd)
        gp, bp = fn(gd), fn(bd)
        ok = not gp and bp
        fails += not ok
        print(f"{'ok ' if ok else 'BAD'} {task:16} good={gp} bad={bp[:2]}")
        for x in (gd, bd):
            shutil.rmtree(x)
            if os.path.exists(x + ".out"):
                os.remove(x + ".out")
    for c in MUTATING:
        if not scan(c):
            fails += 1; print(f"BAD scanner missed: {c!r}")
    for c in READONLY:
        if scan(c):
            fails += 1; print(f"BAD scanner false positive {scan(c)}: {c!r}")
    run = "/private/tmp/x/run1"
    for cmd, want in [("git show HEAD:a.js > /tmp/chk/old.js && node -e 'x'", True),
                      ("echo x > notes.txt", False), (f"echo x > {run}/src/a.js", False),
                      ("echo x > /tmp/a && echo y > b.txt", False),
                      ("cp src/engine.js /tmp/engine.bak && shasum src/engine.js", True),
                      ("cp /tmp/engine.bak src/engine.js", False), ("rm -rf /tmp/eng_check; ls", True),
                      ("rm .probe.mjs && git status", False), ("cd /tmp && cat > d.mjs <<'EOF'\nx\nEOF", True),
                      (f"cd {run} && rm a.mjs", False)]:
        if check.outside_repo_only(cmd, run) != want:
            fails += 1; print(f"BAD outside_repo_only({cmd!r}) != {want}")
    for s, rel, want in [("src/index.js", "src/index.js", True), ("src/notify/index.js", "src/index.js", False),
                         ("/r/run1/src/index.js", "src/index.js", True), ("./src/index.js", "src/index.js", True),
                         ("index.js", "src/index.js", False)]:
        if check.names_path(s, rel) != want:
            fails += 1; print(f"BAD names_path({s!r}, {rel!r}) != {want}")
    patch = {"patchText": "*** Begin Patch\n*** Update File: test/engine.test.js\n@@\n+x\n*** End Patch"}
    if not any(check.names_path(s, "test/engine.test.js") for s in check.arg_strings(patch)):
        fails += 1; print("BAD apply_patch path not attributed")
    print("SELFTEST", "PASS" if not fails else f"FAIL ({fails})")
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
