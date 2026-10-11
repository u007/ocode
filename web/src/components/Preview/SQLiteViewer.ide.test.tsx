import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import SQLiteViewer from "./SQLiteViewer";
import { api, ApiError } from "../../api/client";

vi.mock("../../api/client", () => ({
  ApiError: class ApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  },
  api: {
    dbInfo: vi.fn(),
    dbTable: vi.fn(),
    dbQuery: vi.fn(),
    dbExec: vi.fn(),
    dbRow: vi.fn(),
    dbSchema: vi.fn(),
    dbMaintenance: vi.fn(),
    dbBlobDownload: vi.fn(),
    dbBlobUpload: vi.fn(),
    openFileWithOS: vi.fn().mockResolvedValue({}),
  },
}));

// jsdom has no PointerEvent and `fireEvent.pointerDown` silently drops
// `clientX` when it falls back to a plain Event — the table-list resize drag
// relies on clientX propagation, so polyfill it (same as GitPanel.test.tsx).
if (typeof window.PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 1;
    }
  }
  // @ts-expect-error assigning a minimal polyfill onto jsdom's window
  window.PointerEvent = PointerEventPolyfill;
}

const dbInfo = vi.mocked(api.dbInfo);
const dbTable = vi.mocked(api.dbTable);
const dbRow = vi.mocked(api.dbRow);
const dbMaintenance = vi.mocked(api.dbMaintenance);
const download = vi.mocked(api.dbBlobDownload);

const INFO = {
  is_sqlite: true,
  path: "app.db",
  tables: [{ name: "people", type: "table" as const, rows: 3 }],
};

const TABLE = {
  schema: {
    name: "people",
    type: "table",
    columns: [
      { name: "id", decl_type: "INTEGER", not_null: true, pk: 1, generated: false },
      { name: "name", decl_type: "TEXT", not_null: false, pk: 0, generated: false },
    ],
    indexes: [],
    foreign_keys: [],
    ddl: "CREATE TABLE people(id INTEGER PRIMARY KEY, name TEXT)",
    rowid: true,
  },
  result: {
    columns: [
      { name: "id", decl_type: "INTEGER" },
      { name: "name", decl_type: "TEXT" },
    ],
    rows: [
      [1, "ann"],
      [2, "bob"],
      [3, "cat"],
    ] as (number | string | null)[][],
    row_count: 3,
    truncated: false,
    elapsed_ms: 1,
  },
};

beforeEach(() => {
  dbInfo.mockReset().mockResolvedValue(INFO);
  dbTable.mockReset().mockResolvedValue(TABLE);
  dbRow.mockReset().mockResolvedValue({ rows_affected: 1, elapsed_ms: 1 });
  dbMaintenance.mockReset().mockResolvedValue({ rows: ["ok"] });
  download.mockReset().mockResolvedValue({ data: null, mediaType: "", byteLength: 0, isNull: true });
  vi.mocked(api.dbSchema).mockReset();
  vi.mocked(api.dbExec).mockReset();
  vi.mocked(api.dbQuery).mockReset().mockResolvedValue({
    columns: [],
    rows: [],
    row_count: 0,
    truncated: false,
    elapsed_ms: 0,
  });
});

async function openTable() {
  render(<SQLiteViewer path="app.db" />);
  fireEvent.click(await screen.findByText("people"));
  await screen.findByText("ann");
}

