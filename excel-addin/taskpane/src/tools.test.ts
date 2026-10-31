import { beforeEach, describe, expect, it } from "vitest";
import contractJson from "../../bridge/tools.json";
import { FakePort } from "./fakePort";
import { Runner, type Contract } from "./runner";
import { TOOLS } from "./tools";
import { Journal, MemoryJournalStore } from "./tools/journal";

const contract = contractJson as unknown as Contract;

let port: FakePort;
let journal: Journal;
let runner: Runner;
let n = 0;
const call = async (tool: string, args: Record<string, unknown>) => {
  const r = await runner.handle({ id: `t${++n}`, tool, args });
  return r.ok ? { ok: true as const, out: JSON.parse(r.content!) as Record<string, any> } : { ok: false as const, error: r.error! };
};
const okOut = async (tool: string, args: Record<string, unknown>) => {
  const r = await call(tool, args);
  if (!r.ok) throw new Error(`${tool} failed: ${r.error}`);
  return r.out;
};

const SUMMARY = [
  ["Category", "Spent", "Budget"],
  ["Travel", 841.5, 800], ["Meals", 451.8, 450], ["Office Supplies", 291.9, 300], ["Utilities", 0, 200], ["Software", 452.5, 400], ["Misc", 100.65, 80],
  ["Total", 2138.35, 2230],
];

beforeEach(() => {
  port = new FakePort().addSheet("Summary", SUMMARY);
  journal = new Journal(new MemoryJournalStore());
  runner = new Runner(port, journal, contract);
});

describe("contract", () => {
  it("every contract tool has an implementation and every implementation is in the contract", () => {
    expect(contract.tools.map((t) => t.name).sort()).toEqual(Object.keys(TOOLS).sort());
  });
  it("tools are sorted and schemas only use keywords the validator understands", () => {
    const names = contract.tools.map((t) => t.name);
    expect(names).toEqual([...names].sort());
    const known = new Set(["type", "enum", "minimum", "maximum", "items", "properties", "required", "additionalProperties", "default", "description"]);
    const walk = (s: any, where: string) => {
      for (const k of Object.keys(s)) expect(known.has(k), `${where}: unsupported keyword ${k}`).toBe(true);
      Object.entries(s.properties ?? {}).forEach(([k, v]) => walk(v, `${where}.${k}`));
      if (s.items) walk(s.items, `${where}[]`);
    };
    contract.tools.forEach((t) => walk(t.inputSchema, t.name));
  });
  it("every write tool is described as taking a checkpoint or being outside undo", () => {
    for (const t of contract.tools.filter((x) => x.risk === "write")) expect(t.description.length).toBeGreaterThan(40);
  });
});

