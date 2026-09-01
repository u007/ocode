import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
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

const stateFake = vi.hoisted(() => ({
  projects: [] as Project[],
  groups: [] as ProjectGroup[],
  activeProject: null as Project | null,
  loading: false,
  tabsByProject: {} as Record<string, unknown[]>,
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: stateFake,
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
  }),
}));

vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: { sessions: Record<string, unknown> }) => unknown) =>
    sel({ sessions: {} }),
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