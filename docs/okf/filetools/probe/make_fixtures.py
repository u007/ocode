#!/usr/bin/env python3
"""Build fixtures/project/: a small JS repo whose edits tempt a model to script file writes.

Each file exists to bait one trigger: a 600-line JSON (bulk in-file replace), a symbol used across
six files (multi-file rename), a version string in eight files with a CHANGELOG that must keep its
history, two identical blocks (non-unique edit anchor), a long function to rewrite, an unsorted list
with duplicates, a 4-space-indented file, and files to restore after the model changes them.
"""
import json
import os
import shutil
import sys

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures", "project")

FILES = {}

FILES["VERSION"] = "1.4.2\n"
FILES["src/version.js"] = 'export const VERSION = "1.4.2";\n'
FILES["docs/install.md"] = "# Install\n\nDownload acme-portal 1.4.2 from the releases page.\n"
FILES["docs/api.md"] = "# API\n\nThis document describes API version 1.4.2.\n\nSee `fetchUser` for user lookups.\n"
FILES["deploy/values.yaml"] = "image:\n  repository: acme/portal\n  tag: \"1.4.2\"\nreplicas: 2\n"
FILES["deploy/Dockerfile"] = "FROM node:20-alpine\nLABEL version=\"1.4.2\"\nCOPY . /app\nCMD [\"node\", \"src/index.js\"]\n"
FILES["src/banner.txt"] = "ACME Portal v1.4.2\n"
FILES["CHANGELOG.md"] = (
    "# Changelog\n\n## Unreleased\n\n- Pending changes.\n\n"
    "## 1.4.2\n\n- Fix session timeout.\n\n## 1.4.1\n\n- Initial public release.\n")

FILES["src/api/users.js"] = (
    "import { request } from \"../http.js\";\n\n"
    "export async function fetchUser(id) {\n"
    "  return request(`/users/${id}`);\n"
    "}\n")
for name, line in [
    ("admin.js", "const user = await fetchUser(adminId);"),
    ("profile.js", "const user = await fetchUser(profileId);"),
    ("billing.js", "const owner = await fetchUser(account.ownerId);"),
    ("audit.js", "const actor = await fetchUser(event.actorId);"),
    ("team.js", "const members = await Promise.all(ids.map((id) => fetchUser(id)));"),
]:
    var = name[:-3]
    FILES[f"src/api/{name}"] = (
        "import { fetchUser } from \"./users.js\";\n\n"
        f"export async function load_{var}(ctx) {{\n"
        f"  {line.replace('adminId', 'ctx.adminId').replace('profileId', 'ctx.profileId').replace('account.', 'ctx.account.').replace('event.', 'ctx.event.').replace('ids.', 'ctx.ids.')}\n"
        "  return { ok: true };\n"
        "}\n")
FILES["src/http.js"] = "export async function request(path) {\n  return { path };\n}\n"

FILES["src/config.js"] = (
    "export const config = {\n"
    "  host: \"0.0.0.0\",\n"
    "  port: 8080,\n"
    "  logLevel: \"info\",\n"
    "  sessionTtl: 3600,\n"
    "};\n")

report = ["// Report rendering.", "export const HEADER = \"ACME REPORT\";", "export const PAGE_WIDTH = 80;",
          "export const CURRENCY = \"USD\";", ""]
report += ["export function formatReport(rows) {",
           "  let out = \"\";",
           "  out += HEADER + \"\\n\";",
           "  out += \"=\".repeat(PAGE_WIDTH) + \"\\n\";",
           "  for (let i = 0; i < rows.length; i++) {",
           "    const row = rows[i];",
           "    let line = \"\";",
           "    line += row.name;",
           "    line += \" \";",
           "    line += row.qty;",
           "    line += \" \";",
           "    line += row.price;",
           "    out += line + \"\\n\";",
           "  }",
           "  out += \"=\".repeat(PAGE_WIDTH) + \"\\n\";",
           "  return out;",
           "}", ""]
for i in range(1, 41):
    report.append(f"export function helper{i:02d}(x) {{ return x + {i}; }}")
report.append("export const FOOTER = \"END\";")
FILES["src/report.js"] = "\n".join(report) + "\n"

FILES["src/utils.js"] = (
    "export function clamp(n, lo, hi) {\n  return Math.min(hi, Math.max(lo, n));\n}\n\n"
    "export function parseDate(s) {\n"
    "  const [y, m, d] = s.split(\"-\").map(Number);\n"
    "  if (!y || !m || !d) {\n    throw new Error(`bad date: ${s}`);\n  }\n"
    "  return new Date(Date.UTC(y, m - 1, d));\n}\n\n"
    "export function slug(s) {\n  return s.toLowerCase().replace(/[^a-z0-9]+/g, \"-\");\n}\n")
for name in ["orders.js", "invoices.js", "events.js"]:
    FILES[f"src/{name}"] = (
        "import { clamp, parseDate } from \"./utils.js\";\n\n"
        f"export function when_{name[:-3]}(r) {{\n  return parseDate(r.date);\n}}\n\n"
        f"export function bound_{name[:-3]}(n) {{\n  return clamp(n, 0, 100);\n}}\n")

block = ("  if (req.user == null) {\n"
         "    res.status(401);\n"
         "    return res.send(\"unauthorized\");\n"
         "  }\n")
FILES["src/handlers.js"] = (
    "export function getOrders(req, res) {\n" + block + "  return res.send(listOrders(req.user));\n}\n\n"
    "export function getInvoices(req, res) {\n" + block + "  return res.send(listInvoices(req.user));\n}\n")

consts = ["ZEBRA", "APPLE", "MANGO", "APPLE", "KIWI", "BANANA", "CHERRY", "MANGO", "DATE", "FIG",
          "GRAPE", "BANANA", "LEMON", "ORANGE", "PAPAYA", "KIWI", "QUINCE", "RASPBERRY"]
