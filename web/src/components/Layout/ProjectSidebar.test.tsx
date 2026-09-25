import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { SessionSlice } from "../../stores/chatStore";
import type { Project, ProjectGroup } from "../../api/types";
import ProjectSidebar, { buildProjectSidebarOrder } from "./ProjectSidebar";

const project = (path: string, group: string): Project => ({
  path,
  name: path.slice(1),
  added_at: "",
  last_used_at: "",
  order: 0,
  group,
});

describe("buildProjectSidebarOrder", () => {
  it("orders groups by group order while preserving persisted order within each group", () => {
    // Raw store order interleaves the two groups (backend List() sorts by a
    // global order/AddedAt, not by group). The canonical order must regroup.
    const projects = [
      project("/A1", "A"),
      project("/B1", "B"),
      project("/A2", "A"),
      project("/B2", "B"),
      project("/U1", ""),
    ];
    const groups: ProjectGroup[] = [
      { name: "A", order: 1, collapsed: false },
      { name: "B", order: 2, collapsed: false },
    ];

    const items = buildProjectSidebarOrder(projects, groups);

    // Project items (ignoring group headers) must be the canonical order.
    expect(
      items
        .filter((item) => item.type === "project")
        .map((item) => (item.data as Project).path),
    ).toEqual(["/A1", "/A2", "/B1", "/B2", "/U1"]);
    // Header positions: A before its projects, B before its projects.
    expect(items.map((item) => (item.type === "group" ? `group:${(item.data as ProjectGroup).name}` : (item.data as Project).path))).toEqual([
      "group:A",
      "/A1",
      "/A2",
      "group:B",
      "/B1",
      "/B2",
      "/U1",
    ]);
  });

  it("omits projects of collapsed groups, matching the expanded list", () => {
    const items = buildProjectSidebarOrder(
      [project("/A1", "A"), project("/B1", "B"), project("/U1", "")],
      [
        { name: "A", order: 1, collapsed: true },
        { name: "B", order: 2, collapsed: false },
      ],
    );
    expect(
      items
        .filter((item) => item.type === "project")
        .map((item) => (item.data as Project).path),
    ).toEqual(["/B1", "/U1"]);
  });

  it("handles empty inputs", () => {
    expect(buildProjectSidebarOrder([], [])).toEqual([]);
  });

  it("omits projects whose group no longer exists (no header to render them under)", () => {
    // Cannot normally happen (HandleDeleteGroup ungroups first), but a stale
    // dangling group must not resurrect a project in only one surface.
    expect(buildProjectSidebarOrder([project("/X", "gone")], [])).toEqual([]);
  });
});

// ── Collapsed rail component tests ─────────────────────────────────────────
// Fixtures simulate the backend's global-order array (interleaved across
// groups) so the regression is observable end-to-end in the DOM.

// ── Mutable mock state (hoisted, per-test configurable) ──────────────────────
const stateFake = vi.hoisted(() => ({
  projects: [] as Project[],
  groups: [] as ProjectGroup[],
  activeProject: null as Project | null,
  loading: false,
  tabsByProject: {} as Record<string, { id: string }[]>,
  // Read by the row's hover-prefetch handler; the real ProjectState always
  // carries this map (see its initialState), so the fixture must too.
  activeTabByProject: {} as Record<string, string | null>,
}));

const chatSessionsFake = vi.hoisted(() => ({} as Record<string, Partial<SessionSlice>>));

const terminalStateFake = vi.hoisted(() => ({
  byProject: {} as Record<
    string,
    { terminals?: { id: string }[]; alerts?: Record<string, boolean> }
  >,
}));

const actionsFake = vi.hoisted(() => ({
  selectProject: vi.fn(),
  addProject: vi.fn(),
  removeProject: vi.fn(),
  renameProject: vi.fn(),
  reorderProjects: vi.fn(),
  setProjectGroup: vi.fn(),
  createGroup: vi.fn(),
  deleteGroup: vi.fn(),
  renameGroup: vi.fn(),
  reorderGroups: vi.fn(),
  setGroupCollapsed: vi.fn(),
  prefetchProjectSessions: vi.fn(),
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: stateFake,
    ...actionsFake,
    openSessionTab: vi.fn(),
  }),
  projectSessionKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
}));

vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: { sessions: Record<string, unknown> }) => unknown) =>
    sel({ sessions: chatSessionsFake }),
}));

vi.mock("../../stores/terminalStore", () => ({
  useTerminalState: () => ({
    state: terminalStateFake,
    attachTerminal: vi.fn(),
    killTerminal: vi.fn(() => Promise.resolve()),
  }),
  getProjectTerminals: () => ({ terminals: [], activeId: "", live: false }),
}));

// The remote inventory reads the host's terminal list; stub it so a connected
// host renders a terminal row without a real fetch.
const remoteTerminalsFake = vi.hoisted(() => ({
  terminals: [] as { id: string; title: string; pid: number; started_at: string; attached: boolean }[],
}));
vi.mock("../../hooks/useRemoteTerminals", () => ({
  useRemoteTerminals: () => ({
    terminals: remoteTerminalsFake.terminals,
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}));

const remoteHostFake = vi.hoisted(() => ({
  connected: false,
}));

vi.mock("../../hooks/useRemoteHostStatus", () => ({
  useRemoteHostStatus: () => ({
    status: {
      host: "dev@box",
      connected: remoteHostFake.connected,
      version: "",
      local_version: "",
      outdated: false,
      pid: 0,
    },
    loading: false,
    error: null,
    busy: "idle",
    refresh: vi.fn(),
    connect: vi.fn(),
    restart: vi.fn(),
  }),
}));

// Git counts for the project list come from a shared store that fetches
// GET /api/git/status itself; stub it so a mounted row makes no real request
// (and so `enabled` — the remote cold-connect gate — is observable).
const gitCountsFake = vi.hoisted(() => ({
  calls: [] as Array<{ project: string; host?: string; enabled?: boolean }>,
  byKey: {} as Record<string, { staged: number; unstaged: number; total: number; isRepo: boolean }>,
}));

vi.mock("../../lib/projectGitCounts", () => {
  const zero = { staged: 0, unstaged: 0, total: 0, isRepo: false };
  return {
    NO_GIT_COUNTS: zero,
    useProjectGitCounts: (project: string, host?: string, enabled?: boolean) => {
      gitCountsFake.calls.push({ project, host, enabled });
      if (enabled === false) return zero;
      return gitCountsFake.byKey[`${host ?? ""}\u0000${project}`] ?? zero;
    },
  };
});

const prefetchSessionFake = vi.hoisted(() => vi.fn());
vi.mock("../../lib/sessionPrefetch", () => ({
  prefetchSession: (...a: unknown[]) => prefetchSessionFake(...a),
}));

function railLabels(): (string | null)[] {
  return screen
    .getAllByRole("button")
    .map((b) => b.getAttribute("aria-label"))
    .filter(Boolean);
}

/** The open confirm dialog, or null when none is open. */
function confirmDialog(): HTMLElement | null {
  return screen.queryByRole("dialog");
}

/** Click a confirm dialog's destructive button (default: "Remove"). */
function clickConfirm(name: RegExp = /^Remove$/): void {
  fireEvent.click(within(confirmDialog()!).getByRole("button", { name }));
}

// Every test starts from a clean git-count fixture and a disconnected remote
// host; individual tests opt in (e.g. set connected = true) after this runs.
beforeEach(() => {
  gitCountsFake.calls.length = 0;
  Object.keys(gitCountsFake.byKey).forEach((k) => delete gitCountsFake.byKey[k]);
  remoteHostFake.connected = false;
});

describe("ProjectSidebar collapsed rail", () => {
  beforeEach(() => {
    stateFake.projects = [
      project("/A1", "A"),
      project("/B1", "B"),
      project("/A2", "A"),
      project("/B2", "B"),
      project("/U1", ""),
      project("/U2", ""),
    ];
    stateFake.groups = [
      { name: "A", order: 1, collapsed: false },
      { name: "B", order: 2, collapsed: false },
    ];
    stateFake.activeProject = null;
  });

  it("renders every project in the expanded list order, uncapped", () => {
    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);

    // First button is the expand toggle (no label). The rail must show ALL
    // projects in canonical order (A1, A2, B1, B2, U1, U2) — the old rail
    // capped at five, and the raw array order would interleave the groups.
    expect(railLabels()).toEqual(["A1", "A2", "B1", "B2", "U1", "U2"]);
  });

  it("does not cap the rail: 12 projects are all rendered inside a scroll area", () => {
    stateFake.projects = Array.from({ length: 12 }, (_, i) => project(`/P${i}`, ""));
    stateFake.groups = [];

    const { container } = render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);

    expect(railLabels().filter((l) => l?.startsWith("P"))).toHaveLength(12);
    // The overflow is scrollable rather than clipped/hidden.
    expect(container.querySelector('[data-radix-scroll-area-viewport]')).not.toBeNull();
  });

  it("hides projects of collapsed groups, matching the expanded list", () => {
    stateFake.groups = [
      { name: "B", order: 1, collapsed: false },
      { name: "A", order: 2, collapsed: true },
    ];

    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);

    // Group A is collapsed, so A1/A2 are hidden exactly as in the expanded view.
    expect(railLabels()).toEqual(["B1", "B2", "U1", "U2"]);
  });

  it("shows the git changed-file count on a rail icon", () => {
    gitCountsFake.byKey["\u0000/U1"] = { staged: 0, unstaged: 4, total: 4, isRepo: true };

    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);

    const badge = screen.getByTitle("4 changed files (0 staged · 4 unstaged)");
    expect(badge.getAttribute("aria-label")).toBe("4 git changed files");
    expect(badge.textContent).toBe("4");
    // Clean siblings show no git badge at all.
    expect(screen.getAllByLabelText("4 git changed files")).toHaveLength(1);
  });
});