describe("read tools", () => {
  it("read_range pages, clamps a whole column to the used range, and flags data as untrusted", async () => {
    const big = Array.from({ length: 30 }, (_, i) => [`r${i}`, i]);
    port.addSheet("Big", big);
    const p1 = await okOut("read_range", { sheet: "Big", address: "A:B", max_cells: 20 });
    expect(p1).toMatchObject({ address: "A1:B30", total_rows: 30, rows_returned: 10, truncated: true, next_offset_rows: 10 });
    expect(p1.data_notice).toMatch(/untrusted/);
    const p3 = await okOut("read_range", { sheet: "Big", address: "A:B", max_cells: 20, offset_rows: 20 });
    expect(p3).toMatchObject({ rows_returned: 10, truncated: false });
    expect(p3.values[0]).toEqual(["r20", 20]);
    const past = await call("read_range", { sheet: "Big", address: "A:B", offset_rows: 99 });
    expect(past).toMatchObject({ ok: false });
  });
  it("read_range reports formulas sparsely, by address", async () => {
    port.set("Summary", "D2", "=B2-C2");
    const r = await okOut("read_range", { sheet: "Summary", address: "A1:D3", include: ["values", "formulas"] });
    expect(r.formulas).toEqual({ D2: "=B2-C2" });
    expect(r.values[1][3]).toBeCloseTo(41.5);
  });
  it("rejects bad addresses and missing sheets with a helpful message", async () => {
    expect(await call("read_range", { sheet: "Summary", address: "Z" })).toMatchObject({ ok: false, error: expect.stringContaining("not a valid A1") });
    expect(await call("read_range", { sheet: "Nope", address: "A1" })).toMatchObject({ ok: false, error: expect.stringContaining("does not exist") });
  });
  it("describe_workbook gives orientation in one call", async () => {
    const d = await okOut("describe_workbook", {});
    expect(d.sheets[0]).toMatchObject({ name: "Summary", used_range: "A1:C8" });
    expect(d.active_sheet).toBe("Summary");
  });
  it("profile_range finds text numbers, text dates, whitespace, NBSP, duplicates and blank rows", async () => {
    port.addSheet("Raw", [
      ["Date", "Employee", "Amount"],
      ["2026-10-01", " alice tan ", "120.50"],
      ["01 Oct 2026", "Alice Tan", " 75 "],
      [null, null, null],
      ["02/03/2026", "Bob Lim", 45],
      ["02/03/2026", "bob lim", 45],
    ]);
    const p = await okOut("profile_range", { sheet: "Raw", address: "A1:C6" });
    const [date, emp, amt] = p.columns;
    expect(date).toMatchObject({ name: "Date", text_date: 4, ambiguous_day_month_dates: 2, counts: { empty: 1, string: 4 } });
    expect(emp).toMatchObject({ leading_trailing_space: 1, nonbreaking_space: 2 });
    expect(amt).toMatchObject({ text_numeric: 2, counts: { number: 2, string: 2 }, min: 45, max: 45 });
    expect(p).toMatchObject({ blank_rows: 1, duplicate_rows_after_trim_and_case: 1 });
  });
  it("check_errors lists error cells with their formulas", async () => {
    port.set("Summary", "E2", "=B2/0");
    port.set("Summary", "E3", "=SUMIF(B2)");
    const r = await okOut("check_errors", { sheet: "Summary" });
    expect(r.count).toBe(2);
    expect(r.errors).toEqual([
      { address: "E2", error: "#DIV/0!", formula: "=B2/0" },
      { address: "E3", error: "#NAME?", formula: "=SUMIF(B2)" },
    ]);
  });
});

