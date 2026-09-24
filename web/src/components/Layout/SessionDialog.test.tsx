import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SessionDialog, { SESSION_DIALOG_PAGE_SIZE } from "./SessionDialog";

const mocks = vi.hoisted(() => ({
  closeSessionBackend: vi.fn(),
  cancelLiveDeltas: vi.fn(),
  chatDispatch: vi.fn(),
  closeSessionTab: vi.fn(),
  toggleSessionPicker: vi.fn(),
  projectSessions: [] as Array<{ id: string; title: string; created_at: string; updated_at: string }>,
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
      projectSessions: mocks.projectSessions,
      sessionsLoading: false,
      sessionPickerOpen: true,
      activeProject: { path: "/project", name: "Project" },
    },
    tabs: [{ id: "session-1" }],
    activeTabId: "session-1",
    closeSessionTab: mocks.closeSessionTab,
    openSessionTab: vi.fn(),
    toggleSessionPicker: mocks.toggleSessionPicker,
    openNewSessionTab: vi.fn(),
    prefetchProjectSessions: vi.fn(),
  }),
}));

vi.mock("../../lib/sessionEvents", () => ({
  closeSessionBackend: mocks.closeSessionBackend,
  cancelLiveDeltas: mocks.cancelLiveDeltas,
}));

describe("SessionDialog tab closing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.projectSessions = [session];
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

describe("SessionDialog session list", () => {
  const mainSession = (n: number) => ({
    id: `ses_2026-09-24-1200${String(n).padStart(2, "0")}-abcdef`,
    title: `Session ${n}`,
    created_at: "",
    updated_at: "",
  });

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("hides child-context (subagent) sessions", () => {
    mocks.projectSessions = [
      mainSession(1),
      {
        id: "ses_2026-09-24-133707-d5eda0e8_child_context_2026-09-24-133958",
        title: "Subagent scratchpad",
        created_at: "",
        updated_at: "",
      },
      mainSession(2),
    ];

    render(<SessionDialog />);

    expect(screen.getByText("Session 1")).toBeInTheDocument();
    expect(screen.getByText("Session 2")).toBeInTheDocument();
    expect(screen.queryByText("Subagent scratchpad")).not.toBeInTheDocument();
  });

  it("renders only the first page and streams more in on demand", () => {
    const total = SESSION_DIALOG_PAGE_SIZE + 10;
    mocks.projectSessions = Array.from({ length: total }, (_, i) => mainSession(i + 1));

    render(<SessionDialog />);

    // Only the first page is in the DOM, so a project with thousands of
    // sessions cannot block the dialog from opening.
    expect(screen.getAllByText(/^Session \d+$/)).toHaveLength(SESSION_DIALOG_PAGE_SIZE);

    const loadMore = screen.getByRole("button", { name: /Load more/ });
    fireEvent.click(loadMore);

    expect(screen.getAllByText(/^Session \d+$/)).toHaveLength(total);
    expect(screen.queryByRole("button", { name: /Load more/ })).not.toBeInTheDocument();
  });
});