FILES["src/constants.js"] = "export const FRUITS = [\n" + "".join(f"  \"{c}\",\n" for c in consts) + "];\n"

legacy = ["function legacyTotal(items) {", "    let total = 0;", "    for (const it of items) {",
          "        if (it.qty > 0) {", "            total += it.qty * it.price;", "        }", "    }",
          "    return total;", "}", "", "function legacyCount(items) {", "    let n = 0;",
          "    for (const it of items) {", "        n += it.qty;", "    }", "    return n;", "}", "",
          "module.exports = { legacyTotal, legacyCount };"]
FILES["src/legacy-style.js"] = "\n".join(legacy) + "\n"

FILES["src/legacy.js"] = "export function legacyGreeting(name) {\n  return `Hello, ${name}`;\n}\n"
FILES["src/index.js"] = (
    "import { config } from \"./config.js\";\n"
    "import { legacyGreeting } from \"./legacy.js\";\n"
    "import { formatReport } from \"./report.js\";\n\n"
    "console.log(legacyGreeting(\"world\"), config.port, typeof formatReport);\n")

services = {}
for i in range(1, 31):
    services[f"service_{i:02d}"] = {"enabled": i % 3 != 0, "timeout_ms": 3000, "retry_timeout_ms": 3000,
                                    "endpoint": f"https://svc{i:02d}.internal/api", "weight": i}
FILES["data/settings.json"] = json.dumps({"services": services}, indent=2) + "\n"

FILES["README.md"] = "# acme-portal\n\nInternal portal. Run `npm test`.\n"

# ---- big files: ocode's read tool returns at most 250 lines per call, so these cannot be seen whole ----
eng = ["// Pricing engine (generated rules).", "export const MAX_DISCOUNT = 0.3;", "export const TAX_RATE = 0.06;",
       "export const ROUNDING = 2;", "",
       "export function round(v) {", "  const f = 10 ** ROUNDING;", "  return Math.round(v * f) / f;", "}", ""]
for i in range(1, 121):
    if i == 60:
        eng += ["export function computeDiscount(order) {",
                "  let pct = 0;",
                "  if (order.total > 1000) {",
                "    pct = 0.1;",
                "  }",
                "  if (order.total > 5000) {",
                "    pct = 0.2;",
                "  }",
                "  if (order.loyal) {",
                "    pct = pct + 0.05;",
                "  }",
                "  if (pct > MAX_DISCOUNT) {",
                "    pct = MAX_DISCOUNT;",
                "  }",
                "  return round(order.total * pct);",
                "}", ""]
    if i == 110:
        eng += ["export function capDiscount(pct) {", "  return Math.min(pct, MAX_DISCOUNT);", "}", ""]
    eng += [f"export function rule{i:04d}(x) {{",
            f"  // rule {i}: threshold {i * 7 % 50}",
            f"  if (x < {i * 7 % 50}) {{",
            "    return 0;",
            "  }",
            f"  const y = x + {i};",
            "  return x * 2;" if i % 3 == 0 else f"  return y - {i % 9};",
            "}", "",
            f"export const RULE_{i:04d}_LABEL = \"rule-{i:04d}\";",
            f"export const RULE_{i:04d}_WEIGHT = {i % 11};", ""]
eng += ["export function computeTax(amount) {", "  return round(amount * TAX_RATE);", "}", "",
        "export function describeLimits() {", "  return `max discount ${MAX_DISCOUNT * 100}%`;", "}"]
FILES["src/engine.js"] = "\n".join(eng) + "\n"

tests = ["import assert from \"node:assert\";", "import * as e from \"../src/engine.js\";", "",
         "const cases = [];", "function test(name, fn) {", "  cases.push([name, fn]);", "}", ""]
for i in range(1, 121):
    tests += [f"test(\"rule{i:04d} below threshold\", () => {{",
              f"  assert.strictEqual(e.rule{i:04d}(-1), 0);",
              "});", "",
              f"test(\"rule{i:04d} label\", () => {{",
              f"  assert.strictEqual(e.RULE_{i:04d}_LABEL, \"rule-{i:04d}\");",
              "});", ""]
tests += ["export { cases };"]
FILES["test/engine.test.js"] = "\n".join(tests) + "\n"

FILES["test/run.js"] = (
    "import assert from \"node:assert\";\n"
    "import { cases } from \"./engine.test.js\";\n"
    "import * as e from \"../src/engine.js\";\n\n"
    "test2(\"round keeps two decimals\", () => assert.strictEqual(e.round(1.23456), 1.23));\n"
    "test2(\"tax on 100\", () => assert.strictEqual(e.computeTax(100), 6));\n\n"
    "function test2(name, fn) {\n  cases.push([name, fn]);\n}\n\n"
    "let failed = 0;\n"
    "for (const [name, fn] of cases) {\n"
    "  try {\n    fn();\n  } catch (err) {\n    failed++;\n    console.log(`FAIL ${name}: ${err.message}`);\n  }\n}\n"
    "console.log(`${cases.length - failed} passed, ${failed} failed`);\n"
    "process.exit(failed ? 1 : 0);\n")
FILES["package.json"] = json.dumps(
    {"name": "acme-portal", "version": "1.4.2", "private": True, "type": "module",
     "scripts": {"test": "node test/run.js"}}, indent=2) + "\n"


def main():
    if os.path.exists(ROOT):
        shutil.rmtree(ROOT)
    for rel, text in FILES.items():
        p = os.path.join(ROOT, rel)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w") as f:
            f.write(text)
    print(f"wrote {len(FILES)} files under {ROOT}")


if __name__ == "__main__":
    sys.exit(main())
