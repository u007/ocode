import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
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
  it("uses group order for the collapsed rail while keeping collapsed-group projects", () => {
    const projects = [
      project("/ungrouped", ""),
      project("/second", "second"),
      project("/first", "first"),
    ];
    const groups: ProjectGroup[] = [
      { name: "first", order: 2, collapsed: false },
      { name: "second", order: 1, collapsed: true },
    ];

    const { orderedProjects, visibleItems } = buildProjectSidebarOrder(projects, groups);

    expect(orderedProjects.map((p) => p.path)).toEqual([
      "/second",
      "/first",
      "/ungrouped",
    ]);
    expect(
      visibleItems
        .filter((item) => item.type === "project")
        .map((item) => (item.data as Project).path),
    ).toEqual(["/first", "/ungrouped"]);
    expect(orderedProjects.slice(0, 5).map((p) => p.path)).toEqual([
      "/second",
      "/first",
      "/ungrouped",
    ]);
  });

  it("groups interleaved projects by group while preserving order within each group", () => {
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

    const { orderedProjects, visibleItems } = buildProjectSidebarOrder(projects, groups);

    expect(orderedProjects.map((p) => p.path)).toEqual([
      "/A1",
      "/A2",
      "/B1",
      "/B2",
      "/U1",
    ]);
    // The expanded list shows the identical project sequence (plus headers).
    expect(
      visibleItems
        .filter((item) => item.type === "project")
        .map((item) => (item.data as Project).path),
    ).toEqual(["/A1", "/A2", "/B1", "/B2", "/U1"]);
  });

  it("handles empty inputs", () => {
    expect(buildProjectSidebarOrder([], []).orderedProjects).toEqual([]);
    expect(buildProjectSidebarOrder([], []).visibleItems).toEqual([]);
  });

  it("keeps projects whose group no longer exists at the end of the collapsed rail", () => {
    // Cannot normally happen (HandleDeleteGroup ungroups first), but the old
    // collapsed rail included every project; preserve that, while the expanded
    // view continues to omit orphans (no group header to render them under).
    const { orderedProjects, visibleItems } = buildProjectSidebarOrder(
      [project("/X", "gone")],
      [],
    );
    expect(orderedProjects.map((p) => p.path)).toEqual(["/X"]);
    expect(visibleItems).toEqual([]);
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
}));

const chatSessionsFake = vi.hoisted(() => ({} as Record<string, Partial<SessionSlice>>));

const terminalStateFake = vi.hoisted(() => ({
  byProject: {} as Record<string, { alerts?: Record<string, boolean> }>,
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
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: stateFake,
    ...actionsFake,
  }),
}));

vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: { sessions: Record<string, unknown> }) => unknown) =>
    sel({ sessions: chatSessionsFake }),
}));

vi.mock("../../stores/terminalStore", () => ({
  useTerminalState: () => ({
    state: terminalStateFake,
  }),
}));

function railLabels(): (string | null)[] {
  return screen
    .getAllByRole("button")
    .map((b) => b.getAttribute("aria-label"))
    .filter(Boolean);
}

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

  it("renders icons in the expanded list order, not the raw interleaved store order", () => {
    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);

    // First button is the expand toggle (no label). The rail must show the
    // first five projects in canonical order (A1, A2, B1, B2, U1) — the raw
    // array order [A1, B1, A2, B2, U1] would interleave the groups.
    expect(railLabels()).toEqual(["A1", "A2", "B1", "B2", "U1"]);
  });

  it("keeps projects of collapsed groups in the rail at their group position", () => {
    stateFake.groups = [
      { name: "B", order: 1, collapsed: false },
      { name: "A", order: 2, collapsed: true },
    ];

    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);

    expect(railLabels()).toEqual(["B1", "B2", "A1", "A2", "U1"]);
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
    terminalStateFake.byProject["/proj"] = { alerts: { t1: true } };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 terminal beep")).toBeDefined();
  });

  it("renders terminal beep even with no open chat sessions", () => {
    // Terminal-only project: no tabs, but has an alerted terminal
    terminalStateFake.byProject["/proj"] = { alerts: { t1: true } };
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    expect(screen.getByTitle("1 terminal beep")).toBeDefined();
    // Should NOT show session count (no sessions)
    expect(screen.queryByText("0")).toBeNull();
  });

  it("does not render badges when project has no activity", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // No badges rendered for an empty project
    expect(screen.queryByTitle(/session/)).toBeNull();
    expect(screen.queryByTitle(/streaming/)).toBeNull();
    expect(screen.queryByTitle(/pending/)).toBeNull();
    expect(screen.queryByTitle(/beep/)).toBeNull();
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
});
