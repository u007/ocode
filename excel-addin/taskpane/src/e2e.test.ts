// End-to-end: the REAL Go bridge, the real Runner + BridgeClient on an in-memory workbook, a fake ocode
// upstream, and an MCP caller that speaks exactly what ocode's remote MCP client sends.
import { ChildProcess, spawn, spawnSync } from "node:child_process";
import { createServer, type Server } from "node:http";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import contractJson from "../../bridge/tools.json";
import { BridgeClient } from "./bridgeClient";
import { BridgeJournalStore } from "./bridgeStore";
import { FakePort } from "./fakePort";
import { OcodeClient } from "./ocodeClient";
import { Runner, type Contract } from "./runner";
import { SessionIndex } from "./identity";
import { Journal } from "./tools/journal";

const hasGo = spawnSync("go", ["version"]).status === 0;
const KEY = "e2e-key";
const freePort = () => new Promise<number>((res) => { const s = createServer(); s.listen(0, "127.0.0.1", () => { const p = (s.address() as any).port; s.close(() => res(p)); }); });

describe.skipIf(!hasGo)("pane <-> bridge <-> MCP (real Go bridge)", () => {
  let proc: ChildProcess;
  let upstream: Server;
  let base = "";
  const seen: { auth?: string; key?: string; path?: string; body?: string }[] = [];
  const port = new FakePort().addSheet("Summary", [["Category", "Spent"], ["Travel", 841.5], ["Meals", 451.8]]);
  let client: BridgeClient;
  const dataDir = mkdtempSync(join(tmpdir(), "bridge-data-"));
  const runnerFor = () => new Runner(port, new Journal(new BridgeJournalStore(base, KEY, "wb1")), contractJson as unknown as Contract);
  let runner: Runner;

  const mcp = async (method: string, params: unknown) => {
    const r = await fetch(`${base}/mcp`, { method: "POST", headers: { "Content-Type": "application/json", "X-Bridge-Key": KEY }, body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }) });
    return (await r.json()) as any;
  };
  const callTool = async (name: string, args: Record<string, unknown>) => {
    const out = await mcp("tools/call", { name, arguments: args });
    return { isError: !!out.result.isError, text: out.result.content.map((c: any) => c.text).join(""), raw: out };
  };

  beforeAll(async () => {
    const bin = join(mkdtempSync(join(tmpdir(), "bridge-bin-")), "bridge");
    const b = spawnSync("go", ["build", "-o", bin, "."], { cwd: join(__dirname, "../../bridge") });
    if (b.status !== 0) throw new Error(`go build failed: ${b.stderr}`);
    upstream = createServer((req, res) => {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        seen.push({ auth: req.headers.authorization, key: req.headers["x-bridge-key"] as string, path: req.url, body });
        res.setHeader("Content-Type", "application/json");
        res.end(JSON.stringify({ sessionId: "ses_1", model: "m", sessions: [], content: "" }));
      });
    });
    const up = await new Promise<number>((r) => upstream.listen(0, "127.0.0.1", () => r((upstream.address() as any).port)));
    const p = await freePort();
    proc = spawn(bin, ["-listen", `127.0.0.1:${p}`, "-ocode", `http://127.0.0.1:${up}`, "-data-dir", dataDir, "-call-timeout", "1500ms"], {
      env: { ...process.env, EXCEL_BRIDGE_PANE_KEY: KEY, OCODE_TOKEN: "ocode-secret" },
    });
    await new Promise<void>((res, rej) => { proc.stdout!.on("data", (d) => String(d).includes("excel bridge on") && res()); proc.on("exit", () => rej(new Error("bridge exited"))); setTimeout(() => rej(new Error("bridge did not start")), 10000); });
    base = `http://127.0.0.1:${p}`;
    runner = runnerFor();
    await new Promise<void>((res) => { client = new BridgeClient({ base, key: KEY, workbook: "wb1", runner, onStatus: (s) => s === "connected" && res() }); client.start(); });
  });

  afterAll(() => { client?.stop(); proc?.kill(); upstream?.close(); });

  it("lists exactly the contract tools, unprefixed", async () => {
    const out = await mcp("tools/list", {});
    expect(out.result.tools.map((t: any) => t.name)).toEqual((contractJson as any).tools.map((t: any) => t.name));
  });

  it("an MCP tools/call reaches Excel (FakePort) and the evaluated result comes back", async () => {
    const r = await callTool("write_range", { sheet: "Summary", address: "C1", values: [["Budget"], [800], [450]] });
    expect(r.isError).toBe(false);
    expect(JSON.parse(r.text)).toMatchObject({ step_id: "s1", cells_written: 3, new_errors: [] });
    expect(port.value("Summary", "C2")).toBe(800);
  });

  it("a tool-level refusal comes back as an MCP tool error the model can read", async () => {
    const r = await callTool("write_range", { sheet: "Summary", address: "C1", values: [["x"]] });
    expect(r).toMatchObject({ isError: true });
    expect(r.text).toMatch(/not empty/);
    expect(r.raw.error).toBeUndefined();
  });

  it("bad arguments are rejected by the pane's schema check, not run", async () => {
    const r = await callTool("read_range", { sheet: "Summary" });
    expect(r).toMatchObject({ isError: true });
    expect(r.text).toMatch(/address is required/);
  });

  it("a chart with the Total row in the range is refused end to end", async () => {
    port.addSheet("Pie", [["Category", "Spent"], ["Travel", 5], ["Total", 5]]);
    const r = await callTool("create_chart", { sheet: "Pie", data_address: "A1:B3", type: "pie", anchor: "E1" });
    expect(r.isError).toBe(true);
    expect(r.text).toMatch(/Row 3/);
  });

  it("the undo journal is stored in the bridge and survives a new pane (new Runner, same workbook)", async () => {
    const fresh = runnerFor();
    const out = await fresh.handle({ id: "x", tool: "checkpoint", args: {} });
    expect(JSON.parse(out.content!).steps.length).toBeGreaterThan(0);
    const undo = await fresh.handle({ id: "y", tool: "undo_step", args: { step_id: "s1" } });
    expect(undo).toMatchObject({ ok: true });
    expect(port.value("Summary", "C2")).toBe("");
  });

  it("a call the pane cannot finish times out in the bridge and the pane is told to cancel it", async () => {
    const slow = port.read.bind(port);
    port.read = async (...a: Parameters<typeof slow>) => { await new Promise((r) => setTimeout(r, 2500)); return slow(...a); };
    const t0 = Date.now();
    const r = await callTool("read_range", { sheet: "Summary", address: "A1" });
    port.read = slow;
    expect(r.isError).toBe(true);
    expect(r.text).toMatch(/did not finish/);
    expect(Date.now() - t0).toBeLessThan(2400);
  });

  it("a call for a workbook nobody has open is a readable tool error", async () => {
    const r = await callTool("describe_workbook", { workbook: "nope" });
    expect(r).toMatchObject({ isError: true });
    expect(r.text).toMatch(/no Excel task pane is connected/);
  });

  it("/api is proxied to ocode with ITS token; the pane's credentials never go upstream; the key is required", async () => {
    const oc = new OcodeClient({ base, key: KEY });
    const chat = await oc.startChat({ content: "hello", projectPath: "/data/excel/wb1", permissionMode: "normal" });
    expect(chat.sessionId).toBe("ses_1");
    const last = seen[seen.length - 1];
    expect(last).toMatchObject({ auth: "Bearer ocode-secret", path: "/api/chat" });
    expect(last.key).toBeUndefined();
    expect(JSON.parse(last.body!)).toMatchObject({ content: "hello", project_path: "/data/excel/wb1", async: true });
    expect((await fetch(`${base}/api/sessions`)).status).toBe(401);
  });

  it("registers the workspace as an ocode project through the proxy", async () => {
    await new OcodeClient({ base, key: KEY }).addProject("/data/excel/wb1");
    const last = seen[seen.length - 1];
    expect(last).toMatchObject({ auth: "Bearer ocode-secret", path: "/api/projects" });
    expect(JSON.parse(last.body!)).toEqual({ path: "/data/excel/wb1" });
  });

  it("the session index round-trips through the bridge", async () => {
    const idx = new SessionIndex(base, KEY, "wb1");
    expect(await idx.list()).toEqual([]);
    await idx.add({ id: "ses_1", title: "[Excel] Budget.xlsx", created: "2026-10-30" });
    await idx.add({ id: "ses_2", title: "[Excel] Budget.xlsx", created: "2026-10-31" });
    expect((await idx.list()).map((s) => s.id)).toEqual(["ses_2", "ses_1"]);
  });
});
