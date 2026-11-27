// Integration check for the two surfaces that share a project session: the DB
// panel and the Logs panel. Both stay mounted (the shell hides the inactive
// one), so a DB query must survive a visit to Logs and back.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import DBPanel from "./DBPanel";
import LogPanel from "../Logs/LogPanel";

const appApi = vi.hoisted(() => ({
  dbConnectList: vi.fn(),
  dbConnectAdd: vi.fn(),
  dbConnectRemove: vi.fn(),
  dbConnectUnlock: vi.fn(),
  dbConnectLock: vi.fn(),
  dbConnectTables: vi.fn(),
  dbConnectQuery: vi.fn(),
  dbConnectRows: vi.fn(),
  dbConnectRow: vi.fn(),
  getLogs: vi.fn(),
  clearLogs: vi.fn(),
}));

vi.mock("../../api/client", () => {
  class ApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = "ApiError";
      this.status = status;
    }
  }
  const impl: Record<string, unknown> = { ...appApi };
  const client = new Proxy({} as Record<string, unknown>, {
    get: (target, prop: string) => {
      if (!(prop in target)) target[prop] = impl[prop] ?? vi.fn(async () => ({}));
      return target[prop];
    },
  });
  return { ApiError, api: client };
});

const logHandlers: Array<(env: { data: unknown }) => void> = [];
vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (_kind: string, fn: (env: { data: unknown }) => void) => {
      logHandlers.push(fn);
      return () => {
        const i = logHandlers.indexOf(fn);
        if (i >= 0) logHandlers.splice(i, 1);
      };
    },
    onReconnect: () => () => {},
    emit: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
    setProjects: vi.fn(),
    onHostsChange: () => () => {},
    currentHosts: () => [],
    setHosts: vi.fn(),
  },
}));

function Harness() {
  const [tab, setTab] = useState<"db" | "logs">("db");
  return (
    <div>
      <button type="button" onClick={() => setTab("db")}>DB tab</button>
      <button type="button" onClick={() => setTab("logs")}>Logs tab</button>
      <div style={{ display: tab === "db" ? undefined : "none" }}>
        <DBPanel projectPath="/proj" />
      </div>
      <div style={{ display: tab === "logs" ? undefined : "none" }}>
        <LogPanel active={tab === "logs"} sessionId="ses_1" />
      </div>
    </div>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  logHandlers.length = 0;
  localStorage.clear();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  window.matchMedia = ((media: string) => ({
    matches: false,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;

  let unlocked = false;
  appApi.dbConnectList.mockImplementation(async () => ({
    connections: [
      { name: "staging", driver: "postgres", unlocked, fromEnv: true, envFile: ".env.staging" },
    ],
  }));
  appApi.dbConnectUnlock.mockImplementation(async () => {
    unlocked = true;
    return { unlocked: ["staging"], failed: [] };
  });
  appApi.dbConnectLock.mockResolvedValue(undefined);
  appApi.dbConnectTables.mockResolvedValue({ tables: [], hasMore: false });
  appApi.dbConnectQuery.mockResolvedValue({
    columns: ["id"],
    rows: [[42]],
    truncated: false,
    rowsAffected: null,
  });
  appApi.getLogs.mockResolvedValue([{ kind: "AGENT", message: "connected to staging" }]);
  appApi.clearLogs.mockResolvedValue(undefined);
});

describe("DB panel and Logs panel in one project session", () => {
  it("keeps a working DB connection alive across a visit to the Logs tab", async () => {
    render(<Harness />);

    fireEvent.click(await screen.findByRole("button", { name: /Load from \.env/ }));
    await waitFor(() => expect(appApi.dbConnectUnlock).toHaveBeenCalledTimes(1));

    fireEvent.change(await screen.findByLabelText("SQL"), { target: { value: "SELECT 42" } });
    fireEvent.click(screen.getByRole("button", { name: /Run/ }));
    const result = await screen.findByTestId("dbconnect-result");
    expect(result).toHaveTextContent("42");

    fireEvent.click(screen.getByRole("button", { name: "Logs tab" }));
    expect(await screen.findByText(/connected to staging/)).toBeTruthy();
    await waitFor(() => expect(appApi.getLogs).toHaveBeenCalledWith("ses_1", undefined));

    fireEvent.click(screen.getByRole("button", { name: "DB tab" }));
    expect(screen.getByTestId("dbconnect-result")).toBe(result);
    expect(appApi.dbConnectUnlock).toHaveBeenCalledTimes(1);
  });
});
