import { beforeAll, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as XLSX from "xlsx";
import { api } from "../../api/client";
import { mountUniverSheet } from "./univerMount";
import { MAX_COLS, MAX_FILE_BYTES, MAX_ROWS } from "./univerData";
import { rawContentHash } from "./univerSerialize";
import ExcelViewer from "./ExcelViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

vi.mock("../../api/client", () => ({ api: { fetchFileRaw: vi.fn(), saveFileRaw: vi.fn() } }));
// Univer draws to canvas, which jsdom does not have. The converter is covered
// by univerData.test.ts; here the mount is replaced by a spy. The handle's
// onCommandExecuted callback is captured so tests can simulate a mutation.
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

let handle: {
  activateSheet: Mock;
  setDarkMode: Mock;
  dispose: Mock;
  snapshot: Mock;
  activeSheetName: Mock;
  selection: Mock;
  onSelectionChange: Mock;
};
let dirtyCb: (() => void) | null;

beforeEach(() => {
  vi.mocked(api.fetchFileRaw).mockReset();
  vi.mocked(api.saveFileRaw).mockReset();
  vi.mocked(api.saveFileRaw).mockResolvedValue({ path: "x.xlsx", saved: true });
  dirtyCb = null;
  handle = {
    activateSheet: vi.fn(),
    setDarkMode: vi.fn(),
    dispose: vi.fn(),
    snapshot: vi.fn(),
    activeSheetName: vi.fn(() => "Stock"),
    selection: vi.fn(() => null),
    onSelectionChange: vi.fn(() => () => {}),
  };
  vi.mocked(mountUniverSheet).mockReset();
  vi.mocked(mountUniverSheet).mockImplementation(async (_c, _d, _dark, opts) => {
    dirtyCb = opts?.onDirty ?? null;
    // snapshot returns the mounted data so the save path serializes real content.
    handle.snapshot.mockImplementation(() => _d);
    return handle;
  });
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

describe("ExcelViewer editable mode", () => {
  /** Clicks the toolbar Save, then confirms the first-save dialog. */
  async function clickSaveAndConfirm() {
    fireEvent.click(await screen.findByRole("button", { name: "Save spreadsheet" }));
    fireEvent.click(await screen.findByRole("button", { name: "Save" }));
  }

  it("mounts .xlsx editable and shows Save once dirty; save PUTs bytes with the base hash", async () => {
    const onDisk = workbookBytes();
    vi.mocked(api.fetchFileRaw).mockResolvedValue(onDisk);
    render(<ExcelViewer path="budget.xlsx" projectRoot="/root" projectHost="h1" />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const opts = vi.mocked(mountUniverSheet).mock.calls[0][3];
    expect(opts?.editable).toBe(true);

    // No Save button until dirty.
    expect(screen.queryByRole("button", { name: "Save spreadsheet" })).toBeNull();

    // Simulate a mutation through the captured onDirty callback.
    dirtyCb?.();
    await clickSaveAndConfirm();

    await waitFor(() => expect(api.saveFileRaw).toHaveBeenCalledTimes(1));
    const [p, bytes, saveOpts] = vi.mocked(api.saveFileRaw).mock.calls[0];
    expect(p).toBe("budget.xlsx");
    expect(bytes).toBeInstanceOf(ArrayBuffer);
    expect((bytes as ArrayBuffer).byteLength).toBeGreaterThan(0);
    expect(saveOpts?.projectRoot).toBe("/root");
    expect(saveOpts?.host).toBe("h1");
    // The base hash is the SHA-256 of the bytes GET returned, byte for byte.
    expect(saveOpts?.expectedHash).toBe(rawContentHash(onDisk));

    // After a successful save the dirty flag clears: Save disappears.
    await waitFor(() => expect(screen.queryByRole("button", { name: "Save spreadsheet" })).toBeNull());
  });

  it("stays read-only for .xlsm and CSV", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const { unmount } = render(<ExcelViewer path="budget.xlsm" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(vi.mocked(mountUniverSheet).mock.calls[0][3]?.editable).toBe(false);
    unmount();

    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes("a,b\n1,2\n"));
    render(<ExcelViewer path="data.csv" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(2));
    expect(vi.mocked(mountUniverSheet).mock.calls[1][3]?.editable).toBe(false);
    expect(screen.queryByRole("button", { name: "Save spreadsheet" })).toBeNull();
  });

  it("stays read-only when the sheet was truncated", async () => {
    const lines = Array.from({ length: MAX_ROWS + 1 }, (_, i) => `row-${i}`).join("\n");
    vi.mocked(api.fetchFileRaw).mockResolvedValue(textBytes(lines));
    render(<ExcelViewer path="long.csv" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(vi.mocked(mountUniverSheet).mock.calls[0][3]?.editable).toBe(false);
  });

  it("shows a save error and stays dirty on 409", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    vi.mocked(api.saveFileRaw).mockRejectedValue(Object.assign(new Error("file has changed on disk"), { status: 409 }));
    render(<ExcelViewer path="ledger.xlsx" />);

    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    dirtyCb?.();
    await clickSaveAndConfirm();

    expect(await screen.findByText(/file has changed on disk.*Reload/)).toBeDefined();
    // Still dirty: Save remains.
    expect(screen.getByRole("button", { name: "Save spreadsheet" })).toBeDefined();
  });

  it("asks before the first Save and writes nothing when the user cancels", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="cancel.xlsx" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    dirtyCb?.();

    fireEvent.click(await screen.findByRole("button", { name: "Save spreadsheet" }));
    expect(await screen.findByText("Save spreadsheet?")).toBeDefined();
    expect(screen.getByText(/formulas, charts, images and pivot tables/i)).toBeDefined();
    expect(screen.getByText(/cancel\.xlsx\.bak/)).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Save spreadsheet?")).toBeNull());
    expect(api.saveFileRaw).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Save spreadsheet" })).toBeDefined();
  });

  it("skips the confirmation on later Saves of the same file in the session", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="repeat.xlsx" />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    dirtyCb?.();
    await clickSaveAndConfirm();
    await waitFor(() => expect(screen.queryByRole("button", { name: "Save spreadsheet" })).toBeNull());

    dirtyCb?.();
    fireEvent.click(await screen.findByRole("button", { name: "Save spreadsheet" }));
    expect(screen.queryByText("Save spreadsheet?")).toBeNull();
    await waitFor(() => expect(api.saveFileRaw).toHaveBeenCalledTimes(2));
    // The second save's expected_hash is the SHA-256 of what the first save wrote.
    const [, firstBytes] = vi.mocked(api.saveFileRaw).mock.calls[0];
    const [, , secondOpts] = vi.mocked(api.saveFileRaw).mock.calls[1];
    expect(secondOpts?.expectedHash).toBe(rawContentHash(firstBytes as ArrayBuffer));
  });

  it("says edits are saved when Save is clicked, not automatically", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    render(<ExcelViewer path="label.xlsx" />);
    expect(await screen.findByText("Editable — edits are saved when you click Save")).toBeDefined();
  });

  it("clears the dirty flag after an onSave override resolves", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<ExcelViewer path="override.xlsx" onSave={onSave} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    dirtyCb?.();
    await clickSaveAndConfirm();

    await waitFor(() => expect(screen.queryByRole("button", { name: "Save spreadsheet" })).toBeNull());
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("surfaces an onSave override failure and stays dirty", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSave = vi.fn().mockRejectedValue(new Error("disk full"));
    render(<ExcelViewer path="override-fail.xlsx" onSave={onSave} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    dirtyCb?.();
    await clickSaveAndConfirm();

    expect(await screen.findByRole("alert")).toHaveTextContent("disk full");
    expect(screen.getByRole("button", { name: "Save spreadsheet" })).toBeDefined();
  });

  it("reports dirty changes via onDirtyChange", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onDirtyChange = vi.fn();
    render(<ExcelViewer path="budget.xlsx" onDirtyChange={onDirtyChange} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    dirtyCb?.();
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true));
  });

  it("exposes its save handler through onSaveRef", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSaveRef = { current: null as (() => Promise<boolean>) | null };
    render(<ExcelViewer path="budget.xlsx" onSaveRef={onSaveRef} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(typeof onSaveRef.current).toBe("function");
    const first = onSaveRef.current?.();
    // First save opens the confirm dialog rather than writing, and the handler
    // waits for the user's answer.
    await waitFor(() => expect(screen.getByText(/Save spreadsheet\?/)).toBeDefined());
    expect(api.saveFileRaw).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await first).toBe(true);
    expect(api.saveFileRaw).toHaveBeenCalledTimes(1);
  });

  it("resolves the first-save request false when the dialog is cancelled", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSaveRef = { current: null as (() => Promise<boolean>) | null };
    render(<ExcelViewer path="cancel-ref.xlsx" onSaveRef={onSaveRef} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const first = onSaveRef.current?.();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(await first).toBe(false);
    expect(api.saveFileRaw).not.toHaveBeenCalled();
  });

  it("shares one dialog and one answer between requests made while it is open", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSaveRef = { current: null as (() => Promise<boolean>) | null };
    render(<ExcelViewer path="shared-ask.xlsx" onSaveRef={onSaveRef} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const first = onSaveRef.current?.();
    const second = onSaveRef.current?.();
    await waitFor(() => expect(screen.getAllByText("Save spreadsheet?")).toHaveLength(1));
    fireEvent.click(await screen.findByRole("button", { name: "Save" }));
    expect(await first).toBe(true);
    expect(await second).toBe(true);
    expect(api.saveFileRaw).toHaveBeenCalledTimes(1);
  });

  it("joins a save already in flight instead of sending a second PUT", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSaveRef = { current: null as (() => Promise<boolean>) | null };
    render(<ExcelViewer path="in-flight.xlsx" onSaveRef={onSaveRef} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const confirmed = onSaveRef.current?.();
    fireEvent.click(await screen.findByRole("button", { name: "Save" }));
    expect(await confirmed).toBe(true);

    let release: () => void = () => {};
    vi.mocked(api.saveFileRaw).mockImplementation(
      () => new Promise((resolve) => {
        release = () => resolve({ path: "in-flight.xlsx", saved: true });
      }),
    );
    // Two requests in the same tick, before React re-renders: both see the same
    // closure, so only an in-flight check (not the `saving` state) can join them.
    const a = onSaveRef.current?.();
    const b = onSaveRef.current?.();
    await waitFor(() => expect(api.saveFileRaw).toHaveBeenCalledTimes(2));
    release();
    expect(await a).toBe(true);
    expect(await b).toBe(true);
    expect(api.saveFileRaw).toHaveBeenCalledTimes(2);
  });

  it("resolves a pending first-save request false when the viewer unmounts", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSaveRef = { current: null as (() => Promise<boolean>) | null };
    const { unmount } = render(<ExcelViewer path="unmount-ask.xlsx" onSaveRef={onSaveRef} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    const pending = onSaveRef.current?.();
    unmount();
    expect(await pending).toBe(false);
  });

  it("clears onSaveRef on unmount", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    const onSaveRef = { current: null as (() => Promise<boolean>) | null };
    const { unmount } = render(<ExcelViewer path="budget.xlsx" onSaveRef={onSaveRef} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(onSaveRef.current).not.toBeNull();
    unmount();
    expect(onSaveRef.current).toBeNull();
  });

  it("does not show Save in controlled (content) mode", async () => {
    render(<ExcelViewer path="cities.tsv" content={"a\tb\n"} />);
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    expect(vi.mocked(mountUniverSheet).mock.calls[0][3]?.editable).toBe(false);
    expect(screen.queryByRole("button", { name: "Save spreadsheet" })).toBeNull();
  });
});

