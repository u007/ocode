#!/usr/bin/env node
// usage: node scripts/gen-manifest.mjs [--base https://localhost:3443] [--key <pane key>]
// Writes manifest/manifest.xml (gitignored: it embeds the pane key). The key must equal the bridge's
// EXCEL_BRIDGE_PANE_KEY; generate one once and keep it in your shell profile or a secrets manager.
import { readFileSync, writeFileSync } from "node:fs";
import { randomBytes, randomUUID } from "node:crypto";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const arg = (n, d) => { const i = process.argv.indexOf(`--${n}`); return i > 0 ? process.argv[i + 1] : d; };
const base = arg("base", process.env.EXCEL_BRIDGE_BASE ?? "https://localhost:3443").replace(/\/$/, "");
const key = arg("key", process.env.EXCEL_BRIDGE_PANE_KEY ?? "");
if (!/^https:\/\//.test(base)) { console.error("The task pane URL must be https:// (Office requires it)"); process.exit(1); }
if (!key) { console.error(`No pane key. Generate one and use it for both the manifest and the bridge:\n  export EXCEL_BRIDGE_PANE_KEY=${randomBytes(24).toString("hex")}`); process.exit(1); }
if (!/^[A-Za-z0-9_-]{16,}$/.test(key)) { console.error("pane key must be 16+ chars of [A-Za-z0-9_-] (it goes in a URL)"); process.exit(1); }

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const template = readFileSync(join(root, "manifest/manifest.template.xml"), "utf8");
// Excel rejects a manifest whose <Version> major is 0 ("version too low") and the sideload gives no clue
// which field is wrong, so fail here where the fix is obvious.
const version = /<Version>(\d+)\.(\d+)\.(\d+)\.(\d+)<\/Version>/.exec(template);
if (!version || Number(version[1]) < 1) {
  console.error("<Version> in manifest/manifest.template.xml must be >= 1.0.0.0 (Excel rejects 0.x manifests)");
  process.exit(1);
}
// Values are substituted into XML attributes, so escape them. `key` is already
// charset-validated, but `--base` and `--guid` are caller-supplied: an `&` in a
// base URL yields an invalid manifest, and Excel's sideload gives no clue which
// field is wrong.
const xml = (s) => String(s)
  .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
  .replace(/"/g, "&quot;").replace(/'/g, "&apos;");
const guid = arg("guid", randomUUID());
if (!/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(guid)) {
  console.error("--guid must be a UUID (8-4-4-4-12 hex)");
  process.exit(1);
}
const out = template
  .replaceAll("{{BASE_URL}}", xml(base)).replaceAll("{{KEY}}", xml(key)).replaceAll("{{GUID}}", xml(guid));
writeFileSync(join(root, "manifest/manifest.xml"), out);
console.log(`wrote excel-addin/manifest/manifest.xml for ${base}`);
