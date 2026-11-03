import { beforeAll, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as XLSX from "xlsx";
import { api } from "../../api/client";
import { mountUniverSheet } from "./univerMount";
import { MAX_COLS, MAX_FILE_BYTES, MAX_ROWS } from "./univerData";
import ExcelViewer from "./ExcelViewer";

vi.mock("../../api/client", () => ({ api: { fetchFileRaw: vi.fn() } }));
// Univer draws to canvas, which jsdom does not have. The converter is covered
// by univerData.test.ts; here the mount is replaced by a spy.
vi.mock("./univerMount", () => ({ mountUniverSheet: vi.fn() }));

const theme = vi.hoisted(() => ({ appearance: "light" as "light" | "dark" }));
vi.mock("../../hooks/useResolvedEditorTheme", () => ({
  useResolvedEditorTheme: () => ({ effectiveAppearance: theme.appearance }),
}));

// ExcelViewer loads ExcelJS with a dynamic import. Its first evaluation in a
// fresh worker takes ~1.5s, longer than findBy's 1s default, so warm it up once.
beforeAll(async () => {
  await import("exceljs");
});

let handle: { activateSheet: Mock; setDarkMode: Mock; dispose: Mock };

beforeEach(() => {
  vi.mocked(api.fetchFileRaw).mockReset();
  handle = { activateSheet: vi.fn(), setDarkMode: vi.fn(), dispose: vi.fn() };
  vi.mocked(mountUniverSheet).mockReset();
  vi.mocked(mountUniverSheet).mockImplementation(async () => handle);
  theme.appearance = "light";
});

function xlsxBytes(build: (wb: XLSX.WorkBook) => void): ArrayBuffer {
  const wb = XLSX.utils.book_new();
  build(wb);
  const out = XLSX.write(wb, { type: "array", bookType: "xlsx" });
  // xlsx 0.18 write(array) yields a plain number array; normalize for the reader.
  return new Uint8Array(out as unknown as number[]).buffer as ArrayBuffer;
}

function workbookBytes(): ArrayBuffer {
  return xlsxBytes((wb) => {
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["name", "qty"], ["apples", 3]]), "Stock");
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["city"], ["Oslo"]]), "Cities");
  });
}

function textBytes(text: string): ArrayBuffer {
  return new TextEncoder().encode(text).buffer as ArrayBuffer;
}

/** The IWorkbookData the viewer handed to the (mocked) Univer mount. */
function mountedData(call = 0) {
  const args = vi.mocked(mountUniverSheet).mock.calls[call];
  if (args === undefined) throw new Error(`mount call ${call} did not happen`);
  return args[1];
}

describe("ExcelViewer", () => {
  it("renders sheet tabs and mounts every sheet into the Univer grid", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="budget.xlsx" />);

    expect(await screen.findByRole("tab", { name: "Stock" })).toBeDefined();
    expect(screen.getByRole("tab", { name: "Cities" })).toBeDefined();
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));

    const [container, , dark] = vi.mocked(mountUniverSheet).mock.calls[0];
    expect(container).toBeInstanceOf(HTMLElement);
    expect(dark).toBe(false);
    const data = mountedData();
    expect(data.sheetOrder).toEqual(["sheet-1", "sheet-2"]);
    expect(data.sheets["sheet-1"]?.name).toBe("Stock");
    expect(data.sheets["sheet-1"]?.cellData?.[1]?.[0]?.v).toBe("apples");
    // Content is a Univer grid, not an HTML table.
    expect(document.querySelector("table")).toBeNull();
  });

  it("renders a macro-enabled .xlsm through the ExcelJS path", async () => {
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["name", "qty"], ["apples", 3]]), "Stock");
    const out = XLSX.write(wb, { type: "array", bookType: "xlsm" });
    const bytes = new Uint8Array(out as unknown as number[]).buffer as ArrayBuffer;
    // STORE packing leaves the content type readable, so this fixture really is
    // macro-typed rather than an xlsx renamed to .xlsm.
    expect(new TextDecoder().decode(bytes)).toContain("macroEnabled");
    vi.mocked(api.fetchFileRaw).mockResolvedValue(bytes);
    render(<ExcelViewer path="budget.xlsm" />);

    expect(await screen.findByRole("tab", { name: "Stock" })).toBeDefined();
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(mountedData().sheets["sheet-1"]?.cellData?.[1]?.[0]?.v).toBe("apples");
  });

  it("switches the active Univer sheet when a tab is clicked", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="budget.xlsx" />);

    await waitFor(() => expect(handle.activateSheet).toHaveBeenCalledWith("sheet-1"));
    fireEvent.click(await screen.findByRole("tab", { name: "Cities" }));
    expect(handle.activateSheet).toHaveBeenLastCalledWith("sheet-2");
  });

  it("shows a failure state when the file cannot be read", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(api.fetchFileRaw).mockRejectedValue(new Error("nope"));
    render(<ExcelViewer path="broken.xlsx" />);
    expect(await screen.findByText(/Spreadsheet failed: nope/)).toBeDefined();
    expect(mountUniverSheet).not.toHaveBeenCalled();
  });

  it("shows the failure state when an .xlsx cannot be parsed", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes("not a zip"));
    render(<ExcelViewer path="corrupt.xlsx" />);
    expect(await screen.findByText(/Spreadsheet failed/)).toBeDefined();
  });

  it("shows the failure state when the Univer grid cannot mount", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    vi.mocked(mountUniverSheet).mockRejectedValue(new Error("no canvas"));
    render(<ExcelViewer path="budget.xlsx" />);
    expect(await screen.findByText(/Spreadsheet failed: no canvas/)).toBeDefined();
  });

  it("refuses a file above 15 MiB before any parser runs", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new ArrayBuffer(MAX_FILE_BYTES + 1));
    render(<ExcelViewer path="huge.xlsx" />);
    expect(await screen.findByText(/refuses files above 15 MiB/)).toBeDefined();
    expect(mountUniverSheet).not.toHaveBeenCalled();
  });

  it("says a capped tall sheet was truncated", async () => {
    const lines = Array.from({ length: MAX_ROWS + 1 }, (_, i) => `row-${i}`).join("\n");
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes(lines));
    render(<ExcelViewer path="long.csv" />);

    expect(await screen.findByText(/Showing first 50000 rows — large sheet truncated/)).toBeDefined();
    const data = mountedData();
    expect(data.sheets?.["sheet-1"]?.rowCount).toBe(MAX_ROWS);
  });

  it("says a capped wide sheet was truncated", async () => {
    const wide = Array.from({ length: MAX_COLS + 1 }, (_, i) => `c${i}`).join(",");
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes(wide));
    render(<ExcelViewer path="wide.csv" />);
    expect(await screen.findByText(/Showing first 200 columns — large sheet truncated/)).toBeDefined();
  });

  it("renders empty sheets without crashing", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(
      xlsxBytes((wb) => XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([]), "Empty")),
    );
    render(<ExcelViewer path="empty.xlsx" />);
    expect(await screen.findByText("(empty sheet)")).toBeDefined();
  });

  it("parses CSV files as text instead of binary", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes("a,b\n1,2\n"));
    render(<ExcelViewer path="data.csv" />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const cells = mountedData().sheets["sheet-1"]?.cellData;
    expect(cells?.[0]?.[0]?.v).toBe("a");
    expect(cells?.[1]?.[1]?.v).toBe("2");
  });
});

