import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { Tabs } from "@/components/ui/tabs";
import TopTabs from "./TopTabs";
import { tabLoadKey } from "@/hooks/useKeyedLoad";
import { api } from "@/api/client";

vi.mock("./SyncStatusWidget", () => ({ default: () => null }));
vi.mock("./PortMapsWidget", () => ({ default: () => null }));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { activeProject: { path: "/proj", name: "proj" }, projects: [], tabsByProject: {} },
  }),
}));

vi.mock("../Terminal/terminalPersistence", () => ({
  loadProjectTerminals: () => ({ terminals: [] }),
}));

vi.mock("../../api/client", () => ({
  api: { getGitStatus: vi.fn().mockResolvedValue({ staged_files: [], changed_files: [] }) },
}));

vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {} },
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
