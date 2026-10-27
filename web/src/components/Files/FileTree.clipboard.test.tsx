import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor, fireEvent, act, cleanup } from "@testing-library/react";
import FileTree, { pasteDestination, pasteDestinationLabel } from "./FileTree";

const mocks = vi.hoisted(() => ({
  fsCopy: vi.fn(),
  fsMove: vi.fn(),
  fsDelete: vi.fn(),
  fsUpload: vi.fn(),
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocks.getPathsConfig,
      fsCopy: mocks.fsCopy,
      fsMove: mocks.fsMove,
      fsDelete: mocks.fsDelete,
      fsUpload: mocks.fsUpload,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

// Stock shadcn Dialog (Radix portal + focus guards) deadlocks jsdom/React when
// opened inside a test container; the tree only needs the New file/folder
// prompt to exist, never to be driven here.
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

/** The pane wrapper Radix's empty-area trigger is attached to: the ScrollArea
 *  root's parent (viewport -> root -> pane). */
function paneEl(): HTMLElement {
  const viewport = document.querySelector("[data-radix-scroll-area-viewport]") as HTMLElement;
  return viewport.parentElement!.parentElement! as HTMLElement;
}

describe("pasteDestination / pasteDestinationLabel", () => {
  it("a focused folder is the destination", () => {
    expect(pasteDestination("/proj", { path: "src", isDir: true }, 1)).toBe("/proj/src");
  });

  it("a focused file pastes beside itself", () => {
    expect(pasteDestination("/proj", { path: "src/a.ts", isDir: false }, 1)).toBe("/proj/src");
    expect(pasteDestination("/proj", { path: "a.ts", isDir: false }, 1)).toBe("/proj");
  });

  it("no focus falls back to the tree root", () => {
    expect(pasteDestination("/proj", null, 0)).toBe("/proj");
  });

  it("a multi-selection has no single current folder, so it uses the root", () => {
    expect(pasteDestination("/proj", { path: "src", isDir: true }, 3)).toBe("/proj");
  });

  it("tolerates a trailing separator on the root", () => {
    expect(pasteDestination("/proj/", { path: "a.ts", isDir: false }, 1)).toBe("/proj");
    expect(pasteDestination("/proj/", { path: "src", isDir: true }, 1)).toBe("/proj/src");
  });

  it("handles a Windows root with mixed separators", () => {
    expect(pasteDestination("C:\\proj", { path: "a.ts", isDir: false }, 1)).toBe("C:\\proj");
    expect(pasteDestination("C:\\proj", { path: "src\\a.ts", isDir: false }, 1)).toBe("C:\\proj/src");
  });

  it("labels the root by project folder name and a subfolder relative to it", () => {
    expect(pasteDestinationLabel("/proj", "/proj")).toBe("proj/");
    expect(pasteDestinationLabel("/proj/", "/proj")).toBe("proj/");
    expect(pasteDestinationLabel("/proj", "/proj/src")).toBe("src/");
    expect(pasteDestinationLabel("/proj", "/proj/src/deep")).toBe("src/deep/");
  });

  it("labels a Windows root correctly", () => {
    expect(pasteDestinationLabel("C:\\proj", "C:\\proj\\src")).toBe("src/");
    expect(pasteDestinationLabel("C:\\proj", "C:\\proj")).toBe("proj/");
  });
});

describe("FileTree clipboard: paste to project root", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsCopy.mockResolvedValue({ success: true });
    mocks.fsMove.mockResolvedValue({ success: true });
    mocks.fsDelete.mockResolvedValue({ success: true });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    cleanup();
  });

  async function renderTree() {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");
  }

  async function copyFileViaMenu(name: string) {
    fireEvent.contextMenu(screen.getByText(name));
    const copy = await screen.findByRole("menuitem", { name: /^Copy$/ });
    fireEvent.click(copy);
    // Clipboard state is what enables every paste affordance.
    await screen.findByLabelText("Paste to project root");
  }

  it("right-clicking blank space offers Paste to project root, disabled until something is copied", async () => {
    await renderTree();
    fireEvent.contextMenu(paneEl());
    const item = await screen.findByRole("menuitem", { name: /Paste to project root/ });
    expect(item.getAttribute("aria-disabled")).toBe("true");
  });

  it("pastes into the project root from the empty-area menu", async () => {
    await renderTree();
    await copyFileViaMenu("a.ts");

    fireEvent.contextMenu(paneEl());
    const paste = await screen.findByRole("menuitem", { name: /Paste to project root/ });
    fireEvent.click(paste);

    await waitFor(() =>
      expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined),
    );
  });

  it("exposes a toolbar paste button targeting the root and names the destination in the notice", async () => {
    await renderTree();
    await copyFileViaMenu("a.ts");

    fireEvent.click(screen.getByLabelText("Paste to project root"));

    await waitFor(() =>
      expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined),
    );
    expect(await screen.findByText("Copied 1 item to proj/")).toBeTruthy();
  });

  it("moves (cut + paste) into the root and clears the clipboard afterwards", async () => {
    await renderTree();
    fireEvent.contextMenu(screen.getByText("a.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: /^Cut$/ }));

    fireEvent.click(await screen.findByLabelText("Paste to project root"));

    await waitFor(() =>
      expect(mocks.fsMove).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined),
    );
    // The clipboard is consumed by a cut, so the affordance disappears.
    await waitFor(() => expect(screen.queryByLabelText("Paste to project root")).toBeNull());
  });

  it("right-clicking the columns-view root header offers the same root actions", async () => {
    localStorage.setItem("ocode.ui.filetree_view.v1", "columns");
    await renderTree();
    await copyFileViaMenu("a.ts");

    // The root column's header (same title the tree already uses for it).
    fireEvent.contextMenu(screen.getByTitle("/proj"));
    const paste = await screen.findByRole("menuitem", { name: /Paste to project root/ });
    fireEvent.click(paste);

    await waitFor(() =>
      expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined),
    );
  });
});

