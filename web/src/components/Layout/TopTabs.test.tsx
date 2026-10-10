import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { Tabs } from "@/components/ui/tabs";
import TopTabs from "./TopTabs";
import { tabLoadKey } from "@/hooks/useKeyedLoad";
import { api } from "@/api/client";

vi.mock("./SyncStatusWidget", () => ({ default: () => null }));
vi.mock("./PortMapsWidget", () => ({ default: () => null }));

const activeProject = vi.hoisted(() => ({ current: { path: "/proj", name: "proj" } as { path: string; name: string; host?: string } }));
vi.mock("../../stores/projectStore", () => ({
  // resolveSessionHost (useSessionHost.ts) imports this directly, so the
  // real implementation runs against the stub state in these tests.
  findTabForSession: () => undefined,
  useProjectState: () => ({
    state: { activeProject: activeProject.current, projects: [], tabsByProject: {} },
  }),
}));

const hostConnected = vi.hoisted(() => ({ current: false }));
vi.mock("../../hooks/useRemoteHostStatus", () => ({
  useRemoteHostStatus: (host: string | undefined) => ({
    status: host ? { connected: hostConnected.current } : null,
  }),
}));

vi.mock("../Terminal/terminalPersistence", () => ({
  loadProjectTerminals: () => ({ terminals: [] }),
}));

vi.mock("../../api/client", () => ({
  api: { getGitStatus: vi.fn().mockResolvedValue({ staged_files: [], changed_files: [] }) },
}));

const busHandlers = vi.hoisted(() => [] as Array<(env: { project?: string }) => void>);
vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (_event: string, handler: (env: { project?: string }) => void) => {
      busHandlers.push(handler);
      return () => {};
    },
  },
}));

function renderTopTabs(props: { onMenuToggle?: () => void; loadingStates?: ReadonlyMap<string, { phase: "initial" | "refresh" | "error" | "idle" }> } = {}) {
  return render(
    <Tabs value="sessions" onValueChange={() => {}}>
      <TopTabs activeTab="sessions" onTabSelect={() => {}} {...props} />
    </Tabs>,
  );
}

// jsdom has no ResizeObserver; TopTabs uses one to detect tab-strip overflow.
class ResizeObserverStub {
  static instances: Array<() => void> = [];
  constructor(private readonly callback: () => void) {
    ResizeObserverStub.instances.push(() => this.callback());
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeEach(() => {
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub;
  ResizeObserverStub.instances.length = 0;
  vi.clearAllMocks();
  busHandlers.length = 0;
  activeProject.current = { path: "/proj", name: "proj" };
  hostConnected.current = false;
});

describe("TopTabs assistant toggle", () => {
  it("puts the one Toggle assistant control in the always-visible top bar", () => {
    renderTopTabs();
    expect(screen.getAllByRole("button", { name: "Toggle assistant" })).toHaveLength(1);
  });
});

describe("TopTabs mobile project-drawer launcher", () => {
  it("renders no launcher without onMenuToggle (desktop / preview hosts)", () => {
    renderTopTabs();
    expect(screen.queryByRole("button", { name: "Open projects sidebar" })).toBeNull();
  });

  it("renders the launcher and calls onMenuToggle when tapped", () => {
    const onMenuToggle = vi.fn();
    renderTopTabs({ onMenuToggle });
    const btn = screen.getByRole("button", { name: "Open projects sidebar" });
    // Hidden from md up so it only exists in the mobile breakpoint.
    expect(btn.className).toMatch(/md:hidden/);
    fireEvent.click(btn);
    expect(onMenuToggle).toHaveBeenCalledTimes(1);
  });
});

describe("TopTabs loading indicators", () => {
  it("marks a refreshing tab busy and renders its inline spinner", () => {
    const key = tabLoadKey(undefined, "/proj", "git");
    renderTopTabs({ loadingStates: new Map([[key, { phase: "refresh" }]]) });
    expect(ResizeObserverStub.instances.length).toBeGreaterThan(0);
    const trigger = screen.getByRole("tab", { name: /Git/ });
    expect(trigger).toHaveAttribute("aria-busy", "true");
    expect(trigger).toHaveTextContent("Loading Git");
  });

  it("renders an error dot for an initial load failure", () => {
    const key = tabLoadKey(undefined, "/proj", "files");
    renderTopTabs({ loadingStates: new Map([[key, { phase: "error" }]]) });
    expect(screen.getByRole("tab", { name: /Files/ })).toHaveTextContent("Loading Files");
  });

  it("does not turn the independent Git badge poll into loading state", async () => {
    const key = tabLoadKey(undefined, "/proj", "git");
    renderTopTabs({ loadingStates: new Map([[key, { phase: "refresh" }]]) });
    await waitFor(() => expect(api.getGitStatus).toHaveBeenCalledWith("/proj", undefined));
    expect(screen.getByRole("tab", { name: /Git/ })).toHaveTextContent("Loading Git");
  });

  it("counts conflicted files in the Git badge and calls them out in the title", async () => {
    // Conflicted paths are excluded from staged_files/changed_files on the
    // server, so the badge must add the conflicts list or a halted merge would
    // show a total that is too low.
    vi.mocked(api.getGitStatus).mockResolvedValue({
      branch: "main",
      staged_files: ["a.ts"],
      changed_files: ["b.ts"],
      conflicts: [{ path: "c.ts", code: "UU", ours: true, theirs: true }],
      has_changes: true,
      is_repo: true,
      ahead: 0,
      behind: 0,
      has_upstream: false,
    });

    renderTopTabs();

    const trigger = await screen.findByRole("tab", { name: /Git/ });
    // The total and the breakdown title live on the count badge, not the tab.
    const badge = await within(trigger).findByLabelText("Git count 3");
    expect(badge).toHaveAttribute("title", "1 conflicted · 1 staged · 1 unstaged");
    // 1 staged + 1 unstaged + 1 conflicted.
    expect(badge).toHaveTextContent("3");
  });

  it("keeps the indicator inside the overflow Select portal", async () => {
    const originalScrollIntoView = HTMLElement.prototype.scrollIntoView;
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    const key = tabLoadKey(undefined, "/proj", "git");
    renderTopTabs({ loadingStates: new Map([[key, { phase: "refresh" }]]) });
    const tabList = screen.getByRole("tablist");
    Object.defineProperty(tabList, "scrollWidth", { configurable: true, value: 1000 });
    Object.defineProperty(tabList, "clientWidth", { configurable: true, value: 100 });
    expect(tabList.scrollWidth).toBe(1000);
    expect(tabList.clientWidth).toBe(100);
    act(() => {
      for (const trigger of ResizeObserverStub.instances) trigger();
      fireEvent.resize(window);
    });
    const more = await screen.findByRole("combobox", { name: "More tabs" });
    fireEvent.click(more);
    await waitFor(() => expect(within(document.body).getByRole("option", { name: /Git/ })).toHaveTextContent("Loading Git"));
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: originalScrollIntoView,
    });
  });
});

