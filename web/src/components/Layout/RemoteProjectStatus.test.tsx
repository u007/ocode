import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteHostStatusState } from "@/hooks/useRemoteHostStatus";
import type { Project } from "@/api/types";
import { RemoteProjectStatus } from "./RemoteProjectStatus";

const mockTerminals = vi.fn();
vi.mock("@/hooks/useRemoteTerminals", () => ({
  useRemoteTerminals: (...a: unknown[]) => mockTerminals(...a),
}));

vi.mock("@/lib/eventBus", () => ({ eventBus: { on: () => () => {} } }));

const authedFetchMock = vi.fn((..._args: unknown[]) => Promise.resolve({ ok: true, status: 204 }));
vi.mock("@/api/client", () => ({
  authedFetch: (...a: unknown[]) => authedFetchMock(...a),
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
}));

const mockAttach = vi.fn();
vi.mock("@/stores/terminalStore", () => ({
  getProjectTerminals: () => ({ terminals: [], activeId: "", live: false }),
  useTerminalState: () => ({ state: {}, attachTerminal: mockAttach }),
}));

const sessions = [
  { id: "s1", title: "Chat one" },
  { id: "s2", title: "Chat two" },
];
vi.mock("@/stores/projectStore", () => ({
  projectSessionKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
  useProjectState: () => ({
    state: {
      sessionsByProject: { "dev@box::/srv": { sessions } },
      tabsByProject: { "/srv": [] },
    },
    prefetchProjectSessions: vi.fn(),
    openSessionTab: vi.fn(),
  }),
}));

const project = { path: "/srv", host: "dev@box", name: "srv" } as unknown as Project;

const connected = (over: Partial<RemoteHostStatusState["status"]> = {}) => ({
  host: "dev@box",
  connected: true,
  version: "1.2.3",
  local_version: "2.0.0",
  outdated: false,
  pid: 1,
  ...over,
});

function state(over: Partial<RemoteHostStatusState> = {}): RemoteHostStatusState {
  return {
    status: connected(),
    loading: false,
    error: null,
    busy: "idle",
    refresh: vi.fn(),
    connect: vi.fn(),
    restart: vi.fn(),
    ...over,
  };
}

describe("RemoteProjectStatus", () => {
  beforeEach(() => {
    mockTerminals.mockReset();
    mockAttach.mockReset();
    mockTerminals.mockReturnValue({
      terminals: [
        { id: "t1", title: "shell one", pid: 1, started_at: "", attached: false },
        { id: "t2", title: "shell two", pid: 2, started_at: "", attached: true },
        { id: "t3", title: "shell three", pid: 3, started_at: "", attached: false },
      ],
      loading: false,
      error: null,
      refresh: vi.fn(),
    });
  });

  it("renders not connected with a Connect action when disconnected", () => {
    const s = state({ status: connected({ connected: false, version: "", pid: 0 }) });
    render(<RemoteProjectStatus project={project} statusState={s} />);
    expect(screen.getByText("not connected")).toBeTruthy();
    fireEvent.click(screen.getByText("Connect"));
    expect(s.connect).toHaveBeenCalledTimes(1);
  });

  it("renders version, chat and terminal counts when connected", () => {
    render(<RemoteProjectStatus project={project} statusState={state()} />);
    expect(screen.getByText(/1\.2\.3 · 2 chats · 3 terminals/)).toBeTruthy();
    expect(screen.queryByText("Restart")).toBeNull();
    expect(screen.queryByLabelText("outdated")).toBeNull();
  });

  it("shows an amber outdated marker and Restart only when outdated, and restarts on click", () => {
    const s = state({ status: connected({ version: "1.0.0", outdated: true }) });
    render(<RemoteProjectStatus project={project} statusState={s} />);
    expect(screen.getByLabelText("outdated")).toBeTruthy();
    fireEvent.click(screen.getByText("Restart"));
    expect(s.restart).toHaveBeenCalledTimes(1);
  });

  it("expands to chats and terminals, attaching a terminal by id", () => {
    render(<RemoteProjectStatus project={project} statusState={state()} />);
    fireEvent.click(screen.getByTestId("remote-project-status"));

    expect(screen.getByText("Chat one")).toBeTruthy();
    expect(screen.getByText("Chat two")).toBeTruthy();
    expect(screen.getByText("shell one")).toBeTruthy();

    fireEvent.click(screen.getByText("shell one"));
    expect(mockAttach).toHaveBeenCalledWith("/srv", "dev@box", "t1", "shell one");
  });

  it("kills an inventory terminal through the proxy with the project header", () => {
    const refresh = vi.fn();
    mockTerminals.mockReturnValue({
      terminals: [{ id: "t1", title: "shell one", pid: 1, started_at: "", attached: false }],
      loading: false,
      error: null,
      refresh,
    });
    render(<RemoteProjectStatus project={project} statusState={state()} />);
    fireEvent.click(screen.getByTestId("remote-project-status"));
    fireEvent.click(screen.getByLabelText("kill terminal t1"));

    expect(authedFetchMock).toHaveBeenCalledWith(
      `/api/remote/${encodeURIComponent("dev@box")}/api/terminal/t1`,
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("shows the busy label and hides action buttons while restarting", () => {
    const s = state({ busy: "restarting", status: connected({ outdated: true }) });
    render(<RemoteProjectStatus project={project} statusState={s} />);
    expect(screen.getByText("restarting…")).toBeTruthy();
  });
});
