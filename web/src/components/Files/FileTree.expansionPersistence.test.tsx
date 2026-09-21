import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FileTree from "./FileTree";
import { fileTreeRootKey, saveExpandedDirs, FILE_TREE_EXPANSION_STORAGE_KEY } from "./fileTreeExpansionPersistence";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: { ...actual.api, getPathsConfig: vi.fn(async () => ({ extra_allowed_paths: [] })) },
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

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const A_ROOT = { children: [{ name: "asrc", path: "asrc", is_dir: true }, { name: "a.txt", path: "a.txt", is_dir: false }], truncated: false };
const B_ROOT = { children: [{ name: "bsrc", path: "bsrc", is_dir: true }, { name: "b.txt", path: "b.txt", is_dir: false }], truncated: false };

function mockFetch(opts: { asrcStatus?: number } = {}) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (!url.includes("/api/files/tree")) return json({});
    const u = new URL(url, "http://test");
    const path = u.searchParams.get("path") ?? "";
    if (path.endsWith("/asrc")) {
      if (opts.asrcStatus && opts.asrcStatus >= 400) return json({ error: "directory not found" }, opts.asrcStatus);
      return json({ children: [{ name: "a-child.ts", path: "asrc/a-child.ts", is_dir: false }], truncated: false });
    }
    if (path.endsWith("/bsrc")) {
      return json({ children: [{ name: "b-child.ts", path: "bsrc/b-child.ts", is_dir: false }], truncated: false });
    }
    return json(path.includes("/projA") ? A_ROOT : B_ROOT);
  });
}

beforeAll(() => {
  if (!(globalThis as any).PointerEvent) (globalThis as any).PointerEvent = MouseEvent;
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

describe("FileTree expansion persistence", () => {
  let fetchSpy: ReturnType<typeof mockFetch>;

  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy = mockFetch();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    window.localStorage.clear();
  });

  it("restores a folder's expansion after switching away and back", async () => {
    const onOpenFile = vi.fn();
    const { rerender } = render(<FileTree onOpenFile={onOpenFile} projectPath="/projA" />);
    fireEvent.click(await screen.findByText("asrc"));
    expect(await screen.findByText("a-child.ts")).toBeDefined();

    // Persisted under project A's root key.
    expect(window.localStorage.getItem(FILE_TREE_EXPANSION_STORAGE_KEY)).toContain("/projA");

    rerender(<FileTree onOpenFile={onOpenFile} projectPath="/projB" />);
    await screen.findByText("b.txt");
    expect(screen.queryByText("a-child.ts")).toBeNull();
    // Project B has its own (empty) expansion — A's must not leak in.
    expect(screen.queryByText("b-child.ts")).toBeNull();

    rerender(<FileTree onOpenFile={onOpenFile} projectPath="/projA" />);
    // No click: expansion is restored from persistence.
    expect(await screen.findByText("a-child.ts")).toBeDefined();
  });

  it("collapses a persisted folder that no longer exists without erroring", async () => {
    fetchSpy.mockRestore();
    fetchSpy = mockFetch({ asrcStatus: 404 });
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    saveExpandedDirs(fileTreeRootKey("/projA", undefined, "/projA"), ["asrc"]);

    render(<FileTree onOpenFile={vi.fn()} projectPath="/projA" />);
    await screen.findByText("asrc");

    // The stale entry is pruned and the node collapses; no error is surfaced.
    await waitFor(() =>
      expect(window.localStorage.getItem(FILE_TREE_EXPANSION_STORAGE_KEY) ?? "").not.toContain('"asrc"'),
    );
    expect(screen.queryByText("a-child.ts")).toBeNull();
    expect(errSpy).not.toHaveBeenCalledWith("File tree children error:", expect.anything());

    errSpy.mockRestore();
    warnSpy.mockRestore();
  });
});
