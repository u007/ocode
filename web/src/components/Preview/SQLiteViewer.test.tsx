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
    openFileWithOS: vi.fn().mockResolvedValue({}),
  },
}));

const dbInfo = vi.mocked(api.dbInfo);
const dbTable = vi.mocked(api.dbTable);
const dbQuery = vi.mocked(api.dbQuery);
const dbExec = vi.mocked(api.dbExec);
const dbRow = vi.mocked(api.dbRow);
const dbSchema = vi.mocked(api.dbSchema);

const INFO = {
  is_sqlite: true,
  path: "app.db",
  tables: [
    { name: "users", type: "table" as const, rows: 2 },
    { name: "user_view", type: "view" as const, rows: -1 },
  ],
};

const TABLE_RESPONSE = {
  schema: {
    name: "users",
    type: "table",
    columns: [
      { name: "id", decl_type: "INTEGER", not_null: true, pk: 1, generated: false },
      { name: "name", decl_type: "TEXT", not_null: false, default: "'anon'", pk: 0, generated: false },
    ],
    indexes: [{ name: "idx_users_name", unique: true, columns: ["name"], origin: "c" }],
    foreign_keys: [],
    ddl: "CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT DEFAULT 'anon')",
    rowid: true,
  },
  result: {
    columns: [
      { name: "id", decl_type: "INTEGER" },
      { name: "name", decl_type: "TEXT" },
    ],
    rows: [
      [1, "alice"],
      [2, null],
    ] as (number | string | null)[][],
    row_count: 2,
    truncated: false,
    elapsed_ms: 1,
  },
};

beforeEach(() => {
  dbInfo.mockReset().mockResolvedValue(INFO);
  dbTable.mockReset().mockResolvedValue(TABLE_RESPONSE);
  dbExec.mockReset();
  dbRow.mockReset();
  dbSchema.mockReset();
  dbQuery.mockReset().mockResolvedValue({
    columns: [{ name: "n", decl_type: "" }],
    rows: [[2]],
    row_count: 1,
    truncated: false,
    elapsed_ms: 1,
  });
});

describe("SQLiteViewer", () => {
  it("lists tables from dbInfo", async () => {
    render(<SQLiteViewer path="app.db" />);
    expect(await screen.findByText("users")).toBeTruthy();
    expect(screen.getByText("user_view")).toBeTruthy();
  });

  it("loads rows when a table is selected", async () => {
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("users"));

    await waitFor(() => expect(dbTable).toHaveBeenCalledWith("app.db", "users", expect.objectContaining({ limit: 100, offset: 0 })));
    expect(await screen.findByText("alice")).toBeTruthy();
    // NULL renders as the muted marker, not the string "null".
    expect(screen.getByText("NULL")).toBeTruthy();
  });

  it("shows the schema pane for the selected table", async () => {
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("users"));
    await screen.findByText("alice");
    fireEvent.click(screen.getByRole("button", { name: "schema" }));

    expect(await screen.findByTestId("sqlite-schema")).toBeTruthy();
    expect(screen.getByText(/CREATE TABLE users/)).toBeTruthy();
    expect(screen.getByText(/idx_users_name/)).toBeTruthy();
  });

  it("runs a query and shows the results", async () => {
    render(<SQLiteViewer path="app.db" />);
    await screen.findByText("users");
    fireEvent.click(screen.getByRole("button", { name: "query" }));

    const editor = screen.getByLabelText("SQL query");
    fireEvent.change(editor, { target: { value: "SELECT count(*) AS n FROM users" } });
    fireEvent.click(screen.getByTestId("sqlite-run-query"));

    await waitFor(() => expect(dbQuery).toHaveBeenCalledWith("app.db", "SELECT count(*) AS n FROM users", expect.anything()));
    const grid = await screen.findByTestId("sqlite-query-grid");
    // Scoped: "2" also appears as the users table's row-count badge.
    expect(within(grid).getByText("2")).toBeTruthy();
  });

  it("surfaces a query error (e.g. a rejected write)", async () => {
    dbQuery.mockRejectedValueOnce(new Error("This preview is read-only; writing is not enabled yet."));
    render(<SQLiteViewer path="app.db" />);
    await screen.findByText("users");
    fireEvent.click(screen.getByRole("button", { name: "query" }));
    fireEvent.click(screen.getByTestId("sqlite-run-query"));

    const err = await screen.findByTestId("sqlite-query-error");
    // Scoped: the footer also says "read-only preview".
    expect(within(err).getByText(/read-only/)).toBeTruthy();
  });

  it("renders the fallback for a non-SQLite .db file", async () => {
    dbInfo.mockResolvedValueOnce({ is_sqlite: false, path: "notes.db" });
    render(<SQLiteViewer path="notes.db" />);
    expect(await screen.findByTestId("sqlite-fallback")).toBeTruthy();
  });

  it("refetches on a revision bump without clearing the query editor", async () => {
    const { rerender } = render(<SQLiteViewer path="app.db" revision={0} />);
    await screen.findByText("users");
    fireEvent.click(screen.getByRole("button", { name: "query" }));
    fireEvent.change(screen.getByLabelText("SQL query"), { target: { value: "SELECT 42" } });

    dbInfo.mockClear();
    rerender(<SQLiteViewer path="app.db" revision={1} />);

    await waitFor(() => expect(dbInfo).toHaveBeenCalledTimes(1));
    // The unsaved query text survives the refresh.
    expect((screen.getByLabelText("SQL query") as HTMLTextAreaElement).value).toBe("SELECT 42");
  });
});