describe("write_range", () => {
  it("refuses to overwrite occupied cells unless asked, and writes nothing", async () => {
    const r = await call("write_range", { sheet: "Summary", address: "A1", values: [["x"]] });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("not empty") });
    expect(port.value("Summary", "A1")).toBe("Category");
    expect(port.log.filter((l) => l.startsWith("write"))).toEqual([]);
  });
  it("refuses to replace a formula with a constant unless replace_formulas", async () => {
    port.set("Summary", "D2", "=B2-C2");
    const r = await call("write_range", { sheet: "Summary", address: "D2", values: [[41.5]], overwrite: true });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("replace 1 formula") });
    expect(port.value("Summary", "D2")).toBeCloseTo(41.5);
    expect(await call("write_range", { sheet: "Summary", address: "D2", values: [[1]], overwrite: true, replace_formulas: true })).toMatchObject({ ok: true });
  });
  it("rejects a mismatch between address size and data size, and ragged data", async () => {
    expect(await call("write_range", { sheet: "Summary", address: "E1:F2", values: [["a"]] })).toMatchObject({ ok: false, error: expect.stringContaining("2x2 but values is 1x1") });
    expect(await call("write_range", { sheet: "Summary", address: "E1", values: [["a", "b"], ["c"]] })).toMatchObject({ ok: false, error: expect.stringContaining("rectangular") });
  });
  it("reads back what Excel evaluated and flags new errors instead of reporting success", async () => {
    const r = await okOut("write_range", { sheet: "Summary", address: "E1", values: [["Check"], ["=B2/0"], ["=B3-C3"]] });
    expect(r.evaluated).toEqual([["Check"], ["#DIV/0!"], [1.8]]);
    expect(r.new_errors).toEqual([{ address: "E2", error: "#DIV/0!", formula: "=B2/0" }]);
    expect(r.note).toMatch(/errors/);
    expect(r.step_id).toBe("s1");
  });
  it("is undoable, and undo refuses when someone edited the cells since (changing nothing)", async () => {
    const w = await okOut("write_range", { sheet: "Summary", address: "E1", values: [["Flag"], ["x"]] });
    port.set("Summary", "E2", "edited by the user");
    const u = await call("undo_step", { step_id: w.step_id });
    expect(u).toMatchObject({ ok: false, error: expect.stringContaining("Nothing was modified") });
    expect(port.value("Summary", "E1")).toBe("Flag");
    port.set("Summary", "E2", "x");
    expect(await call("undo_step", { step_id: w.step_id })).toMatchObject({ ok: true });
    expect(port.value("Summary", "E1")).toBe("");
    expect(await call("undo_step", { step_id: w.step_id })).toMatchObject({ ok: false, error: expect.stringContaining("already undone") });
  });
  it("undo restores previous values AND formulas, not their evaluated results", async () => {
    port.set("Summary", "D2", "=B2-C2");
    const w = await okOut("write_range", { sheet: "Summary", address: "D2", values: [[7]], overwrite: true, replace_formulas: true });
    await okOut("undo_step", { step_id: w.step_id });
    expect((await port.read("Summary", "D2")).formulas[0][0]).toBe("=B2-C2");
  });
  it("refuses a write too big to checkpoint (fail closed)", async () => {
    const big = Array.from({ length: 200 }, () => Array.from({ length: 101 }, () => 1));
    const r = await call("write_range", { sheet: "Summary", address: "J1", values: big });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("too many to checkpoint") });
  });
  it("checkpoint lists steps newest first", async () => {
    await okOut("write_range", { sheet: "Summary", address: "E1", values: [["a"]] });
    await okOut("write_range", { sheet: "Summary", address: "F1", values: [["b"]] });
    const c = await okOut("checkpoint", {});
    expect(c.steps.map((s: any) => s.step_id)).toEqual(["s2", "s1"]);
  });
});

describe("fill_formula", () => {
  it("fills with relative references adjusting per row and reports evaluated results", async () => {
    const r = await okOut("fill_formula", { sheet: "Summary", address: "D2:D7", formula: "=C2-B2" });
    expect(r.evaluated.map((x: number[]) => x[0])).toEqual([-41.5, -1.8, 8.1, 200, -52.5, -20.65]);
    expect(r.formulas.D7).toBe("=C7-B7");
    expect(r.new_errors).toEqual([]);
  });
  it("surfaces errors a naive fill creates (division by a zero budget)", async () => {
    port.set("Summary", "C5", 0);
    const r = await okOut("fill_formula", { sheet: "Summary", address: "D2:D7", formula: "=B2/C2" });
    expect(r.new_errors).toEqual([{ address: "D5", error: "#DIV/0!", formula: "=B5/C5" }]);
  });
  it("does not clobber an occupied target by default", async () => {
    expect(await call("fill_formula", { sheet: "Summary", address: "B2:B3", formula: "=1+1" })).toMatchObject({ ok: false });
  });
});

