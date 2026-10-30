import { describe, expect, it } from "vitest";
import { FakePort } from "./fakePort";
import { Journal, MAX_JOURNAL_BYTES, MAX_STEP_BYTES, MemoryJournalStore, type JournalStore, type Step } from "./tools/journal";
import { ToolError, type ToolCtx } from "./tools/util";
import { undoStep, writeRange } from "./tools/write";

/** A journal store whose load or save can be made to fail, backed by a MemoryJournalStore. */
class FlakyStore implements JournalStore {
  inner = new MemoryJournalStore();
  loadFails = false;
  saveFails = false;
  async load() {
    if (this.loadFails) throw new Error("bridge down");
    return this.inner.load();
  }
  async save(steps: Step[]) {
    if (this.saveFails) throw new Error("journal save 413");
    return this.inner.save(steps);
  }
}

const SHEET = "Summary";
const ADDR = { sheet: SHEET, address: "E1", overwrite: false, replace_formulas: false };

function setup(store: JournalStore = new MemoryJournalStore()) {
  const port = new FakePort().addSheet(SHEET, [["Name", "Amount"], ["a", 1]]);
  const journal = new Journal(store);
  const ctx: ToolCtx = { port, journal, signal: new AbortController().signal };
  return { port, journal, ctx };
}

/** A string of `n` two-byte characters: 2n bytes of UTF-8 but only n JavaScript string units. */
const twoByte = (n: number) => "é".repeat(n);

describe("checkpoint before write: load and save", () => {
  it("a journal that cannot load refuses the write before any cell changes", async () => {
    const store = new FlakyStore();
    store.loadFails = true;
    const { port, ctx } = setup(store);
    await expect(writeRange(ctx, { ...ADDR, values: [["x"]] })).rejects.toThrow("bridge down");
    expect(port.log.filter((l) => l.startsWith("write"))).toEqual([]);
    expect(port.value(SHEET, "E1")).toBeNull();
  });

  it("a failed save rolls the cells back to their prior values and says so; the next write is s1, not poisoned", async () => {
    const store = new FlakyStore();
    const { port, journal, ctx } = setup(store);
    port.set(SHEET, "E1", "keep");
    store.saveFails = true;

    const err = await writeRange(ctx, { ...ADDR, values: [["new"]], overwrite: true }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ToolError);
    expect((err as ToolError).message).toMatch(/rolled back/);
    expect(port.value(SHEET, "E1")).toBe("keep");
    expect(await journal.list()).toEqual([]);

    store.saveFails = false;
    const ok = await writeRange(ctx, { ...ADDR, values: [["after"]], overwrite: true });
    expect(ok.step_id).toBe("s1");
    expect(port.value(SHEET, "E1")).toBe("after");
    expect((await journal.list()).map((s) => s.id)).toEqual(["s1"]);
  });

  it("a rollback restores the prior number format too", async () => {
    const store = new FlakyStore();
    const { port, ctx } = setup(store);
    store.saveFails = true;
    await expect(writeRange(ctx, { ...ADDR, values: [["007"]], number_format: "@" })).rejects.toThrow(/rolled back/);
    expect((await port.read(SHEET, "E1")).numberFormats[0][0]).toBe("General");
    expect(port.value(SHEET, "E1")).toBe(""); // empty before, empty after
  });

  it("when the rollback also fails, the error says the write stands and names the affected range", async () => {
    const store = new FlakyStore();
    const { port, ctx } = setup(store);
    store.saveFails = true;
    let writes = 0;
    const realWrite = port.write.bind(port);
    port.write = async (sheet, address, cells, fmt) => {
      if (++writes > 1) throw new Error("port gone");
      return realWrite(sheet, address, cells, fmt);
    };
    const err = await writeRange(ctx, { ...ADDR, values: [["new"]] }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ToolError);
    expect((err as ToolError).message).toMatch(/could NOT be checkpointed/);
    expect((err as ToolError).message).toContain(`${SHEET}!E1`);
    expect((err as ToolError).message).toContain("port gone");
  });

  it("a save failure keeps the in-memory journal as it was, including steps trimmed to make room", async () => {
    const store = new FlakyStore();
    const journal = new Journal(store);
    const chunk = twoByte(600_000); // about 1.2 MB of UTF-8 per step
    for (let i = 0; i < 3; i++) await journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[chunk]], after: [["x"]] });
    store.saveFails = true;
    await expect(journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[chunk]], after: [["x"]] })).rejects.toThrow("413");
    expect((await journal.list(10)).map((s) => s.id)).toEqual(["s3", "s2", "s1"]);
    store.saveFails = false;
    expect((await journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[""]], after: [[""]] })).id).toBe("s4");
  });
});

