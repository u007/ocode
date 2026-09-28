import { render, waitFor, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import FileTree from "./FileTree";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: { ...actual.api, getPathsConfig: vi.fn(async () => ({ extra_allowed_paths: [] })) },
    apiPath: (path: string) => path,
    authHeaders: () => ({}),
  };
});

// Mock eventBus to capture handlers so tests can emit events
const busHandlers = new Map<string, Set<(env: unknown) => void>>();
const busEnv = (event: string, session_id: string | undefined, data: unknown) => ({
  event,
  session_id,
  seq: 1,
  data,
});
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      let set = busHandlers.get(event);
      if (!set) {
        set = new Set();
        busHandlers.set(event, set);
      }
      set.add(handler);
      return () => set!.delete(handler);
    },
  },
}));
function emit(event: string, session_id: string | undefined, data: unknown) {
  for (const h of busHandlers.get(event) ?? []) h(busEnv(event, session_id, data));
}

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function mockTreeFetch() {
  let fetchCount = 0;
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.includes("/api/files/tree")) {
      fetchCount++;
      return response({ children: [], truncated: false, is_git_repo: false });
    }
    return response({});
  });
  return { fetchSpy, getCount: () => fetchCount };
}

describe("FileTree refresh button", () => {
  beforeEach(() => {
    window.localStorage.clear();
    busHandlers.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it("re-fetches the tree when the refresh button is clicked", async () => {
    const { fetchSpy, getCount } = mockTreeFetch();

    const { getByTitle } = render(
      <FileTree onOpenFile={vi.fn()} projectPath="/project" />,
    );

    await waitFor(() => expect(getCount()).toBe(1));

    const refreshBtn = getByTitle("Refresh file tree");
    fireEvent.click(refreshBtn);

    await waitFor(() => expect(getCount()).toBe(2));
    fetchSpy.mockRestore();
  });
});

describe("FileTree auto-refresh on tool_start", () => {
  beforeEach(() => {
    window.localStorage.clear();
    busHandlers.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it("refreshes when a mutating tool targets a path within the tree root", async () => {
    const { fetchSpy, getCount } = mockTreeFetch();

    render(<FileTree onOpenFile={vi.fn()} projectPath="/project" />);
    await waitFor(() => expect(getCount()).toBe(1));

    emit("tool_start", "ses-1", { tool: "write", command: '{"path":"src/foo.ts","content":"hello"}' });

    // Wait for the debounce (500ms) to fire
    await new Promise((r) => setTimeout(r, 700));

    expect(getCount()).toBe(2);
    fetchSpy.mockRestore();
  }, 10000);

  it("does NOT refresh for non-mutating tools like read", async () => {
    const { fetchSpy, getCount } = mockTreeFetch();

    render(<FileTree onOpenFile={vi.fn()} projectPath="/project" />);
    await waitFor(() => expect(getCount()).toBe(1));

    emit("tool_start", "ses-1", { tool: "read", command: '{"path":"src/foo.ts"}' });

    await new Promise((r) => setTimeout(r, 700));

    expect(getCount()).toBe(1);
    fetchSpy.mockRestore();
  }, 10000);

  it("does NOT refresh when the tool targets a path outside the tree root", async () => {
    const { fetchSpy, getCount } = mockTreeFetch();

    render(<FileTree onOpenFile={vi.fn()} projectPath="/project" />);
    await waitFor(() => expect(getCount()).toBe(1));

    emit("tool_start", "ses-1", { tool: "write", command: '{"path":"/other/project/foo.ts","content":"x"}' });

    await new Promise((r) => setTimeout(r, 700));

    expect(getCount()).toBe(1);
    fetchSpy.mockRestore();
  }, 10000);

  it("merges a burst of edits into a single refresh", async () => {
    const { fetchSpy, getCount } = mockTreeFetch();

    render(<FileTree onOpenFile={vi.fn()} projectPath="/project" />);
    await waitFor(() => expect(getCount()).toBe(1));

    // Simulate 3 rapid writes (burst) — should all land within the debounce window
    emit("tool_start", "ses-1", { tool: "write", command: '{"path":"src/a.ts","content":"a"}' });
    emit("tool_start", "ses-1", { tool: "write", command: '{"path":"src/b.ts","content":"b"}' });
    emit("tool_start", "ses-1", { tool: "edit", command: '{"path":"src/c.ts","old_string":"x","new_string":"y"}' });

    await new Promise((r) => setTimeout(r, 700));

    // Should be exactly 2 (initial + 1 merged refresh), not 4
    expect(getCount()).toBe(2);
    fetchSpy.mockRestore();
  }, 10000);
});
