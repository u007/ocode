import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import FileTree from "./FileTree";

const mocks = vi.hoisted(() => ({
  getPathsConfig: vi.fn(),
  revealInFileManager: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocks.getPathsConfig,
      revealInFileManager: mocks.revealInFileManager,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

// The real shadcn Dialog deadlocks jsdom/React; this suite never opens one but
// FileTree imports it, so stub it out (same as the other FileTree suites).
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

const TREE = {
  children: [
    { name: "src", path: "src", is_dir: true },
    { name: "a.ts", path: "a.ts", is_dir: false },
  ],
  truncated: false,
  is_git_repo: false,
};

function mockTree() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation(
    (async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/files/tree")) {
        return new Response(JSON.stringify(TREE), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify({}), { status: 404 });
    }) as any,
  );
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

describe("FileTree reveal in OS file manager", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.revealInFileManager.mockResolvedValue({ path: "x", status: "opened" });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  async function openRowMenu(rowName: string) {
    fireEvent.contextMenu(screen.getByText(rowName));
  }

  it("labels the action for macOS as Finder and calls the API with the path", async () => {
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "darwin" });
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    const item = await screen.findByRole("menuitem", { name: "Show in Finder" });
    fireEvent.click(item);

    await waitFor(() =>
      expect(mocks.revealInFileManager).toHaveBeenCalledWith("/proj/a.ts", "/proj"),
    );
  });

  it("uses 'Open in Finder' for a directory (folder opens directly)", async () => {
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "darwin" });
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("src");

    await openRowMenu("src");
    const item = await screen.findByRole("menuitem", { name: "Open in Finder" });
    fireEvent.click(item);

    await waitFor(() =>
      expect(mocks.revealInFileManager).toHaveBeenCalledWith("/proj/src", "/proj"),
    );
  });

  it("labels for Windows as Explorer", async () => {
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "windows" });
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    expect(await screen.findByRole("menuitem", { name: "Show in Explorer" })).toBeInTheDocument();
  });

  it("labels for Linux as File Manager", async () => {
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "linux" });
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    expect(await screen.findByRole("menuitem", { name: "Show in File Manager" })).toBeInTheDocument();
  });

  it("hides the action for a remote project (reveal runs on the local server)", async () => {
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "darwin" });
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" projectHost="ci.local" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    // The menu still opens (other actions present) but no reveal item.
    await screen.findByRole("menuitem", { name: "Copy path" });
    expect(screen.queryByRole("menuitem", { name: /Finder|Explorer|File Manager/ })).not.toBeInTheDocument();
  });
});