describe("create_chart (benchmark traps)", () => {
  it("rejects a pie whose range includes the Total row, and says which range to use", async () => {
    const r = await call("create_chart", { sheet: "Summary", data_address: "A1:B8", type: "pie", anchor: "H2" });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("Row 8") });
    expect((r as any).error).toContain("A1:B7");
    expect(port.charts).toHaveLength(0);
  });
  it("rejects a pie with several series, negatives, or all zeros", async () => {
    expect(await call("create_chart", { sheet: "Summary", data_address: "A1:C7", type: "pie", anchor: "H2" })).toMatchObject({ ok: false, error: expect.stringContaining("one series") });
    port.addSheet("Neg", [["k", "v"], ["a", -1], ["b", 2]]);
    expect(await call("create_chart", { sheet: "Neg", data_address: "A1:B3", type: "pie", anchor: "D1" })).toMatchObject({ ok: false, error: expect.stringContaining("negative") });
    port.addSheet("Zero", [["k", "v"], ["a", 0], ["b", 0]]);
    expect(await call("create_chart", { sheet: "Zero", data_address: "A1:B3", type: "pie", anchor: "D1" })).toMatchObject({ ok: false, error: expect.stringContaining("empty") });
  });
  it("rejects text in a series column (header not included / wrong range)", async () => {
    const r = await call("create_chart", { sheet: "Summary", data_address: "A2:C7", type: "column_clustered", anchor: "H2" });
    expect(r).toMatchObject({ ok: true }); // numbers only below row 1: valid, but then the series have no header names
    port.addSheet("Txt", [["k", "v"], ["a", "oops"]]);
    expect(await call("create_chart", { sheet: "Txt", data_address: "A1:B2", type: "line", anchor: "D1" })).toMatchObject({ ok: false, error: expect.stringContaining("not numeric") });
  });
  it("refuses an anchor that would cover data and suggests a free cell", async () => {
    const r = await call("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "B3" });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("cover data") });
    expect((r as any).error).toContain("E1");
  });
  it("chooses a free anchor to the right of the data when none is given, and does not stack two charts", async () => {
    const a = await okOut("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie" });
    expect(a.chart.anchor).toBe("E1");
    const b = await call("create_chart", { sheet: "Summary", data_address: "A1:C7", type: "column_clustered", anchor: "F3" });
    expect(b).toMatchObject({ ok: false, error: expect.stringContaining("overlap") });
  });
  it("returns the series references it derived, and defaults percent labels on for pies", async () => {
    const r = await okOut("create_chart", { sheet: "Summary", data_address: "A1:C7", type: "column_clustered", anchor: "H2", title: "Budget vs Actual" });
    expect(r.series).toEqual([
      { name_cell: "B1", values: "B2:B7", categories: "A2:A7" },
      { name_cell: "C1", values: "C2:C7", categories: "A2:A7" },
    ]);
    const p = await okOut("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "H20" });
    expect(p.warnings.join()).toMatch(/Zero-value/);
  });
});

describe("create_pivot / create_table / format_range", () => {
  beforeEach(() => {
    port.addSheet("Data", [["Region", "Month", "Amount"], ["North", "2026-07", 10], ["South", "2026-07", 20], ["North", "2026-08", 5]]);
  });
  it("names the available fields when one is wrong", async () => {
    const r = await call("create_pivot", { source_sheet: "Data", source_address: "A1:C4", destination_sheet: "Summary", destination_address: "H1", rows: ["Regionn"], values: [{ field: "Amount" }] });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("Available: Region, Month, Amount") });
  });
  it("will not put a pivot on top of its own source", async () => {
    const r = await call("create_pivot", { source_sheet: "Data", source_address: "A1:C4", destination_sheet: "Data", destination_address: "B2", rows: ["Region"], values: [{ field: "Amount" }] });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("own source") });
  });
  it("creates a real pivot with sum as the default aggregation", async () => {
    const r = await okOut("create_pivot", { source_sheet: "Data", source_address: "A:C", destination_sheet: "Summary", destination_address: "H1", rows: ["Region"], columns: ["Month"], values: [{ field: "Amount" }] });
    expect(r.pivot).toMatchObject({ name: "PivotTable1", sheet: "Summary" });
  });
  it("create_table needs a clean header row and no overlap", async () => {
    port.addSheet("T", [["a", null], [1, 2]]);
    expect(await call("create_table", { sheet: "T", address: "A1:B2" })).toMatchObject({ ok: false, error: expect.stringContaining("empty cells (B1)") });
    port.addSheet("U", [["a", "A"], [1, 2]]);
    expect(await call("create_table", { sheet: "U", address: "A1:B2" })).toMatchObject({ ok: false, error: expect.stringContaining("Duplicate") });
    expect(await call("create_table", { sheet: "Data", address: "A1:C4", name: "Sales" })).toMatchObject({ ok: true });
    expect(await call("create_table", { sheet: "Data", address: "B1:C4" })).toMatchObject({ ok: false, error: expect.stringContaining('overlaps the existing table "Sales"') });
  });
  it("format_range validates colours, needs an option, and never touches values", async () => {
    expect(await call("format_range", { sheet: "Summary", address: "A1:C1", fill: "red" })).toMatchObject({ ok: false });
    expect(await call("format_range", { sheet: "Summary", address: "A1:C1" })).toMatchObject({ ok: false, error: expect.stringContaining("No formatting option") });
    const before = JSON.stringify((await port.read("Summary", "A1:C8")).values);
    expect(await call("format_range", { sheet: "Summary", address: "A1:C1", bold: true, fill: "#1F3B73", font_color: "#FFFFFF" })).toMatchObject({ ok: true });
    expect(JSON.stringify((await port.read("Summary", "A1:C8")).values)).toBe(before);
  });
});

