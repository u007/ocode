import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

import { api } from "../../api/client";
import { ChatProvider } from "../../stores/chatStore";
import { TerminalProvider } from "../../stores/terminalStore";
import { BrowserTabsProvider } from "../../stores/browserTabsStore";
import { browserStore } from "../../lib/browserStore";
import type { FocusedKind } from "../../lib/viewPersistence";
import UnifiedTabBar from "./UnifiedTabBar";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: () => () => {}, off: () => {}, onReconnect: () => () => {}, offReconnect: () => {} },
}));

vi.mock("@/hooks/useTerminalConfig", () => ({ useTerminalConfig: () => ({ available: true }) }));

vi.mock("../../api/client", () => ({
  api: {
    setSessionTitle: vi.fn().mockResolvedValue(undefined),
    closeSession: vi.fn().mockResolvedValue({ cancelled: true }),
    // terminalStore restores the shared server-side tab list on mount; a bare
    // mock without it logs a restore failure on every render.
    getTerminalTabs: vi.fn().mockResolvedValue({ projects: {} }),
  },
  authedFetch: vi.fn().mockResolvedValue({ ok: true, status: 200 }),
  remoteApiBase: (host: string) => `/api/remote/${encodeURIComponent(host)}`,
}));

let projectFake: {
  state: { activeProject: { path: string; name: string } | null; projects: unknown[] };
  tabs: { id: string; projectPath: string; title: string; activeSubTab: "chat" }[];
  activeTabId: string | null;
};
const openSessionTab = vi.fn();
const closeSessionTab = vi.fn();
const openNewSessionTab = vi.fn(() => "new-1");
const toggleSessionPicker = vi.fn();
const projectDispatch = vi.fn();

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: projectFake.state,
    tabs: projectFake.tabs,
    activeTabId: projectFake.activeTabId,
    openSessionTab,
    closeSessionTab,
    openNewSessionTab,
    toggleSessionPicker,
    dispatch: projectDispatch,
  }),
  findTabForSession: () => undefined,
  findProjectPathForTab: () => projectFake.tabs[0]?.projectPath ?? null,
}));

function renderBar(focusedKind: FocusedKind = "chat") {
  const onFocusKindChange = vi.fn();
  const utils = render(
    <ChatProvider>
      <TerminalProvider>
        <BrowserTabsProvider>
          <UnifiedTabBar focusedKind={focusedKind} onFocusKindChange={onFocusKindChange} />
        </BrowserTabsProvider>
      </TerminalProvider>
    </ChatProvider>,
  );
  return { onFocusKindChange, ...utils };
}

beforeEach(() => {
  window.localStorage.clear();
  browserStore.setState(() => ({ byKey: {} }));
  copyTextToClipboard.mockClear();
  copyTextToClipboard.mockResolvedValue(true);
  openSessionTab.mockClear();
  closeSessionTab.mockClear();
  openNewSessionTab.mockClear();
  toggleSessionPicker.mockClear();
  projectDispatch.mockClear();
  (api.closeSession as unknown as ReturnType<typeof vi.fn>).mockClear?.();
  (api.setSessionTitle as unknown as ReturnType<typeof vi.fn>).mockClear?.();
  projectFake = {
    state: { activeProject: { path: "/proj", name: "proj" }, projects: [] },
    tabs: [{ id: "s1", projectPath: "/proj", title: "Chat One", activeSubTab: "chat" }],
    activeTabId: "s1",
  };
});

describe("UnifiedTabBar — copy session ID", () => {
  it("copies the chat session id from the tab pill", async () => {
    renderBar();
    await act(async () => {
      fireEvent.click(screen.getByTestId("tab-copy-session-id-chat:s1"));
    });
    expect(copyTextToClipboard).toHaveBeenCalledWith("s1");
  });

  // The pill is the tab switcher: a copy click that bubbled would navigate away
  // from the session whose ID was just copied.
  it("does not switch tabs when the copy button is clicked", async () => {
    projectFake.tabs = [
      { id: "s1", projectPath: "/proj", title: "Chat One", activeSubTab: "chat" },
      { id: "s2", projectPath: "/proj", title: "Chat Two", activeSubTab: "chat" },
    ];
    projectFake.activeTabId = "s1";
    const { onFocusKindChange } = renderBar();

    await act(async () => {
      fireEvent.click(screen.getByTestId("tab-copy-session-id-chat:s2"));
    });

    expect(copyTextToClipboard).toHaveBeenCalledWith("s2");
    expect(projectDispatch).not.toHaveBeenCalled();
    expect(onFocusKindChange).not.toHaveBeenCalled();
  });

  it("shows a copy button per chat pill, one per session", () => {
    projectFake.tabs = [
      { id: "s1", projectPath: "/proj", title: "Chat One", activeSubTab: "chat" },
      { id: "s2", projectPath: "/proj", title: "Chat Two", activeSubTab: "chat" },
    ];
    renderBar();
    expect(screen.getByTestId("tab-copy-session-id-chat:s1")).toBeTruthy();
    expect(screen.getByTestId("tab-copy-session-id-chat:s2")).toBeTruthy();
  });

  // A `new-*` tab id is a client-side draft placeholder; there is no stored
  // session behind it, so offering a copy button would hand the user an id that
  // resolves to nothing.
  it("omits the copy button for an unsaved draft chat tab", () => {
    projectFake.tabs = [{ id: "new-1", projectPath: "/proj", title: "New session", activeSubTab: "chat" }];
    projectFake.activeTabId = "new-1";
    renderBar();
    expect(screen.queryByTestId("tab-copy-session-id-chat:new-1")).toBeNull();
  });

  // Terminal and browser pills have no session id; a copy button there would
  // either be dead or copy something misleading.
  it("does not add a copy button to non-chat pills", () => {
    const { onFocusKindChange } = renderBar();
    fireEvent.click(screen.getByRole("button", { name: /new browser tab/i }));
    const browserPill = screen.getByRole("tab", { name: /new tab/i });
    expect(within(browserPill).queryByLabelText("Copy session ID")).toBeNull();
    expect(onFocusKindChange).toHaveBeenCalled();
  });

  // The copy button reveals with `group-hover:opacity-100`, which only resolves
  // if an ANCESTOR carries the `group` class. Missing it is a failure jsdom
  // cannot see (it has no CSS engine): the button renders, keeps its classes,
  // passes every DOM assertion — and in a real browser stays at opacity 0 with
  // pointer events disabled, i.e. invisible and unclickable. Found exactly
  // that way. This asserts the DOM contract that makes the CSS work.
  it("puts the copy button inside an ancestor carrying the `group` class", () => {
    renderBar();
    const btn = screen.getByTestId("tab-copy-session-id-chat:s1");
    expect(btn.className).toContain("group-hover:opacity-100");
    const groupAncestor = btn.closest(".group");
    expect(groupAncestor).not.toBeNull();
    // The group must be the tab pill itself, so hovering the title reveals it.
    expect(groupAncestor!.getAttribute("role")).toBe("tab");
  });
});