describe("TopTabs git badge poll", () => {
  it("never overlaps requests: events during an in-flight fetch collapse into one trailing fetch", async () => {
    // A slow remote git status (SSH, up to 30s) used to be re-requested on
    // every git_status event and 10s tick, piling up concurrent requests.
    const resolvers: Array<() => void> = [];
    vi.mocked(api.getGitStatus).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolvers.push(() => resolve({ staged_files: [], changed_files: [] } as never));
        }),
    );
    renderTopTabs();
    await waitFor(() => expect(api.getGitStatus).toHaveBeenCalledTimes(1));

    act(() => {
      for (let i = 0; i < 5; i++) busHandlers.forEach((h) => h({ project: "/proj" }));
    });
    expect(api.getGitStatus).toHaveBeenCalledTimes(1);

    await act(async () => resolvers[0]());
    await waitFor(() => expect(api.getGitStatus).toHaveBeenCalledTimes(2));

    await act(async () => resolvers[1]());
    expect(api.getGitStatus).toHaveBeenCalledTimes(2);
  });
});

describe("TopTabs git badge errors", () => {
  it("keeps the last counts and shows an error dot when a refresh fails, instead of reading as clean", async () => {
    vi.mocked(api.getGitStatus).mockResolvedValueOnce({
      branch: "main",
      staged_files: ["a.ts"],
      changed_files: ["b.ts"],
      conflicts: [],
      has_changes: true,
      is_repo: true,
      ahead: 0,
      behind: 0,
      has_upstream: false,
    });
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    renderTopTabs();
    const trigger = await screen.findByRole("tab", { name: /Git/ });
    await within(trigger).findByLabelText("Git count 2");

    vi.mocked(api.getGitStatus).mockRejectedValueOnce(new Error("remote git status: Connection refused"));
    await act(async () => busHandlers.forEach((h) => h({ project: "/proj" })));

    await waitFor(() =>
      expect(within(trigger).getByRole("status")).toHaveTextContent("Git status unavailable: remote git status: Connection refused"),
    );
    expect(within(trigger).getByLabelText("Git count 2")).toBeInTheDocument();
    errSpy.mockRestore();
  });

  it("clears the error dot once a refresh succeeds again", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(api.getGitStatus).mockRejectedValueOnce(new Error("boom"));
    renderTopTabs();
    const trigger = await screen.findByRole("tab", { name: /Git/ });
    await waitFor(() => expect(within(trigger).getByRole("status")).toHaveTextContent("Git status unavailable: boom"));

    vi.mocked(api.getGitStatus).mockResolvedValueOnce({ staged_files: [], changed_files: [] } as never);
    await act(async () => busHandlers.forEach((h) => h({ project: "/proj" })));
    await waitFor(() => expect(within(trigger).queryByRole("status")).toBeNull());
    errSpy.mockRestore();
  });
});

describe("TopTabs git badge on a remote project", () => {
  it("does not dial a disconnected host, and starts once it connects", async () => {
    activeProject.current = { path: "/srv/app", name: "app", host: "me@box" };
    hostConnected.current = false;
    const { rerender } = renderTopTabs();
    act(() => busHandlers.forEach((h) => h({ project: "/srv/app" })));
    expect(api.getGitStatus).not.toHaveBeenCalled();

    hostConnected.current = true;
    rerender(
      <Tabs value="sessions" onValueChange={() => {}}>
        <TopTabs activeTab="sessions" onTabSelect={() => {}} />
      </Tabs>,
    );
    await waitFor(() => expect(api.getGitStatus).toHaveBeenCalledWith("/srv/app", "me@box"));
  });
});
