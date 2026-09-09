import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FileTree from "./FileTree";

const mocks = vi.hoisted(() => ({
  fsDelete: vi.fn(),
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocks.getPathsConfig,
      fsDelete: mocks.fsDelete,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

// Root has two dirs and a file. "kept" stays untouched; "gone" is deleted via
// the file tree's own delete flow, which triggers a refresh().
const ROOT = {
  children: [
    { name: "kept", path: "kept", is_dir: true },
    { name: "gone", path: "gone", is_dir: true },
    { name: "readme.md", path: "readme.md", is_dir: false },
  ],
  truncated: false,
  is_git_repo: false,
};

const KEPT_CHILDREN = {
  children: [{ name: "inner.ts", path: "kept/inner.ts", is_dir: false }],
  truncated: false,
};

function mockFetch() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation((async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/files/tree")) {
      const u = new URL(url, "http://test");
      const path = u.searchParams.get("path") ?? "";
      if (path.endsWith("/kept") || path === "kept") {
        return new Response(JSON.stringify(KEPT_CHILDREN), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify(ROOT), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }
    return new Response(JSON.stringify({}), { status: 404 });
  }) as any);
}

beforeAll(() => {
  if (!(globalThis as any).PointerEvent) {
    (globalThis as any).PointerEvent = MouseEvent;
  }
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

describe("FileTree preserves expanded state across a delete-triggered refresh", () => {
  let fetchSpy: ReturnType<typeof mockFetch>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsDelete.mockResolvedValue({ success: true });
    fetchSpy = mockFetch();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("keeps an expanded sibling folder open and its children rendered after deleting another folder", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("kept");
    await screen.findByText("gone");

    // Expand "kept" — its children load lazily.
    fireEvent.click(screen.getByText("kept"));
    await screen.findByText("inner.ts");

    // Delete the sibling "gone" folder via the context menu + confirm dialog.
    fireEvent.contextMenu(screen.getByText("gone"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(mocks.fsDelete).toHaveBeenCalledWith(["gone"], "/proj"));

    // "kept" must still be expanded, showing its child, instead of the whole
    // tree collapsing back to depth 1 (the reported bug).
    await waitFor(() => expect(screen.getByText("inner.ts")).toBeInTheDocument());
    expect(screen.getByText("kept")).toBeInTheDocument();
  });
});
