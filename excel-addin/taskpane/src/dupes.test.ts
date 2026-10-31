// Duplicate-workbook and undo-ordering behaviour: copied files get a fresh id, a second task pane
// evicts the first one (and the first one stops), and the UI's Undo runs through the serialising Runner.
import { describe, expect, it } from "vitest";
import contractJson from "../../bridge/tools.json";
import { BridgeClient, type BridgeStatus } from "./bridgeClient";
import { FakePort } from "./fakePort";
import { workbookId } from "./identity";
import { Runner, type Contract } from "./runner";
import { TOOLS } from "./tools";
import { Journal, MemoryJournalStore } from "./tools/journal";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
async function until(cond: () => boolean, ms = 2000) {
  const end = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > end) throw new Error("condition not met in time");
    await sleep(2);
  }
}
const contract = contractJson as unknown as Contract;

describe("workbook id survives copies as a fork", () => {
  const ID = "ocode.workbook_id";
  const URL_KEY = "ocode.file_url";
  const FORKED = "ocode.forked_from";

  it("the first call stores the id and the file url", async () => {
    const port = new FakePort();
    expect(await workbookId(port, "https://files.example/a.xlsx", () => "id-1")).toEqual({ id: "id-1", forked: false });
    expect(port.settings.get(ID)).toBe("id-1");
    expect(port.settings.get(URL_KEY)).toBe("https://files.example/a.xlsx");
  });

  it("the same url keeps the same id", async () => {
    const port = new FakePort();
    await workbookId(port, "https://files.example/a.xlsx", () => "id-1");
    expect(await workbookId(port, "https://files.example/a.xlsx", () => "id-2")).toEqual({ id: "id-1", forked: false });
    expect(port.settings.get(ID)).toBe("id-1");
    expect(port.settings.has(FORKED)).toBe(false);
  });

  it("a different non-empty url mints a new id, records the origin, and then sticks", async () => {
    const port = new FakePort();
    await workbookId(port, "https://files.example/a.xlsx", () => "id-1");
    expect(await workbookId(port, "https://files.example/copy.xlsx", () => "id-2")).toEqual({ id: "id-2", forked: true });
    expect(port.settings.get(ID)).toBe("id-2");
    expect(port.settings.get(FORKED)).toBe("id-1");
    expect(port.settings.get(URL_KEY)).toBe("https://files.example/copy.xlsx");
    // The copy's new location is now the reference: reopening it is not another fork.
    expect(await workbookId(port, "https://files.example/copy.xlsx", () => "id-3")).toEqual({ id: "id-2", forked: false });
  });

  it("an empty url never forks, in either direction", async () => {
    const port = new FakePort();
    await workbookId(port, "https://files.example/a.xlsx", () => "id-1");
    expect(await workbookId(port, "", () => "id-2")).toEqual({ id: "id-1", forked: false });
    expect(port.settings.get(URL_KEY)).toBe("https://files.example/a.xlsx");
  });

  it("an id minted while unsaved adopts the first real url without forking", async () => {
    const port = new FakePort();
    expect(await workbookId(port, "", () => "id-1")).toEqual({ id: "id-1", forked: false });
    expect(await workbookId(port, "https://files.example/a.xlsx", () => "id-2")).toEqual({ id: "id-1", forked: false });
    expect(port.settings.get(URL_KEY)).toBe("https://files.example/a.xlsx");
  });
});

// A fetch stand-in for the bridge stream: each call takes the next scripted response (the last one repeats).
function scriptedFetch(responses: (() => Response)[]) {
  const urls: string[] = [];
  const f = (async (input: RequestInfo | URL) => {
    urls.push(String(input));
    return responses[Math.min(urls.length - 1, responses.length - 1)]();
  }) as typeof fetch;
  return { f, urls };
}
const bodyOf = (...frames: string[]) => {
  const enc = new TextEncoder();
  return new ReadableStream<Uint8Array>({
    start(c) {
      for (const f of frames) c.enqueue(enc.encode(f));
      c.close();
    },
  });
};
const HELLO = "event: hello\ndata: {}\n\n";
const EVICTED = 'event: evicted\ndata: {"reason":"another task pane opened this workbook"}\n\n';

