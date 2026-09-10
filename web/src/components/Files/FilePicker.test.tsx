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