// ── Project indicator tests ──────────────────────────────────────────────────

function makeSessionSlice(overrides: Partial<SessionSlice> = {}): Partial<SessionSlice> {
  return {
    messages: [],
    live: [],
    isStreaming: false,
    error: null,
    pendingPermission: null,
    permissionQueue: [],
    pendingQuestion: null,
    totalMessages: 0,
    hasMore: false,
    loadingMore: false,
    initialized: true,
    collapsedRunIds: [],
    tuiStatus: null,
    turnActive: false,
    lastHeartbeatAt: null,
    bootstrapStage: null,
    turnStalled: false,
    statusLoading: false,
    wasInterrupted: false,
    ...overrides,
  };
}

describe("ProjectSidebar project indicators", () => {
  beforeEach(() => {
    stateFake.projects = [project("/proj", "")];
    stateFake.groups = [];
    stateFake.activeProject = null;
    stateFake.tabsByProject = {};
    // Reset mock state
    Object.keys(chatSessionsFake).forEach((k) => delete chatSessionsFake[k]);
    terminalStateFake.byProject = {};
  });

  it("renders session count badge in expanded row", () => {
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }, { id: "s2" }] };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // Should show session count badge with "2"
    expect(screen.getByText("2")).toBeDefined();
  });

  it("renders streaming badge when a session is streaming", () => {
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }] };
    chatSessionsFake["s1"] = makeSessionSlice({ isStreaming: true });
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // The streaming badge title should be present
    expect(screen.getByTitle("1 streaming")).toBeDefined();
  });

  it("renders stalled badge when a session is stalled, not streaming", () => {
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }] };
    chatSessionsFake["s1"] = makeSessionSlice({ turnActive: true, turnStalled: true });
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 stalled (streaming stopped)")).toBeDefined();
    // Should NOT also show streaming badge
    expect(screen.queryByTitle("1 streaming")).toBeNull();
  });

  it("renders pending permission badge", () => {
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }] };
    chatSessionsFake["s1"] = makeSessionSlice({
      pendingPermission: { tool: "bash", request_id: "r1" },
    });
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 pending permission")).toBeDefined();
  });

  it("renders pending question badge", () => {
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }] };
    chatSessionsFake["s1"] = makeSessionSlice({
      pendingQuestion: { request_id: "q1", questions: [] },
    });
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 pending permission")).toBeDefined();
  });

  it("renders terminal beep badge", () => {
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }] };
    terminalStateFake.byProject["/proj"] = { terminals: [{ id: "t1" }], alerts: { t1: true } };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 terminal beep")).toBeDefined();
  });

  it("renders terminal beep even with no open chat sessions", () => {
    // Terminal-only project: no tabs, but has an alerted terminal
    terminalStateFake.byProject["/proj"] = { terminals: [{ id: "t1" }], alerts: { t1: true } };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 terminal beep")).toBeDefined();
    // Should NOT show session count (no sessions)
    expect(screen.queryByText("0")).toBeNull();
  });

  it("renders terminal beep badge for a remote project keyed by host::path", () => {
    // The terminal store keys a remote project's entry by `host::path`; the
    // sidebar must read that same key or a remote shell's bell never surfaces.
    stateFake.projects = [remoteProject("/home/user/app", "devbox")];
    terminalStateFake.byProject["devbox::/home/user/app"] = {
      terminals: [{ id: "t1" }],
      alerts: { t1: true },
    };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 terminal beep")).toBeDefined();
  });

  it("ignores an alert stranded for a terminal that no longer exists", () => {
    // Defence in depth for the stuck-bell report: even if a stale entry slips
    // into the alert map, the badge must only count terminals that still exist.
    terminalStateFake.byProject["/proj"] = {
      terminals: [{ id: "alive" }],
      alerts: { gone: true },
    };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.queryByTitle("1 terminal beep")).toBeNull();
  });

  it("does not render badges when project has no activity", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // No badges rendered for an empty project
    expect(screen.queryByTitle(/session/)).toBeNull();
    expect(screen.queryByTitle(/streaming/)).toBeNull();
    expect(screen.queryByTitle(/pending/)).toBeNull();
    expect(screen.queryByTitle(/beep/)).toBeNull();
  });

  it("wraps the badge cluster below a narrow row instead of squeezing the label", () => {
    // jsdom has no layout engine, so this cannot assert geometry. It pins the
    // CSS contract that produces the wrap: the row is a wrapping flex line
    // (flex-wrap) and the label block holds a minimum width (min-w-[5rem])
    // so a crowded row drops the badges to a second line rather than
    // truncating the name to nothing. Verified in a real browser from
    // 160px-500px: no horizontal overflow at any width; 4-5 badges wrap at
    // <=240px while 1-3 stay on the right until ~200px.
    stateFake.tabsByProject = { "/proj": [{ id: "s1" }, { id: "s2" }] };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);

    const nameEl = screen.getByText("proj");
    const row = nameEl.closest('[role="button"]') as HTMLElement;
    const labelBlock = nameEl.parentElement as HTMLElement;
    const badgeCluster = screen.getByTitle("2 sessions open").parentElement as HTMLElement;

    expect(row.className).toContain("flex-wrap");
    expect(labelBlock.className).toContain("min-w-[5rem]");
    // The badge cluster must not shrink (it wraps as a unit, never compresses).
    expect(badgeCluster.className).toContain("shrink-0");
    // The label is a direct flex child of the row, so it participates in the
    // same wrap line as the badge cluster.
    expect(row.contains(badgeCluster)).toBe(true);
  });

  it("shows the git changed-files badge for a dirty project, even with no open session", () => {
    gitCountsFake.byKey["\u0000/proj"] = { staged: 1, unstaged: 2, total: 3, isRepo: true };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("3 changed files (1 staged · 2 unstaged)")).toBeDefined();
    expect(screen.getByLabelText("3 git changed files")).toBeDefined();
  });

  it("renders no git badge when the project has no changes", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.queryByTitle(/changed file/)).toBeNull();
    expect(screen.queryByLabelText(/git changed files/)).toBeNull();
  });

  it("asks for git counts only when the project's remote host is connected", () => {
    // GET /api/git/status?host= is the cold-connect path: a disconnected
    // host must never be dialed by rendering its row (sidebar hang guard).
    remoteHostFake.connected = false;
    stateFake.projects = [project("/proj", ""), remoteProject("/home/user/app", "devbox")];
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const remote = gitCountsFake.calls.filter((c) => c.host === "devbox");
    expect(remote.length).toBeGreaterThan(0);
    expect(remote.every((c) => c.enabled === false)).toBe(true);
    expect(gitCountsFake.calls.some((c) => !c.host && c.enabled === true)).toBe(true);
  });
});

