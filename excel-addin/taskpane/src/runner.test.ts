import { beforeEach, describe, expect, it } from "vitest";
import contractJson from "../../bridge/tools.json";
import { FakePort } from "./fakePort";
import { READ_ONLY_REFUSAL, Runner, type Contract } from "./runner";
import type { WorkbookInfo } from "./port";
import { Journal, MemoryJournalStore } from "./tools/journal";

const contract = contractJson as unknown as Contract;
let port: FakePort;
let runner: Runner;
/** What the guard sees, as the InfoReporter would have collected it. */
let info: WorkbookInfo | null;
let n = 0;

const call = (tool: string, args: Record<string, unknown>) => runner.handle({ id: `g${++n}`, tool, args });
/** Collect the state the way InfoReporter does: the guard reads this value, not the port directly. */
const observe = async () => { info = await port.workbookInfo(); };

const WRITE = { sheet: "Summary", address: "E1", values: [[1]] };

beforeEach(() => {
  port = new FakePort().addSheet("Summary", [["Category", "Spent"], ["Travel", 841.5]]);
  runner = new Runner(port, new Journal(new MemoryJournalStore()), contract);
  info = null;
  runner.setInfoSource(() => info);
});

describe("write guard", () => {
  it("refuses a write while the workbook is read-only, without touching Excel", async () => {
    port.setInfo({ readOnly: true });
    await observe();
    const r = await call("write_range", WRITE);
    expect(r).toEqual({ ok: false, error: READ_ONLY_REFUSAL });
    expect(READ_ONLY_REFUSAL).toBe("The workbook is read-only / in Protected View, so ocode cannot change it. Enable editing in Excel first.");
    expect(port.log.filter((l) => l.startsWith("write"))).toEqual([]);
    expect(port.value("Summary", "E1")).toBeNull();
  });

  it("refuses every non-read tool while in Protected View, including undo", async () => {
    port.setInfo({ protectedView: true });
    await observe();
    for (const [tool, args] of [
      ["fill_formula", { sheet: "Summary", address: "C2:C3", formula: "=B2" }],
      ["format_range", { sheet: "Summary", address: "A1", bold: true }],
      ["undo_step", { step_id: "s1" }],
    ] as const) {
      expect(await call(tool, args), tool).toEqual({ ok: false, error: READ_ONLY_REFUSAL });
    }
    expect(port.log).toEqual([]);
  });

  it("still runs read tools while read-only", async () => {
    port.setInfo({ readOnly: true, protectedView: true });
    await observe();
    const r = await call("read_range", { sheet: "Summary", address: "A1:B2" });
    expect(r.ok).toBe(true);
  });

  it("lets writes through when the workbook is editable", async () => {
    port.setInfo({ readOnly: false, protectedView: false });
    await observe();
    expect(await call("write_range", WRITE)).toMatchObject({ ok: true });
    expect(port.value("Summary", "E1")).toBe(1);
  });

  it("lets writes through while the state is unknown (no source, or the source says null)", async () => {
    expect(await call("write_range", WRITE)).toMatchObject({ ok: true });
    info = { ...(await port.workbookInfo()), readOnly: null, protectedView: null };
    expect(await call("write_range", { ...WRITE, address: "E2" })).toMatchObject({ ok: true });
  });

  it("follows the latest state: a workbook turned editable can be written again", async () => {
    port.setInfo({ readOnly: true });
    await observe();
    expect(await call("write_range", WRITE)).toMatchObject({ ok: false });
    port.setInfo({ readOnly: false });
    await observe();
    expect(await call("write_range", WRITE)).toMatchObject({ ok: true });
  });

  it("invalid arguments are still reported as such, before the state is consulted", async () => {
    port.setInfo({ readOnly: true });
    await observe();
    expect(await call("write_range", { sheet: "Summary" })).toMatchObject({ ok: false, error: expect.stringContaining("Invalid arguments") });
  });
});
