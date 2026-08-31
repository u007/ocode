import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import FileTree from "./FileTree";

const mocks = vi.hoisted(() => ({
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: { ...actual.api, getPathsConfig: mocks.getPathsConfig },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) => (open ? <>{children}</> : null),
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

const ROOT = {
  children: [
    { name: "src", path: "src", is_dir: true },
    { name: "readme.md", path: "readme.md", is_dir: false },
  ],
  truncated: false,
  is_git_repo: false,
};

const SRC_CHILDREN = {
  children: [
    { name: "app.ts", path: "src/app.ts", is_dir: false },
    { name: "lib", path: "src/lib", is_dir: true },
  ],
  truncated: false,
};

function mockFetch() {
  return (vi.spyOn as any)(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/files/tree")) {
      if (url.includes("src%2Flib") || url.includes("src/lib")) {
        return new Response(JSON.stringify({ children: [], truncated: false }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.includes("src") && !url.includes("depth=0")) {
        if (url.includes(encodeURIComponent("src")) || url.includes("path=src")) {
          const u = new URL(url, "http://test");
          const path = u.searchParams.get("path") ?? "";
          if (path.endsWith("/src") || path === "src") {
            return new Response(JSON.stringify(SRC_CHILDREN), { status: 200, headers: { "Content-Type": "application/json" } });
          }
        }
      }
      return new Response(JSON.stringify(ROOT), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response(JSON.stringify({}), { status: 200 });
  });
}

describe("FileTree column view", () => {
  beforeEach(() => {
    window.localStorage.clear();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [] });
  });
  afterEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
  });

  it("renders list/column toggle and persists choice", async () => {
    const fetchSpy = mockFetch();
    const onOpenFile = vi.fn();
    render(<FileTree onOpenFile={onOpenFile} projectPath="/proj" />);
    // toggle group exists
    expect(await screen.findByRole("group", { name: /view mode/i })).toBeDefined();
    expect(screen.getByLabelText("List view")).toBeDefined();
    expect(screen.getByLabelText("Column view")).toBeDefined();

    // FileTree persists the default ("tree") on mount via useEffect, so the
    // initial value is "tree", not null. The meaningful assertion is that
    // toggling writes the chosen mode.
    expect(window.localStorage.getItem("ocode.ui.filetree_view.v1")).toBe("tree");

    fireEvent.click(screen.getByLabelText("Column view"));
    expect(window.localStorage.getItem("ocode.ui.filetree_view.v1")).toBe("columns");

    fireEvent.click(screen.getByLabelText("List view"));
    expect(window.localStorage.getItem("ocode.ui.filetree_view.v1")).toBe("tree");

    fetchSpy.mockRestore();
  });

  it("restores columns view from storage and renders Miller columns", async () => {
    window.localStorage.setItem("ocode.ui.filetree_view.v1", "columns");
    const fetchSpy = mockFetch();
    const onOpenFile = vi.fn();
    render(<FileTree onOpenFile={onOpenFile} projectPath="/proj" />);

    // root column should appear with src and readme
    expect(await screen.findByText("src")).toBeDefined();
    expect(screen.getByText("readme.md")).toBeDefined();

    // column headers exist (Root or proj)
    // The first column header shows project folder name
    expect(screen.getByTitle("/proj")).toBeDefined();

    fetchSpy.mockRestore();
  });

  it("drills into directory and shows next column", async () => {
    window.localStorage.setItem("ocode.ui.filetree_view.v1", "columns");
    const fetchSpy = mockFetch();
    const onOpenFile = vi.fn();
    render(<FileTree onOpenFile={onOpenFile} projectPath="/proj" />);

    const srcRow = await screen.findByText("src");
    fireEvent.click(srcRow);

    // next column should appear with src's children
    expect(await screen.findByText("app.ts")).toBeDefined();
    expect(screen.getByText("lib")).toBeDefined();

    // clicking a file opens editor and does not add another column
    fireEvent.click(screen.getByText("app.ts"));
    expect(onOpenFile).toHaveBeenCalledWith("src/app.ts", "/proj");

    fetchSpy.mockRestore();
  });

  it("falls back to list view while filtering even when columns mode is stored", async () => {
    window.localStorage.setItem("ocode.ui.filetree_view.v1", "columns");
    // for isFiltering we need fullTree (depth=0) to return children that match filter
    const fullTree = {
      children: [
        { name: "src", path: "src", is_dir: true, children: [{ name: "app.ts", path: "src/app.ts", is_dir: false }] },
        { name: "readme.md", path: "readme.md", is_dir: false },
      ],
      truncated: false,
    };
    const fetchSpy = (vi.spyOn as any)(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("depth=0")) return new Response(JSON.stringify(fullTree), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/files/tree")) return new Response(JSON.stringify(ROOT), { status: 200, headers: { "Content-Type": "application/json" } });
      return new Response(JSON.stringify({}), { status: 200 });
    });

    const onOpenFile = vi.fn();
    render(<FileTree onOpenFile={onOpenFile} projectPath="/proj" />);

    // wait for initial render
    await screen.findByText("src");

    // type filter keyword -> should switch to filtered list rendering, not columns
    const filterInput = screen.getByPlaceholderText("Filter by keywords...");
    fireEvent.change(filterInput, { target: { value: "app" } });

    // filtered result should show app.ts in list mode (not column headers duplication)
    expect(await screen.findByText("app.ts")).toBeDefined();

    fetchSpy.mockRestore();
  });
});