// ── Project removal confirmation ────────────────────────────────────────────
// Removing a project is a one-click list edit with no undo, so every entry
// point (expanded context menu, expanded trash button, collapsed rail menu)
// must land in a confirm dialog first. Removing only rewrites projects.json
// (files/transcripts survive), and the dialog must say so rather than
// "This cannot be undone".
describe("ProjectSidebar project removal confirmation", () => {
  beforeEach(() => {
    stateFake.projects = [project("/proj", "")];
    stateFake.groups = [];
    stateFake.activeProject = null;
    stateFake.tabsByProject = {};
    Object.keys(chatSessionsFake).forEach((k) => delete chatSessionsFake[k]);
    terminalStateFake.byProject = {};
    actionsFake.removeProject.mockClear();
    actionsFake.removeProject.mockResolvedValue(undefined);
  });

  it("does not remove the project until the confirm dialog is accepted", async () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByText("proj"));
    fireEvent.click(screen.getByText("Remove"));

    // Dialog is up, nothing removed yet.
    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.removeProject).not.toHaveBeenCalled();

    clickConfirm();
    await waitFor(() =>
      expect(actionsFake.removeProject).toHaveBeenCalledWith("/proj", undefined),
    );
    // The dialog closes once the removal resolves, not before it starts.
    await waitFor(() => expect(confirmDialog()).toBeNull());
  });

  it("keeps the project when the confirm dialog is cancelled", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByText("proj"));
    fireEvent.click(screen.getByText("Remove"));
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Cancel" }));

    expect(actionsFake.removeProject).not.toHaveBeenCalled();
    expect(confirmDialog()).toBeNull();
  });

  it("shows the host and path of a remote project, and reassures that files are kept", () => {
    stateFake.projects = [remoteProject("/home/user/app", "devbox")];
    const { container } = render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // A remote row renders the name AND the host:path subtitle, both reading
    // "devbox:/home/user/app", so scope the right-click to the name node.
    fireEvent.contextMenu(
      container.querySelector(".group.relative .truncate.font-medium")!,
    );
    fireEvent.click(screen.getByText("Remove"));

    const dialog = within(confirmDialog()!);
    // Two projects can share a path, so the confirm must name host + path.
    expect(dialog.getByText("devbox:/home/user/app")).toBeDefined();
    // Removal rewrites projects.json only — say so instead of claiming it
    // cannot be undone.
    expect(dialog.getByText(/not deleted/i)).toBeDefined();
    expect(dialog.queryByText(/cannot be undone/i)).toBeNull();

    clickConfirm();
    expect(actionsFake.removeProject).toHaveBeenCalledWith("/home/user/app", "devbox");
  });

  it("the expanded row's trash button also confirms", async () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.click(screen.getByTitle("Remove proj from the project list"));

    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.removeProject).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() => expect(actionsFake.removeProject).toHaveBeenCalledTimes(1));
  });

  it("the collapsed rail's Remove also confirms", async () => {
    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByLabelText("proj"));
    fireEvent.click(screen.getByText("Remove"));

    // The rail is a separate render branch with no dialogs of its own; the
    // confirm must be reachable there too.
    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.removeProject).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() => expect(actionsFake.removeProject).toHaveBeenCalledTimes(1));
  });

  it("keeps the confirm open and shows the reason when the removal fails", async () => {
    // The store used to swallow this: the dialog closed as if the project were
    // gone, and the user was never told the removal did not happen.
    actionsFake.removeProject.mockRejectedValueOnce(new Error("remove project: 404"));
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByText("proj"));
    fireEvent.click(screen.getByText("Remove"));
    clickConfirm();

    await waitFor(() =>
      expect(within(confirmDialog()!).getByRole("alert").textContent).toContain("404"),
    );
    expect(confirmDialog()).not.toBeNull();
  });
});

