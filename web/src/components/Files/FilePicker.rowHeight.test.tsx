import { describe, it, expect, vi, beforeEach, afterEach, beforeAll, afterAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FilePicker from "./FilePicker";

// Layout shim for @tanstack/react-virtual in jsdom (no layout engine).
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

/**
 * The picker's result rows are virtualized with a FIXED height. The row
 * height and the virtualizer's `estimateSize` must agree exactly, otherwise
 * the virtualizer's total size is shorter than the real content and the tail
 * of the list is clipped — the last matches exist in the DOM but can never be
 * scrolled into view. That is the "18 matches but I only see 12" symptom.
 */
describe("FilePicker virtualized row height", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  const many = Array.from({ length: 40 }, (_, i) => ({
    name: `file${String(i).padStart(2, "0")}.ts`,
    path: `src/dir${i % 5}/file${String(i).padStart(2, "0")}.ts`,
    is_dir: false,
  }));

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ children: many, truncated: false }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("sizes the virtual list so every match is reachable by scrolling", async () => {
    const onOpenFile = vi.fn();
    const onClose = vi.fn();
    render(<FilePicker open={true} onClose={onClose} onOpenFile={onOpenFile} projectPath="/proj" />);

    // The match count only appears once a query narrows the list.
    const input = screen.getByPlaceholderText("Filter by keywords...") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "file" } });
    await waitFor(() => expect(screen.getByText(/40 matches/)).toBeInTheDocument());

    // The virtualizer's total size is the sizer div's inline height. It must
    // be exactly count * rowHeight, otherwise the scroll extent does not match
    // the content and the last rows are clipped off the bottom.
    const sizer = document.querySelector('[cmdk-group-items] > div') as HTMLElement | null;
    expect(sizer).not.toBeNull();
    const totalSize = Number.parseFloat(sizer!.style.height);

    // 40 rows. The row height is the cmdk item box: py-3 (12+12) + a 20px
    // line = 44px. Pin it here so a change to the markup that alters the row
    // height fails this test instead of silently clipping the list again.
    const ROW_HEIGHT = 44;
    expect(totalSize).toBe(40 * ROW_HEIGHT);
  });

  it("keeps the row height fixed so a long path cannot grow the row", async () => {
    const onOpenFile = vi.fn();
    const onClose = vi.fn();
    render(<FilePicker open={true} onClose={onClose} onOpenFile={onOpenFile} projectPath="/proj" />);

    const input = screen.getByPlaceholderText("Filter by keywords...") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "file" } });
    await waitFor(() => expect(screen.getByText(/40 matches/)).toBeInTheDocument());

    // The path span is truncated to one line (Tailwind's `truncate`), so a
    // long path cannot wrap and change the row height. jsdom does not apply
    // Tailwind, so assert the classes that carry the behaviour.
    const pathSpan = screen.getByText("src/dir0/file00.ts").closest("span");
    expect(pathSpan).not.toBeNull();
    const cls = pathSpan!.className;
    expect(cls).toContain("truncate");
    expect(cls).toContain("min-w-0");
  });
});

/**
 * The server caps its tree walk, so a huge project's file list is incomplete.
 * The picker must say so in the UI: a user searching for a file that exists
 * on disk must not be told it does not exist.
 */
describe("FilePicker truncated tree", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          children: [{ name: "a.ts", path: "a.ts", is_dir: false }],
          truncated: true,
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      ),
    );
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("tells the user the result set is incomplete", async () => {
    render(<FilePicker open={true} onClose={vi.fn()} onOpenFile={vi.fn()} projectPath="/proj" />);

    await waitFor(() => expect(screen.getByText(/results may be incomplete/)).toBeInTheDocument());
  });
});
