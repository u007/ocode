import { describe, it, expect, vi, beforeEach, afterEach, beforeAll, afterAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FilePicker from "./FilePicker";

// Layout shim for @tanstack/react-virtual in jsdom (no layout engine)
class ResizeObserverMock {
  el: Element | null = null;
  constructor(private cb: (entries: unknown[]) => void) {}
  observe(el: Element) {
    this.el = el;
    this.cb([
      {
        target: el,
        contentRect: { width: 400, height: 300 },
        borderBoxSize: [{ inlineSize: 400, blockSize: 300 }],
      },
    ]);
  }
  unobserve() {}
  disconnect() {}
}

let originalGBCR: () => DOMRect;

beforeAll(() => {
  if (!(globalThis as any).ResizeObserver) (globalThis as any).ResizeObserver = ResizeObserverMock;
  originalGBCR = HTMLElement.prototype.getBoundingClientRect;
  HTMLElement.prototype.getBoundingClientRect = function () {
    return { width: 400, height: 300, top: 0, left: 0, right: 400, bottom: 300, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
  };
});

afterAll(() => {
  if (originalGBCR) HTMLElement.prototype.getBoundingClientRect = originalGBCR;
});


vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

function mockTreeResponse(children: unknown) {
  return new Response(JSON.stringify({ children, truncated: false }), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

describe("FilePicker keyword filter", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  const tree = [
    { name: "alpha.ts", path: "src/alpha.ts", is_dir: false },
    { name: "beta.ts", path: "src/beta.ts", is_dir: false },
    { name: "alpha_test.go", path: "src/alpha_test.go", is_dir: false },
    { name: "readme.md", path: "readme.md", is_dir: false },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockResolvedValue(mockTreeResponse(tree));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("filters by multiple keywords (AND), case-insensitive, and supports keyboard selection", async () => {
    const onOpenFile = vi.fn();
    const onClose = vi.fn();

    render(<FilePicker open={true} onClose={onClose} onOpenFile={onOpenFile} projectPath="/proj" />);

    // Wait for files to load and appear
    await waitFor(() => expect(screen.getByText("src/alpha.ts")).toBeInTheDocument());
    expect(screen.getByText("src/beta.ts")).toBeInTheDocument();
    expect(screen.getByText("src/alpha_test.go")).toBeInTheDocument();

    const input = screen.getByPlaceholderText("Filter by keywords...") as HTMLInputElement;

    function setQuery(value: string) {
      fireEvent.change(input, { target: { value } });
    }

    // Single keyword — should narrow to 2 matches containing "alpha"
    setQuery("alpha");
    await waitFor(() => expect(screen.getByText(/2 matches/)).toBeInTheDocument());
    expect(screen.getByText("src/alpha.ts")).toBeInTheDocument();
    expect(screen.getByText("src/alpha_test.go")).toBeInTheDocument();
    expect(screen.queryByText("src/beta.ts")).not.toBeInTheDocument();

    // Multi-keyword AND — "alpha ts" should match only src/alpha.ts (contains both)
    setQuery("alpha ts");
    await waitFor(() => expect(screen.getByText(/1 match/)).toBeInTheDocument());
    expect(screen.getByText("src/alpha.ts")).toBeInTheDocument();
    expect(screen.queryByText("src/alpha_test.go")).not.toBeInTheDocument();

    // Case-insensitive
    setQuery("ALPHA");
    await waitFor(() => expect(screen.getByText("src/alpha.ts")).toBeInTheDocument());
    expect(screen.getByText("src/alpha_test.go")).toBeInTheDocument();

    // Keyboard navigation: filter to "beta", ArrowDown to focus the item, Enter to select.
    // Also verifies the fix for the Command value/onValueChange bug: ArrowDown must not clobber the query.
    setQuery("beta");
    await waitFor(() => expect(screen.getByText("src/beta.ts")).toBeInTheDocument());
    expect(input.value).toBe("beta");
    fireEvent.keyDown(input, { key: "ArrowDown", code: "ArrowDown" });
    expect(input.value).toBe("beta"); // query must remain the filter, not become a file path
    const item = screen.getByText("src/beta.ts");
    fireEvent.click(item);
    expect(onOpenFile).toHaveBeenCalledWith("src/beta.ts", "/proj");
    expect(onClose).toHaveBeenCalled();

    // Empty state for no matches
    onOpenFile.mockClear();
    onClose.mockClear();
    setQuery("zzz_no_match");
    await waitFor(() => expect(screen.getByText("No matching files")).toBeInTheDocument());
    expect(screen.queryByText("src/alpha.ts")).not.toBeInTheDocument();
  });
});

describe("FilePicker initialQuery", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  const tree = [
    { name: "alpha.ts", path: "src/alpha.ts", is_dir: false },
    { name: "beta.ts", path: "src/beta.ts", is_dir: false },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockImplementation(async () => mockTreeResponse(tree));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("seeds the filter with the requested basename", async () => {
    render(
      <FilePicker
        open={true}
        onClose={vi.fn()}
        onOpenFile={vi.fn()}
        projectPath="/proj"
        initialQuery="beta.ts"
      />,
    );
    const input = screen.getByPlaceholderText("Filter by keywords...") as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("beta.ts"));
    await waitFor(() => expect(screen.getByText("src/beta.ts")).toBeInTheDocument());
    expect(screen.queryByText("src/alpha.ts")).not.toBeInTheDocument();
  });

  it("does not carry the seed into a later open without one", async () => {
    const props = { onClose: vi.fn(), onOpenFile: vi.fn(), projectPath: "/proj" };
    const { rerender } = render(
      <FilePicker {...props} open={true} initialQuery="beta.ts" />,
    );
    const input = screen.getByPlaceholderText("Filter by keywords...") as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("beta.ts"));

    rerender(<FilePicker {...props} open={false} />);
    rerender(<FilePicker {...props} open={true} />);
    // The dialog content unmounts while closed, so re-query the reopened input.
    const reopened = (await screen.findByPlaceholderText(
      "Filter by keywords...",
    )) as HTMLInputElement;
    await waitFor(() => expect(reopened.value).toBe(""));
    await waitFor(() => expect(screen.getByText("src/alpha.ts")).toBeInTheDocument());
  });
});

describe("FilePicker shortest-path ordering", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  const tree = [
    { name: "model.go", path: "internal/tui/model.go", is_dir: false },
    { name: "model.go", path: "model.go", is_dir: false },
    { name: "readme.md", path: "docs/guide/deep/readme.md", is_dir: false },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockResolvedValue(mockTreeResponse(tree));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  function renderedPaths() {
    return screen.getAllByRole("option").map((el) => el.textContent);
  }

  it("orders the unfiltered list shortest path first", async () => {
    render(<FilePicker open onClose={() => {}} onOpenFile={() => {}} projectPath="/proj" />);
    await waitFor(() => expect(screen.getByText("model.go")).toBeInTheDocument());
    await waitFor(() =>
      expect(renderedPaths()).toEqual(["model.go", "internal/tui/model.go", "docs/guide/deep/readme.md"]),
    );
  });

  it("breaks relevance ties by shortest path", async () => {
    render(<FilePicker open onClose={() => {}} onOpenFile={() => {}} projectPath="/proj" />);
    await waitFor(() => expect(screen.getByText("model.go")).toBeInTheDocument());

    const input = screen.getByPlaceholderText("Filter by keywords...") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "model" } });

    // Both files match "model" with the same relevance (exact word in the
    // filename); the shallower path must rank first. The deep readme is filtered out.
    await waitFor(() => expect(renderedPaths()).toEqual(["model.go", "internal/tui/model.go"]));
  });
});