// ── Group deletion confirmation ─────────────────────────────────────────────
// Deleting a group is a bulk edit: HandleDeleteGroup (handler_projects.go:505)
// ungroups EVERY project in the group before dropping it, so the confirm has to
// say how many projects move to Ungrouped and that moving them back is manual.
describe("ProjectSidebar group deletion confirmation", () => {
  beforeEach(() => {
    stateFake.projects = [
      project("/w1", "Work"),
      project("/w2", "Work"),
      project("/u1", ""),
    ];
    stateFake.groups = [{ name: "Work", order: 1, collapsed: false }];
    stateFake.activeProject = null;
    stateFake.tabsByProject = {};
    Object.keys(chatSessionsFake).forEach((k) => delete chatSessionsFake[k]);
    terminalStateFake.byProject = {};
    actionsFake.deleteGroup.mockClear();
    actionsFake.deleteGroup.mockResolvedValue(undefined);
  });

  /** Open the group header's context menu and click Delete group. */
  function requestGroupDelete() {
    fireEvent.contextMenu(screen.getByText("Work"));
    fireEvent.click(screen.getByText("Delete group"));
  }

  it("does not delete the group until the confirm is accepted", async () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();

    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.deleteGroup).not.toHaveBeenCalled();
    clickConfirm(/^Delete group$/);
    await waitFor(() => expect(actionsFake.deleteGroup).toHaveBeenCalledWith("Work"));
  });

  it("keeps the group when the confirm is cancelled", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Cancel" }));

    expect(actionsFake.deleteGroup).not.toHaveBeenCalled();
    expect(confirmDialog()).toBeNull();
  });

  it("says how many projects move to Ungrouped and that moving back is manual", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();

    const dialog = within(confirmDialog()!);
    // The group holds 2 of the 3 fixture projects.
    expect(dialog.getByText(/2 projects/i)).toBeDefined();
    expect(dialog.getByText(/one by one/i)).toBeDefined();
  });

  it("uses the singular for a one-project group", () => {
    stateFake.projects = [project("/w1", "Work"), project("/u1", "")];
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();

    const dialog = within(confirmDialog()!);
    expect(dialog.getByText(/Its 1 project will move to Ungrouped/i)).toBeDefined();
    // "move them one by one" is wrong for a single project.
    expect(dialog.getByText(/to put it back/i)).toBeDefined();
  });

  it("says there is nothing to move for an empty group", () => {
    // A group can legitimately end up empty (its projects were dragged out);
    // the copy must not claim N projects are being moved.
    stateFake.projects = [project("/u1", "")];
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();

    const dialog = within(confirmDialog()!);
    expect(dialog.getByText(/No projects are in this group/i)).toBeDefined();
    expect(dialog.queryByText(/Ungrouped/)).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the delete fails", async () => {
    actionsFake.deleteGroup.mockRejectedValueOnce(new Error("delete group: 404"));
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    clickConfirm(/^Delete group$/);

    await waitFor(() =>
      expect(within(confirmDialog()!).getByRole("alert").textContent).toContain("404"),
    );
    expect(confirmDialog()).not.toBeNull();
  });
});

