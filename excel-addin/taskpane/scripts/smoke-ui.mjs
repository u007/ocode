#!/usr/bin/env node
// Real-browser smoke test of the task pane (no Excel needed: the pane runs with ?fake=1 on an in-memory workbook).
//   real Go bridge  +  fake `ocode serve` (this file)  +  headless Chromium via Playwright
// The fake ocode behaves like the agent: it streams a reply and calls an Excel tool over MCP.
// usage: npm run build && node scripts/smoke-ui.mjs [screenshot.png]      (needs go, playwright, chromium)
import { spawn, spawnSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import assert from "node:assert/strict";

const here = dirname(fileURLToPath(import.meta.url));
const { chromium } = createRequire(import.meta.url)("playwright");
const KEY = "smoke-key";

const bin = join(mkdtempSync(join(tmpdir(), "smoke-")), "bridge");
const b = spawnSync("go", ["build", "-o", bin, "."], { cwd: join(here, "../../bridge"), encoding: "utf8" });
assert.equal(b.status, 0, b.stderr);

// ---- fake ocode serve ----
const subs = new Set();
let seq = 0;
const emit = (event, data, session_id = "ses_1") => { const f = `event: envelope\ndata: ${JSON.stringify({ event, session_id, seq: ++seq, data })}\n\n`; subs.forEach((r) => r.write(f)); };
const posted = [];
const bridgePort = await new Promise((r) => { const s = createServer(); s.listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => r(p)); }); });
const mcp = async (name, args) => (await (await fetch(`http://127.0.0.1:${bridgePort}/mcp`, { method: "POST", headers: { "Content-Type": "application/json", "X-Bridge-Key": KEY }, body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name, arguments: args } }) })).json()).result;

const ocode = createServer((req, res) => {
  let body = ""; req.on("data", (c) => (body += c));
  req.on("end", async () => {
    const url = req.url.split("?")[0];
    if (url === "/api/events") { if (process.env.DEBUG_SMOKE) console.error("events connect", req.url, "subs", subs.size + 1); res.writeHead(200, { "Content-Type": "text/event-stream" }); res.write(": connected\n\n"); subs.add(res); res.on("close", () => { subs.delete(res); if (process.env.DEBUG_SMOKE) console.error("events closed"); }); return; }
    res.setHeader("Content-Type", "application/json");
    if (url === "/api/chat") {
      posted.push(JSON.parse(body));
      res.statusCode = 202; res.end(JSON.stringify({ sessionId: "ses_1", model: "m" }));
      emit("user_message", { content: posted.at(-1).content, user_seq: 1 }); emit("turn_started", {});
      setTimeout(async () => {
        emit("text", { delta: "I will add a note. " });
        emit("tool_start", { tool: "excel_write_range", call_id: "c1", command: '{"sheet":"Summary","address":"E1","values":[["Checked"]]}' });
        const r = await mcp("write_range", { sheet: "Summary", address: "E1", values: [["Checked"]] });   // the "agent" calls Excel
        emit("tool_result", { tool: "tool", call_id: "c1", output: r.content[0].text });
        emit("permission", { request_id: "p1", tool: "excel_format_range", summary: "bold the header" });
      }, 100);
      return;
    }
    if (url === "/api/permissions/resolve") { emit("permission_resolved", { request_id: "p1" }); emit("text", { delta: "Done." }); emit("turn_done", {}); res.end("{}"); return; }
    if (url.endsWith("/title")) { res.end("{}"); return; }
    if (url === "/api/sessions/ses_1") { res.end(JSON.stringify({ id: "ses_1", title: "t", messages: [{ role: "user", content: "[ocode:excel]\nx\n[/ocode:excel]\n\nhello" }, { role: "assistant", content: "hi" }], total: 2 })); return; }
    res.statusCode = 404; res.end(JSON.stringify({ error: "not found " + url }));
  });
});
const ocodePort = await new Promise((r) => ocode.listen(0, "127.0.0.1", () => r(ocode.address().port)));

