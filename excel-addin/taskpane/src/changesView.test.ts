import { describe, expect, it } from "vitest";
import { FakePort } from "./fakePort";
import type { Step } from "./tools/journal";
import { isThisTurn } from "./ui/ChangesView";

const step = (at: string): Step => ({ id: "s1", tool: "write_range", sheet: "Summary", address: "B2", at, before: [], after: [] });

describe("isThisTurn", () => {
  it("marks no step as this turn before any Send", () => {
    expect(isThisTurn(step("2026-11-01T10:00:00.000Z"), null)).toBe(false);
  });

  it("counts steps written at or after the Send and not before it", () => {
    const sent = Date.parse("2026-11-01T10:00:05.000Z");
    expect(isThisTurn(step("2026-11-01T10:00:04.999Z"), sent)).toBe(false);
    expect(isThisTurn(step("2026-11-01T10:00:05.000Z"), sent)).toBe(true);
    expect(isThisTurn(step("2026-11-01T10:00:09.000Z"), sent)).toBe(true);
  });
});

describe("FakePort.select", () => {
  it("makes the sheet active and the range the selection, and leaves cells alone", async () => {
    const port = new FakePort().addSheet("Other").addSheet("Summary", [["a", 1]]);
    expect(await port.activeSheet()).toBe("Other");
    await port.select("Summary", "A1:B2");
    expect(await port.activeSheet()).toBe("Summary");
    expect(await port.selection()).toEqual({ sheet: "Summary", address: "A1:B2" });
    expect(port.value("Summary", "A1")).toBe("a");
    expect(port.log).toEqual(["select Summary!A1:B2"]);
  });

  it("rejects an unknown sheet", async () => {
    await expect(new FakePort().addSheet("Summary").select("Nope", "A1")).rejects.toThrow('The sheet "Nope" does not exist');
  });
});
