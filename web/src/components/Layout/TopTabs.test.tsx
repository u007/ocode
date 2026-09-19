import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Tabs } from "@/components/ui/tabs";
import TopTabs from "./TopTabs";

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

function renderTopTabs(props: { onMenuToggle?: () => void } = {}) {
  return render(
    <Tabs value="sessions" onValueChange={() => {}}>
      <TopTabs activeTab="sessions" onTabSelect={() => {}} {...props} />
    </Tabs>,
  );
}

// jsdom has no ResizeObserver; TopTabs uses one to detect tab-strip overflow.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeEach(() => {
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub;
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