// ---- real bridge serving dist/ ----
const proc = spawn(bin, ["-listen", `127.0.0.1:${bridgePort}`, "-ocode", `http://127.0.0.1:${ocodePort}`, "-static", join(here, "../dist"), "-workspace-root", mkdtempSync(join(tmpdir(), "ws-"))],
  { env: { ...process.env, EXCEL_BRIDGE_PANE_KEY: KEY } });
await new Promise((res, rej) => { proc.stdout.on("data", (d) => String(d).includes("excel bridge on") && res()); setTimeout(() => rej(new Error("bridge did not start")), 10000); });

const browser = await chromium.launch();
let failed = null;
let page;
const errors = [];
try {
  page = await browser.newPage({ viewport: { width: 360, height: 640 } });
  // Chromium logs every non-2xx fetch (the expected 404 for first-time empty state) and the blocked office.js as console errors; only uncaught exceptions and app-level errors fail the run.
  page.on("console", (m) => m.type() === "error" && !/Failed to load resource/.test(m.text()) && errors.push(`console: ${m.text()}`));
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.route("**/appsforoffice.microsoft.com/**", (r) => r.abort()); // no network in CI; fake mode does not need Office.js
  await page.goto(`http://127.0.0.1:${bridgePort}/taskpane.html?fake=1&key=${KEY}`);

  await page.waitForSelector('.dot.on[title^="bridge"]', { timeout: 10000 });
  await page.waitForSelector('.dot.on[title^="ocode events"]', { timeout: 10000 });
  assert.ok(!page.url().includes("key="), "the key must be removed from the address bar");

  await page.fill('textarea[aria-label="Message"]', "add a Checked column header");
  await page.click("button.primary");

  await page.waitForSelector(".msg.user");
  assert.equal((await page.textContent(".msg.user")).trim(), "add a Checked column header", "context block must be hidden from the user bubble");
  assert.match(posted[0].content, /^\[ocode:excel\]\nworkbook=[\w-]+\nfile=Sample\.xlsx\n/, "context block goes to the model");
  assert.equal(posted[0].async, true);
  assert.match(posted[0].project_path, /\/ws-[^/]+\/[\w-]+$/, "session is rooted in the per-workbook workspace");

  await page.waitForSelector(".msg.tool", { timeout: 10000 });
  assert.match(await page.textContent(".msg.tool"), /Wrote Summary!E1/);
  await page.waitForSelector(".card", { timeout: 10000 });
  assert.match(await page.textContent(".card"), /excel_format_range/);

  // the agent's tool call really changed the (fake) workbook and is listed as an undoable change
  await page.waitForSelector(".changes li", { timeout: 10000 });
  assert.match(await page.textContent(".changes li"), /write_range Summary!E1/);

  await page.click(".card button.primary");           // Allow
  await page.waitForFunction(() => document.body.textContent.includes("Done."), null, { timeout: 10000 });
  assert.equal(await page.locator(".card").count(), 0, "permission card goes away once resolved");

  await page.click(".changes li button");              // Undo
  await page.waitForSelector(".changes li s", { timeout: 10000 });
  assert.equal(errors.length, 0, errors.join("\n"));
  await page.screenshot({ path: process.argv[2] ?? join(tmpdir(), "excel-pane-smoke.png") });
  console.log("smoke-ui: OK");
} catch (e) {
  failed = e;
  try { console.error("--- page text:", (await page.textContent("body"))?.slice(0, 500), "\n--- errors:", errors, "\n--- requests:", await page.evaluate(() => performance.getEntriesByType("resource").map((r) => r.name.replace(location.origin, ""))), "\n--- fake ocode: subscribers", subs.size, "posted", posted.length, "seq", seq); } catch { /* page already gone */ }
} finally {
  await browser.close(); proc.kill(); ocode.close(); subs.forEach((r) => r.end());
}
if (failed) { console.error("smoke-ui: FAILED\n", failed); process.exit(1); }
process.exit(0);