describe("FileTree clipboard: keyboard shortcuts", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsCopy.mockResolvedValue({ success: true });
    mocks.fsMove.mockResolvedValue({ success: true });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    vi.useRealTimers();
    window.getSelection()?.removeAllRanges();
    cleanup();
  });

  async function renderTree() {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");
  }

  it("⌘C copies the clicked row and ⌘V (paste event) pastes into the project root", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);

    fireEvent.keyDown(document.body, { key: "c", metaKey: true });
    await screen.findByLabelText("Paste to project root");

    fireEvent.paste(document.body);

    await waitFor(() =>
      expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined),
    );
  });

  it("⌘V pastes into the focused folder, not the root", async () => {
    await renderTree();
    // Copy a.ts first (focus becomes a.ts), then focus the folder.
    const fileRow = screen.getByText("a.ts");
    fireEvent.pointerDown(fileRow);
    fireEvent.click(fileRow);
    fireEvent.keyDown(document.body, { key: "c", metaKey: true });
    await screen.findByLabelText("Paste to project root");

    const dirRow = screen.getByText("src");
    fireEvent.pointerDown(dirRow);
    fireEvent.click(dirRow);

    fireEvent.paste(document.body);

    await waitFor(() =>
      expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj/src", "/proj", undefined),
    );
  });

  it("falls back to the app clipboard when no paste event arrives", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);
    fireEvent.keyDown(document.body, { key: "c", metaKey: true });
    await screen.findByLabelText("Paste to project root");

    vi.useFakeTimers();
    await act(async () => {
      fireEvent.keyDown(document.body, { key: "v", metaKey: true });
      await vi.advanceTimersByTimeAsync(200);
    });

    expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined);
  });

  it("treats a duplicate paste delivery as one paste", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);
    fireEvent.keyDown(document.body, { key: "c", metaKey: true });
    await screen.findByLabelText("Paste to project root");

    // The desktop shell routes one Cmd+V through two delivery paths.
    fireEvent.paste(document.body);
    fireEvent.paste(document.body);

    await waitFor(() => expect(mocks.fsCopy).toHaveBeenCalledTimes(1));
  });

  it("does not hijack ⌘C inside the search field", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);

    const search = screen.getByPlaceholderText(/Filter by keywords|Search file contents/i);
    fireEvent.keyDown(search, { key: "c", metaKey: true });

    expect(screen.queryByLabelText("Paste to project root")).toBeNull();
  });

  it("is inert while the pane is hidden behind another tab", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);

    paneEl().classList.add("hidden");
    fireEvent.keyDown(document.body, { key: "c", metaKey: true });

    expect(screen.queryByLabelText("Paste to project root")).toBeNull();
  });

  it.each(["c", "x"])("leaves ⌘%s to the browser when nothing is selected in the tree", async (key) => {
    await renderTree();
    fireEvent.pointerDown(paneEl());

    // fireEvent returns false when the handler called preventDefault.
    const handled = fireEvent.keyDown(document.body, { key, metaKey: true });

    expect(handled).toBe(true);
    expect(screen.queryByLabelText("Paste to project root")).toBeNull();
  });

  it.each(["c", "x"])("leaves ⌘%s to the browser when the user has native text selected", async (key) => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);
    // A tree row is selected, but the user also selected text: native copy wins.
    const range = document.createRange();
    range.selectNodeContents(row);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);

    const handled = fireEvent.keyDown(document.body, { key, metaKey: true });

    expect(handled).toBe(true);
    expect(screen.queryByLabelText("Paste to project root")).toBeNull();
  });
});

