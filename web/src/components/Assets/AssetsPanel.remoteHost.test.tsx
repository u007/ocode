import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssetsPanel from "./AssetsPanel";

vi.mock("@/api/client", () => ({
  apiPath: (path: string) => path,
  // Mirrors the real implementation: the local server reverse-proxies
  // /api/remote/<encoded-host>/… to the host's `ocode serve --remote`.
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { activeProject: { path: "~/www/aimsai2", host: "james@217.216.72.49" } },
  }),
}));

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

describe("AssetsPanel remote host scoping", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(response([]));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    vi.restoreAllMocks();
  });

  it("proxies the uploads listing to the project's remote host", async () => {
    render(<AssetsPanel loadingKey="assets-remote" onLoadingEvent={() => {}} projectHost="james@217.216.72.49" />);

    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const url = String(fetchSpy.mock.calls[0][0]);
    expect(url).toBe(
      `/api/remote/james%40217.216.72.49/api/uploads?project=${encodeURIComponent("~/www/aimsai2")}`,
    );
  });
});