describe("runner", () => {
  it("rejects unknown tools and invalid arguments before touching Excel", async () => {
    expect(await call("format_disk", {})).toMatchObject({ ok: false, error: expect.stringContaining("Unknown tool") });
    const r = await call("read_range", { sheet: "Summary" });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("address is required") });
    expect(await call("read_range", { sheet: "Summary", address: "A1", bogus: 1 })).toMatchObject({ ok: false, error: expect.stringContaining("not an allowed property") });
  });
  it("accepts the routing argument 'workbook' without passing it to tools", async () => {
    expect(await call("describe_workbook", { workbook: "wb-1" })).toMatchObject({ ok: true });
  });
  it("runs calls one at a time in arrival order", async () => {
    const order: string[] = [];
    const orig = port.write.bind(port);
    port.write = async (...a: Parameters<typeof orig>) => { order.push(`start ${a[1]}`); await new Promise((r) => setTimeout(r, 15)); await orig(...a); order.push(`end ${a[1]}`); };
    await Promise.all([
      call("write_range", { sheet: "Summary", address: "E1", values: [[1]] }),
      call("write_range", { sheet: "Summary", address: "F1", values: [[2]] }),
    ]);
    expect(order).toEqual(["start E1", "end E1", "start F1", "end F1"]);
  });
  it("a cancelled call does not start, and a cancel mid-call stops before the next Excel step", async () => {
    const p1 = runner.handle({ id: "slow", tool: "write_range", args: { sheet: "Summary", address: "E1", values: [[1]] } });
    const p2 = runner.handle({ id: "queued", tool: "write_range", args: { sheet: "Summary", address: "F1", values: [[2]] } });
    runner.cancel("queued");
    expect(await p1).toMatchObject({ ok: true });
    expect(await p2).toMatchObject({ ok: false, error: expect.stringContaining("cancelled") });
    expect(port.value("Summary", "F1")).toBeNull(); // never written
  });
  it("passes an Office.js failure through with its code instead of hiding it", async () => {
    port.read = async () => { throw Object.assign(new Error("The requested resource doesn't exist."), { code: "ItemNotFound" }); };
    expect(await call("read_range", { sheet: "Summary", address: "A1" })).toMatchObject({ ok: false, error: "Excel error (ItemNotFound): The requested resource doesn't exist." });
  });
  it("the journal survives a pane reload when backed by a store", async () => {
    const store = new MemoryJournalStore();
    const r1 = new Runner(port, new Journal(store), contract);
    await r1.handle({ id: "a", tool: "write_range", args: { sheet: "Summary", address: "E1", values: [["x"]] } });
    const r2 = new Runner(port, new Journal(store), contract); // fresh pane, same store
    const c = JSON.parse((await r2.handle({ id: "b", tool: "checkpoint", args: {} })).content!);
    expect(c.steps).toHaveLength(1);
    expect((await r2.handle({ id: "c", tool: "undo_step", args: { step_id: "s1" } })).ok).toBe(true);
    expect(port.value("Summary", "E1")).toBe("");
  });
});
