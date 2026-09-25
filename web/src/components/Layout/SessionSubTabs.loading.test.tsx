import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { tabLoadKey } from "@/hooks/useKeyedLoad";
import SessionSubTabs from "./SessionSubTabs";

const activeTab = {
  id: "session-a",
  projectPath: "/project",
  activeSubTab: "chat" as const,
};

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    tabs: [activeTab],
    activeTabId: activeTab.id,
    dispatch: vi.fn(),
  }),
}));
vi.mock("../../stores/chatStore", () => ({
  getSessionSlice: vi.fn(() => ({})),
  useChatSelector: vi.fn(() => ({})),
}));
vi.mock("@/hooks/useSessionHost", () => ({
  resolveSessionHost: () => undefined,
}));

describe("SessionSubTabs loading indicators", () => {
  it("marks the Changes sub-tab busy from its session-specific key", () => {
    const key = tabLoadKey(undefined, "/project", "session-a:changes");
    render(<SessionSubTabs loadingStates={new Map([[key, { phase: "refresh" }]])} />);
    const changes = screen.getByRole("button", { name: /Changes/ });
    expect(changes).toHaveAttribute("aria-busy", "true");
    expect(changes).toHaveTextContent("Loading Changes");
  });
});
