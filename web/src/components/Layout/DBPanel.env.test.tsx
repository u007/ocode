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

const SURFACE_RE = /^db-panel::\/proj:p\d+$/;

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
    render(<DBPanel projectPath="/proj" />);

    const badge = await screen.findByTitle(/read from this project's \.env/);
    expect(badge.textContent).toBe("env");
  });

  it("does not mark a literal connection as env-backed", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "prod", driver: "postgres", unlocked: true, fromEnv: false }],
    });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("prod", { selector: "span" });
    expect(screen.queryByTitle(/read from this project's \.env/)).toBeNull();
  });

  it("loads an environment connection from the project .env with no password", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "env", driver: "postgres", unlocked: false, fromEnv: true }],
    });
    mockUnlock.mockResolvedValue({ unlocked: ["env"], failed: [] });
    render(<DBPanel projectPath="/proj" />);

    const load = await screen.findByRole("button", { name: /Load from \.env/ });
    expect(screen.queryByLabelText("Master password")).toBeNull();
    fireEvent.click(load);

    await waitFor(() => {
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "", "/proj");
    });
  });

  it("sends the project path when saving an environment connection", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "env" });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "env" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "${DATABASE_URL}" },
    });
    // An environment URL is not a secret: the password is not required and the
    // field is disabled rather than demanding an encryption password.
    expect((screen.getByLabelText(/Master password/) as HTMLInputElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith("env", "${DATABASE_URL}", "");
    });
  });

  it("explains that the URL may come from the environment", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    const hint = screen.getByText(/read the URL from the/);
    expect(hint.textContent).toContain(".env");
  });
});
