import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DBTableBrowser from "./DBTableBrowser";
import { ApiError, api } from "../../api/client";

vi.mock("../../api/client", () => {
  class ApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = "ApiError";
      this.status = status;
    }
  }
  return {
    ApiError,
    api: {
      dbConnectRows: vi.fn(),
      dbConnectRow: vi.fn(),
    },
  };
});

const mockRows = vi.mocked(api.dbConnectRows);
const mockRow = vi.mocked(api.dbConnectRow);

const SURFACE = "db-panel:s1";
const KEYED = {
  columns: [
    { name: "id", type: "bigint", pk: 1 },
    { name: "amount", type: "numeric(10,2)", pk: 0 },
  ],
  rows: [
    ["9007199254740993", "12.34"],
    ["9007199254740994", "5.00"],
  ],
  hasMore: false,
  primaryKey: ["id"],
};
const KEYLESS = {
  columns: [
    { name: "a", type: "integer", pk: 0 },
    { name: "b", type: "text", pk: 0 },
  ],
  rows: [["1", "x"]],
  hasMore: false,
  primaryKey: [],
};

function renderBrowser(table = "orders") {
  return render(<DBTableBrowser surface={SURFACE} connection="prod" table={table} />);
}

beforeEach(() => {
  vi.clearAllMocks();
  mockRows.mockResolvedValue(KEYED);
  mockRow.mockResolvedValue({ rowsAffected: 1 });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("DBTableBrowser", () => {
  it("loads the first page sorted by primary key", async () => {
    renderBrowser();
    expect(await screen.findByTestId("dbconnect-rows")).toBeTruthy();
    expect(mockRows).toHaveBeenCalledWith(SURFACE, "prod", "orders", {
      sort: "",
      dir: "asc",
      filter: "",
      limit: 100,
      offset: 0,
    });
  });

  it("sorts by a header click, then flips direction on the same column", async () => {
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Sort by amount" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ sort: "amount", dir: "asc", offset: 0 })),
    );
    fireEvent.click(screen.getByRole("button", { name: "Sort by amount" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ sort: "amount", dir: "desc" })),
    );
  });

  it("applies the filter and returns to the first page", async () => {
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "amount > 10" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ filter: "amount > 10", offset: 0 })),
    );
  });

  it("pages forward by the page size", async () => {
    mockRows.mockResolvedValue({ ...KEYED, hasMore: true });
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Next page" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ offset: 100 })),
    );
  });

  it("updates a row by its exact primary key and sends the typed value as typed", async () => {
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Edit row 1" }));
    const amount = await screen.findByLabelText("amount");
    fireEvent.change(amount, { target: { value: "99.99" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(SURFACE, "prod", "update", "orders",
        { id: "9007199254740993" },
        { id: "9007199254740993", amount: "99.99" }),
    );
  });

  it("deletes a row only after confirmation, keyed by the primary key", async () => {
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Delete row 2" }));
    expect(mockRow).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Delete row" }));

    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(SURFACE, "prod", "delete", "orders",
        { id: "9007199254740994" }, {}),
    );
  });

  it("inserts a row with no key", async () => {
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.click(screen.getByRole("button", { name: /Add row/ }));
    fireEvent.change(await screen.findByLabelText("amount"), { target: { value: "1.00" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(SURFACE, "prod", "insert", "orders", {}, expect.objectContaining({ amount: "1.00" })),
    );
  });

  it("shows a 409 from a row write verbatim inside the editor", async () => {
    mockRow.mockRejectedValue(
      new ApiError("No row matched — it may have changed or been deleted.", 409),
    );
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Edit row 1" }));
    fireEvent.click(await screen.findByTestId("sqlite-row-save"));

    expect(await screen.findByText("No row matched — it may have changed or been deleted.")).toBeTruthy();
  });

  it("keeps the filter usable after a failed load, so a corrected filter recovers the view", async () => {
    mockRows.mockRejectedValueOnce(new ApiError("the filter must be a read-only expression", 400));
    renderBrowser();
    expect(await screen.findByText("the filter must be a read-only expression")).toBeTruthy();

    // The filter input is still there: the failed load must not hide the controls.
    fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "amount > 10" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(await screen.findByTestId("dbconnect-rows")).toBeTruthy();
    expect(screen.queryByText("the filter must be a read-only expression")).toBeNull();
    expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
      expect.objectContaining({ filter: "amount > 10", offset: 0 }));
  });

  it("makes a table without a primary key read-only", async () => {
    mockRows.mockResolvedValue(KEYLESS);
    renderBrowser("events");
    expect(await screen.findByText(/no primary key, so its rows are read-only/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit row 1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete row 1" })).toBeNull();
  });
});
