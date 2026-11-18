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

const TREE = {
  children: [
    { name: "a.ts", path: "src/a.ts", is_dir: false },
    { name: "lib", path: "src/lib", is_dir: true },
  ],
  truncated: false,
};

// Expanding src/lib must not echo the root listing back (that would nest forever).
function mockTree() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation(
    (async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/files/tree")) {
        const body = decodeURIComponent(url).includes("src/lib") ? { children: [], truncated: false } : TREE;
        return new Response(JSON.stringify(body), {
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

describe("FileTree inline rename (click the selected file name again)", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsRename.mockResolvedValue({ success: true, path: "src/b.ts" });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  const renameBox = () => screen.queryByRole("textbox", { name: "Rename a.ts" });

  async function renderTree() {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");
  }

  it("first click only selects; second click opens the inline input with the name", async () => {
    await renderTree();

    fireEvent.click(screen.getByText("a.ts"));
    expect(renameBox()).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("a.ts"));
    const input = await screen.findByRole("textbox", { name: "Rename a.ts" });
    expect(input).toHaveValue("a.ts");
  });

  it("typing a new name and pressing Enter renames once and closes the input", async () => {
    await renderTree();
    fireEvent.click(screen.getByText("a.ts"));
    fireEvent.click(screen.getByText("a.ts"));
    const input = await screen.findByRole("textbox", { name: "Rename a.ts" });

    fireEvent.change(input, { target: { value: "b.ts" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(mocks.fsRename).toHaveBeenCalledTimes(1));
    expect(mocks.fsRename.mock.calls[0][1]).toBe("b.ts");
    expect(renameBox()).not.toBeInTheDocument();

    // The input's blur after unmount must not commit a second time.
    fireEvent.blur(input);
    expect(mocks.fsRename).toHaveBeenCalledTimes(1);
  });

  it("Escape closes the input without renaming", async () => {
    await renderTree();
    fireEvent.click(screen.getByText("a.ts"));
    fireEvent.click(screen.getByText("a.ts"));
    const input = await screen.findByRole("textbox", { name: "Rename a.ts" });

    fireEvent.change(input, { target: { value: "b.ts" } });
    fireEvent.keyDown(input, { key: "Escape" });

    await waitFor(() => expect(renameBox()).not.toBeInTheDocument());
    expect(mocks.fsRename).not.toHaveBeenCalled();
  });

  it("Enter with the unchanged name makes no rename call", async () => {
    await renderTree();
    fireEvent.click(screen.getByText("a.ts"));
    fireEvent.click(screen.getByText("a.ts"));
    const input = await screen.findByRole("textbox", { name: "Rename a.ts" });

    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(renameBox()).not.toBeInTheDocument());
    expect(mocks.fsRename).not.toHaveBeenCalled();
  });

  it.each([
    ["Enter (commit)", "Enter"],
    ["Escape (cancel)", "Escape"],
  ])("focus returns to the row button after %s", async (_label, key) => {
    await renderTree();
    const row = screen.getByText("a.ts").closest("[data-tree-path]")!;
    fireEvent.click(screen.getByText("a.ts"));
    fireEvent.click(screen.getByText("a.ts"));
    const input = await screen.findByRole("textbox", { name: "Rename a.ts" });

    fireEvent.keyDown(input, { key });

    await waitFor(() => expect(renameBox()).not.toBeInTheDocument());
    await waitFor(() =>
      expect(row.querySelector("button")).toHaveFocus(),
    );
  });

  it("a double-click (detail 2) on the name does not start rename", async () => {
    await renderTree();
    fireEvent.click(screen.getByText("a.ts"));
    fireEvent.click(screen.getByText("a.ts"), { detail: 2 });

    expect(renameBox()).not.toBeInTheDocument();
    expect(mocks.fsRename).not.toHaveBeenCalled();
  });

  it("a second click on a directory name does not start rename", async () => {
    await renderTree();
    fireEvent.click(screen.getByText("lib"));
    await screen.findByText("lib");
    fireEvent.click(screen.getByText("lib"));

    expect(screen.queryByRole("textbox", { name: "Rename lib" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/^Rename /)).not.toBeInTheDocument();
    expect(mocks.fsRename).not.toHaveBeenCalled();
  });
});