describe("filter, sort and count", () => {
  it("refetches with the filter expression and asks for a count", async () => {
    await openTable();
    const input = screen.getByLabelText("Filter rows");
    fireEvent.change(input, { target: { value: "age > 30" } });

    await waitFor(() =>
      expect(dbTable).toHaveBeenCalledWith(
        "app.db",
        "people",
        expect.objectContaining({ filter: "age > 30", count: true }),
      ),
    );
  });

  it("shows the real total returned by the server, not the ANALYZE estimate", async () => {
    dbTable.mockResolvedValue({ ...TABLE, total: 4231 });
    await openTable();
    fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "1=1" } });

    expect(await screen.findByTestId("sqlite-total")).toHaveTextContent("4231");
  });

  it("clears the filter when the input is emptied", async () => {
    await openTable();
    const input = screen.getByLabelText("Filter rows");
    fireEvent.change(input, { target: { value: "x" } });
    await waitFor(() => expect(dbTable).toHaveBeenCalledWith("app.db", "people", expect.objectContaining({ filter: "x" })));
    fireEvent.change(input, { target: { value: "" } });

    await waitFor(() =>
      expect(dbTable).toHaveBeenCalledWith(
        "app.db",
        "people",
        expect.objectContaining({ filter: undefined }),
      ),
    );
  });

  it("resets to the first page when the filter changes", async () => {
    dbTable.mockResolvedValue({
      ...TABLE,
      result: { ...TABLE.result, truncated: true },
    });
    await openTable();
    fireEvent.click(screen.getByLabelText("Next page"));
    await waitFor(() => expect(dbTable).toHaveBeenCalledWith("app.db", "people", expect.objectContaining({ offset: 100 })));

    fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "1=1" } });
    await waitFor(() =>
      expect(dbTable).toHaveBeenCalledWith(
        "app.db",
        "people",
        expect.objectContaining({ offset: 0, filter: "1=1" }),
      ),
    );
  });

  it("toggles sort direction when a column header is clicked twice", async () => {
    await openTable();
    fireEvent.click(screen.getByRole("button", { name: /^Sort by name$/ }));
    await waitFor(() =>
      expect(dbTable).toHaveBeenCalledWith(
        "app.db",
        "people",
        expect.objectContaining({ sort: "name", dir: "asc" }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: /^Sort by name$/ }));
    await waitFor(() =>
      expect(dbTable).toHaveBeenCalledWith(
        "app.db",
        "people",
        expect.objectContaining({ sort: "name", dir: "desc" }),
      ),
    );
  });

  it("surfaces a rejected filter as a 400 message, not a silent empty grid", async () => {
    await openTable();
    // Only the refetch fails: a rejected mount would take the whole pane down,
    // which is a different path (asserted by SQLiteViewer.test.tsx).
    dbTable.mockRejectedValue(new ApiError("no such column: age", 400));
    fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "age > 1" } });

    expect(await screen.findByTestId("sqlite-filter-error")).toHaveTextContent("no such column: age");
  });
});

