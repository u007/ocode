import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoadRequestEvent } from "@/hooks/useKeyedLoad";
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

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

describe("FileTree keyed loading", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    fetchSpy?.mockRestore();
    window.localStorage.clear();
  });

  it("emits a keyed initial start and an empty completion", async () => {
    let resolveTree!: (value: Response) => void;
    const treeResponse = new Promise<Response>((resolve) => {
      resolveTree = resolve;
    });
    fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (String(input).includes("/api/files/tree")) return treeResponse;
      return response({});
    });
    const events: LoadRequestEvent[] = [];
    const key = "host\0/project\0files";

    render(
      <FileTree
        onOpenFile={vi.fn()}
        projectPath="/project"
        loadingKey={key}
        onLoadingEvent={(event) => events.push(event)}
      />,
    );

    await waitFor(() => expect(events[0]?.status).toBe("start"));
    expect(events[0].originKey).toBe(key);
    resolveTree(response({ children: [], truncated: false }));
    await waitFor(() => expect(events[events.length - 1]?.status).toBe("empty"));
  });
});
