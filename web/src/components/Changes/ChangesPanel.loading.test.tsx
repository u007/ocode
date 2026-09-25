import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LoadRequestEvent } from "@/hooks/useKeyedLoad";

const mocks = vi.hoisted(() => ({ listChanges: vi.fn() }));
vi.mock("@/api/client", () => ({
  api: {
    listChanges: mocks.listChanges,
    getChangeDiff: vi.fn(async () => ({ path: "", patch: "" })),
    undoChangeFile: vi.fn(),
    undoChangeBlock: vi.fn(),
  },
}));
vi.mock("./ChangesFileList", () => ({
  default: ({ files }: { files: Array<{ path: string }> }) => (
    <div data-testid="files">{files.map((file) => file.path).join(",")}</div>
  ),
}));
vi.mock("./ChangesDiffView", () => ({ default: () => <div data-testid="diff" /> }));

import ChangesPanel from "./ChangesPanel";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listChanges.mockResolvedValue([]);
});

describe("ChangesPanel keyed loading", () => {
  it("keeps two sessions' loading events independent", async () => {
    const events: LoadRequestEvent[] = [];
    render(
      <>
        <ChangesPanel
          session="session-a"
          loadingKey="host-a\0/project\0session-a:changes"
          onLoadingEvent={(event) => events.push(event)}
        />
        <ChangesPanel
          session="session-b"
          loadingKey="host-b\0/project\0session-b:changes"
          onLoadingEvent={(event) => events.push(event)}
        />
      </>,
    );
    await waitFor(() => expect(events.filter((event) => event.status === "start")).toHaveLength(2));
    expect(new Set(events.filter((event) => event.status === "start").map((event) => event.originKey)).size).toBe(2);
  });

  it("drops a late response after the loading key changes", async () => {
    let resolveFirst!: (value: Array<{ path: string }>) => void;
    let resolveSecond!: (value: Array<{ path: string }>) => void;
    const first = new Promise<Array<{ path: string }>>((resolve) => { resolveFirst = resolve; });
    const second = new Promise<Array<{ path: string }>>((resolve) => { resolveSecond = resolve; });
    mocks.listChanges.mockReturnValueOnce(first).mockReturnValueOnce(second);
    const events: LoadRequestEvent[] = [];
    const { rerender } = render(
      <ChangesPanel
        session="same-session"
        loadingKey="host\0/project-a\0same-session:changes"
        onLoadingEvent={(event) => events.push(event)}
      />,
    );
    await waitFor(() => expect(mocks.listChanges).toHaveBeenCalledTimes(1));
    rerender(
      <ChangesPanel
        session="same-session"
        loadingKey="host\0/project-b\0same-session:changes"
        onLoadingEvent={(event) => events.push(event)}
      />,
    );
    await waitFor(() => expect(mocks.listChanges).toHaveBeenCalledTimes(2));
    resolveFirst([{ path: "stale.txt" }]);
    await waitFor(() => expect(screen.getByText("Loading changes…")).toBeTruthy());
    resolveSecond([{ path: "current.txt" }]);
    await waitFor(() => expect(screen.getByTestId("files")).toHaveTextContent("current.txt"));
    expect(screen.getByTestId("files")).not.toHaveTextContent("stale.txt");
  });
});