describe("ExcelViewer report to the chat session", () => {
  beforeEach(() => resetViewerStates());

  it("reports the selected cells, tab-separated, when the selection changes", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    setViewerDoc("s1", { path: "budget.xlsx", kind: "excel" });
    const listener = { cb: null as (() => void) | null };
    handle.onSelectionChange.mockImplementation((cb: () => void) => {
      listener.cb = cb;
      return () => {};
    });
    render(
      <ViewerReportContext.Provider value="s1">
        <ExcelViewer path="budget.xlsx" />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(readViewerState("s1")?.location.sheet).toBe("Stock"));

    handle.selection.mockReturnValue({
      sheet: "Stock",
      range: "A1:B2",
      values: [
        ["name", "qty"],
        ["apples", "3"],
      ],
    });
    listener.cb?.();

    const state = readViewerState("s1");
    expect(state?.location).toEqual({ sheet: "Stock", range: "A1:B2" });
    // The store keeps tabs (column boundaries) and drops blank lines.
    expect(state?.selection).toBe("name\tqty\napples\t3");
  });

  it("reports the new sheet with no range when a sheet tab is clicked", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    setViewerDoc("s1", { path: "budget.xlsx", kind: "excel" });
    const listener = { cb: null as (() => void) | null };
    handle.onSelectionChange.mockImplementation((cb: () => void) => {
      listener.cb = cb;
      return () => {};
    });
    render(
      <ViewerReportContext.Provider value="s1">
        <ExcelViewer path="budget.xlsx" />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(readViewerState("s1")?.location.sheet).toBe("Stock"));

    // After a switch, Univer activates the sheet, fires its change event and
    // reports the sheet's default selection: A1, no cell content.
    handle.activeSheetName.mockReturnValue("Cities");
    handle.selection.mockReturnValue({ sheet: "Cities", range: "A1", values: [[""]] });
    fireEvent.click(await screen.findByRole("tab", { name: "Cities" }));
    listener.cb?.();

    const state = readViewerState("s1");
    expect(state?.location).toEqual({ sheet: "Cities", range: undefined });
    expect(state?.selection).toBeNull();
  });

  it("keeps the range of a single non-empty cell the user selected", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(workbookBytes());
    setViewerDoc("s1", { path: "budget.xlsx", kind: "excel" });
    const listener = { cb: null as (() => void) | null };
    handle.onSelectionChange.mockImplementation((cb: () => void) => {
      listener.cb = cb;
      return () => {};
    });
    render(
      <ViewerReportContext.Provider value="s1">
        <ExcelViewer path="budget.xlsx" />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(mountUniverSheet).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(readViewerState("s1")?.location.sheet).toBe("Stock"));

    handle.selection.mockReturnValue({ sheet: "Stock", range: "B2", values: [["3"]] });
    listener.cb?.();

    const state = readViewerState("s1");
    expect(state?.location).toEqual({ sheet: "Stock", range: "B2" });
    expect(state?.selection).toBe("3");
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
