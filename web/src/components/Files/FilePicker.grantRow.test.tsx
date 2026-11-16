import { describe, it, expect, vi, beforeEach, afterEach, beforeAll, afterAll } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import FilePicker from "./FilePicker";

// The virtualizer needs a measurable scroll container; jsdom reports zeros.
class ResizeObserverMock {
  constructor(private cb: (entries: unknown[]) => void) {}
  observe(el: Element) {
    this.cb([{ target: el, contentRect: { width: 400, height: 300 }, borderBoxSize: [{ inlineSize: 400, blockSize: 300 }] }]);
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
  return { ...actual, apiPath: (p: string) => p, authHeaders: () => ({}) };
});

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const OUTSIDE = "/private/tmp/claude-501/-Users-james-app-nylakid/ca60ef1a/scratchpad";

describe("FilePicker outside-project row", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    // The server confines every file surface to the project root, so a stat for
    // an outside path is a plain "does not exist" — the case the row rescues.
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockImplementation(async (input: any) => {
      const url = String(input);
      if (url.includes("/api/files/stat")) return json({ exists: false });
      return json({ children: [{ name: "a.ts", path: "src/a.ts", is_dir: false }], truncated: false });
    });
  });
  afterEach(() => {
    fetchSpy.mockRestore();
  });

  const type = (value: string) =>
    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value } });

  it("offers the grant for a pasted absolute path outside the project", async () => {
    const onOpenOutsideProject = vi.fn();
    const onClose = vi.fn();
    render(
      <FilePicker
        open
        onClose={onClose}
        onOpenFile={vi.fn()}
        onOpenOutsideProject={onOpenOutsideProject}
        projectPath="/proj"
      />,
    );

    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());
    type(OUTSIDE);
    const row = screen.getByText(`Open outside the project: ${OUTSIDE}`);
    fireEvent.click(row);

    expect(onOpenOutsideProject).toHaveBeenCalledWith(OUTSIDE);
    expect(onClose).toHaveBeenCalled();
  });

  it("strips a pasted folder's trailing slash before handing the path back", async () => {
    const onOpenOutsideProject = vi.fn();
    render(
      <FilePicker
        open
        onClose={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenOutsideProject={onOpenOutsideProject}
        projectPath="/proj"
      />,
    );

    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());
    type(`${OUTSIDE}/`);
    fireEvent.click(screen.getByText(`Open outside the project: ${OUTSIDE}`));

    expect(onOpenOutsideProject).toHaveBeenCalledWith(OUTSIDE);
  });

  it("offers nothing for a relative path that simply does not exist", async () => {
    render(
      <FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} onOpenOutsideProject={vi.fn()} projectPath="/proj" />,
    );

    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());
    type("src/missing.ts");
    expect(screen.queryByText(/Open outside the project/)).toBeNull();
  });

  it("offers nothing for an absolute path INSIDE the project", async () => {
    render(
      <FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} onOpenOutsideProject={vi.fn()} projectPath="/proj" />,
    );

    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());
    type("/proj/src/missing.ts");
    expect(screen.queryByText(/Open outside the project/)).toBeNull();
  });

  it("treats a sibling whose path merely starts with the root as outside", async () => {
    // "/proj2" is NOT inside "/proj": the containment check needs the
    // separator boundary, not a bare prefix test.
    render(
      <FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} onOpenOutsideProject={vi.fn()} projectPath="/proj" />,
    );

    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());
    type("/proj2/src/missing.ts");
    expect(screen.getByText("Open outside the project: /proj2/src/missing.ts")).toBeInTheDocument();
  });

  it("renders no row when the caller passes no grant handler", async () => {
    render(<FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} projectPath="/proj" />);
    // Let the tree fetch land, so its state update is inside the test.
    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());

    type(OUTSIDE);
    expect(screen.queryByText(/Open outside the project/)).toBeNull();
  });
});
