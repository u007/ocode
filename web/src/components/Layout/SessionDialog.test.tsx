import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SessionDialog from "./SessionDialog";

const mocks = vi.hoisted(() => ({
  closeSessionBackend: vi.fn(),
  cancelLiveDeltas: vi.fn(),
  chatDispatch: vi.fn(),
  closeSessionTab: vi.fn(),
  toggleSessionPicker: vi.fn(),
}));

const session = {
  id: "session-1",
  title: "Alpha session",
  created_at: "",
  updated_at: "",
};

vi.mock("../../stores/chatStore", () => ({
  useChatDispatch: () => mocks.chatDispatch,
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: {
      projectSessions: [session],
      sessionsLoading: false,
      sessionPickerOpen: true,
      activeProject: { path: "/project", name: "Project" },
    },
    tabs: [{ id: session.id }],
    activeTabId: session.id,
    closeSessionTab: mocks.closeSessionTab,
    openSessionTab: vi.fn(),
    toggleSessionPicker: mocks.toggleSessionPicker,
    openNewSessionTab: vi.fn(),
  }),
}));

vi.mock("../../lib/sessionEvents", () => ({
  closeSessionBackend: mocks.closeSessionBackend,
  cancelLiveDeltas: mocks.cancelLiveDeltas,
}));

describe("SessionDialog tab closing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("asks for confirmation when the X is clicked", () => {
    render(<SessionDialog />);

    fireEvent.click(screen.getByRole("button", { name: "Close Alpha session" }));

    expect(screen.getByText("Close session tab?")).toBeInTheDocument();
    expect(mocks.closeSessionBackend).not.toHaveBeenCalled();
    expect(mocks.closeSessionTab).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByText("Close session tab?")).not.toBeInTheDocument();
    expect(mocks.closeSessionBackend).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Close Alpha session" }));
    fireEvent.click(screen.getByRole("button", { name: "Close tab" }));

    expect(mocks.closeSessionBackend).toHaveBeenCalledTimes(1);
    expect(mocks.closeSessionBackend).toHaveBeenCalledWith(session.id);
    expect(mocks.closeSessionTab).toHaveBeenCalledTimes(1);
    expect(mocks.closeSessionTab).toHaveBeenCalledWith(session.id);
  });

  it("closes immediately on middle-click without showing confirmation", () => {
    render(<SessionDialog />);

    fireEvent.mouseDown(screen.getByText("Alpha session"), { button: 1 });

    expect(mocks.closeSessionBackend).toHaveBeenCalledWith(session.id);
    expect(mocks.closeSessionTab).toHaveBeenCalledWith(session.id);
    expect(screen.queryByText("Close session tab?")).not.toBeInTheDocument();
  });
});
