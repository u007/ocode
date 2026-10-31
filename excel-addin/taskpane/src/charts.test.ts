import { beforeEach, describe, expect, it } from "vitest";
import contractJson from "../../bridge/tools.json";
import { parseAddress } from "./a1";
import { FakePort } from "./fakePort";
import { boxesOverlap, bordersFor } from "./officePort";
import { Runner, type Contract } from "./runner";
import { validateChartData } from "./tools/chartpivot";
import { Journal, MemoryJournalStore } from "./tools/journal";

const contract = contractJson as unknown as Contract;

let port: FakePort;
let runner: Runner;
let n = 0;
const runnerFor = (p: FakePort) => new Runner(p, new Journal(new MemoryJournalStore()), contract);
const call = async (tool: string, args: Record<string, unknown>) => {
  const r = await runner.handle({ id: `c${++n}`, tool, args });
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
  runner = runnerFor(port);
});

// ------------------------------------------------------------------ bug 1: overlap is decided by the port

describe("chart overlap is decided by findChartOverlap", () => {
  it("two create_chart calls with no anchor do not stack: the second is refused with the overlap message", async () => {
    const a = await okOut("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie" });
    expect(a.chart.anchor).toBe("E1");
    const b = await call("create_chart", { sheet: "Summary", data_address: "A1:C7", type: "column_clustered" });
    expect(b).toMatchObject({ ok: false, error: expect.stringContaining('overlap the existing chart "Chart 1"') });
    expect(port.charts).toHaveLength(1);
  });

  it("refuses a chart whose box overlaps an existing one through an explicit anchor", async () => {
    await okOut("create_chart", { sheet: "Summary", data_address: "A1:C7", type: "column_clustered", anchor: "H2" });
    const r = await call("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "J10" });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining('existing chart "Chart 1" at J10:Q25') });
    expect(port.charts).toHaveLength(1);
  });

  it("allows a chart whose edge only touches an existing chart (next column, next row)", async () => {
    await okOut("create_chart", { sheet: "Summary", data_address: "A1:C7", type: "column_clustered", anchor: "H2" });
    // H2 covers H..O by rows 2..17: P2 is the next column, H18 the next row.
    expect(await call("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "P2" })).toMatchObject({ ok: true });
    expect(await call("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "H18" })).toMatchObject({ ok: true });
    expect(port.charts).toHaveLength(3);
  });

  it("regression: a port whose listCharts has no anchors is still refused, because createChart asks findChartOverlap", async () => {
    class OpaquePort extends FakePort {
      queries: string[] = [];
      async listCharts(sheet: string) {
        return [{ name: "Ghost", sheet, type: "pie", title: "", dataAddress: "", anchor: "" }];
      }
      async findChartOverlap(sheet: string, address: string) {
        this.queries.push(`${sheet}!${address}`);
        return "Ghost";
      }
    }
    const p = new OpaquePort().addSheet("Summary", SUMMARY);
    const r = await runnerFor(p).handle({
      id: "x1", tool: "create_chart", args: { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "H2" },
    });
    expect(r.ok).toBe(false);
    expect(r.error).toContain('overlap the existing chart "Ghost" at H2:O17');
    expect(p.queries).toEqual(["Summary!H2:O17"]);
  });

  it("when the port reports no overlap, the chart is created even though the listed anchors are empty", async () => {
    class ClearPort extends FakePort {
      async listCharts(sheet: string) {
        return [{ name: "Ghost", sheet, type: "pie", title: "", dataAddress: "", anchor: "" }];
      }
      async findChartOverlap() { return null; }
    }
    const p = new ClearPort().addSheet("Summary", SUMMARY);
    const r = await runnerFor(p).handle({
      id: "x2", tool: "create_chart", args: { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "H2" },
    });
    expect(r.ok).toBe(true);
  });

  it("FakePort.findChartOverlap only looks at charts on the named sheet", async () => {
    port.addSheet("Other");
    await okOut("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "H2" });
    expect(await port.findChartOverlap("Summary", "H2:H2")).toBe("Chart 1");
    expect(await port.findChartOverlap("Other", "H2:H2")).toBeNull();
    expect(await port.findChartOverlap("Summary", "P2:P2")).toBeNull();
  });
});

describe("describe_workbook chart data", () => {
  it("omits the data key when the port cannot report a chart's data range", async () => {
    port.charts.push({ name: "Chart 1", sheet: "Summary", type: "pie", title: "Spend", dataAddress: "", anchor: "" });
    const out = await okOut("describe_workbook", {});
    const chart = out.sheets.find((s: any) => s.name === "Summary").charts[0];
    expect(chart).toEqual({ name: "Chart 1", type: "pie", title: "Spend" });
    expect("data" in chart).toBe(false);
  });

  it("keeps the data key when the data range is known", async () => {
    await okOut("create_chart", { sheet: "Summary", data_address: "A1:B7", type: "pie", anchor: "H2" });
    const out = await okOut("describe_workbook", {});
    expect(out.sheets.find((s: any) => s.name === "Summary").charts[0].data).toBe("A1:B7");
  });
});

describe("boxesOverlap (the rule behind OfficePort.findChartOverlap)", () => {
  const box = (left: number, top: number, width: number, height: number) => ({ left, top, width, height });
  it("overlapping boxes overlap, and boxes apart do not", () => {
    expect(boxesOverlap(box(0, 0, 100, 100), box(50, 50, 100, 100))).toBe(true);
    expect(boxesOverlap(box(0, 0, 100, 100), box(200, 0, 100, 100))).toBe(false);
  });
  it("boxes that only touch along an edge do not overlap, on either axis", () => {
    expect(boxesOverlap(box(0, 0, 100, 100), box(100, 0, 100, 100))).toBe(false);
    expect(boxesOverlap(box(0, 0, 100, 100), box(0, 100, 100, 100))).toBe(false);
  });
  it("an overlap within the tolerance counts as touching; a larger one is an overlap", () => {
    expect(boxesOverlap(box(0, 0, 100, 100), box(99.8, 0, 100, 100))).toBe(false);
    expect(boxesOverlap(box(0, 0, 100, 100), box(99, 0, 100, 100))).toBe(true);
  });
});

// ------------------------------------------------------------------ bug 2: border edges

describe("bordersFor (format_range borders must not name inside lines that do not exist)", () => {
  it("a single cell gets the four edges only", () => {
    expect(bordersFor(1, 1)).toEqual(["EdgeTop", "EdgeBottom", "EdgeLeft", "EdgeRight"]);
  });
  it("a single row adds only InsideVertical; a single column adds only InsideHorizontal", () => {
    expect(bordersFor(1, 4)).toEqual(["EdgeTop", "EdgeBottom", "EdgeLeft", "EdgeRight", "InsideVertical"]);
    expect(bordersFor(4, 1)).toEqual(["EdgeTop", "EdgeBottom", "EdgeLeft", "EdgeRight", "InsideHorizontal"]);
  });
  it("a block with several rows and columns gets all six", () => {
    expect(bordersFor(2, 2)).toEqual(["EdgeTop", "EdgeBottom", "EdgeLeft", "EdgeRight", "InsideHorizontal", "InsideVertical"]);
  });
  it("importing officePort does not touch the Excel or Office globals", () => {
    expect(typeof (globalThis as any).Excel).toBe("undefined");
    expect(typeof (globalThis as any).Office).toBe("undefined");
  });
});

// ------------------------------------------------------------------ bug 3: total-looking labels

/** A header row, then one data row per label, with a numeric value in column B. */
const rowsWith = (labels: string[]) => [["Region", "Sales"], ...labels.map((l, i) => [l, 10 + i])];

describe("total-label check: only a final row is an error", () => {
  it("accepts 'Total Eclipse Sales' in the middle and as the last row, with no warning", async () => {
    port.addSheet("Mid", rowsWith(["Total Eclipse Sales", "North", "South"]));
    port.addSheet("Last", rowsWith(["North", "South", "Total Eclipse Sales"]));
    const mid = await okOut("create_chart", { sheet: "Mid", data_address: "A1:B4", type: "column_clustered", anchor: "E2" });
    const last = await okOut("create_chart", { sheet: "Last", data_address: "A1:B4", type: "column_clustered", anchor: "E2" });
    expect(mid.warnings).toEqual([]);
    expect(last.warnings).toEqual([]);
  });

  it("accepts 'Totals Region A' as a category", async () => {
    port.addSheet("Totals", rowsWith(["North", "Totals Region A"]));
    expect(await call("create_chart", { sheet: "Totals", data_address: "A1:B3", type: "column_clustered", anchor: "E2" })).toMatchObject({ ok: true });
  });

  it.each(["Total", "Grand Total:", "Subtotal", "Sub Total"])("rejects a final row labelled %s and suggests the range without it", async (label) => {
    port.addSheet("Last", rowsWith(["North", "South", label]));
    const r = await call("create_chart", { sheet: "Last", data_address: "A1:B4", type: "column_clustered", anchor: "E2" });
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining("Row 4") });
    expect((r as any).error).toContain("A1:B3");
    expect((r as any).error).toContain("include_total_rows");
  });

  it("a total-looking label in a middle row is a warning, not an error", async () => {
    port.addSheet("Mid", rowsWith(["North", "Total", "South"]));
    const out = await okOut("create_chart", { sheet: "Mid", data_address: "A1:B4", type: "column_clustered", anchor: "E2" });
    expect(out.warnings).toEqual(['Row 3 ("Total") looks like a total; check it belongs in the chart.']);
  });

  it("include_total_rows=true accepts a final Total row and warns about it", async () => {
    port.addSheet("Last", rowsWith(["North", "South", "Total"]));
    const out = await okOut("create_chart", { sheet: "Last", data_address: "A1:B4", type: "column_clustered", anchor: "E2", include_total_rows: true });
    expect(out.warnings).toEqual(['Row 4 ("Total") looks like a total; check it belongs in the chart.']);
    expect(out.data_points).toBe(3);
  });

  it("validateChartData: a label that only starts with a total word produces no warning", () => {
    const values = rowsWith(["Subtotal services", "North"]);
    const types = values.map((r) => r.map((v) => (typeof v === "number" ? "Double" : "String")));
    const plan = validateChartData("column_clustered", parseAddress("A1:B3")!, values, types);
    expect(plan.warnings).toEqual([]);
  });

  it("the create_chart schema advertises include_total_rows as an optional boolean defaulting to false", () => {
    const tool = contract.tools.find((t) => t.name === "create_chart") as any;
    expect(tool.inputSchema.properties.include_total_rows).toMatchObject({ type: "boolean", default: false });
    expect(tool.inputSchema.required).not.toContain("include_total_rows");
    expect(tool.description).toContain("include_total_rows");
  });
});
