import { describe, expect, it, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import FileTree from "./FileTree";

const mocksTree = vi.hoisted(() => ({
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocksTree.getPathsConfig,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

const SIMPLE_TREE = {
  children: [
    { name: "a.ts", path: "src/a.ts", is_dir: false },
    { name: "b.ts", path: "src/b.ts", is_dir: false },
  ],
  truncated: false,
};

function mockSimpleTree() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation(
    (async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/files/tree")) {
        return new Response(JSON.stringify(SIMPLE_TREE), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify({}), { status: 404 });
    }) as any,
  );
}

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

describe("FileTree includedPaths indicator", () => {
  let fetchSpy: ReturnType<typeof mockSimpleTree>;
  beforeEach(() => {
    vi.clearAllMocks();
    mocksTree.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    fetchSpy = mockSimpleTree();
  });
  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("shows a blue dot for files included in chat context and hides it when not included", async () => {
    const { rerender } = render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" includedPaths={["src/a.ts"]} />);
    await waitFor(() => expect(screen.getByText("a.ts")).toBeInTheDocument());
    const dots = screen.getAllByLabelText("Included in chat context");
    expect(dots).toHaveLength(1);
    expect(screen.getByText("b.ts")).toBeInTheDocument();
    expect(dots[0].getAttribute("title")).toContain("Included in chat context");
    rerender(<FileTree onOpenFile={vi.fn()} projectPath="/proj" includedPaths={[]} />);
    await waitFor(() => expect(screen.getByText("b.ts")).toBeInTheDocument());
    expect(screen.queryAllByLabelText("Included in chat context")).toHaveLength(0);
  });
});
