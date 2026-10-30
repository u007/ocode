import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react";
import FileTree, { dropDestination, transferHasFolder } from "./FileTree";

const mocks = vi.hoisted(() => ({
  fsDownload: vi.fn(),
  fsUpload: vi.fn(),
  saveBlobAsFile: vi.fn(),
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocks.getPathsConfig,
      fsDownload: mocks.fsDownload,
      fsUpload: mocks.fsUpload,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

vi.mock("@/lib/saveBlob", () => ({ saveBlobAsFile: mocks.saveBlobAsFile }));

// Stock shadcn Dialog (Radix portal + focus guards) deadlocks jsdom/React when
// opened inside a test container; these tests never drive a dialog.
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
};

function mockTree() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation(
    (async (input: RequestInfo | URL) => {
      if (String(input).includes("/api/files/tree")) {
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

/** The pane wrapper the tree's drop handlers are attached to (viewport -> root -> pane). */
function paneEl(): HTMLElement {
  const viewport = document.querySelector("[data-radix-scroll-area-viewport]") as HTMLElement;
  return viewport.parentElement!.parentElement! as HTMLElement;
}

function rowEl(treePath: string): HTMLElement {
  const row = document.querySelector<HTMLElement>(`[data-tree-path="${treePath}"]`);
  if (!row) throw new Error(`no tree row for ${treePath}`);
  return row;
}

function osFile(name: string, body = "bytes") {
  return new File([body], name, { type: "image/png" });
}

/** A drop carrying OS files, the way a Finder/Explorer drag arrives. */
function dropFiles(target: Element, files: File[], opts: { folder?: boolean } = {}) {
  const items = opts.folder
    ? [{ kind: "file", type: "", getAsFile: () => null, webkitGetAsEntry: () => ({ isDirectory: true }) }]
    : files.map((file) => ({
        kind: "file",
        type: file.type,
        getAsFile: () => file,
        webkitGetAsEntry: () => ({ isDirectory: false }),
      }));
  fireEvent.drop(target, { dataTransfer: { types: ["Files"], files, items } });
}

describe("dropDestination / transferHasFolder", () => {
  /** A row-shaped fixture: a marked wrapper holding one inner button. */
  function rowFixture(treePath: string | null, isDir: boolean): HTMLElement {
    const wrapper = document.createElement("div");
    if (treePath !== null) wrapper.setAttribute("data-tree-path", treePath);
    if (isDir) wrapper.setAttribute("data-tree-dir", "");
    const inner = document.createElement("button");
    wrapper.appendChild(inner);
    document.body.appendChild(wrapper);
    return inner;
  }

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("a folder row takes the files", () => {
    expect(dropDestination("/proj", rowFixture("src", true))).toBe("/proj/src");
  });

  it("a file row sends the files beside itself", () => {
    expect(dropDestination("/proj", rowFixture("src/a.ts", false))).toBe("/proj/src");
  });

  it("anything outside a row lands in the browsed root", () => {
    expect(dropDestination("/proj", rowFixture(null, false))).toBe("/proj");
    expect(dropDestination("/proj", null)).toBe("/proj");
  });

  it("a folder entry in the payload is detected, a plain file is not", () => {
    const folder = {
      items: [{ kind: "file", webkitGetAsEntry: () => ({ isDirectory: true }) }],
    } as unknown as DataTransfer;
    const plain = {
      items: [{ kind: "file", webkitGetAsEntry: () => ({ isDirectory: false }) }],
    } as unknown as DataTransfer;
    expect(transferHasFolder(folder)).toBe(true);
    expect(transferHasFolder(plain)).toBe(false);
  });
});

describe("FileTree download and drop", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsDownload.mockResolvedValue(new Blob(["remote bytes"]));
    mocks.fsUpload.mockResolvedValue({ success: true, path: "/proj/a.png" });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    localStorage.clear();
    cleanup();
  });

  async function renderTree() {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");
  }

  describe("right-click Download", () => {
    it("is offered on files and not on folders", async () => {
      await renderTree();

      fireEvent.contextMenu(screen.getByText("a.ts"));
      expect(await screen.findByRole("menuitem", { name: /^Download$/ })).toBeTruthy();
      cleanup();

      await renderTree();
      fireEvent.contextMenu(screen.getByText("src"));
      await screen.findByRole("menuitem", { name: /^Copy$/ });
      expect(screen.queryByRole("menuitem", { name: /^Download$/ })).toBeNull();
    });

    it("saves the file under its own name, fetched relative to the project root", async () => {
      await renderTree();
      fireEvent.contextMenu(screen.getByText("a.ts"));
      fireEvent.click(await screen.findByRole("menuitem", { name: /^Download$/ }));

      await waitFor(() => expect(mocks.saveBlobAsFile).toHaveBeenCalledTimes(1));
      expect(mocks.fsDownload).toHaveBeenCalledWith("a.ts", "/proj", undefined);
      const [blob, name] = mocks.saveBlobAsFile.mock.calls[0];
      expect(blob).toBeInstanceOf(Blob);
      expect(name).toBe("a.ts");
      await waitFor(() => expect(screen.getByText("Downloaded 1 file")).toBeTruthy());
    });

    it("reports a failed download and keeps the notice honest", async () => {
      mocks.fsDownload.mockRejectedValueOnce(new Error("file is too large to download"));
      await renderTree();
      fireEvent.contextMenu(screen.getByText("a.ts"));
      fireEvent.click(await screen.findByRole("menuitem", { name: /^Download$/ }));

      await waitFor(() =>
        expect(screen.getByText("Downloaded 0 files — a.ts: file is too large to download")).toBeTruthy(),
      );
      expect(mocks.saveBlobAsFile).not.toHaveBeenCalled();
    });

    it("is offered from the columns view too", async () => {
      localStorage.setItem("ocode.ui.filetree_view.v1", "columns");
      await renderTree();
      fireEvent.contextMenu(screen.getByText("a.ts"));
      fireEvent.click(await screen.findByRole("menuitem", { name: /^Download$/ }));

      await waitFor(() => expect(mocks.fsDownload).toHaveBeenCalledWith("a.ts", "/proj", undefined));
    });
  });

  describe("drag OS files onto the tree", () => {
    it("drops into a folder row", async () => {
      await renderTree();
      dropFiles(rowEl("src"), [osFile("photo.png")]);

      await waitFor(() =>
        expect(mocks.fsUpload).toHaveBeenCalledWith("/proj/src", expect.any(File), "/proj", undefined, expect.any(AbortSignal)),
      );
    });

    it("drops beside a file row, into its parent folder", async () => {
      await renderTree();
      dropFiles(rowEl("a.ts"), [osFile("photo.png")]);

      await waitFor(() =>
        expect(mocks.fsUpload).toHaveBeenCalledWith("/proj", expect.any(File), "/proj", undefined, expect.any(AbortSignal)),
      );
    });

    it("drops on empty pane space into the project root", async () => {
      await renderTree();
      dropFiles(paneEl(), [osFile("photo.png")]);

      await waitFor(() =>
        expect(mocks.fsUpload).toHaveBeenCalledWith("/proj", expect.any(File), "/proj", undefined, expect.any(AbortSignal)),
      );
    });

    it("refuses a dropped folder with an explanation and uploads nothing", async () => {
      await renderTree();
      dropFiles(rowEl("src"), [], { folder: true });

      await waitFor(() => expect(screen.getByText("Folders can't be added — drop the files inside instead")).toBeTruthy());
      expect(mocks.fsUpload).not.toHaveBeenCalled();
    });

    it("ignores drags that carry no files, so they pass through", async () => {
      await renderTree();
      const notPrevented = fireEvent.dragOver(paneEl(), { dataTransfer: { types: ["text/plain"] } });
      expect(notPrevented).toBe(true);
      fireEvent.drop(paneEl(), { dataTransfer: { types: ["text/plain"], files: [], items: [] } });
      expect(mocks.fsUpload).not.toHaveBeenCalled();
    });

    it("accepts a file drag over a row (dragover is claimed so the drop fires)", async () => {
      await renderTree();
      const notPrevented = fireEvent.dragOver(rowEl("src"), { dataTransfer: { types: ["Files"] } });
      expect(notPrevented).toBe(false);
    });
  });
});
