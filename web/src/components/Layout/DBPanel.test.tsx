import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import DBPanel from "./DBPanel";
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
      dbConnectList: vi.fn(),
      dbConnectAdd: vi.fn(),
      dbConnectRemove: vi.fn(),
      dbConnectUnlock: vi.fn(),
      dbConnectLock: vi.fn(),
      dbConnectTables: vi.fn(),
      dbConnectQuery: vi.fn(),
      dbConnectRows: vi.fn(),
      dbConnectRow: vi.fn(),
    },
  };
});

const mockList = vi.mocked(api.dbConnectList);
const mockUnlock = vi.mocked(api.dbConnectUnlock);
const mockLock = vi.mocked(api.dbConnectLock);
const mockTables = vi.mocked(api.dbConnectTables);
const mockQuery = vi.mocked(api.dbConnectQuery);
const mockAdd = vi.mocked(api.dbConnectAdd);
const mockRows = vi.mocked(api.dbConnectRows);

const SURFACE = "db-panel:session-1";
const UNLOCKED = { connections: [{ name: "prod", driver: "postgres", unlocked: true }] };

// jsdom has no layout, so the panel's width is fed in through the observer it creates.
type ResizeCallback = (entries: { contentRect: { width: number } }[]) => void;
const observed: { target: Element; cb: ResizeCallback }[] = [];

class ResizeObserverStub {
  private readonly cb: ResizeCallback;
  constructor(cb: ResizeCallback) {
    this.cb = cb;
  }
  observe(target: Element) {
    observed.push({ target, cb: this.cb });
  }
  unobserve() {}
  disconnect() {}
}

function resizePanel(width: number) {
  const pane = screen.getByTestId("dbconnect-table-pane");
  act(() => {
    for (const o of observed) {
      if (o.target.contains(pane)) o.cb([{ contentRect: { width } }]);
    }
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  // The table list's collapsed state and width persist in localStorage between tests.
  localStorage.clear();
  mockLock.mockResolvedValue(undefined);
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
});

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  observed.length = 0;
});

