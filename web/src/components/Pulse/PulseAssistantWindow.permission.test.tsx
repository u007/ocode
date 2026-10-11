import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ChatProvider, useChatDispatch } from "../../stores/chatStore";
import { ProjectProvider } from "../../stores/projectStore";
import { PulseAssistantWindow } from "./PulseAssistantWindow";
import { resetAssistantPrefsForTests, setAssistantOpen } from "./pulseAssistantPrefs";

// The real useChat, chat store and PermissionDialog run here: the point is that
// a tool ask raised mid-turn for the assistant session (its write tools go
// through the normal permission Ask) reaches the user and resolves correctly.

const mockResolvePermission = vi.fn();
const mockGetSessionState = vi.fn();
vi.mock("../../api/client", async () => {
  const actual = await vi.importActual<typeof import("../../api/client")>("../../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      getPulseAssistant: vi.fn().mockResolvedValue({ session_id: "pulse_abc", model: "openai/gpt-x" }),
      setPulseModel: vi.fn(),
      resolvePermission: (...a: unknown[]) => mockResolvePermission(...a),
      getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
      listProjects: vi.fn().mockResolvedValue([]),
      getCurrentProject: vi.fn().mockResolvedValue(null),
      listProjectSessions: vi.fn().mockResolvedValue([]),
      listGroups: vi.fn().mockResolvedValue([]),
      getTabs: vi.fn().mockResolvedValue({ projects: {} }),
      setTabs: vi.fn().mockResolvedValue({ status: "ok" }),
    },
  };
});
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {} },
}));
vi.mock("../Chat/ChatPanel", () => ({ default: () => <div data-testid="chat-panel" /> }));
vi.mock("../Chat/ChatInput", () => ({ default: () => <div data-testid="chat-input" /> }));
vi.mock("../../hooks/useTurnWatchdog", () => ({ useTurnWatchdogAll: () => {} }));
vi.mock("../Layout/ModelDialog", () => ({ default: () => null }));

let dispatchRef: ReturnType<typeof useChatDispatch>;
function Probe() {
  dispatchRef = useChatDispatch();
  return null;
}

function mount() {
  // The window renders only while open, and open state is store state.
  setAssistantOpen(true);
  return render(
    <ProjectProvider>
      <ChatProvider>
        <Probe />
        <PulseAssistantWindow />
      </ChatProvider>
    </ProjectProvider>,
  );
}

/** The same action the SSE router dispatches for a `permission` frame. */
function raiseAsk(sessionId: string, over: Record<string, unknown> = {}) {
  act(() => {
    dispatchRef({
      type: "PERMISSION_REQUEST",
      sessionId,
      permission: { request_id: "req_1", tool: "session_send", command: "send hello to ses_9", scope: "tool", ...over },
    });
  });
}

beforeEach(() => {
  resetAssistantPrefsForTests();
  window.localStorage.clear();
  mockGetSessionState.mockReset();
  mockGetSessionState.mockResolvedValue({});
  mockResolvePermission.mockReset();
  mockResolvePermission.mockResolvedValue({});
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("PulseAssistantWindow permission asks from write tools", () => {
  it("renders the ask for the assistant session inside the drawer and Allow resolves by request id", async () => {
    mount();
    await screen.findByTestId("chat-panel");

    raiseAsk("pulse_abc");

    const dialog = await screen.findByRole("dialog");
    expect(screen.getByTestId("pulse-assistant-surface").contains(dialog)).toBe(true);
    expect(within(dialog).getByText("send hello to ses_9")).toBeInTheDocument();

    fireEvent.click(within(dialog).getByText("Allow once"));

    // Local-only: no host argument, even if a remote project were active.
    await waitFor(() =>
      expect(mockResolvePermission).toHaveBeenCalledWith("req_1", "pulse_abc", "allow", undefined),
    );
  });

  it("sends always_tool only after the confirm step", async () => {
    mount();
    await screen.findByTestId("chat-panel");
    raiseAsk("pulse_abc");
    const dialog = await screen.findByRole("dialog");

    fireEvent.click(within(dialog).getByText("Always allow tool"));
    expect(mockResolvePermission).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByText("Confirm"));

    await waitFor(() =>
      expect(mockResolvePermission).toHaveBeenCalledWith("req_1", "pulse_abc", "always_tool", undefined),
    );
  });

  it("denies through the dialog", async () => {
    mount();
    await screen.findByTestId("chat-panel");
    raiseAsk("pulse_abc");
    const dialog = await screen.findByRole("dialog");

    fireEvent.click(within(dialog).getByText("Deny"));

    await waitFor(() =>
      expect(mockResolvePermission).toHaveBeenCalledWith("req_1", "pulse_abc", "deny", undefined),
    );
  });

  it("recovers an ask raised before the drawer mounted, from the live session state", async () => {
    mockGetSessionState.mockResolvedValue({
      pending_asks: {
        permissions: [{ request_id: "req_old", tool: "session_send", command: "send earlier", scope: "tool" }],
      },
    });
    mount();

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("send earlier")).toBeInTheDocument();
    expect(mockGetSessionState).toHaveBeenCalledWith("pulse_abc", undefined);
  });

  it("ignores an ask raised for a different session", async () => {
    mount();
    await screen.findByTestId("chat-panel");

    raiseAsk("ses_other");

    await act(async () => {});
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
