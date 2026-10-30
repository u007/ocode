import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Tabs } from "@/components/ui/tabs";
import TopTabs from "./TopTabs";

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: {
      activeProject: { name: "proj", path: "/proj" },
      tabsByProject: {},
      projects: [],
    },
  }),
}));
vi.mock("@/hooks/useRemoteHostStatus", () => ({
  useRemoteHostStatus: () => ({ status: null }),
}));
vi.mock("@/api/client", () => ({
  api: {
    getGitStatus: vi.fn().mockResolvedValue({
      staged_files: [],
      changed_files: [],
      conflicts: [],
      ahead: 0,
      behind: 0,
      has_upstream: false,
    }),
  },
}));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: () => () => {} },
}));
vi.mock("../Terminal/terminalPersistence", () => ({
  loadProjectTerminals: () => null,
}));
vi.mock("./SyncStatusWidget", () => ({ default: () => null }));
vi.mock("./PortMapsWidget", () => ({ default: () => null }));
vi.mock("../Pulse/PulseAssistantWindow", () => ({ AssistantToggleButton: () => null }));

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

describe("TopTabs", () => {
  it("shows the DB project tab immediately beside Assets", () => {
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    try {
      render(
        <Tabs value="sessions">
          <TopTabs activeTab="sessions" onTabSelect={() => {}} />
        </Tabs>,
      );
      const labels = screen.getAllByRole("tab").map((tab) => tab.textContent ?? "");
      const assets = labels.findIndex((label) => label.includes("Assets"));
      const db = labels.indexOf("DB");
      expect(assets).toBeGreaterThanOrEqual(0);
      expect(db).toBe(assets + 1);
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
