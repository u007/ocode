import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import * as XLSX from "xlsx";
import { api } from "../../api/client";
import ExcelViewer from "./ExcelViewer";

vi.mock("../../api/client", () => ({ api: { fetchFileRaw: vi.fn() } }));

function workbookBytes(): ArrayBuffer {
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["name", "qty"], ["apples", 3]]), "Stock");
  XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["city"], ["Oslo"]]), "Cities");
  const out = XLSX.write(wb, { type: "array", bookType: "xlsx" });
  // xlsx 0.18 write(array) yields a plain number array; normalize for the reader.
  return new Uint8Array(out as unknown as number[]).buffer as ArrayBuffer;
}

describe("ExcelViewer", () => {
  it("renders sheet tabs and the first sheet's cells as selectable text", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="budget.xlsx" />);

    expect(await screen.findByRole("tab", { name: "Stock" })).toBeDefined();
    expect(await screen.findByRole("tab", { name: "Cities" })).toBeDefined();
    expect(await screen.findByText("apples")).toBeDefined();

    // Content renders as selectable text (copyable), never an editor.
    const table = document.querySelector("table.select-text");
    expect(table).not.toBeNull();
    expect(document.querySelector(".monaco-editor")).toBeNull();
  });

  it("switches sheets on tab click", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="budget.xlsx" />);

    fireEvent.click(await screen.findByRole("tab", { name: "Cities" }));
    expect(await screen.findByText("Oslo")).toBeDefined();
  });

  it("shows a failure state when the file cannot be read", async () => {
    vi.mocked(api.fetchFileRaw).mockRejectedValue(new Error("nope"));
    render(<ExcelViewer path="broken.xlsx" />);
    expect(await screen.findByText(/Spreadsheet failed/)).toBeDefined();
  });

  it("caps tall sheets with a visible notice instead of freezing", async () => {
    const wb = XLSX.utils.book_new();
    const rows: string[][] = [];
    for (let i = 0; i < 1205; i++) rows.push([`row-${i}`, "x"]);
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet(rows), "Big");
    const out = XLSX.write(wb, { type: "array", bookType: "xlsx" });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new Uint8Array(out as unknown as number[]).buffer as ArrayBuffer);
    render(<ExcelViewer path="big.xlsx" />);

    expect(await screen.findByText("row-0")).toBeDefined();
    expect(await screen.findByText(/large sheet capped/)).toBeDefined();
    // Rows past the cap never reach the DOM.
    expect(screen.queryByText("row-1204")).toBeNull();
  });

  it("caps wide sheets with a visible notice", async () => {
    const wb = XLSX.utils.book_new();
    const wide: string[] = [];
    for (let i = 0; i < 150; i++) wide.push(`c${i}`);
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([wide]), "Wide");
    const out = XLSX.write(wb, { type: "array", bookType: "xlsx" });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new Uint8Array(out as unknown as number[]).buffer as ArrayBuffer);
    render(<ExcelViewer path="wide.xlsx" />);

    expect(await screen.findByText("c0")).toBeDefined();
    expect(await screen.findByText(/large sheet capped/)).toBeDefined();
    expect(screen.queryByText("c149")).toBeNull();
  });

  it("renders empty sheets without crashing", async () => {
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([]), "Empty");
    const out = XLSX.write(wb, { type: "array", bookType: "xlsx" });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new Uint8Array(out as unknown as number[]).buffer as ArrayBuffer);
    render(<ExcelViewer path="empty.xlsx" />);

    expect(await screen.findByText("(empty sheet)")).toBeDefined();
  });

  it("parses CSV files as text instead of binary", async () => {
    const bytes = new TextEncoder().encode("a,b\n1,2\n").buffer as ArrayBuffer;
    vi.mocked(api.fetchFileRaw).mockResolvedValue(bytes);
    render(<ExcelViewer path="data.csv" />);

    expect(await screen.findByText("a")).toBeDefined();
    expect(await screen.findByText("2")).toBeDefined();
  });
});