describe("FilePicker folder results", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  const FOLDER = "benchmark-artifacts/stand-height/2026-10-12-rerun-cke0002";
  const tree = [
    {
      name: "benchmark-artifacts",
      path: "benchmark-artifacts",
      is_dir: true,
      children: [
        {
          name: "stand-height",
          path: "benchmark-artifacts/stand-height",
          is_dir: true,
          children: [
            {
              name: "2026-10-12-rerun-cke0002",
              path: FOLDER,
              is_dir: true,
              children: [{ name: "result.json", path: `${FOLDER}/result.json`, is_dir: false }],
            },
          ],
        },
      ],
    },
    { name: "readme.md", path: "readme.md", is_dir: false },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockResolvedValue(mockTreeResponse(tree));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("lists no folders unless a folder handler is given", async () => {
    render(<FilePicker open onClose={() => {}} onOpenFile={() => {}} projectPath="/proj" />);
    const input = (await screen.findByPlaceholderText("Filter by keywords...")) as HTMLInputElement;
    await waitFor(() => expect(screen.getByText("readme.md")).toBeInTheDocument());

    fireEvent.change(input, { target: { value: "2026-10-12-rerun-cke0002" } });
    await waitFor(() => expect(screen.getByText(`${FOLDER}/result.json`)).toBeInTheDocument());
    expect(screen.queryByText(FOLDER)).not.toBeInTheDocument();
  });

  it("lists a matching folder and selecting it calls onOpenFolder, not onOpenFile", async () => {
    const onOpenFile = vi.fn();
    const onOpenFolder = vi.fn();
    const onClose = vi.fn();
    render(
      <FilePicker
        open
        onClose={onClose}
        onOpenFile={onOpenFile}
        onOpenFolder={onOpenFolder}
        projectPath="/proj"
      />,
    );
    const input = (await screen.findByPlaceholderText("Filter by keywords...")) as HTMLInputElement;
    await waitFor(() => expect(screen.getByText("readme.md")).toBeInTheDocument());

    fireEvent.change(input, { target: { value: FOLDER } });
    const folderRow = (await screen.findByText(FOLDER)).closest("[cmdk-item]");
    expect(folderRow).toHaveTextContent("folder");

    fireEvent.click(screen.getByText(FOLDER));
    expect(onOpenFolder).toHaveBeenCalledWith(FOLDER, "/proj");
    expect(onOpenFile).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("still opens a file through onOpenFile when folders are listed", async () => {
    const onOpenFile = vi.fn();
    const onOpenFolder = vi.fn();
    render(
      <FilePicker open onClose={() => {}} onOpenFile={onOpenFile} onOpenFolder={onOpenFolder} projectPath="/proj" />,
    );
    const input = (await screen.findByPlaceholderText("Filter by keywords...")) as HTMLInputElement;
    await waitFor(() => expect(screen.getByText("readme.md")).toBeInTheDocument());

    fireEvent.change(input, { target: { value: "readme" } });
    fireEvent.click(await screen.findByText("readme.md"));
    expect(onOpenFile).toHaveBeenCalledWith("readme.md", "/proj");
    expect(onOpenFolder).not.toHaveBeenCalled();
  });
});

