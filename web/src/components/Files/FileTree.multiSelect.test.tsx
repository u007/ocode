import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import FileTree from "./FileTree";

const mocks = vi.hoisted(() => ({
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocks.getPathsConfig,
    },
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

// Radix context menu + ScrollArea need these in jsdom.
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

// The per-row checkbox exposes `aria-label="<name> — select|deselect"`, so it
// doubles as the “is this path in the selection?” probe.
const selected = (name: string) =>
  screen.getByLabelText(`${name} — deselect`) as HTMLButtonElement;
const unselected = (name: string) =>
  screen.getByLabelText(`${name} — select`) as HTMLButtonElement;

describe("FileTree multi-select", () => {
  let fetchSpy: ReturnType<typeof mockTree>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    fetchSpy = mockTree();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("cmd-click toggles a file into the selection without opening it", async () => {
    const onOpen = vi.fn();
    render(<FileTree onOpenFile={onOpen} projectPath="/proj" />);
    await screen.findByText("a.ts");

    // Plain click opens the file and selects just it.
    fireEvent.click(screen.getByText("a.ts"));
    expect(onOpen).toHaveBeenCalledWith("src/a.ts", "/proj");
    expect(selected("a.ts")).toBeTruthy();

    onOpen.mockClear();
    // Modifier click extends the selection and must NOT open the file.
    fireEvent.click(screen.getByText("c.ts"), { metaKey: true });
    expect(onOpen).not.toHaveBeenCalled();
    expect(selected("a.ts")).toBeTruthy();
    expect(selected("c.ts")).toBeTruthy();
    expect(unselected("b.ts")).toBeTruthy();

    // Cmd-clicking an already-selected file removes it again.
    fireEvent.click(screen.getByText("c.ts"), { metaKey: true });
    expect(unselected("c.ts")).toBeTruthy();
    expect(selected("a.ts")).toBeTruthy();
  });

  it("shift-click selects a contiguous range and shift-clicking it again clears it", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await screen.findByText("a.ts");

    // Anchor on a.ts.
    fireEvent.click(screen.getByText("a.ts"));
    expect(selected("a.ts")).toBeTruthy();

    // Shift-click b.ts selects a.ts..b.ts (c.ts stays out).
    fireEvent.click(screen.getByText("b.ts"), { shiftKey: true });
    expect(selected("a.ts")).toBeTruthy();
    expect(selected("b.ts")).toBeTruthy();
    expect(unselected("c.ts")).toBeTruthy();

    // Both ends are selected, so shift-clicking b.ts again deselects the range.
    fireEvent.click(screen.getByText("b.ts"), { shiftKey: true });
    expect(unselected("a.ts")).toBeTruthy();
    expect(unselected("b.ts")).toBeTruthy();
  });
});