/**
 * A .tsv from disk took the `type: "array"` branch, which hands SheetJS raw
 * BYTES. SheetJS decodes those as latin-1, so every non-ASCII cell rendered as
 * mojibake ("café" → "cafÃ©"). The .tsv path must decode as UTF-8 first.
 */
describe("ExcelViewer tab-separated files", () => {
  it("decodes a .tsv as UTF-8 instead of mojibaking non-ASCII cells", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes("name\tcity\ncafé\tnaïve\n"));
    render(<ExcelViewer path="cities.tsv" />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const cells = mountedData().sheets["sheet-1"]?.cellData;
    // Not "cafÃ©" / "naÃ¯ve" — the latin-1 reading of the same UTF-8 bytes.
    expect(cells?.[1]?.[0]?.v).toBe("café");
    expect(cells?.[1]?.[1]?.v).toBe("naïve");
  });

  it("still splits a .tsv into columns", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes("name\tqty\tprice\nwidget\t3\t1.5\n"));
    render(<ExcelViewer path="stock.tsv" />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const cells = mountedData().sheets["sheet-1"]?.cellData;
    expect(cells?.[0]?.[2]?.v).toBe("price");
    expect(cells?.[1]?.[2]?.v).toBe("1.5");
  });

  it("parses a .tsv from the live editor string too (controlled mode)", async () => {
    render(<ExcelViewer path="cities.tsv" content={"name\tcity\ncafé\tnaïve\n"} />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(api.fetchFileRaw).not.toHaveBeenCalled();
    expect(mountedData().sheets["sheet-1"]?.cellData?.[1]?.[0]?.v).toBe("café");
  });

  it("re-mounts on each live edit and disposes the previous grid", async () => {
    const { rerender } = render(<ExcelViewer path="cities.tsv" content={"a\tb\n"} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));

    rerender(<ExcelViewer path="cities.tsv" content={"a\tb\nc\td\n"} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(2));
    expect(handle.dispose).toHaveBeenCalledTimes(1);
  });
});

describe("ExcelViewer lifecycle and appearance", () => {
  it("disposes the Univer grid on unmount", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const { unmount } = render(<ExcelViewer path="budget.xlsx" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));

    unmount();
    expect(handle.dispose).toHaveBeenCalledTimes(1);
  });

  it("mounts the grid in dark mode when the editor appearance is dark", async () => {
    theme.appearance = "dark";
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="budget.xlsx" />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(vi.mocked(mountUniverSheet).mock.calls[0][2]).toBe(true);
  });

  it("toggles the open grid's dark mode when the appearance changes", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const { rerender } = render(<ExcelViewer path="budget.xlsx" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));

    theme.appearance = "dark";
    rerender(<ExcelViewer path="budget.xlsx" />);
    await waitFor(() => expect(handle.setDarkMode).toHaveBeenLastCalledWith(true));
    expect(mountUniverSheet).toHaveBeenCalledTimes(1);
  });
});
