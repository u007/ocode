import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DBPanel from "./DBPanel";
import { api } from "../../api/client";

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
const mockAdd = vi.mocked(api.dbConnectAdd);
const mockUnlock = vi.mocked(api.dbConnectUnlock);
const mockLock = vi.mocked(api.dbConnectLock);
const mockTables = vi.mocked(api.dbConnectTables);

const SURFACE = "db-panel:session-1";

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockLock.mockResolvedValue(undefined);
  mockTables.mockResolvedValue({ tables: [], hasMore: false });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("DBPanel environment-backed connections", () => {
  it("marks an environment-backed connection with an env badge", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "prod", driver: "postgres", unlocked: true, fromEnv: true }],
    });
    render(<DBPanel sessionId="session-1" />);

    const badge = await screen.findByTitle(/comes from an environment variable/);
    expect(badge.textContent).toBe("env");
  });

  it("does not mark a literal connection as env-backed", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "prod", driver: "postgres", unlocked: true, fromEnv: false }],
    });
    render(<DBPanel sessionId="session-1" />);

    await screen.findByText("prod", { selector: "span" });
    expect(screen.queryByTitle(/comes from an environment variable/)).toBeNull();
  });

  it("sends the session id when unlocking so the project .env is used", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "env", driver: "postgres", unlocked: false, fromEnv: true }],
    });
    mockUnlock.mockResolvedValue({ unlocked: ["env"] });
    render(<DBPanel sessionId="session-1" />);

    fireEvent.change(await screen.findByLabelText("Master password"), {
      target: { value: "master-pw" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Unlock/ }));

    await waitFor(() => {
      expect(mockUnlock).toHaveBeenCalledWith(SURFACE, "master-pw", "session-1");
    });
  });

  it("sends the session id when saving a connection", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "env" });
    render(<DBPanel sessionId="session-1" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "env" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "${DATABASE_URL}" },
    });
    fireEvent.change(screen.getByLabelText("Master password (encrypts the URL)"), {
      target: { value: "master-pw" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith("env", "${DATABASE_URL}", "master-pw", "session-1");
    });
  });

  it("explains that the URL may come from the environment", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel sessionId="session-1" />);

    await screen.findByText("Add a PostgreSQL connection");
    const hint = screen.getByText(/read the URL from the environment/);
    expect(hint.textContent).toContain(".env");
  });
});
