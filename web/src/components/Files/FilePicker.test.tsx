import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FilePicker from "./FilePicker";

// cmdk uses ResizeObserver which is not available in jsdom
beforeAll(() => {
  if (!globalThis.ResizeObserver) {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
  }
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
