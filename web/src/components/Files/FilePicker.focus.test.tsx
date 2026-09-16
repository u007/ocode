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
        contentRect: { width: 400, height: 300, borderBoxSize: [{ inlineSize: 400, blockSize: 300 }] },
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

const TREE = {
  children: [{ name: "a.ts", path: "src/a.ts", is_dir: false }],
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

// Regression: the file picker (ctrl/cmd+p) must land focus on the filter
// input, NOT on the "Normal/Hidden" toggle button that precedes it in DOM
// order — that button is the first tabbable element, which is what Radix's
// default open-focus pass used to grab.
describe("FilePicker initial focus", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("opens with focus on the filter input, not the hidden-files toggle", async () => {
    render(<FilePicker open onClose={() => {}} onOpenFile={() => {}} projectPath="/proj" />);
    const input = await screen.findByPlaceholderText("Filter by keywords...");
    await waitFor(() => expect(input).toHaveFocus());
    const toggle = screen.getByRole("button", { name: /Normal|Hidden/ });
    expect(toggle).not.toHaveFocus();
  });

  it("keeps the filter input focused after typing", async () => {
    render(<FilePicker open onClose={() => {}} onOpenFile={() => {}} projectPath="/proj" />);
    const input = await screen.findByPlaceholderText("Filter by keywords...");
    await waitFor(() => expect(input).toHaveFocus());
    fireEvent.change(input, { target: { value: "a.ts" } });
    expect(input).toHaveFocus();
  });
});
