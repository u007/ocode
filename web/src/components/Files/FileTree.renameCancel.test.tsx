import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FileTree from "./FileTree";

const mocks = vi.hoisted(() => ({
  fsRename: vi.fn(),
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocks.getPathsConfig,
      fsRename: mocks.fsRename,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

// The real shadcn Dialog (Radix portal + focus guards) deadlocks jsdom/React 18
// in an infinite microtask loop. Render the dialog body inline for tests.
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) =>
    open ? <div data-testid="dialog-root">{children}</div> : <div data-testid="dialog-root" />,
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

const TREE = {
  children: [
    { name: "a.ts", path: "src/a.ts", is_dir: false },
  ],
  truncated: false,
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

describe("FileTree rename cancel regression", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsRename.mockResolvedValue({ success: true, path: "src/renamed.ts" });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("right-click → Rename → Cancel keeps dialog root mounted and page interactive", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    // Open context menu and click Rename.
    const row = screen.getByText("a.ts");
    fireEvent.contextMenu(row);
    await screen.findByRole("menuitem", { name: /rename/i });
    fireEvent.click(screen.getByRole("menuitem", { name: /rename/i }));

    // Dialog opens with rename input.
    expect(await screen.findByDisplayValue("a.ts")).toBeInTheDocument();

    // Cancel closes the dialog but keeps the root mounted (modal={false}).
    const cancelBtn = screen.getByRole("button", { name: "Cancel" });
    fireEvent.click(cancelBtn);

    await waitFor(() => {
      // The input should be gone but the dialog root should remain mounted.
      expect(screen.queryByDisplayValue("a.ts")).not.toBeInTheDocument();
      expect(screen.getByTestId("dialog-root")).toBeInTheDocument();
    });

    // No API call made.
    expect(mocks.fsRename).not.toHaveBeenCalled();

    // The page should still be interactive: we can open another context menu.
    fireEvent.contextMenu(row);
    await screen.findByRole("menuitem", { name: /rename/i });
    expect(screen.getByRole("menuitem", { name: /rename/i })).toBeInTheDocument();
  });

  it("Escape key also cancels and keeps the dialog root mounted", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    const row = screen.getByText("a.ts");
    fireEvent.contextMenu(row);
    await screen.findByRole("menuitem", { name: /rename/i });
    fireEvent.click(screen.getByRole("menuitem", { name: /rename/i }));

    const input = await screen.findByDisplayValue("a.ts");
    fireEvent.keyDown(input, { key: "Escape", code: "Escape" });

    await waitFor(() => {
      expect(screen.queryByDisplayValue("a.ts")).not.toBeInTheDocument();
      expect(screen.getByTestId("dialog-root")).toBeInTheDocument();
    });
    expect(mocks.fsRename).not.toHaveBeenCalled();
  });
});
