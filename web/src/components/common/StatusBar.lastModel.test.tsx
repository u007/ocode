import { render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { ChatProvider, useChatDispatch } from "../../stores/chatStore";
import StatusBar from "./StatusBar";

// jsdom ships no matchMedia and StatusBar now reads the mobile breakpoint via
// useIsMobile; default to the desktop answer (matches: false).
beforeAll(() => {
  window.matchMedia = ((media: string) => ({
    matches: false,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

vi.mock("../../stores/projectStore", () => ({
  // resolveSessionHost (useSessionHost.ts) imports this directly, so the
  // real implementation runs against the stub state in these tests.
  findTabForSession: () => undefined,
  // StatusBar now resolves the session's host for the workspace chip; an
  // unknown path must resolve to "no host", not throw.
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({ state: {}, activeTabId: "s1" }),
}));

vi.mock("../../components/Speech/SpeechProvider", () => ({
  useSpeech: () => ({ toolbarVisible: false, toggleToolbar: vi.fn() }),
}));

vi.mock("./statusBarCollapse", () => ({
  loadStatusBarCollapsed: () => false,
  saveStatusBarCollapsed: vi.fn(),
  subscribeStatusBarCollapsed: () => () => {},
}));

function Seed({ lastDispatchedModel }: { lastDispatchedModel?: string }) {
  const dispatch = useChatDispatch();
  useEffect(() => {
    dispatch({ type: "SET_TUI_STATUS", sessionId: "s1", status: { main_model: "sidebar/model" } });
    if (lastDispatchedModel) {
      dispatch({ type: "SET_LAST_DISPATCHED_MODEL", sessionId: "s1", model: lastDispatchedModel } as never);
    }
  }, [dispatch, lastDispatchedModel]);
  return null;
}

function renderBar(lastDispatchedModel?: string) {
  return render(
    <ChatProvider>
      <Seed lastDispatchedModel={lastDispatchedModel} />
      <StatusBar />
    </ChatProvider>,
  );
}

describe("StatusBar last dispatched model", () => {
  it("does not present the sidebar model before a dispatch", async () => {
    renderBar();
    await waitFor(() => expect(screen.queryByText(/model:/)).toBeNull());
  });

  it("shows the dispatched model instead of the sidebar model", async () => {
    renderBar("backend/model");
    await waitFor(() => expect(screen.getByText(/last model: backend\/model/)).toBeTruthy());
    expect(screen.queryByText(/sidebar\/model/)).toBeNull();
  });
});