describe("inline cell editing", () => {
  it("saves an edited cell through dbRow update keyed by the primary key", async () => {
    await openTable();
    fireEvent.doubleClick(screen.getByText("bob"));
    const input = await screen.findByLabelText("Edit name");
    fireEvent.change(input, { target: { value: "bobby" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() =>
      expect(dbRow).toHaveBeenCalledWith(
        "app.db",
        "update",
        "people",
        expect.objectContaining({ key: { id: 2 }, values: { name: "bobby" } }),
      ),
    );
  });

  it("does not save on Escape, and re-renders the original value", async () => {
    await openTable();
    fireEvent.doubleClick(screen.getByText("bob"));
    const input = await screen.findByLabelText("Edit name");
    fireEvent.change(input, { target: { value: "nope" } });
    fireEvent.keyDown(input, { key: "Escape" });

    await waitFor(() => expect(screen.queryByLabelText("Edit name")).toBeNull());
    expect(dbRow).not.toHaveBeenCalled();
    expect(screen.getByText("bob")).toBeTruthy();
  });

  it("keeps the editor open and shows the error when the save fails", async () => {
    dbRow.mockRejectedValue(new ApiError("No row matched — it may have changed.", 409));
    await openTable();
    fireEvent.doubleClick(screen.getByText("bob"));
    const input = await screen.findByLabelText("Edit name");
    fireEvent.change(input, { target: { value: "x" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(await screen.findByTestId("sqlite-inline-error")).toHaveTextContent("No row matched");
    expect(screen.getByLabelText("Edit name")).toBeTruthy();
  });

  it("refetches after a successful inline save", async () => {
    await openTable();
    const before = dbTable.mock.calls.length;
    fireEvent.doubleClick(screen.getByText("bob"));
    const input = await screen.findByLabelText("Edit name");
    fireEvent.change(input, { target: { value: "bobby" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(dbTable.mock.calls.length).toBeGreaterThan(before));
  });
});

describe("multi-row delete", () => {
  it("confirms with the row count before deleting every selected row", async () => {
    await openTable();
    fireEvent.click(screen.getByLabelText("Select row 1"));
    fireEvent.click(screen.getByLabelText("Select row 3"));

    fireEvent.click(await screen.findByTestId("sqlite-delete-selected"));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/2 rows/)).toBeTruthy();

    fireEvent.click(within(dialog).getByRole("button", { name: /Delete/ }));
    await waitFor(() => expect(dbRow).toHaveBeenCalledTimes(2));
    expect(dbRow).toHaveBeenCalledWith("app.db", "delete", "people", expect.objectContaining({ key: { id: 1 } }));
    expect(dbRow).toHaveBeenCalledWith("app.db", "delete", "people", expect.objectContaining({ key: { id: 3 } }));
  });

  it("deletes nothing when the confirmation is cancelled", async () => {
    await openTable();
    fireEvent.click(screen.getByLabelText("Select row 1"));
    fireEvent.click(await screen.findByTestId("sqlite-delete-selected"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(dbRow).not.toHaveBeenCalled();
  });

  it("reports how many deletions actually happened when one fails", async () => {
    dbRow
      .mockResolvedValueOnce({ rows_affected: 1, elapsed_ms: 1 })
      .mockRejectedValueOnce(new ApiError("No row matched", 409));
    await openTable();
    fireEvent.click(screen.getByLabelText("Select row 1"));
    fireEvent.click(screen.getByLabelText("Select row 2"));
    fireEvent.click(await screen.findByTestId("sqlite-delete-selected"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete/ }));

    const notice = await screen.findByTestId("sqlite-notice");
    expect(notice).toHaveTextContent("Deleted 1 of 2");
    expect(notice).toHaveTextContent("No row matched");
  });

  it("clears the selection once the delete finishes", async () => {
    await openTable();
    fireEvent.click(screen.getByLabelText("Select row 1"));
    fireEvent.click(await screen.findByTestId("sqlite-delete-selected"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete/ }));

    await waitFor(() => expect(screen.queryByTestId("sqlite-delete-selected")).toBeNull());
  });
});

describe("CSV export", () => {
  it("exports the visible page of the selected table", async () => {
    await openTable();
    const clicks: string[] = [];
    const origCreate = URL.createObjectURL;
    URL.createObjectURL = () => "blob:x";
    const origRevoke = URL.revokeObjectURL;
    URL.revokeObjectURL = () => {};
    const origClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function click(this: HTMLAnchorElement) {
      clicks.push(this.download);
    };
    try {
      fireEvent.click(screen.getByTestId("sqlite-export-csv"));
    } finally {
      URL.createObjectURL = origCreate;
      URL.revokeObjectURL = origRevoke;
      HTMLAnchorElement.prototype.click = origClick;
    }
    expect(clicks).toHaveLength(1);
    expect(clicks[0]).toMatch(/^people-.*\.csv$/);
  });
});

describe("maintenance", () => {
  it("runs ANALYZE through the maintenance endpoint", async () => {
    await openTable();
    fireEvent.click(screen.getByTestId("sqlite-analyze"));
    await waitFor(() => expect(dbMaintenance).toHaveBeenCalledWith("app.db", "analyze", expect.anything()));
    expect(await screen.findByTestId("sqlite-notice")).toHaveTextContent(/analy/i);
  });

  it("confirms before VACUUM, which rewrites the whole file", async () => {
    await openTable();
    fireEvent.click(screen.getByTestId("sqlite-vacuum"));
    expect(dbMaintenance).not.toHaveBeenCalled();

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Vacuum/ }));
    await waitFor(() => expect(dbMaintenance).toHaveBeenCalledWith("app.db", "vacuum", expect.anything()));
  });

  it("reports a clean integrity check", async () => {
    await openTable();
    fireEvent.click(screen.getByTestId("sqlite-integrity"));
    expect(await screen.findByTestId("sqlite-notice")).toHaveTextContent("ok");
  });

  it("surfaces the damage lines an integrity check reports", async () => {
    dbMaintenance.mockResolvedValue({ rows: ["page 3: orphan"] });
    await openTable();
    fireEvent.click(screen.getByTestId("sqlite-integrity"));
    expect(await screen.findByTestId("sqlite-notice")).toHaveTextContent("page 3: orphan");
  });

  it("shows the failure when a maintenance op cannot run", async () => {
    dbMaintenance.mockRejectedValue(new ApiError("database is locked", 500));
    await openTable();
    fireEvent.click(screen.getByTestId("sqlite-analyze"));
    expect(await screen.findByTestId("sqlite-notice")).toHaveTextContent("database is locked");
  });
});
describe("blob cells", () => {
  it("opens the blob viewer from the grid chip", async () => {
    dbTable.mockResolvedValue({
      ...TABLE,
      result: {
        columns: [
          { name: "id", decl_type: "INTEGER" },
          { name: "data", decl_type: "BLOB" },
        ],
        rows: [[1, { $blob: true, bytes: 12, preview: "89504e47" }]] as never,
        row_count: 1,
        truncated: false,
        elapsed_ms: 1,
      },
    });
    download.mockResolvedValue({
      data: new Uint8Array([0x89, 0x50, 0x4e, 0x47]).buffer,
      mediaType: "image/png",
      byteLength: 4,
      isNull: false,
    });

    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("people"));
    fireEvent.click(await screen.findByTestId("sqlite-blob-chip"));

    await waitFor(() =>
      expect(download).toHaveBeenCalledWith("app.db", "people", "data", { id: 1 }, expect.anything()),
    );
  });
});

describe("exact row keys (ids past 2^53)", () => {
  // The id as the browser is forced to hold it: a JS number cannot represent
  // 2^53+1, so the CELL arrives already rounded to ...992. Only row_keys has the
  // exact value.
  const BIG_TABLE = {
    ...TABLE,
    result: {
      columns: [
        { name: "id", decl_type: "INTEGER" },
        { name: "name", decl_type: "TEXT" },
      ],
      rows: [[9007199254740992, "bob"]] as (number | string | null)[][],
      row_count: 1,
      truncated: false,
      elapsed_ms: 1,
    },
    row_keys: [{ id: "9007199254740993" }],
  };

  it("uses the exact string key rather than the rounded cell value", async () => {
    dbTable.mockResolvedValue(BIG_TABLE);
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("people"));
    await screen.findByText("bob");

    fireEvent.click(screen.getByLabelText("Delete row 1"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete/ }));

    await waitFor(() => expect(dbRow).toHaveBeenCalledTimes(1));
    // The whole point: ...993, NOT the rounded ...992 the cell holds.
    expect(dbRow).toHaveBeenCalledWith(
      "app.db",
      "delete",
      "people",
      expect.objectContaining({ key: { id: "9007199254740993" } }),
    );
  });

  it("sends the exact key from an inline edit and from a blob fetch too", async () => {
    dbTable.mockResolvedValue(BIG_TABLE);
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("people"));

    fireEvent.doubleClick(await screen.findByText("bob"));
    const input = await screen.findByLabelText("Edit name");
    fireEvent.change(input, { target: { value: "bobby" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(dbRow).toHaveBeenCalledTimes(1));
    expect(dbRow).toHaveBeenCalledWith(
      "app.db",
      "update",
      "people",
      expect.objectContaining({ key: { id: "9007199254740993" } }),
    );
  });

  it("falls back to the cell-derived key when the server omits row_keys", async () => {
    // An older remote server: no row_keys, so the previous behaviour stands
    // rather than the mutation being refused.
    const { row_keys: _omitted, ...withoutKeys } = BIG_TABLE;
    dbTable.mockResolvedValue(withoutKeys);
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("people"));
    await screen.findByText("bob");

    fireEvent.click(screen.getByLabelText("Delete row 1"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete/ }));

    await waitFor(() => expect(dbRow).toHaveBeenCalledTimes(1));
    expect(dbRow).toHaveBeenCalledWith(
      "app.db",
      "delete",
      "people",
      expect.objectContaining({ key: { id: 9007199254740992 } }),
    );
  });

  it("falls back per-row when the server could not express one row's key", async () => {
    // A null ENTRY means "no exact key for THIS row", not "no key at all".
    dbTable.mockResolvedValue({ ...BIG_TABLE, row_keys: [null] });
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("people"));
    await screen.findByText("bob");

    fireEvent.click(screen.getByLabelText("Delete row 1"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete/ }));

    await waitFor(() => expect(dbRow).toHaveBeenCalledTimes(1));
    expect(dbRow).toHaveBeenCalledWith(
      "app.db",
      "delete",
      "people",
      expect.objectContaining({ key: { id: 9007199254740992 } }),
    );
  });
});

