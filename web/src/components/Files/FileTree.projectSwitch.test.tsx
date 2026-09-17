import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import FileTree from "./FileTree";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: { ...actual.api, getPathsConfig: vi.fn(async () => ({ extra_allowed_paths: [] })) },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) => (open ? <>{children}</> : null),
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

/** Two projects share a relative dir name `src` but live on different hosts.
 *  Each has a distinct file under `src`, so a stale subtree is detectable. */
function mockFetch() {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (!url.includes("/api/files/tree")) return new Response("{}", { status: 200 });
    const u = new URL(url, "http://test");
    const path = u.searchParams.get("path") ?? "";
    const host = u.searchParams.get("host") ?? "";
    const remote = host !== "";
    if (path.endsWith("/src") || path === "src") {
      return json({
        children: remote
          ? [{ name: "remote-only.ts", path: "src/remote-only.ts", is_dir: false }]
          : [{ name: "local-only.ts", path: "src/local-only.ts", is_dir: false }],
        truncated: false,
      });
    }
    return json({
      children: [
        { name: "src", path: "src", is_dir: true },
        { name: "root.txt", path: "root.txt", is_dir: false },
      ],
      truncated: false,
    });
  });
}

describe("FileTree project switch (remote <-> local)", () => {
  let fetchSpy: ReturnType<typeof mockFetch>;
  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy = mockFetch();
  });
  afterEach(() => {
    fetchSpy.mockRestore();
    window.localStorage.clear();
  });

  it("tree view: expanded subtree refetches against the new project's host", async () => {
    const onOpenFile = vi.fn();
    const { rerender } = render(
      <FileTree onOpenFile={onOpenFile} projectPath="/remote/proj" projectHost="me@ssh-host" />,
    );

    fireEvent.click(await screen.findByText("src"));
    expect(await screen.findByText("remote-only.ts")).toBeDefined();

    rerender(<FileTree onOpenFile={onOpenFile} projectPath="/local/proj" />);
    await screen.findByText("root.txt");

    // Re-expand the (remounted) local `src` — must load local children, never remote.
    fireEvent.click(screen.getByText("src"));
    expect(await screen.findByText("local-only.ts")).toBeDefined();
    expect(screen.queryByText("remote-only.ts")).toBeNull();
  });

  it("tree view: local -> remote refetches with the remote host", async () => {
    const onOpenFile = vi.fn();
    const { rerender } = render(<FileTree onOpenFile={onOpenFile} projectPath="/local/proj" />);

    fireEvent.click(await screen.findByText("src"));
    expect(await screen.findByText("local-only.ts")).toBeDefined();

    rerender(<FileTree onOpenFile={onOpenFile} projectPath="/remote/proj" projectHost="me@ssh-host" />);
    await screen.findByText("root.txt");

    fireEvent.click(screen.getByText("src"));
    expect(await screen.findByText("remote-only.ts")).toBeDefined();
    expect(screen.queryByText("local-only.ts")).toBeNull();
  });

  it("columns view: previous project's deeper columns are dropped on switch", async () => {
    window.localStorage.setItem("ocode.ui.filetree_view.v1", "columns");
    const onOpenFile = vi.fn();
    const { rerender } = render(
      <FileTree onOpenFile={onOpenFile} projectPath="/remote/proj" projectHost="me@ssh-host" />,
    );

    fireEvent.click(await screen.findByText("src"));
    expect(await screen.findByText("remote-only.ts")).toBeDefined();

    rerender(<FileTree onOpenFile={onOpenFile} projectPath="/local/proj" />);

    await waitFor(() => expect(screen.queryByText("remote-only.ts")).toBeNull());
  });
});
