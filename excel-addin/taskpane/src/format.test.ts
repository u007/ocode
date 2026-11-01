import { describe, expect, it } from "vitest";
import { formatTokens, stripContext, summarizeTool } from "./format";

describe("format", () => {
  it("hides the injected context block but keeps the user's words", () => {
    const m = "[ocode:excel]\nworkbook=w\nfile=a.xlsx\n[/ocode:excel]\n\nadd a total row";
    expect(stripContext(m)).toBe("add a total row");
    expect(stripContext("plain")).toBe("plain");
  });
  it("summarises tool calls on one line, tolerating partial JSON", () => {
    expect(summarizeTool({ tool: "excel_write_range", args: '{"sheet":"S","address":"A1:B2"}', status: "done" })).toBe("Wrote S!A1:B2");
    expect(summarizeTool({ tool: "excel_create_chart", args: '{"sheet":"S","data_address":"A1:B7","type":"pie"}' })).toBe("Added a pie chart from S!A1:B7");
    expect(summarizeTool({ tool: "excel_read_range", args: '{"sheet":"S","addr' })).toBe("Read ");
    expect(summarizeTool({ tool: "excel_write_range", args: "{}", output: "3 target cell(s) are not empty", status: "done" })).toMatch(/refused/);
  });
  it("formats token counts for the gauge", () => {
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(481)).toBe("481");
    expect(formatTokens(999)).toBe("999");
    expect(formatTokens(1000)).toBe("1k");
    expect(formatTokens(1234)).toBe("1.2k");
    expect(formatTokens(12400)).toBe("12.4k");
    expect(formatTokens(999949)).toBe("999.9k");
    expect(formatTokens(999950)).toBe("1M");
    expect(formatTokens(1000000)).toBe("1M");
    expect(formatTokens(1234567)).toBe("1.2M");
    expect(formatTokens(999.6)).toBe("1k");
  });
  it("refuses a count that is not a non-negative number", () => {
    expect(() => formatTokens(-1)).toThrow(RangeError);
    expect(() => formatTokens(Number.NaN)).toThrow(RangeError);
  });
});
