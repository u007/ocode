import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DBTableBrowser, { dbCallErrorText } from "./DBTableBrowser";
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
// Postgres metadata: id and createdAt are database-defaulted; name is required.
const WITH_DEFAULTS = {
  columns: [
    {
      name: "id",
      type: "character varying(255)",
      pk: 1,
      not_null: true,
      has_default: true,
      default: "gen_random_uuid()",
    },
    { name: "name", type: "character varying(255)", pk: 0, not_null: true },
    {
      name: "createdAt",
      type: "timestamp with time zone",
      pk: 0,
      not_null: true,
      has_default: true,
      default: "CURRENT_TIMESTAMP",
    },
  ],
  rows: [["r1", "admin", "2026-01-01 00:00:00+00"]],
  hasMore: false,
  primaryKey: ["id"],
};

// A textual key whose sampled rows are all UUIDv4: the Add-row dialog can
// pre-fill a fresh one.
const UUID_KEYS = {
  columns: [
    { name: "id", type: "character varying(255)", pk: 1, not_null: true },
    { name: "name", type: "text", pk: 0, not_null: true },
  ],
  rows: [
    ["b15540d4-36b2-43ce-a4b9-00a33851ac3c", "admin"],
    ["98b82bc4-2643-41bb-9e23-8359d53db3ed", "user"],
  ],
  hasMore: false,
  primaryKey: ["id"],
};
// A slug key: textual and primary, but the values prove nothing about UUIDs.
const SLUG_KEYS = {
  columns: [
    { name: "id", type: "text", pk: 1, not_null: true },
    { name: "name", type: "text", pk: 0, not_null: true },
  ],
  rows: [
    ["admin", "Administrator"],
    ["user", "User"],
  ],
  hasMore: false,
  primaryKey: ["id"],
};

// A remote project's calls are routed to its host, so the target is forwarded on every call.
const TARGET = { host: "me@host", projectPath: "~/proj" };
const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function renderBrowser(table = "orders") {
  return render(<DBTableBrowser surface={SURFACE} connection="prod" table={table} target={TARGET} />);
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
    }, TARGET);
  });

  it("sorts by a header click, then flips direction on the same column", async () => {
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Sort by amount" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ sort: "amount", dir: "asc", offset: 0 }), TARGET),
    );
    fireEvent.click(screen.getByRole("button", { name: "Sort by amount" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ sort: "amount", dir: "desc" }), TARGET),
    );
  });

  it("applies the filter and returns to the first page", async () => {
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "amount > 10" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ filter: "amount > 10", offset: 0 }), TARGET),
    );
  });

  it("pages forward by the page size", async () => {
    mockRows.mockResolvedValue({ ...KEYED, hasMore: true });
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Next page" }));
    await waitFor(() =>
      expect(mockRows).toHaveBeenLastCalledWith(SURFACE, "prod", "orders",
        expect.objectContaining({ offset: 100 }), TARGET),
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
        { id: "9007199254740993", amount: "99.99" }, TARGET),
    );
  });

  it("deletes a row only after confirmation, keyed by the primary key", async () => {
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: "Delete row 2" }));
    expect(mockRow).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Delete row" }));

    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(SURFACE, "prod", "delete", "orders",
        { id: "9007199254740994" }, {}, TARGET),
    );
  });

  it("inserts a row with no key", async () => {
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.click(screen.getByRole("button", { name: /Add row/ }));
    fireEvent.change(await screen.findByLabelText("amount"), { target: { value: "1.00" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(SURFACE, "prod", "insert", "orders", {}, expect.objectContaining({ amount: "1.00" }), TARGET),
    );
  });

  it("omits empty database-defaulted columns on insert and requires the rest", async () => {
    mockRows.mockResolvedValue(WITH_DEFAULTS);
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.click(screen.getByRole("button", { name: /Add row/ }));

    // name has no default, so an empty value is refused before the server call.
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(screen.getByTestId("db-required-error").textContent).toContain("name");
    expect(mockRow).not.toHaveBeenCalled();

    fireEvent.change(await screen.findByLabelText("name"), { target: { value: "editor" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(
        SURFACE,
        "prod",
        "insert",
        "orders",
        {},
        { name: "editor" },
        TARGET,
      ),
    );
  });

  it("shows the Postgres not-null and default metadata in the editor", async () => {
    mockRows.mockResolvedValue(WITH_DEFAULTS);
    renderBrowser();
    fireEvent.click(await screen.findByRole("button", { name: /Add row/ }));
    expect(screen.getAllByText("not null")).toHaveLength(3);
    expect(screen.getByText("default gen_random_uuid()")).toBeTruthy();
    expect(screen.getByText("default CURRENT_TIMESTAMP")).toBeTruthy();
  });

  it("pre-fills a UUID id when the page proves the key is a UUID, and sends it", async () => {
    mockRows.mockResolvedValue(UUID_KEYS);
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.click(screen.getByRole("button", { name: /Add row/ }));

    const id = (await screen.findByLabelText("id")) as HTMLInputElement;
    expect(id.value).toMatch(UUID_V4);
    fireEvent.change(screen.getByLabelText("name"), { target: { value: "auditor" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    await waitFor(() =>
      expect(mockRow).toHaveBeenCalledWith(
        SURFACE,
        "prod",
        "insert",
        "orders",
        {},
        { id: id.value, name: "auditor" },
        TARGET,
      ),
    );
  });

  it("does not invent a UUID for a slug key", async () => {
    mockRows.mockResolvedValue(SLUG_KEYS);
    renderBrowser();
    await screen.findByTestId("dbconnect-rows");
    fireEvent.click(screen.getByRole("button", { name: /Add row/ }));

    expect((screen.getByLabelText("id") as HTMLInputElement).value).toBe("");
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(screen.getByTestId("db-required-error").textContent).toContain("id");
    expect(mockRow).not.toHaveBeenCalled();
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
      expect.objectContaining({ filter: "amount > 10", offset: 0 }), TARGET);
  });

  it("makes a table without a primary key read-only", async () => {
    mockRows.mockResolvedValue(KEYLESS);
    renderBrowser("events");
    expect(await screen.findByText(/no primary key, so its rows are read-only/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit row 1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete row 1" })).toBeNull();
  });
});

describe("dbCallErrorText", () => {
  it("names the host and the status for a remote 404 or 405", () => {
    const text404 = dbCallErrorText(new ApiError("not found", 404), TARGET);
    expect(text404).toBe("Remote host me@host answered HTTP 404 for the database connector: not found");
    const text405 = dbCallErrorText(new ApiError("method not allowed", 405), TARGET);
    expect(text405).toContain("answered HTTP 405");
  });

  it("shows a remote 409 or a local 404 verbatim", () => {
    expect(dbCallErrorText(new ApiError("connection is busy", 409), TARGET)).toBe("connection is busy");
    expect(dbCallErrorText(new ApiError("not found", 404), undefined)).toBe("not found");
  });
});
