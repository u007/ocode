import { describe, it, expect, vi, beforeEach, afterEach, beforeAll, afterAll } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import FilePicker from "./FilePicker";

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

// Answers one stat request for the typed path. The default echoes the typed
// path back as the canonical one, as the server does for a plain relative path.
type StatHandler = (typed: string, signal: AbortSignal | undefined) => Promise<Response> | Response;

describe("FilePicker direct path hit", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;
  let errorSpy: ReturnType<typeof vi.spyOn>;
  let statHandler: StatHandler;
  const stats: string[] = [];

  beforeEach(() => {
    stats.length = 0;
    statHandler = (typed) => json({ exists: true, is_dir: true, path: typed });
    errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    // The tree never contains the typed directory (truncated/slow index); only
    // the stat route knows it exists.
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockImplementation(async (input: any, init?: any) => {
      const url = String(input);
      if (url.includes("/api/files/stat")) {
        stats.push(url);
        const typed = new URL(url, "http://localhost").searchParams.get("path") ?? "";
        return statHandler(typed, init?.signal);
      }
      return json({ children: [{ name: "a.ts", path: "src/a.ts", is_dir: false }], truncated: true });
    });
  });
  afterEach(() => {
    fetchSpy.mockRestore();
    errorSpy.mockRestore();
  });

  it("lists a typed directory (trailing slash stripped) first and opens it as a folder", async () => {
    const onOpenFolder = vi.fn();
    render(<FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} onOpenFolder={onOpenFolder} projectPath="/proj" />);
    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());

    const input = screen.getByPlaceholderText("Filter by keywords...");
    fireEvent.change(input, { target: { value: "bench/site/2026-11-03-beyond/" } });

    const row = await screen.findByText("bench/site/2026-11-03-beyond");
    expect(stats[0]).toContain("path=bench%2Fsite%2F2026-11-03-beyond&");
    fireEvent.click(row);
    expect(onOpenFolder).toHaveBeenCalledWith("bench/site/2026-11-03-beyond", "/proj");
  });

  it("sends host for remote projects", async () => {
    render(<FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} onOpenFolder={vi.fn()} projectPath="/proj" projectHost="h1" />);
    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value: "x/y" } });
    await waitFor(() => expect(stats.length).toBeGreaterThan(0));
    expect(stats[0]).toContain("host=h1");
  });

  it("opens the server's canonical path for a ./-prefixed typed file", async () => {
    // The server spells the entry as the tree does; the typed "./" must not
    // reach the row or the open call (that would open a second tab).
    statHandler = (typed) => json({ exists: true, is_dir: false, path: typed.replace(/^\.\//, "") });
    const onOpenFile = vi.fn();
    render(<FilePicker open onClose={vi.fn()} onOpenFile={onOpenFile} projectPath="/proj" />);
    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value: "./src/a.ts" } });

    const rows = await screen.findAllByText("src/a.ts");
    expect(rows).toHaveLength(1);
    expect(screen.queryByText("./src/a.ts")).toBeNull();
    fireEvent.click(rows[0]);
    expect(onOpenFile).toHaveBeenCalledWith("src/a.ts", "/proj");
  });

  it("lists a typed file once when the tree already has it", async () => {
    statHandler = (typed) => json({ exists: true, is_dir: false, path: typed });
    render(<FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} projectPath="/proj" />);
    await waitFor(() => expect(screen.getByText("src/a.ts")).toBeInTheDocument());
    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value: "src/a.ts" } });

    await waitFor(() => expect(stats.length).toBeGreaterThan(0));
    // Let the stat answer land, then the row must still be listed once.
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.getAllByText("src/a.ts")).toHaveLength(1);
  });

  it("logs a non-ok stat response and shows no hit", async () => {
    statHandler = () => json({ error: "remote stat failed" }, 502);
    render(<FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} projectPath="/proj" projectHost="h1" />);
    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value: "bench/site/x" } });

    await waitFor(() => expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining("HTTP 502")));
    expect(screen.queryByText("bench/site/x")).toBeNull();
  });

  it("does not log a superseded (aborted) stat as an error", async () => {
    // The first query's stat hangs until the picker aborts it, as a slow
    // remote stat does when the user keeps typing.
    statHandler = (typed, signal) =>
      typed === "bench/site/x"
        ? new Promise<Response>((_, reject) =>
            signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError"))),
          )
        : json({ exists: false });
    render(<FilePicker open onClose={vi.fn()} onOpenFile={vi.fn()} projectPath="/proj" />);
    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value: "bench/site/x" } });
    await waitFor(() => expect(stats.length).toBe(1));

    fireEvent.change(screen.getByPlaceholderText("Filter by keywords..."), { target: { value: "bench/site/y" } });
    // The second stat fires after the 120ms debounce, by which time the aborted
    // rejection has been handled.
    await waitFor(() => expect(stats.length).toBe(2));
    expect(errorSpy).not.toHaveBeenCalled();
  });
});
