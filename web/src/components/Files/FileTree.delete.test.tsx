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

// The real shadcn Dialog (Radix portal + focus guards) deadlocks jsdom/React 18
// in an infinite microtask loop (known React-jsdom issue when opening a Radix
// dialog inside a test container). The dialog here is stock shadcn UI; what we
// test is FileTree's delete wiring, so render the dialog body inline.
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

const TREE = {
  children: [
    { name: "a.ts", path: "src/a.ts", is_dir: false },
    { name: "b.ts", path: "src/b.ts", is_dir: false },
    { name: "c.ts", path: "src/c.ts", is_dir: false },
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

// The Radix context menu and ScrollArea need these in jsdom.
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

describe("FileTree delete flow (replaces window.confirm, unsupported in Wails webview)", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsDelete.mockResolvedValue({ success: true });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  async function openRowMenu(rowName: string) {
    const row = screen.getByText(rowName);
    fireEvent.contextMenu(row);
    await screen.findByRole("menuitem", { name: "Delete" });
  }

  async function confirmDeleteDialog() {
    const confirmBtn = await screen.findByRole("button", { name: "Delete" });
    fireEvent.click(confirmBtn);
  }

  it("right-click → Delete opens an in-app confirmation dialog naming the file", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(await screen.findByText("Delete 1 item?")).toBeInTheDocument();
    expect(screen.getByText("src/a.ts")).toBeInTheDocument();
    // No API call was made yet — only the confirm dialog opened.
    expect(mocks.fsDelete).not.toHaveBeenCalled();
  });

  it("Cancel closes the dialog and never calls the API", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    const cancelBtn = await screen.findByRole("button", { name: "Cancel" });
    fireEvent.click(cancelBtn);

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument(),
    );
    expect(mocks.fsDelete).not.toHaveBeenCalled();
  });

  it("confirming deletes the single file and dispatches ocode:fs-delete", async () => {
    const onDelete = vi.fn();
    window.addEventListener("ocode:fs-delete", onDelete);
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    await confirmDeleteDialog();

    await waitFor(() => expect(mocks.fsDelete).toHaveBeenCalledWith(["src/a.ts"], "/proj"));
    expect(onDelete).toHaveBeenCalledWith(
      expect.objectContaining({ detail: { paths: ["src/a.ts"], projectRoot: "/proj" } }),
    );
    // Dialog closes on success.
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument(),
    );
    window.removeEventListener("ocode:fs-delete", onDelete);
  });

  it("multi-select delete sends every selected path", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    fireEvent.click(screen.getByLabelText("a.ts — select"));
    fireEvent.click(screen.getByLabelText("b.ts — select"));

    await openRowMenu("a.ts"); // right-click a selected row → whole selection
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(await screen.findByText("Delete 2 items?")).toBeInTheDocument();
    expect(screen.getByText("src/a.ts")).toBeInTheDocument();
    expect(screen.getByText("src/b.ts")).toBeInTheDocument();

    await confirmDeleteDialog();
    await waitFor(() =>
      expect(mocks.fsDelete).toHaveBeenCalledWith(["src/a.ts", "src/b.ts"], "/proj"),
    );
  });

  it("right-clicking an unselected node narrows the target to that node only", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    fireEvent.click(screen.getByLabelText("a.ts — select"));
    fireEvent.click(screen.getByLabelText("b.ts — select"));

    await openRowMenu("c.ts"); // unselected → only c.ts is the target
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    await confirmDeleteDialog();

    await waitFor(() => expect(mocks.fsDelete).toHaveBeenCalledWith(["src/c.ts"], "/proj"));
  });

  it("API failure keeps the dialog open and shows the error notice", async () => {
    mocks.fsDelete.mockRejectedValueOnce(new Error("boom"));
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    await openRowMenu("a.ts");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    await confirmDeleteDialog();

    expect(await screen.findByText("Error: boom")).toBeInTheDocument();
    // Dialog must still be open so the user can retry or cancel.
    expect(await screen.findByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(mocks.fsDelete).toHaveBeenCalledTimes(1);
  });
});