// ── Remote project right-click edit tests ────────────────────────────────────
// The expanded row's context menu + inline rename is the edit entry point
// for SSH/WSL entries: right-click → Rename edits the display name inline,
// and group/remove actions must be scoped by (host, path).
// The store mock returns fresh vi.fn()s per call, so tests assert against
// the component's onRename/onRemove wiring via the row callbacks.

const remoteProject = (path: string, host: string, group = ""): Project => ({
  path,
  name: `${host}:${path}`,
  added_at: "",
  last_used_at: "",
  order: 0,
  group,
  host,
});

describe("ProjectSidebar remote right-click edit", () => {
  beforeEach(() => {
    stateFake.projects = [
      remoteProject("/home/user/app", "devbox"),
      remoteProject("/home/user/app", "wsl:Ubuntu"),
    ];
    stateFake.groups = [{ name: "g", order: 1, collapsed: false }];
    stateFake.activeProject = null;
    stateFake.tabsByProject = {};
    Object.keys(chatSessionsFake).forEach((k) => delete chatSessionsFake[k]);
    terminalStateFake.byProject = {};
    actionsFake.renameProject.mockClear();
  });

  it("typing a new name in the inline editor and pressing Enter calls renameProject with host scope", async () => {
    actionsFake.renameProject.mockResolvedValue(undefined);
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // Open context menu on the SSH row and click Rename.
    const nameNode = document.querySelector(".group.relative .truncate.font-medium");
    expect(nameNode?.textContent).toBe("devbox:/home/user/app");
    fireEvent.contextMenu(nameNode!);
    fireEvent.click(screen.getByText("Rename"));
    // Type a new name and press Enter.
    const input = screen.getByRole("textbox") as HTMLInputElement;
    expect(input.value).toBe("devbox:/home/user/app");
    fireEvent.change(input, { target: { value: "renamed-ssh" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });
    // The store action receives the new name with host scope.
    expect(actionsFake.renameProject).toHaveBeenCalledWith(
      "/home/user/app",
      "renamed-ssh",
      "devbox",
    );
  });

  it("rename error from the store is displayed inline", async () => {
    actionsFake.renameProject.mockRejectedValueOnce(new Error("server rejected rename"));
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const nameNode = document.querySelector(".group.relative .truncate.font-medium")!;
    fireEvent.contextMenu(nameNode);
    fireEvent.click(screen.getByText("Rename"));
    const input = screen.getByRole("textbox") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "renamed-ssh" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });
    await waitFor(() => expect(screen.getByText("server rejected rename")).toBeDefined());
    expect(actionsFake.renameProject).toHaveBeenCalledWith(
      "/home/user/app",
      "renamed-ssh",
      "devbox",
    );
  });

  it("right-click Rename on an SSH row opens the inline editor for that row only", () => {
    const { container } = render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // ContextMenu attaches onContextMenu to the `contents` wrapper around the
    // row, so fire on the row's name node (bubbles to the wrapper). The
    // host:path subtitle also matches the text, so scope to the name node.
    const nameNode = container.querySelector(".group.relative .truncate.font-medium");
    expect(nameNode?.textContent).toBe("devbox:/home/user/app");
    fireEvent.contextMenu(nameNode!);
    fireEvent.click(screen.getByText("Rename"));
    // Inline editor appears for the first (SSH) row only.
    const inputs = container.querySelectorAll("input");
    expect(inputs.length).toBe(1);
    expect((inputs[0] as HTMLInputElement).value).toBe("devbox:/home/user/app");
  });

  it("right-click Rename on a WSL row opens the inline editor with the WSL name", () => {
    const { container } = render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const nameNodes = container.querySelectorAll(".group.relative .truncate.font-medium");
    expect(nameNodes.length).toBe(2);
    fireEvent.contextMenu(nameNodes[1]);
    fireEvent.click(screen.getByText("Rename"));
    const inputs = container.querySelectorAll("input");
    expect(inputs.length).toBe(1);
    expect((inputs[0] as HTMLInputElement).value).toBe("wsl:Ubuntu:/home/user/app");
  });

  it("remote rows expose a host-scoped Move-to-group menu entry", () => {
    const { container } = render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // Both remotes share the path; the menu must still offer the group move.
    const nameNode = container.querySelector(".group.relative .truncate.font-medium");
    fireEvent.contextMenu(nameNode!);
    expect(screen.getByText('Move to "g"')).toBeDefined();
  });

  it("collapsed rail exposes Remove for a remote entry", () => {
    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);
    const railButton = screen.getByLabelText("devbox:/home/user/app");
    fireEvent.contextMenu(railButton);
    expect(screen.getByText("Remove")).toBeDefined();
  });

  it("collapsed rail exposes group moves for a grouped remote entry", () => {
    stateFake.projects = [remoteProject("/home/user/app", "devbox", "g")];
    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);
    const railButton = screen.getByLabelText("devbox:/home/user/app");
    fireEvent.contextMenu(railButton);
    expect(screen.getByText("Remove from group")).toBeDefined();
  });

  it("renders the remote status line only for host projects", () => {
    stateFake.projects = [remoteProject("/home/user/app", "devbox"), project("/local/app", "")];
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // One host row gets a status line; the local row does not.
    expect(screen.getAllByTestId("remote-project-status")).toHaveLength(1);
  });

  it("a host row's context menu exposes Restart remote server", () => {
    const { container } = render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const nameNode = container.querySelector(".group.relative .truncate.font-medium");
    fireEvent.contextMenu(nameNode!);
    expect(screen.getByText("Restart remote server")).toBeDefined();
  });
});