describe("an evicted pane stops reconnecting", () => {
  const client = (f: typeof fetch, statuses: BridgeStatus[], instance = "pane-x") =>
    new BridgeClient({
      base: "http://bridge.test", key: "k", workbook: "wb1", instance, runner: new Runner(new FakePort(), new Journal(new MemoryJournalStore()), contract),
      fetchImpl: f, backoff: [1, 2], onStatus: (s) => statuses.push(s),
    });

  it("an evicted event stops the client: one stream request, status evicted, no disconnected", async () => {
    const statuses: BridgeStatus[] = [];
    const { f, urls } = scriptedFetch([() => new Response(bodyOf(HELLO, EVICTED), { status: 200 })]);
    const c = client(f, statuses);
    c.start();
    await until(() => statuses.includes("evicted"));
    await sleep(30); // several backoff periods would have passed by now
    expect(urls).toHaveLength(1);
    expect(urls[0]).toContain("/bridge/stream?workbook=wb1&instance=pane-x");
    expect(statuses).toEqual(["connecting", "connected", "evicted"]);
  });

  it("control: a stream that merely drops still reconnects with backoff", async () => {
    const statuses: BridgeStatus[] = [];
    const { f, urls } = scriptedFetch([() => new Response(bodyOf(HELLO), { status: 200 })]);
    const c = client(f, statuses);
    c.start();
    await until(() => urls.length >= 3);
    c.stop();
    expect(statuses).not.toContain("evicted");
  });
});

// The Undo button must not race a write: both go through the Runner's chain.
function slowWritePort() {
  const port = new FakePort().addSheet("S", []);
  const log: string[] = [];
  const write = port.write.bind(port);
  const read = port.read.bind(port);
  port.write = async (sheet, address, cells, fmt) => {
    log.push(`write:${address}:start`);
    await sleep(40);
    await write(sheet, address, cells, fmt);
    log.push(`write:${address}:end`);
  };
  port.read = async (sheet, address) => {
    log.push(`read:${address}`);
    return read(sheet, address);
  };
  return { port, log };
}
const writeArgs = (address: string, v: number) => ({ sheet: "S", address, values: [[v]] });

describe("UI undo is ordered behind in-flight writes", () => {
  it("runner.handle(undo_step) starts only after a slow write has finished", async () => {
    const { port, log } = slowWritePort();
    const journal = new Journal(new MemoryJournalStore());
    const runner = new Runner(port, journal, contract);
    expect((await runner.handle({ id: "w1", tool: "write_range", args: writeArgs("A1", 1) })).ok).toBe(true);
    const stepId = (await journal.list(1))[0].id;
    log.length = 0;

    const write = runner.handle({ id: "w2", tool: "write_range", args: writeArgs("B1", 2) });
    const undo = runner.handle({ id: `ui-undo-${Date.now()}`, tool: "undo_step", args: { step_id: stepId } });
    const [w, u] = await Promise.all([write, undo]);

    expect(w.ok).toBe(true);
    expect(u).toEqual({ ok: true, content: expect.any(String) });
    const writeEnd = log.indexOf("write:B1:end");
    const undoRead = log.indexOf("read:A1");
    expect(writeEnd).toBeGreaterThanOrEqual(0);
    expect(undoRead).toBeGreaterThan(writeEnd);
  });

  it("control: calling the tool directly (the old Undo path) interleaves with the write", async () => {
    const { port, log } = slowWritePort();
    const journal = new Journal(new MemoryJournalStore());
    const runner = new Runner(port, journal, contract);
    await runner.handle({ id: "w1", tool: "write_range", args: writeArgs("A1", 1) });
    const stepId = (await journal.list(1))[0].id;
    log.length = 0;

    const ctx = { port, journal, signal: new AbortController().signal };
    const write = TOOLS.write_range(ctx, { ...writeArgs("B1", 2), overwrite: false, replace_formulas: false });
    const undo = TOOLS.undo_step(ctx, { step_id: stepId });
    await Promise.allSettled([write, undo]);

    expect(log.indexOf("read:A1")).toBeLessThan(log.indexOf("write:B1:end"));
  });
});
