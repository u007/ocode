import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest";
import FileTree from "./FileTree";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: vi.fn(async () => ({ extra_allowed_paths: [] })),
      listFileGrants: vi.fn(async () => ({ roots: [] })),
    },
    apiPath: (path: string) => path,
    authHeaders: () => ({}),
  };
});

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

/** Tree-listing request URLs, in order. The active root is the `path` query. */
function treeRequests(fetchSpy: MockInstance<typeof fetch>): string[] {
  return fetchSpy.mock.calls
    .map(([input]) => String(input))
    .filter((url) => url.includes("/api/files/tree"));
}

function requestedRoot(url: string): string {
  return new URL(url, "http://test").searchParams.get("path") ?? "";
}

describe("FileTree open-folder event", () => {
  let fetchSpy: MockInstance<typeof fetch>;

  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async () => json({ children: [], truncated: false }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
  });

  it("switches the tree to the granted folder when host and project match", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await waitFor(() => expect(treeRequests(fetchSpy).map(requestedRoot)).toContain("/proj"));

    act(() => {
      window.dispatchEvent(
        new CustomEvent("ocode:open-folder", { detail: { root: "/granted", host: undefined, projectPath: "/proj" } }),
      );
    });

    await waitFor(() => expect(treeRequests(fetchSpy).map(requestedRoot)).toContain("/granted"));
  });

  it("ignores a grant made for another host, then still honours a matching one", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await waitFor(() => expect(treeRequests(fetchSpy).map(requestedRoot)).toContain("/proj"));

    // A local tree must not switch to a root granted on a remote host.
    act(() => {
      window.dispatchEvent(
        new CustomEvent("ocode:open-folder", { detail: { root: "/remote-granted", host: "remote-box", projectPath: "/proj" } }),
      );
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30));
    });
    expect(treeRequests(fetchSpy).map(requestedRoot)).not.toContain("/remote-granted");

    // Control: the same tree does switch for a matching local grant, so the
    // negative above is the host check and not a listener that never fires.
    act(() => {
      window.dispatchEvent(
        new CustomEvent("ocode:open-folder", { detail: { root: "/granted", host: undefined, projectPath: "/proj" } }),
      );
    });
    await waitFor(() => expect(treeRequests(fetchSpy).map(requestedRoot)).toContain("/granted"));
    expect(treeRequests(fetchSpy).map(requestedRoot)).not.toContain("/remote-granted");
  });
});
