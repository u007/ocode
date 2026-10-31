import { describe, expect, it } from "vitest";
import { stripContext, summarizeTool } from "./format";

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
});