describe("DBPanel", () => {
  it("shows only the add form when no connections are saved", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel sessionId="session-1" />);

    expect(await screen.findByText("Add a PostgreSQL connection")).toBeTruthy();
    expect(screen.queryByLabelText("Connection")).toBeNull();
    expect(mockList).toHaveBeenCalledWith(SURFACE);
  });

  it("keeps the master password out of the DOM after a successful unlock", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [{ name: "prod", driver: "postgres", unlocked: false }] })
      .mockResolvedValueOnce(UNLOCKED);
    mockUnlock.mockResolvedValue({ unlocked: ["prod"] });
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel sessionId="session-1" />);
    const pw = await screen.findByLabelText("Master password");
    fireEvent.change(pw, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /Unlock/ }));

    await waitFor(() => expect(mockUnlock).toHaveBeenCalledWith(SURFACE, "hunter2"));
    expect(await screen.findByRole("button", { name: "users" })).toBeTruthy();
    expect((screen.queryByLabelText("Master password") as HTMLInputElement | null)?.value ?? "").toBe("");
  });

  it("shows the server's unlock error verbatim and keeps the form", async () => {
    mockList.mockResolvedValue({ connections: [{ name: "prod", driver: "postgres", unlocked: false }] });
    mockUnlock.mockRejectedValue(new Error("master password does not open connection prod"));

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("Master password"), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /Unlock/ }));

    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "master password does not open connection prod",
    );
    expect(screen.getByRole("button", { name: /Unlock/ })).toBeTruthy();
  });

  it("opens a table in the Data view and reads it through the keyed browse endpoint", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: ['we"ird'], hasMore: false });
    mockRows.mockResolvedValue({
      columns: [
        { name: "id", type: "bigint", pk: 1 },
        { name: "meta", type: "jsonb", pk: 0 },
      ],
      rows: [["9007199254740993", { a: 1 }]],
      hasMore: false,
      primaryKey: ["id"],
    });

    render(<DBPanel sessionId="session-1" />);
    fireEvent.click(await screen.findByRole("button", { name: 'we"ird' }));

    await waitFor(() =>
      expect(mockRows).toHaveBeenCalledWith(SURFACE, "prod", 'we"ird', {
        sort: "",
        dir: "asc",
        filter: "",
        limit: 100,
        offset: 0,
      }),
    );
    expect(await screen.findByTestId("dbconnect-rows")).toBeTruthy();
    // The bigint arrives as its exact digits and is shown as-is.
    expect(screen.getByText("9007199254740993")).toBeTruthy();
    expect(screen.getByText('{"a":1}')).toBeTruthy();
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("shows the truncation notice for a query result on the Query tab", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });
    mockQuery.mockResolvedValue({
      columns: ["id", "meta"],
      rows: [[1, { a: 1 }]],
      truncated: true,
      rowsAffected: null,
    });

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("SQL"), { target: { value: "SELECT * FROM big" } });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));

    expect(await screen.findByTestId("dbconnect-result")).toBeTruthy();
    expect(screen.getByText('{"a":1}')).toBeTruthy();
    expect(screen.getByText("Showing the first 1000 rows.")).toBeTruthy();
  });

  it("renders a query error verbatim", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });
    mockQuery.mockRejectedValue(new Error('query: ERROR: relation "nope" does not exist (SQLSTATE 42P01)'));

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("SQL"), { target: { value: "SELECT * FROM nope" } });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));

    expect(await screen.findByText(/relation "nope" does not exist/)).toBeTruthy();
  });

  it("asks before a write, runs it only when confirmed, and reports the commit", async () => {
    const sql = "INSERT INTO users VALUES (3)";
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });
    mockQuery
      .mockRejectedValueOnce(new ApiError("this statement writes to the database", 409))
      .mockResolvedValueOnce({ columns: [], rows: [], truncated: false, rowsAffected: 1 });

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("SQL"), { target: { value: sql } });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));

    expect(await screen.findByText("Run this write?")).toBeTruthy();
    expect(mockQuery).toHaveBeenCalledTimes(1);
    expect(mockQuery).toHaveBeenLastCalledWith(SURFACE, "prod", sql, false);

    fireEvent.click(screen.getByRole("button", { name: "Run and commit" }));

    await waitFor(() => expect(mockQuery).toHaveBeenCalledTimes(2));
    expect(mockQuery).toHaveBeenLastCalledWith(SURFACE, "prod", sql, true);
    expect(await screen.findByText("1 row(s) affected. Committed.")).toBeTruthy();
  });

  it("shows the rows a confirmed RETURNING write returns, with the count", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });
    mockQuery
      .mockRejectedValueOnce(new ApiError("this statement writes to the database", 409))
      .mockResolvedValueOnce({ columns: ["id"], rows: [[7]], truncated: false, rowsAffected: 1 });

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("SQL"), {
      target: { value: "INSERT INTO users VALUES (7) RETURNING id" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Run and commit" }));

    expect(await screen.findByTestId("dbconnect-result")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
    expect(screen.getByText("1 row(s) returned. Committed.")).toBeTruthy();
  });

  it("cancelling the write confirmation never sends the confirmed request", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });
    mockQuery.mockRejectedValueOnce(new ApiError("this statement writes to the database", 409));

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("SQL"), { target: { value: "DELETE FROM users" } });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));

    expect(await screen.findByText("Run this write?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByText("Run this write?")).toBeNull());
    expect(mockQuery).toHaveBeenCalledTimes(1);
    expect(mockQuery.mock.calls.some((c) => c[3] === true)).toBe(false);
  });

  it("loads the next table page and appends it", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables
      .mockResolvedValueOnce({ tables: ["alpha"], hasMore: true })
      .mockResolvedValueOnce({ tables: ["beta"], hasMore: false });

    render(<DBPanel sessionId="session-1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Load more tables" }));

    expect(await screen.findByRole("button", { name: "beta" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "alpha" })).toBeTruthy();
    expect(mockTables).toHaveBeenLastCalledWith(SURFACE, "prod", 100, 1);
    expect(screen.queryByRole("button", { name: "Load more tables" })).toBeNull();
  });

  it("collapses and restores the table list", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel sessionId="session-1" />);
    const toggle = await screen.findByRole("button", { name: "Hide table list" });
    fireEvent.click(toggle);

    expect(screen.getByRole("button", { name: "Show table list" })).toBeTruthy();
    expect(screen.getByTestId("dbconnect-table-pane").getAttribute("style")).toContain("width: 0");
  });

  it("stacks the table list above the data only when the panel is too narrow for both", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel sessionId="session-1" />);
    const pane = await screen.findByTestId("dbconnect-table-pane");
    expect(pane.getAttribute("data-layout")).toBe("side");

    // Stacking is decided on the narrowest list: 120 + 4 + 320 = 444px.
    resizePanel(400);
    expect(pane.getAttribute("data-layout")).toBe("stacked");
    expect(screen.queryByRole("separator", { name: "Resize table list" })).toBeNull();

    resizePanel(600);
    expect(pane.getAttribute("data-layout")).toBe("side");
    expect(screen.getByRole("separator", { name: "Resize table list" })).toBeTruthy();
  });

  it("clamps a wide saved list to the room left beside the data, without changing the saved width", async () => {
    localStorage.setItem("ocode.ui.dbconnect-table-pane.width", "400");
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel sessionId="session-1" />);
    const pane = await screen.findByTestId("dbconnect-table-pane");
    resizePanel(600);

    // 600 - 4 handle - 320 data minimum leaves 276px for the list.
    expect(pane.getAttribute("data-layout")).toBe("side");
    expect(pane.getAttribute("style")).toContain("width: 276px");
    expect(localStorage.getItem("ocode.ui.dbconnect-table-pane.width")).toBe("400");
  });

  it("locks the surface when the panel unmounts", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });

    const { unmount } = render(<DBPanel sessionId="session-1" />);
    await screen.findByLabelText("SQL");
    unmount();

    expect(mockLock).toHaveBeenCalledWith(SURFACE);
  });

  it("adds a connection with the password and selects it", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValueOnce({ connections: [{ name: "stage", driver: "postgres", unlocked: false }] });
    mockAdd.mockResolvedValue({ name: "stage" });

    render(<DBPanel sessionId="session-1" />);
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "postgres://app:pw@db/app" },
    });
    fireEvent.change(screen.getByLabelText("Master password (encrypts the URL)"), {
      target: { value: "master" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => expect(mockAdd).toHaveBeenCalledWith("stage", "postgres://app:pw@db/app", "master"));
    expect(await screen.findByLabelText("Master password")).toBeTruthy();
  });
});
