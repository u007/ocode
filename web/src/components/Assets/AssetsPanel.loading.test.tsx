import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emitTabLoadEvent, useTabLoadingStore, type LoadRequestEvent } from "@/hooks/useKeyedLoad";
import { TabLoadingOverlay } from "@/components/common/TabLoadingOverlay";
import AssetsPanel from "./AssetsPanel";

vi.mock("@/api/client", () => ({
  apiPath: (path: string) => path,
  authHeaders: () => ({}),
}));
vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ state: { activeProject: null } }),
}));

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

function LoadingOverlayProbe({ loadingKey }: { loadingKey: string }) {
  const state = useTabLoadingStore().get(loadingKey);
  return (
    <TabLoadingOverlay
      active={state?.phase === "initial"}
      error={state?.phase === "error" ? state.error : undefined}
      onRetry={state?.retry}
    />
  );
}

describe("AssetsPanel keyed loading", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(response([]));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("emits an empty completion for an empty uploads response", async () => {
    const events: LoadRequestEvent[] = [];
    render(<AssetsPanel loadingKey="assets-key" onLoadingEvent={(event) => events.push(event)} />);
    await waitFor(() => expect(events.some((event) => event.status === "empty")).toBe(true));
    expect(events[0].originKey).toBe("assets-key");
  });

  it("applies panel data when the initial error retry succeeds", async () => {
    const key = "assets-retry-key";
    let attempt = 0;
    fetchSpy.mockReset();
    fetchSpy.mockImplementation(async () => {
      attempt += 1;
      return attempt === 1
        ? new Response("failed", { status: 500 })
        : response([{ name: "recovered.txt", size: 1, modtime: new Date().toISOString(), mime: "text/plain" }]);
    });
    render(
      <>
        <AssetsPanel loadingKey={key} onLoadingEvent={emitTabLoadEvent} />
        <LoadingOverlayProbe loadingKey={key} />
      </>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    expect(await screen.findByText("recovered.txt")).toBeTruthy();
  });
});
