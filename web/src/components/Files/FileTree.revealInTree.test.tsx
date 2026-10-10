import { describe, it, expect, vi, beforeEach, afterEach, beforeAll, afterAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FileTree from "./FileTree";
import type { TreeRevealRequest } from "./FileTree";
import { FILE_TREE_EXPANSION_STORAGE_KEY } from "./fileTreeExpansionPersistence";

// `fileTreeViewPersistence` keeps its key private; this is the same literal the
// hidden-toggle suite seeds, and the reveal must leave it on "tree".
const VIEW_KEY = "ocode.ui.filetree_view.v1";

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

// The real shadcn Dialog deadlocks jsdom/React; this suite never opens one but
// FileTree imports it, so stub it out (same as the other FileTree suites).
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

// /proj
//   └ src/
//       ├ app/          (directory, only reachable by expanding src)
//       │   └ deep.ts
//       └ lib/util.ts
const ROOT = {
  children: [
    { name: "src", path: "src", is_dir: true },
    { name: "readme.md", path: "readme.md", is_dir: false },
  ],
  truncated: false,
  is_git_repo: false,
};
const SRC = { children: [{ name: "app", path: "src/app", is_dir: true }], truncated: false };
const APP = {
  children: [
    { name: "deep.ts", path: "src/app/deep.ts", is_dir: false },
    { name: "other.ts", path: "src/app/other.ts", is_dir: false },
  ],
  truncated: false,
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const OTHER_ROOT = { children: [{ name: "solo.ts", path: "solo.ts", is_dir: false }], truncated: false };

function mockFetch() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation((async (input: RequestInfo | URL) => {
    const url = String(input);
    if (!url.includes("/api/files/tree")) return json({});
    const path = new URL(url, "http://test").searchParams.get("path") ?? "";
    if (path === "/proj/src/app") return json(APP);
    if (path === "/proj/src") return json(SRC);
    // A second browsable root (an "extra allowed path"), with its own file.
    if (path === "/other") return json(OTHER_ROOT);
    return json(ROOT);
  }) as any);
}

let scrollIntoView: ReturnType<typeof vi.fn>;
let originalScrollIntoView: unknown;

beforeAll(() => {
  if (!(globalThis as any).PointerEvent) (globalThis as any).PointerEvent = MouseEvent;
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
  // jsdom has no layout engine. src/test/setup.ts polyfills a no-op
  // scrollIntoView on HTMLElement.prototype, so the spy has to REPLACE it there
  // — an Element.prototype assignment would be shadowed by it.
  originalScrollIntoView = (globalThis as any).HTMLElement.prototype.scrollIntoView;
  scrollIntoView = vi.fn();
  (globalThis as any).HTMLElement.prototype.scrollIntoView = scrollIntoView;
});

afterAll(() => {
  (globalThis as any).HTMLElement.prototype.scrollIntoView = originalScrollIntoView;
});

function row(path: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[data-tree-path="${path}"]`);
}

function reveal(props: Partial<React.ComponentProps<typeof FileTree>> & { request?: TreeRevealRequest }) {
  const { request, ...rest } = props;
  return render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" revealRequest={request} {...rest} />);
}

describe("FileTree reveal in file tree", () => {
  let fetchSpy: ReturnType<typeof mockFetch>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "darwin" });
    // The expansion set is persisted; a reveal writes to it, so each test must
    // start from "nothing expanded" or it would pass without expanding.
    window.localStorage.removeItem(FILE_TREE_EXPANSION_STORAGE_KEY);
    window.localStorage.removeItem(VIEW_KEY);
    fetchSpy = mockFetch();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("expands every ancestor, scrolls the row into view and highlights it", async () => {
    reveal({ request: { path: "/proj/src/app/deep.ts", root: "/proj", nonce: 1 } });

    // Both levels of ancestors were expanded, so the row exists…
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(row("src")).toBeTruthy();
    expect(row("src/app")).toBeTruthy();
    // …it was scrolled to…
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "center", inline: "nearest" });
    // …and carries the transient revealed highlight.
    expect(row("src/app/deep.ts")?.getAttribute("data-revealed")).toBe("");
  });

  it("accepts a path that is already root-relative", async () => {
    reveal({ request: { path: "src/app/other.ts", root: "/proj", nonce: 1 } });

    await waitFor(() => expect(row("src/app/other.ts")).toBeTruthy());
    expect(scrollIntoView).toHaveBeenCalled();
  });

  it("reveals a file that sits directly in the tree root", async () => {
    reveal({ request: { path: "/proj/readme.md", root: "/proj", nonce: 1 } });

    await waitFor(() => expect(row("readme.md")?.getAttribute("data-revealed")).toBe(""));
    expect(scrollIntoView).toHaveBeenCalled();
  });

  it("selects the revealed row (it is the file the editor has open)", async () => {
    reveal({ request: { path: "/proj/src/app/deep.ts", root: "/proj", nonce: 1 } });

    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    // `selectedPath` paints bg-muted on the row's own button — matched as a
    // WHOLE class, because the unselected state also contains "hover:bg-muted".
    const label = screen.getByText("deep.ts");
    await waitFor(() =>
      expect(label.closest("button")?.className.split(/\s+/)).toContain("bg-muted"),
    );
  });

  it("persists the expansion it performed", async () => {
    reveal({ request: { path: "/proj/src/app/deep.ts", root: "/proj", nonce: 1 } });
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());

    const saved = JSON.parse(window.localStorage.getItem(FILE_TREE_EXPANSION_STORAGE_KEY) ?? "{}");
    expect(saved.roots["/proj"].sort()).toEqual(["src", "src/app"]);
  });

  it("re-reveals the same file when the request carries a new nonce", async () => {
    const { rerender } = reveal({ request: { path: "/proj/src/app/deep.ts", root: "/proj", nonce: 1 } });
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    // The first flash is still up (1.6s): a repeat reveal must re-scroll and
    // re-flash anyway, which is why the flash state carries a sequence number.
    expect(row("src/app/deep.ts")?.getAttribute("data-revealed")).toBe("");
    scrollIntoView.mockClear();

    rerender(
      <FileTree
        onOpenFile={vi.fn()}
        projectPath="/proj"
        revealRequest={{ path: "/proj/src/app/deep.ts", root: "/proj", nonce: 2 }}
      />,
    );
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalled());
  });

  it("does nothing for a request it has already handled", async () => {
    const request = { path: "/proj/src/app/deep.ts", root: "/proj", nonce: 7 };
    const { rerender } = reveal({ request });
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    // The scroll is retried over REVEAL_SCROLL_FRAMES animation frames to
    // survive the pane's width animation, so let that chain finish before
    // clearing the spy — otherwise the clear lands mid-retry.
    await new Promise((r) => setTimeout(r, 300));
    scrollIntoView.mockClear();

    // A NEW object with the SAME nonce: prop identity alone must not re-run the
    // reveal, or every unrelated parent render would scroll the tree again.
    rerender(
      <FileTree
        onOpenFile={vi.fn()}
        projectPath="/proj"
        revealRequest={{ path: "/proj/src/app/deep.ts", root: "/proj", nonce: 7 }}
      />,
    );
    await new Promise((r) => setTimeout(r, 30));
    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  it("switches the browsed root when the tab belongs to another allowed root", async () => {
    // A file opened from an extra allowed path (not the project's own root):
    // the tree must browse THAT root, not report the file as outside.
    mocks.getPathsConfig.mockResolvedValue({
      extra_allowed_paths: ["/other"],
      upload_dir: "",
      platform: "darwin",
    });
    reveal({ request: { path: "/other/solo.ts", root: "/other", nonce: 1 } });

    await waitFor(() => expect(row("solo.ts")).toBeTruthy());
    // The /proj listing is gone: the tree is now browsing the other root.
    expect(row("readme.md")).toBeNull();
    expect(scrollIntoView).toHaveBeenCalled();
  });

  it("says so instead of failing silently when the file is outside the root", async () => {
    reveal({ request: { path: "/elsewhere/src/app/deep.ts", root: "/proj", nonce: 1 } });

    await screen.findByText(/outside the folder this tree is browsing/);
    expect(row("src/app/deep.ts")).toBeNull();
  });

  it("says so when the file sits under a directory the walk hides", async () => {
    reveal({ request: { path: "/proj/.github/workflows/ci.yml", root: "/proj", nonce: 1 } });

    await screen.findByText(/hidden — turn on hidden files/);
    expect(row(".github")).toBeNull();
  });

  it("reports a miss when the target's directory loads without it", async () => {
    reveal({ request: { path: "/proj/src/app/deleted.ts", root: "/proj", nonce: 1 } });

    // The 1.5s budget is the discriminator: the backstop timeout is 15s, so
    // this can only pass if the parent directory's own listing reported the
    // miss rather than the tree sitting on a timer.
    await screen.findByText(/Could not show src\/app\/deleted\.ts/, undefined, { timeout: 1500 });
  });

  it("clears the path filter so the row is not hidden by it", async () => {
    const { rerender } = reveal({});
    await screen.findByText("readme.md");

    // A filter that matches nothing replaces the tree with an empty result set.
    const filter = screen.getByPlaceholderText("Filter by keywords...");
    fireEvent.change(filter, { target: { value: "zzz-no-match" } });
    await screen.findByText("No matching files");

    rerender(
      <FileTree
        onOpenFile={vi.fn()}
        projectPath="/proj"
        revealRequest={{ path: "/proj/src/app/deep.ts", root: "/proj", nonce: 1 }}
      />,
    );
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(screen.queryByText("No matching files")).toBeNull();
  });

  it("switches to list view when the tree is in Miller-column view", async () => {
    window.localStorage.setItem(VIEW_KEY, "columns");
    reveal({ request: { path: "/proj/src/app/deep.ts", root: "/proj", nonce: 1 } });

    // The columns surface renders a "Loading…" placeholder per column; the
    // reveal forces list view so the row can exist at all.
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(window.localStorage.getItem(VIEW_KEY)).toBe("tree");
  });
});