describe("FileTree clipboard: OS file paste (Finder/Explorer)", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    mocks.fsCopy.mockResolvedValue({ success: true });
    mocks.fsMove.mockResolvedValue({ success: true });
    mocks.fsUpload.mockResolvedValue({ success: true, path: "/proj/a.png" });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    cleanup();
  });

  async function renderTree() {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");
  }

  function osFile(name: string, body = "bytes") {
    return new File([body], name, { type: "image/png" });
  }

  /** Dispatch a paste event the way a browser does after copying a file in the
   *  OS file manager: clipboardData carries File objects (and an item entry,
   *  which is also how a copied FOLDER shows up). */
  function pasteFiles(files: File[], opts: { folder?: boolean } = {}) {
    const data = {
      files,
      items: files.length > 0
        ? files.map((file) => ({
            kind: "file",
            type: file.type,
            getAsFile: () => file,
            webkitGetAsEntry: () => ({ isDirectory: false }),
          }))
        : opts.folder
          ? [
              {
                kind: "file",
                type: "",
                getAsFile: () => null,
                webkitGetAsEntry: () => ({ isDirectory: true }),
              },
            ]
          : [],
    };
    fireEvent.paste(document.body, { clipboardData: data });
  }

  it("uploads OS files into the project root", async () => {
    await renderTree();
    fireEvent.pointerDown(paneEl());

    pasteFiles([osFile("photo.png")]);

    await waitFor(() =>
      expect(mocks.fsUpload).toHaveBeenCalledWith(
        "/proj",
        expect.any(File),
        "/proj",
        undefined,
        expect.any(AbortSignal),
      ),
    );
    await waitFor(() => expect(screen.getByText("Added 1 file to proj/")).toBeTruthy());
  });

  it("uploads into the focused folder, not the root", async () => {
    await renderTree();
    const dirRow = screen.getByText("src");
    fireEvent.pointerDown(dirRow);
    fireEvent.click(dirRow);

    pasteFiles([osFile("in-folder.png")]);

    await waitFor(() =>
      expect(mocks.fsUpload).toHaveBeenCalledWith(
        "/proj/src",
        expect.any(File),
        "/proj",
        undefined,
        expect.any(AbortSignal),
      ),
    );
  });

  it("uploads several files sequentially and keeps their order", async () => {
    await renderTree();
    fireEvent.pointerDown(paneEl());
    const first = osFile("one.png", "1");
    const second = osFile("two.png", "2");

    let inFlight = 0;
    let maxInFlight = 0;
    mocks.fsUpload.mockImplementation(async () => {
      inFlight++;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await new Promise((r) => setTimeout(r, 5));
      inFlight--;
      return { success: true, path: "/proj/x" };
    });

    pasteFiles([first, second]);

    await waitFor(() => expect(mocks.fsUpload).toHaveBeenCalledTimes(2));
    expect(mocks.fsUpload.mock.calls[0][1]).toBe(first);
    expect(mocks.fsUpload.mock.calls[1][1]).toBe(second);
    expect(maxInFlight).toBe(1);
    expect(await screen.findByText("Added 2 files to proj/")).toBeTruthy();
  });

  it("a newer paste aborts the superseded upload and keeps its own progress label", async () => {
    await renderTree();
    fireEvent.pointerDown(paneEl());
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const resolvers: Array<() => void> = [];
    mocks.fsUpload.mockImplementation(
      (_dest: string, _file: File, _root: string, _host: string | undefined, signal: AbortSignal) =>
        new Promise((resolve, reject) => {
          signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
          resolvers.push(() => resolve({ success: true, path: "/proj/x" }));
        }),
    );

    pasteFiles([osFile("old.png")]);
    await waitFor(() => expect(mocks.fsUpload).toHaveBeenCalledTimes(1));
    const oldSignal = mocks.fsUpload.mock.calls[0][4] as AbortSignal;

    pasteFiles([osFile("new.png")]);
    expect(oldSignal.aborted).toBe(true);
    await waitFor(() => expect(mocks.fsUpload).toHaveBeenCalledTimes(2));

    // Let the superseded loop observe its rejection before checking the label.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(screen.getByText("Uploading 1/1 to proj/…")).toBeTruthy();
    // An abort is cancellation, not a failure: nothing is logged for it.
    expect(errorSpy).not.toHaveBeenCalled();
    errorSpy.mockRestore();

    resolvers[1]();
    expect(await screen.findByText("Added 1 file to proj/")).toBeTruthy();
  });

  it("refuses a pasted folder with an explanation instead of pasting nothing", async () => {
    await renderTree();
    fireEvent.pointerDown(paneEl());

    pasteFiles([], { folder: true });

    expect(await screen.findByText(/Folders can't be pasted/)).toBeTruthy();
    expect(mocks.fsUpload).not.toHaveBeenCalled();
  });

  it("lets the in-app clipboard win while the app never lost focus", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);
    fireEvent.keyDown(document.body, { key: "c", metaKey: true });
    await screen.findByLabelText("Paste to project root");

    pasteFiles([osFile("stray.png")]);

    await waitFor(() =>
      expect(mocks.fsCopy).toHaveBeenCalledWith(["a.ts"], "/proj", "/proj", undefined),
    );
    expect(mocks.fsUpload).not.toHaveBeenCalled();
  });

  it("prefers OS files over a stale in-app clipboard after leaving the window", async () => {
    await renderTree();
    const row = screen.getByText("a.ts");
    fireEvent.pointerDown(row);
    fireEvent.click(row);
    fireEvent.keyDown(document.body, { key: "c", metaKey: true });
    await screen.findByLabelText("Paste to project root");

    fireEvent.blur(window);
    fireEvent.pointerDown(paneEl());
    pasteFiles([osFile("fresh.png")]);

    await waitFor(() =>
      expect(mocks.fsUpload).toHaveBeenCalledWith(
        "/proj",
        expect.any(File),
        "/proj",
        undefined,
        expect.any(AbortSignal),
      ),
    );
    expect(mocks.fsCopy).not.toHaveBeenCalled();
  });
});
