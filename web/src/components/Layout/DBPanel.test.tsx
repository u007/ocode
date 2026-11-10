import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

// The per-mount suffix is a document tag plus a counter, so two browser windows
// (each restarting its counter at 1) cannot collide.
const SURFACE_RE = /^db-panel::\/proj:[a-z0-9]+-\d+$/;
const UNLOCKED = { connections: [{ name: "prod", driver: "postgres", unlocked: true, fromEnv: false }] };

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
    render(<DBPanel projectPath="/proj" />);

    expect(await screen.findByText("Add a PostgreSQL connection")).toBeTruthy();
    expect(screen.queryByLabelText("Connection")).toBeNull();
    expect(mockList).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), undefined);
  });

  it("keeps the master password out of the DOM after a successful unlock", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [{ name: "prod", driver: "postgres", unlocked: false, fromEnv: false }] })
      .mockResolvedValueOnce(UNLOCKED);
    mockUnlock.mockResolvedValue({ unlocked: ["prod"], failed: [] });
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel projectPath="/proj" />);
    const pw = await screen.findByLabelText("Master password");
    fireEvent.change(pw, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: "Unlock & connect" }));

    await waitFor(() =>
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "hunter2", undefined, "/proj"),
    );
    expect(await screen.findByRole("button", { name: "users" })).toBeTruthy();
    expect((screen.queryByLabelText("Master password") as HTMLInputElement | null)?.value ?? "").toBe("");
  });

  it("shows the server's unlock error verbatim and keeps the form", async () => {
    mockList.mockResolvedValue({ connections: [{ name: "prod", driver: "postgres", unlocked: false, fromEnv: false }] });
    mockUnlock.mockRejectedValue(new Error("master password does not open connection prod"));

    render(<DBPanel projectPath="/proj" />);
    fireEvent.change(await screen.findByLabelText("Master password"), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: "Unlock & connect" }));

    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "master password does not open connection prod",
    );
    expect(screen.getByRole("button", { name: "Unlock & connect" })).toBeTruthy();
  });

  it("explains that a locked URL is encrypted and needs the master password to connect", async () => {
    mockList.mockResolvedValue({ connections: [{ name: "prod", driver: "postgres", unlocked: false, fromEnv: false }] });

    render(<DBPanel projectPath="/proj" />);

    expect(
      await screen.findByText("The URL is encrypted and never shown again. Enter your master password to connect."),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Unlock & connect" })).toBeTruthy();
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

    render(<DBPanel projectPath="/proj" />);
    fireEvent.click(await screen.findByRole("button", { name: 'we"ird' }));

    await waitFor(() =>
      expect(mockRows).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "prod", 'we"ird', {
        sort: "",
        dir: "asc",
        filter: "",
        limit: 100,
        offset: 0,
      }, undefined),
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

    render(<DBPanel projectPath="/proj" />);
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

    render(<DBPanel projectPath="/proj" />);
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

    render(<DBPanel projectPath="/proj" />);
    fireEvent.change(await screen.findByLabelText("SQL"), { target: { value: sql } });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));

    expect(await screen.findByText("Run this write?")).toBeTruthy();
    expect(mockQuery).toHaveBeenCalledTimes(1);
    expect(mockQuery).toHaveBeenLastCalledWith(expect.stringMatching(SURFACE_RE), "prod", sql, false, undefined);

    fireEvent.click(screen.getByRole("button", { name: "Run and commit" }));

    await waitFor(() => expect(mockQuery).toHaveBeenCalledTimes(2));
    expect(mockQuery).toHaveBeenLastCalledWith(expect.stringMatching(SURFACE_RE), "prod", sql, true, undefined);
    expect(await screen.findByText("1 row(s) affected. Committed.")).toBeTruthy();
  });

  it("shows the rows a confirmed RETURNING write returns, with the count", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });
    mockQuery
      .mockRejectedValueOnce(new ApiError("this statement writes to the database", 409))
      .mockResolvedValueOnce({ columns: ["id"], rows: [[7]], truncated: false, rowsAffected: 1 });

    render(<DBPanel projectPath="/proj" />);
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

    render(<DBPanel projectPath="/proj" />);
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

    render(<DBPanel projectPath="/proj" />);
    fireEvent.click(await screen.findByRole("button", { name: "Load more tables" }));

    expect(await screen.findByRole("button", { name: "beta" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "alpha" })).toBeTruthy();
    expect(mockTables).toHaveBeenLastCalledWith(expect.stringMatching(SURFACE_RE), "prod", 100, 1, undefined);
    expect(screen.queryByRole("button", { name: "Load more tables" })).toBeNull();
  });

  it("collapses and restores the table list", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: "Hide table list" });
    fireEvent.click(toggle);

    expect(screen.getByRole("button", { name: "Show table list" })).toBeTruthy();
    expect(screen.getByTestId("dbconnect-table-pane").getAttribute("style")).toContain("width: 0");
  });

  it("stacks the table list above the data only when the panel is too narrow for both", async () => {
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel projectPath="/proj" />);
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

    render(<DBPanel projectPath="/proj" />);
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

    const { unmount } = render(<DBPanel projectPath="/proj" />);
    await screen.findByLabelText("SQL");
    unmount();

    expect(mockLock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), undefined);
  });

  it("locks a remote surface through its host once, on unmount and not on a re-render", async () => {
    const target = { host: "me@host", projectPath: "~/proj" };
    mockList.mockResolvedValue(UNLOCKED);
    mockTables.mockResolvedValue({ tables: [], hasMore: false });

    const { rerender, unmount } = render(<DBPanel projectPath="~/proj" projectHost="me@host" />);
    await screen.findByLabelText("SQL");
    rerender(<DBPanel projectPath="~/proj" projectHost="me@host" />);
    expect(mockLock).not.toHaveBeenCalled();
    unmount();

    expect(mockLock).toHaveBeenCalledTimes(1);
    expect(mockLock).toHaveBeenCalledWith(expect.stringMatching(/^db-panel:me@host:~\/proj:/), target);
  });

  it("refuses to render a remote project without its path rather than use the host's default project", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => render(<DBPanel projectHost="me@host" />)).toThrow(
      "db panel: remote project on me@host has no project path",
    );
    consoleError.mockRestore();
    expect(mockList).not.toHaveBeenCalled();
  });

  it("names the remote host and the HTTP status when the host does not serve the connector", async () => {
    mockList.mockRejectedValue(new ApiError("Not Found", 404));

    render(<DBPanel projectPath="~/proj" projectHost="me@host" />);

    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Remote host me@host answered HTTP 404 for the database connector: Not Found",
    );
  });

  it("adds a connection with the password, selects it, and unlocks it straight away", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValueOnce({ connections: [{ name: "stage", driver: "postgres", unlocked: false, fromEnv: false }] })
      .mockResolvedValue({ connections: [{ name: "stage", driver: "postgres", unlocked: true, fromEnv: false }] });
    mockAdd.mockResolvedValue({ name: "stage" });
    mockUnlock.mockResolvedValue({ unlocked: ["stage"], failed: [] });
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel projectPath="/proj" />);
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "postgres://app:pw@db/app" },
    });
    fireEvent.change(screen.getByLabelText("Create master password"), {
      target: { value: "master" },
    });
    fireEvent.change(screen.getByLabelText("Confirm master password"), {
      target: { value: "master" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() =>
      expect(mockAdd).toHaveBeenCalledWith("stage", "postgres://app:pw@db/app", "master", undefined, undefined),
    );
    await waitFor(() =>
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "master", undefined, "/proj"),
    );
    expect(await screen.findByRole("button", { name: "users" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Unlock & connect" })).toBeNull();
  });

  it("keeps the saved connection locked and shows the error when the auto-unlock fails", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValue({ connections: [{ name: "stage", driver: "postgres", unlocked: false, fromEnv: false }] });
    mockAdd.mockResolvedValue({ name: "stage" });
    mockUnlock.mockRejectedValue(new Error("master password does not open connection stage"));

    render(<DBPanel projectPath="/proj" />);
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "postgres://app:pw@db/app" },
    });
    fireEvent.change(screen.getByLabelText("Create master password"), { target: { value: "master" } });
    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "master" } });
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Connection stage was saved, but unlocking it failed: master password does not open connection stage. Unlock it manually with the master password.",
    );
    expect(mockAdd).toHaveBeenCalledWith("stage", "postgres://app:pw@db/app", "master", undefined, undefined);
    expect(screen.getByRole("button", { name: "Unlock & connect" })).toBeTruthy();
  });

  it("clears the add form and does not report a saved connection as a failed add when the auto-unlock fails", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [{ name: "prod", driver: "postgres", unlocked: false, fromEnv: false }] })
      .mockResolvedValue({
        connections: [
          { name: "prod", driver: "postgres", unlocked: false, fromEnv: false },
          { name: "stage", driver: "postgres", unlocked: false, fromEnv: false },
        ],
      });
    mockAdd.mockResolvedValue({ name: "stage" });
    mockUnlock.mockRejectedValue(new Error("master password does not open connection stage"));

    render(<DBPanel projectPath="/proj" />);
    const addForm = (await screen.findByText("Add another connection")).closest("details") as HTMLElement;
    fireEvent.click(within(addForm).getByText("Add another connection"));
    fireEvent.change(within(addForm).getByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(within(addForm).getByLabelText("Connection URL"), {
      target: { value: "postgres://app:pw@db/app" },
    });
    fireEvent.change(within(addForm).getByLabelText(/^Master password/), { target: { value: "master" } });
    fireEvent.click(within(addForm).getByRole("button", { name: "Save connection" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Connection stage was saved");
    expect(alert.textContent).not.toMatch(/failed to add|could not add/i);
    expect((within(addForm).getByLabelText("Name") as HTMLInputElement).value).toBe("");
    expect(mockAdd).toHaveBeenCalledTimes(1);
  });

  it("disables Save on first setup until the master password is confirmed", async () => {
    mockList.mockResolvedValue({ connections: [] });

    render(<DBPanel projectPath="/proj" />);
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "postgres://app:pw@db/app" },
    });
    fireEvent.change(screen.getByLabelText("Create master password"), { target: { value: "master" } });
    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "masterr" } });
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "master" } });
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(false);
    expect(mockAdd).not.toHaveBeenCalled();
  });

  it("asks to create and confirm the master password on first setup, with autofill kept out of the URL", async () => {
    mockList.mockResolvedValue({ connections: [] });

    render(<DBPanel projectPath="/proj" />);
    const url = await screen.findByLabelText("Connection URL");
    expect(url.getAttribute("data-1p-ignore")).toBe("true");
    expect(url.getAttribute("data-lpignore")).toBe("true");
    fireEvent.change(url, { target: { value: "postgres://app:pw@db/app" } });

    expect(screen.getByLabelText("Create master password").getAttribute("autocomplete")).toBe("new-password");
    expect(screen.getByLabelText("Confirm master password").getAttribute("autocomplete")).toBe("new-password");
    expect(screen.getByText(/Choose a new password now/)).toBeTruthy();
    expect(screen.queryByLabelText("Master password")).toBeNull();
  });

  it("asks for the password that must match the first one when adding another connection", async () => {
    mockList.mockResolvedValue({ connections: [{ name: "prod", driver: "postgres", unlocked: false, fromEnv: false }] });

    render(<DBPanel projectPath="/proj" />);
    const pw = await screen.findByLabelText("Master password (must match the one you created first)");
    expect(pw.getAttribute("autocomplete")).toBe("off");
    expect(screen.queryByLabelText("Create master password")).toBeNull();
    expect(screen.queryByLabelText("Confirm master password")).toBeNull();
  });

  it("loads an environment-backed connection from .env with no master password", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [{ name: "env2", driver: "postgres", unlocked: false, fromEnv: true }] })
      .mockResolvedValueOnce({ connections: [{ name: "env2", driver: "postgres", unlocked: true, fromEnv: true }] });
    mockUnlock.mockResolvedValue({ unlocked: ["env2"], failed: [] });
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel projectPath="/proj" />);

    const load = await screen.findByRole("button", { name: /Load from \.env/ });
    // Nothing is encrypted, so the encryption prompt must not appear.
    expect(screen.queryByLabelText("Master password")).toBeNull();
    fireEvent.click(load);

    await waitFor(() =>
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "", undefined, "/proj"),
    );
    expect(await screen.findByRole("button", { name: "users" })).toBeTruthy();
  });

  it("loads an environment-backed connection on a remote project through the host's server", async () => {
    // The host's server resolves the .env, so the unlock is routed there with the
    // remote project path, and the host's own grant is what the table reads use.
    const target = { host: "me@host", projectPath: "~/proj" };
    mockList
      .mockResolvedValueOnce({ connections: [{ name: "env", driver: "postgres", unlocked: false, fromEnv: true }] })
      .mockResolvedValueOnce({ connections: [{ name: "env", driver: "postgres", unlocked: true, fromEnv: true }] });
    mockUnlock.mockResolvedValue({ unlocked: ["env"], failed: [] });
    mockTables.mockResolvedValue({ tables: ["users"], hasMore: false });

    render(<DBPanel projectPath="~/proj" projectHost="me@host" />);

    // The surface is keyed by the host too, so the regex here includes it.
    const REMOTE_SURFACE_RE = /^db-panel:me@host:~\/proj:[a-z0-9]+-\d+$/;
    expect(mockList).toHaveBeenCalledWith(expect.stringMatching(REMOTE_SURFACE_RE), target);
    expect(await screen.findByText(/reads its URL from the/)).toBeTruthy();
    expect(screen.queryByText(/Not available for a remote project/)).toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: /Load from \.env/ }));

    await waitFor(() =>
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(REMOTE_SURFACE_RE), "", undefined, "~/proj", target),
    );
    expect(await screen.findByRole("button", { name: "users" })).toBeTruthy();
    await waitFor(() =>
      expect(mockTables).toHaveBeenCalledWith(expect.stringMatching(REMOTE_SURFACE_RE), "env", 100, 0, target),
    );
  });

  it("shows the connections that failed to unlock and keeps the granted ones", async () => {
    mockList
      .mockResolvedValueOnce({
        connections: [
          { name: "env", driver: "postgres", unlocked: false, fromEnv: true },
          { name: "bad", driver: "postgres", unlocked: false, fromEnv: true },
        ],
      })
      .mockResolvedValueOnce({
        connections: [
          { name: "env", driver: "postgres", unlocked: true, fromEnv: true },
          { name: "bad", driver: "postgres", unlocked: false, fromEnv: true },
        ],
      });
    mockUnlock.mockResolvedValue({
      unlocked: ["env"],
      failed: [{ name: "bad", error: 'environment variable "DB_URL" is not set' }],
    });
    mockTables.mockResolvedValue({ tables: [], hasMore: false });

    render(<DBPanel projectPath="/proj" />);
    fireEvent.click(await screen.findByRole("button", { name: /Load from \.env/ }));

    expect(await screen.findByText(/Some connections could not be unlocked/)).toBeTruthy();
    expect(screen.getByText(/DB_URL/)).toBeTruthy();
    // The granted half must actually be usable: the point of a per-connection
    // failure is that one bad template does not block the others.
    await waitFor(() =>
      expect(mockTables).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "env", 100, 0, undefined),
    );
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("gives two mounts on the same project different surfaces", async () => {
    // The server keys unlock grants by surface, so a shared id would let one
    // window's unmount revoke the other's grant (and close its pools). The
    // counter alone is per document, which is why the id carries a document tag.
    mockList.mockResolvedValue({ connections: [] });

    const first = render(<DBPanel projectPath="/proj" />);
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));
    const second = render(<DBPanel projectPath="/proj" />);
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));

    const firstSurface = mockList.mock.calls[0][0];
    const secondSurface = mockList.mock.calls[1][0];
    expect(firstSurface).toMatch(SURFACE_RE);
    expect(secondSurface).toMatch(SURFACE_RE);
    expect(firstSurface).not.toBe(secondSurface);

    first.unmount();
    second.unmount();
  });

  it("offers a retry when the connection list cannot be loaded", async () => {
    mockList
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce({ connections: [] });

    render(<DBPanel projectPath="/proj" />);

    expect(await screen.findByRole("alert")).toHaveTextContent("network down");
    expect(screen.queryByText("Add a PostgreSQL connection")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByText("Add a PostgreSQL connection")).toBeTruthy();
  });

  it("does not treat an invalid reference name as a template", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel projectPath="/proj" />);

    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "${1BAD}" } });

    // The server's IsTemplate requires a shell-valid name, so this stays a
    // literal URL and still needs the master password.
    expect((screen.getByLabelText("Create master password") as HTMLInputElement).disabled).toBe(false);
  });

  it("saves an environment URL without a master password", async () => {
    mockList
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValueOnce({ connections: [{ name: "env2", driver: "postgres", unlocked: false, fromEnv: true }] });
    mockAdd.mockResolvedValue({ name: "env2" });

    render(<DBPanel projectPath="/proj" />);
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "env2" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "${DATABASE_URL}" } });

    // The password field is disabled: there is nothing to encrypt.
    expect((screen.getByLabelText(/Master password/) as HTMLInputElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => expect(mockAdd).toHaveBeenCalledWith("env2", "${DATABASE_URL}", "", undefined, undefined));
    // An environment URL is loaded from .env, never auto-unlocked on save.
    expect(mockUnlock).not.toHaveBeenCalled();
  });
});