// ── Remote hover cold-connect guard ──────────────────────────────────────────
// Hovering a remote project row must NOT warm its session list while the host
// is unconnected: the fetch proxies to /api/remote/{host}/... which cold-connects
// the host server-side (SSH provision + server start + tunnel) and made the SPA
// appear hung. Warming resumes once the host reports connected.

describe("ProjectSidebar remote hover cold-connect guard", () => {
  beforeEach(() => {
    remoteHostFake.connected = false;
    prefetchSessionFake.mockClear();
    actionsFake.prefetchProjectSessions.mockClear();
    stateFake.projects = [remoteProject("/home/user/app", "devbox")];
    stateFake.groups = [];
    stateFake.activeProject = null;
    stateFake.tabsByProject = { "/home/user/app": [{ id: "s1" }] };
    stateFake.activeTabByProject = { "/home/user/app": "s1" };
  });

  it("does not prefetch a remote project's sessions on hover while disconnected", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const nameNode = document.querySelector(".group.relative .truncate.font-medium")!;
    fireEvent.mouseEnter(nameNode);
    expect(actionsFake.prefetchProjectSessions).not.toHaveBeenCalled();
  });

  it("prefetches a remote project's sessions on hover once the host is connected", () => {
    remoteHostFake.connected = true;
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const nameNode = document.querySelector(".group.relative .truncate.font-medium")!;
    fireEvent.mouseEnter(nameNode);
    expect(actionsFake.prefetchProjectSessions).toHaveBeenCalledWith(
      expect.objectContaining({ path: "/home/user/app", host: "devbox" }),
    );
  });

  it("still prefetches a local project on hover", () => {
    stateFake.projects = [project("/local/app", "")];
    stateFake.tabsByProject = { "/local/app": [{ id: "s1" }] };
    stateFake.activeTabByProject = { "/local/app": "s1" };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    const nameNode = document.querySelector(".group.relative .truncate.font-medium")!;
    fireEvent.mouseEnter(nameNode);
    expect(actionsFake.prefetchProjectSessions).toHaveBeenCalledWith(
      expect.objectContaining({ path: "/local/app" }),
    );
  });
});

