import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoadRequestEvent } from "@/hooks/useKeyedLoad";

const mocks = vi.hoisted(() => ({
  getGitWorkspace: vi.fn(),
  gitLog: vi.fn(),
  gitStashList: vi.fn(),
  gitStashShow: vi.fn(),
  gitStashApply: vi.fn(),
  gitStashDrop: vi.fn(),
  gitStash: vi.fn(),
  gitHunk: vi.fn(),
  gitShow: vi.fn(),
  gitStage: vi.fn(),
  gitUnstage: vi.fn(),
  gitDiscard: vi.fn(),
  gitCommit: vi.fn(),
  gitPush: vi.fn(),
  gitFetch: vi.fn(),
  gitPull: vi.fn(),
  gitResetRemote: vi.fn(),
  on: vi.fn(() => () => {}),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("@/lib/eventBus", () => ({ eventBus: { on: mocks.on } }));

import GitPanel from "./GitPanel";

const workspace = {
  status: {
    branch: "main",
    staged_files: [],
    changed_files: [],
    has_changes: false,
    is_repo: true,
    ahead: 0,
    behind: 0,
    has_upstream: false,
  },
  staged: [],
  unstaged: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getGitWorkspace.mockResolvedValue(workspace);
  mocks.gitLog.mockResolvedValue([]);
  mocks.gitStashList.mockResolvedValue([]);
  window.matchMedia = ((media: string) => ({
    matches: false,
    media,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  window.localStorage.clear();
});

describe("GitPanel keyed loading", () => {
  it("emits an initial event without coupling the Git badge poll", async () => {
    const events: LoadRequestEvent[] = [];
    const key = "host\0/project\0git";
    render(
      <GitPanel
        projectPath="/project"
        projectHost="host"
        loadingKey={key}
        onLoadingEvent={(event) => events.push(event)}
      />,
    );
    await waitFor(() => expect(events[0]?.status).toBe("start"));
    expect(events[0].originKey).toBe(key);
    expect(mocks.getGitWorkspace).toHaveBeenCalledWith("/project", "host");
  });

  it("keeps the existing refresh button state independent from the event stream", async () => {
    let resolve!: (value: typeof workspace) => void;
    mocks.getGitWorkspace.mockReturnValue(new Promise((r) => { resolve = r; }));
    const events: LoadRequestEvent[] = [];
    render(
      <GitPanel
        projectPath="/project"
        loadingKey="git-key"
        onLoadingEvent={(event) => events.push(event)}
      />,
    );
    await waitFor(() => expect(events.some((event) => event.status === "start")).toBe(true));
    resolve(workspace);
    await act(async () => {
      await Promise.resolve();
    });
    expect(events.filter((event) => event.status === "start")).toHaveLength(1);
  });
});
