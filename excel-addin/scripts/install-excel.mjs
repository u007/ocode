#!/usr/bin/env node
// One-shot Excel add-in install, macOS and Windows: dev cert, stable pane key + GUID, build, manifest, sideload.
// Usage: make install-excel   (then quit and reopen Excel, Home > Add-ins > Ask ocode)
// Re-run after every `make test`: manifest.test.ts overwrites manifest/manifest.xml with a test key.
// State: ~/.config/ocode-excel/{pane-key,guid}; the key must also reach the bridge env and the ocode MCP entry.
import { spawnSync } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { chmodSync, copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { homedir, platform } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const win = platform() === "win32";
const mac = platform() === "darwin";
if (!win && !mac) { console.error("install-excel supports macOS and Windows only"); process.exit(1); }

const state = process.env.OCODE_EXCEL_STATE ?? join(homedir(), ".config", "ocode-excel");
const certDir = process.env.CERT_DIR ?? join(homedir(), ".office-addin-dev-certs");
const base = process.env.EXCEL_BRIDGE_BASE ?? "https://localhost:3443";
const manifest = join(root, "manifest", "manifest.xml");

const run = (cmd, args, cwd = root) => {
  console.log(`$ ${cmd} ${args.join(" ")}`);
  // NEVER `shell: true` with an args ARRAY: Node joins the array into one
  // unquoted command line (it even warns: DEP0190), so a path containing a
  // space — `C:\Users\Jane Doe\...` — is split. On Windows the .cmd shims
  // (pnpm/npx) still need cmd.exe, so spawn it explicitly and let Node quote
  // each argument.
  const r = win
    ? spawnSync("cmd.exe", ["/d", "/s", "/c", cmd, ...args], { cwd, stdio: "inherit" })
    : spawnSync(cmd, args, { cwd, stdio: "inherit" });
  if (r.status !== 0) { console.error(`failed: ${cmd} ${args.join(" ")}`); process.exit(r.status ?? 1); }
};
const stable = (name, make) => {
  const f = join(state, name);
  if (!existsSync(f)) writeFileSync(f, make(), { mode: 0o600, flag: "wx" });
  chmodSync(f, 0o600); // tighten files created by an earlier run (no-op semantics on Windows)
  return readFileSync(f, "utf8").trim();
};

mkdirSync(state, { recursive: true, mode: 0o700 });
chmodSync(state, 0o700);
const key = stable("pane-key", () => randomBytes(24).toString("hex"));
const guid = stable("guid", randomUUID);

if (!existsSync(join(certDir, "localhost.crt"))) {
  console.log(">> installing the trusted dev certificate (asks for your admin password / a Windows prompt)");
  run("npx", ["--yes", "office-addin-dev-certs@3", "install"]);
}

console.log(">> building");
run("pnpm", ["install", "--frozen-lockfile", "--ignore-workspace"], join(root, "taskpane"));
run("pnpm", ["run", "build"], join(root, "taskpane"));
run("go", ["build", "-o", join("..", "bin", win ? "excel-bridge.exe" : "excel-bridge"), "."], join(root, "bridge"));

console.log(">> generating manifest");
run("node", [join(root, "scripts", "gen-manifest.mjs"), "--base", base, "--guid", guid, "--key", key]);

console.log(">> sideloading");
if (mac) {
  const wef = join(homedir(), "Library/Containers/com.microsoft.Excel/Data/Documents/wef");
  // ONE filename everywhere (this script, skills/ocode-excel, preflight.sh).
  // `ocode-excel.xml` rather than `manifest.xml` so it cannot clobber another
  // add-in's generic manifest in the same folder.
  const target = join(wef, "ocode-excel.xml");
  try {
    mkdirSync(wef, { recursive: true });
    // Excel reads EVERY .xml in wef, so a leftover from an earlier install (or a
    // hand copy under the old name) makes the add-in appear twice.
    const stale = readdirSync(wef).filter((f) => f.endsWith(".xml") && f !== "ocode-excel.xml");
    if (stale.length > 0) {
      console.error(`WARNING: ${wef} already holds ${stale.join(", ")}.`);
      console.error(`         Excel reads every .xml there, so the add-in will appear twice.`);
      console.error(`         Remove it: rm ${stale.map((f) => JSON.stringify(join(wef, f))).join(" ")}`);
    }
    copyFileSync(manifest, target);
    console.log(`copied to ${target}`);
  } catch (e) {
    console.error(`FAILED (${e.code}): macOS blocks this terminal from Excel's container.`);
    console.error("Fix one of:");
    console.error("  1. System Settings > Privacy & Security > Full Disk Access: enable this terminal, quit and reopen it, re-run make install-excel");
    console.error(`  2. In Finder COPY (Option-drag, never plain drag) ${manifest} into ${wef}/ and rename it ocode-excel.xml`);
    process.exit(1);
  }
} else {
  // Microsoft's tool registers a trusted catalog for the manifest on Windows. UNTESTED here (see TODO.md).
  run("npx", ["--yes", "office-addin-dev-settings@3", "sideload", manifest]);
}

console.log(`
Done. Remaining:
  1. Start the bridge with the same key (EXCEL_BRIDGE_PANE_KEY=${key.slice(0, 6)}... from ${join(state, "pane-key")}):
     ./bin/excel-bridge -static taskpane/dist -ocode http://127.0.0.1:4096 -cert ${join(certDir, "localhost.crt")} -key ${join(certDir, "localhost.key")}
  2. Register the MCP entry "excel" with header X-Bridge-Key = that key (skills/ocode-excel step 5)
  3. Quit and reopen Excel, Home > Add-ins > Ask ocode
  4. Verify: scripts/preflight.sh (macOS/Linux shell; set EXCEL_BRIDGE_PANE_KEY to the key)`);