describe("SQLiteViewer writes (P2–P4)", () => {
  it("inserts a row through the editor dialog", async () => {
    dbRow.mockResolvedValue({ rows_affected: 1, elapsed_ms: 1 });
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("users"));
    await screen.findByText("alice");

    fireEvent.click(screen.getByTestId("sqlite-add-row"));
    const nameField = await screen.findByTestId("db-field-name");
    fireEvent.change(nameField, { target: { value: "carol" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    await waitFor(() =>
      expect(dbRow).toHaveBeenCalledWith(
        "app.db",
        "insert",
        "users",
        expect.objectContaining({ values: expect.objectContaining({ name: "carol" }) }),
      ),
    );
  });

  it("deletes a row only after the confirmation dialog", async () => {
    dbRow.mockResolvedValue({ rows_affected: 1, elapsed_ms: 1 });
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("users"));
    await screen.findByText("alice");

    fireEvent.click(screen.getByLabelText("Delete row 1"));
    // Nothing is sent until the destructive action is confirmed.
    expect(dbRow).not.toHaveBeenCalled();

    fireEvent.click(await screen.findByTestId("sqlite-confirm-action"));
    await waitFor(() =>
      expect(dbRow).toHaveBeenCalledWith(
        "app.db",
        "delete",
        "users",
        expect.objectContaining({ key: { id: 1 } }),
      ),
    );
  });

  it("escalates a 409 query to a confirmed write", async () => {
    dbQuery.mockRejectedValueOnce(new ApiError("This statement needs confirmation to run.", 409));
    dbExec.mockResolvedValue({ rows_affected: 3, elapsed_ms: 1 });
    render(<SQLiteViewer path="app.db" />);
    await screen.findByText("users");
    fireEvent.click(screen.getByRole("button", { name: "query" }));
    fireEvent.change(screen.getByLabelText("SQL query"), {
      target: { value: "DELETE FROM users" },
    });
    fireEvent.click(screen.getByTestId("sqlite-run-query"));

    // The 409 opens the confirm dialog and does NOT auto-run the write.
    expect(dbExec).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByTestId("sqlite-confirm-action"));
    await waitFor(() =>
      expect(dbExec).toHaveBeenCalledWith("app.db", "DELETE FROM users", expect.anything()),
    );
  });

  it("adds a column from the schema tab", async () => {
    dbSchema.mockResolvedValue({ rows_affected: 0, elapsed_ms: 1 });
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("users"));
    await screen.findByText("alice");
    fireEvent.click(screen.getByRole("button", { name: "schema" }));

    fireEvent.click(await screen.findByTestId("sqlite-add-column"));
    fireEvent.change(await screen.findByTestId("db-col-name"), { target: { value: "age" } });
    fireEvent.click(screen.getByTestId("sqlite-schema-save"));

    await waitFor(() =>
      expect(dbSchema).toHaveBeenCalledWith(
        "app.db",
        "add_column",
        expect.objectContaining({
          table: "users",
          column: expect.objectContaining({ name: "age" }),
        }),
      ),
    );
  });

  it("does not offer row editing for a view", async () => {
    dbTable.mockResolvedValue({
      schema: { ...TABLE_RESPONSE.schema, name: "user_view", type: "view", rowid: false },
      result: TABLE_RESPONSE.result,
    });
    render(<SQLiteViewer path="app.db" />);
    fireEvent.click(await screen.findByText("user_view"));
    await screen.findByText("alice");

    // A view has no key, so the grid stays read-only.
    expect(screen.queryByTestId("sqlite-add-row")).toBeNull();
    expect(screen.queryByLabelText("Delete row 1")).toBeNull();
  });

  it("renders a table whose index, foreign-key and row lists are all empty", async () => {
    // The verbatim body `GET /api/db/table` returned for a table with no index
    // and no foreign key (captured from a live server), which is the response
    // that used to crash this component: the server emitted
    // "indexes": null / "foreign_keys": null / "rows": null and the viewer
    // dereferenced them unguarded.
    dbInfo.mockResolvedValueOnce({
      is_sqlite: true,
      path: "bare.db",
      tables: [{ name: "notes", type: "table", rows: 0 }],
    });
    dbTable.mockResolvedValue({
      schema: {
        name: "notes",
        type: "table",
        columns: [
          { name: "id", decl_type: "INTEGER", not_null: false, pk: 1, generated: false },
          { name: "body", decl_type: "TEXT", not_null: false, pk: 0, generated: false },
        ],
        indexes: [],
        foreign_keys: [],
        ddl: "CREATE TABLE notes(id INTEGER PRIMARY KEY, body TEXT)",
        rowid: true,
      },
      result: {
        columns: [
          { name: "id", decl_type: "INTEGER" },
          { name: "body", decl_type: "TEXT" },
        ],
        rows: [],
        row_count: 0,
        truncated: false,
        elapsed_ms: 0,
      },
    });
    render(<SQLiteViewer path="bare.db" />);
    fireEvent.click(await screen.findByText("notes"));

    // Data tab: an empty row page is a state, not a crash.
    expect(await screen.findByText(/no rows/i)).toBeTruthy();

    // Schema tab: the columns and DDL render; the empty Indexes and Foreign
    // keys sections are omitted rather than throwing.
    fireEvent.click(screen.getByRole("button", { name: "schema" }));
    const schema = await screen.findByTestId("sqlite-schema");
    expect(within(schema).getByText(/CREATE TABLE notes/)).toBeTruthy();
    expect(within(schema).getByText("id")).toBeTruthy();
    expect(within(schema).queryByText("Foreign keys")).toBeNull();
    expect(within(schema).queryByText("Indexes")).toBeNull();
  });
});
