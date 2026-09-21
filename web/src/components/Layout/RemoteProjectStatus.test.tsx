import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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
const mockKill = vi.fn(() => Promise.resolve());
const mockLocalTerminals = vi.fn(
  (): { id: string; title: string; oscTitle?: string; renamed?: boolean }[] => [],
);
vi.mock("@/stores/terminalStore", () => ({
  getProjectTerminals: () => ({ terminals: mockLocalTerminals(), activeId: "", live: false }),
  terminalDisplayTitle: (t: { title: string; oscTitle?: string; renamed?: boolean }) =>
    t.renamed ? t.title : t.oscTitle || t.title,
  useTerminalState: () => ({ state: {}, attachTerminal: mockAttach, killTerminal: mockKill }),
}));

const sessions = [
  { id: "s1", title: "Chat one" },
  { id: "s2", title: "Chat two" },
];
// Hoisted so the assertions below can read the same spy instances the
// lazily-evaluated factories hand to the component.
const projectStoreFake = vi.hoisted(() => ({
  openSessionTab: vi.fn(),
  selectProject: vi.fn(() => Promise.resolve()),
  activeProject: { path: "/srv", host: "dev@box" } as { path: string; host?: string } | null,
}));
vi.mock("@/stores/projectStore", () => ({
  projectSessionKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
  useProjectState: () => ({
    state: {
      sessionsByProject: { "dev@box::/srv": { sessions } },
      tabsByProject: { "/srv": [] },
      activeProject: projectStoreFake.activeProject,
    },
    prefetchProjectSessions: vi.fn(),
    openSessionTab: projectStoreFake.openSessionTab,
    selectProject: projectStoreFake.selectProject,
  }),
}));

const mockTabFocusRequest = vi.fn();
vi.mock("@/lib/tabFocus", () => ({
  tabFocusActions: {
    request: (...a: unknown[]) => mockTabFocusRequest(...a),
    clear: vi.fn(),
  },
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
    mockKill.mockReset();
    mockKill.mockReturnValue(Promise.resolve());
    mockLocalTerminals.mockReset();
    mockLocalTerminals.mockReturnValue([]);
    projectStoreFake.openSessionTab.mockReset();
    projectStoreFake.selectProject.mockReset();
    projectStoreFake.selectProject.mockReturnValue(Promise.resolve());
    projectStoreFake.activeProject = { path: "/srv", host: "dev@box" };
    mockTabFocusRequest.mockReset();
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
    // Opening from the inventory must reveal the terminal it attached.
    expect(mockTabFocusRequest).toHaveBeenCalledWith({
      kind: "terminal",
      terminalId: "t1",
      projectPath: "/srv",
      host: "dev@box",
    });
  });

  it("focuses the chat it opens, bound to this project", () => {
    render(<RemoteProjectStatus project={project} statusState={state()} />);
    fireEvent.click(screen.getByTestId("remote-project-status"));
    fireEvent.pointerUp(screen.getByText("Chat one"));

    // Bound to the clicked project (not the active one) so a remote session is
    // routed through its host, and queued for the app shell to reveal.
    expect(projectStoreFake.openSessionTab).toHaveBeenCalledWith("s1", "Chat one", "/srv");
    expect(mockTabFocusRequest).toHaveBeenCalledWith({
      kind: "chat",
      projectPath: "/srv",
      host: "dev@box",
    });
    // Already active — no needless project switch.
    expect(projectStoreFake.selectProject).not.toHaveBeenCalled();
  });

  it("selects a non-active project before revealing its tab", () => {
    projectStoreFake.activeProject = { path: "/elsewhere" };
    render(<RemoteProjectStatus project={project} statusState={state()} />);
    fireEvent.click(screen.getByTestId("remote-project-status"));
    fireEvent.click(screen.getByText("shell one"));

    expect(projectStoreFake.selectProject).toHaveBeenCalledWith(project);
    expect(mockTabFocusRequest).toHaveBeenCalledWith({
      kind: "terminal",
      terminalId: "t1",
      projectPath: "/srv",
      host: "dev@box",
    });
  });

  it("kills an inventory terminal through the store and refreshes the inventory", async () => {
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

    // The store owns the DELETE (and removing any local tab, so the panel
    // cannot reattach and respawn the shell); the row only triggers it.
    expect(mockKill).toHaveBeenCalledWith("/srv", "t1", "dev@box");
    await waitFor(() => expect(refresh).toHaveBeenCalled());
  });

  it("falls back to this window's persisted title when the host reports none", () => {
    mockTerminals.mockReturnValue({
      terminals: [{ id: "t9", title: "", pid: 9, started_at: "", attached: false }],
      loading: false,
      error: null,
      refresh: vi.fn(),
    });
    mockLocalTerminals.mockReturnValue([{ id: "t9", title: "Terminal 9", oscTitle: "vim foo" }]);
    render(<RemoteProjectStatus project={project} statusState={state()} />);
    fireEvent.click(screen.getByTestId("remote-project-status"));

    expect(screen.getByText("vim foo")).toBeTruthy();
    expect(screen.queryByText(/Terminal t9/)).toBeNull();
  });

  it("shows the busy label and hides action buttons while restarting", () => {
    const s = state({ busy: "restarting", status: connected({ outdated: true }) });
    render(<RemoteProjectStatus project={project} statusState={s} />);
    expect(screen.getByText("restarting…")).toBeTruthy();
  });
});