describe("checkpoint size limits", () => {
  it("refuses a step too large to persist before any cell is written", async () => {
    const { port, ctx } = setup();
    const big = "x".repeat(MAX_STEP_BYTES + 1);
    await expect(writeRange(ctx, { ...ADDR, values: [[big]] })).rejects.toThrow(/smaller blocks/);
    expect(port.log.filter((l) => l.startsWith("write"))).toEqual([]);
  });

  it("bounds the persisted journal by UTF-8 bytes and drops the oldest steps first", async () => {
    const store = new MemoryJournalStore();
    const journal = new Journal(store);
    const chunk = twoByte(600_000); // 1.2 MB of UTF-8 each, only 600k JS string units
    for (let i = 0; i < 4; i++) await journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[chunk]], after: [["x"]] });
    expect(store.steps.map((s) => s.id)).toEqual(["s2", "s3", "s4"]);
    expect(new TextEncoder().encode(JSON.stringify(store.steps)).length).toBeLessThanOrEqual(MAX_JOURNAL_BYTES);
  });

  it("always keeps the newest step, even when it alone exceeds the byte budget", async () => {
    const store = new MemoryJournalStore();
    const journal = new Journal(store);
    await journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[""]], after: [["a"]] });
    const huge = await journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[twoByte(3_000_000)]], after: [["b"]] });
    expect((await journal.list(10)).map((s) => s.id)).toEqual([huge.id]);
  });
});

describe("number formats in undo (bug 2)", () => {
  it("write_range with number_format '@' records both format images, and undo puts General back", async () => {
    const { port, ctx, journal } = setup();
    const w = await writeRange(ctx, { ...ADDR, values: [["007"]], number_format: "@" });
    expect(w.formats_restored).toBe(true);
    const step = await journal.get(w.step_id);
    expect(step?.beforeFormats).toEqual([["General"]]);
    expect(step?.afterFormats).toEqual([["@"]]);

    const u = await undoStep(ctx, { step_id: w.step_id });
    expect(u.formats_restored).toBe(true);
    const back = await port.read(SHEET, "E1");
    expect(back.numberFormats[0][0]).toBe("General");
    expect(back.values[0][0]).toBe("");
  });

  it("undo applies the prior '@' format before writing the text '007' back", async () => {
    const { port, ctx } = setup();
    port.set(SHEET, "E1", "007");
    await port.format(SHEET, "E1", { numberFormat: "@" });
    const w = await writeRange(ctx, { ...ADDR, values: [[5]], number_format: "General", overwrite: true });

    const mark = port.log.length;
    await undoStep(ctx, { step_id: w.step_id });
    const undoLog = port.log.slice(mark);
    const formatAt = undoLog.findIndex((l) => l.startsWith(`formats ${SHEET}!E1`));
    const writeAt = undoLog.findIndex((l) => l.startsWith(`write ${SHEET}!E1`));
    expect(formatAt).toBeGreaterThanOrEqual(0);
    expect(writeAt).toBeGreaterThan(formatAt);

    const back = await port.read(SHEET, "E1");
    expect(back.numberFormats[0][0]).toBe("@");
    expect(back.values[0][0]).toBe("007");
  });

  it("a step recorded without formats still undoes, and skips the format restore", async () => {
    const { port, journal, ctx } = setup();
    await port.write(SHEET, "E1", [["x"]]);
    const step = await journal.record({ tool: "write_range", sheet: SHEET, address: "E1", before: [[""]], after: [["x"]] });
    const mark = port.log.length;
    const u = await undoStep(ctx, { step_id: step.id });
    expect(u.formats_restored).toBe(false);
    expect(port.log.slice(mark).some((l) => l.startsWith("formats"))).toBe(false);
    expect((await port.read(SHEET, "E1")).values[0][0]).toBe("");
  });
});