describe("resizable and collapsible table list", () => {
  const WIDTH_KEY = "ocode.ui.sqlite-viewer.width";
  const COLLAPSED_KEY = "ocode.ui.sqlite-viewer.width.collapsed";

  beforeEach(() => {
    window.localStorage.clear();
  });

  it("resizes the table-list pane by dragging its divider and persists it", async () => {
    await openTable();
    const handle = screen.getByRole("separator", { name: "Resize table list" });
    const pane = screen.getByTestId("sqlite-table-pane");
    expect(pane.style.width).toBe("160px");

    fireEvent.pointerDown(handle, { clientX: 160, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 260, pointerId: 1 });
    expect(pane.style.width).toBe("260px");

    // The width is persisted immediately, not only on pointerup.
    await waitFor(() => expect(window.localStorage.getItem(WIDTH_KEY)).toBe("260"));

    // Releasing the pointer stops tracking.
    fireEvent.pointerUp(window, { pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 100, pointerId: 1 });
    expect(pane.style.width).toBe("260px");
  });

  it("restores a persisted width on mount and resets on double-click", async () => {
    window.localStorage.setItem(WIDTH_KEY, "300");
    await openTable();
    const handle = screen.getByRole("separator", { name: "Resize table list" });
    const pane = screen.getByTestId("sqlite-table-pane");
    expect(pane.style.width).toBe("300px");

    fireEvent.doubleClick(handle);
    expect(pane.style.width).toBe("160px");
  });

  it("collapses the table list from the header toggle and persists it", async () => {
    await openTable();
    const pane = screen.getByTestId("sqlite-table-pane");
    expect(pane.style.width).toBe("160px");
    expect(screen.getByRole("separator", { name: "Resize table list" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Hide table list" }));

    // Collapsed: zero width, and the resize handle is gone.
    expect(pane.style.width).toBe("0px");
    expect(screen.queryByRole("separator", { name: "Resize table list" })).toBeNull();
    expect(screen.getByTestId("sqlite-toggle-table-list").getAttribute("aria-expanded")).toBe(
      "false",
    );
    await waitFor(() => expect(window.localStorage.getItem(COLLAPSED_KEY)).toBe("1"));

    // The toggle lives in the always-visible header, so it can be reopened.
    fireEvent.click(screen.getByRole("button", { name: "Show table list" }));
    expect(pane.style.width).toBe("160px");
  });

  it("gives the result grid two-axis scrolling", async () => {
    await openTable();
    const grid = screen.getByTestId("sqlite-data-grid");
    expect(grid.className).toContain("overflow-auto");
    // `min-w-0` on the flex chain is what lets a wide table scroll inside the
    // pane instead of stretching it.
    expect(grid.className).toContain("min-w-0");
    const table = within(grid).getByRole("table");
    // `w-max`/`min-w-full` lets a wide table overflow horizontally while still
    // filling a narrow container.
    expect(table.className).toContain("min-w-full");
    expect(table.className).toContain("w-max");
  });
});