// ── Mobile off-canvas drawer ─────────────────────────────────────────────────
// On phones the sidebar is a fixed left drawer with a scrim, not an inline
// column and not the 40px collapsed rail — otherwise it permanently eats a
// slice of an already narrow viewport and squeezes the session tab list.

describe("ProjectSidebar mobile drawer", () => {
  beforeEach(() => {
    stateFake.projects = [project("/one", ""), project("/two", "")];
    stateFake.groups = [];
    stateFake.activeProject = null;
    stateFake.loading = false;
    stateFake.tabsByProject = {};
    stateFake.activeTabByProject = {};
    actionsFake.selectProject.mockClear();
  });

  it("renders an off-canvas drawer (not the collapsed rail) when closed", () => {
    const { container } = render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} isMobile />);
    const drawer = container.querySelector(".fixed.inset-y-0.left-0");
    expect(drawer).not.toBeNull();
    expect(drawer!.className).toMatch(/-translate-x-full/);
    // Closed ⇒ no scrim, and the expanded body is still mounted for the slide.
    expect(container.querySelector(".fixed.inset-0")).toBeNull();
    expect(screen.getByText("Add project")).toBeDefined();
  });

  it("renders the drawer on-screen with a backdrop when open, and closes on backdrop tap", () => {
    const onToggle = vi.fn();
    const { container } = render(<ProjectSidebar isOpen onToggle={onToggle} isMobile />);
    const drawer = container.querySelector(".fixed.inset-y-0.left-0");
    expect(drawer!.className).toMatch(/translate-x-0/);
    const backdrop = container.querySelector(".fixed.inset-0.z-40");
    expect(backdrop).not.toBeNull();
    fireEvent.click(backdrop!);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("dismisses the drawer after selecting a project", () => {
    const onToggle = vi.fn();
    render(<ProjectSidebar isOpen onToggle={onToggle} isMobile />);
    fireEvent.click(screen.getByText("one"));
    expect(actionsFake.selectProject).toHaveBeenCalledTimes(1);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("dismisses the drawer when a tab is opened from a remote project's inventory", async () => {
    // Regression: the inventory's controls stop propagation, so the row's
    // onSelect (the only other drawer-dismiss path) never ran and the revealed
    // tab stayed hidden behind the drawer.
    remoteHostFake.connected = true;
    remoteTerminalsFake.terminals = [
      { id: "t1", title: "shell one", pid: 1, started_at: "", attached: false },
    ];
    stateFake.projects = [remoteProject("/srv", "dev@box")];
    const onToggle = vi.fn();
    render(<ProjectSidebar isOpen onToggle={onToggle} isMobile />);

    fireEvent.click(screen.getByTestId("remote-project-status"));
    fireEvent.click(await screen.findByText("shell one"));

    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("keeps the inline collapsed rail on desktop (no mobile prop)", () => {
    const { container } = render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);
    expect(container.querySelector(".fixed.inset-y-0.left-0")).toBeNull();
    expect(container.querySelector(".w-10")).not.toBeNull();
  });
});
