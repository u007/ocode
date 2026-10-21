import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";

// Controllable stand-in for useChat: the suggested-response row is driven
// entirely from the hook's `suggestions` field, so the stub is the only
// seam the test needs.
const chat = vi.hoisted(() => ({
  streaming: false,
  suggestions: [] as string[],
}));
const sendMessage = vi.fn().mockResolvedValue(true);
const executeShell = vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 });
const resume = vi.fn();
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell,
    stop: vi.fn(),
    resume,
    isStreaming: chat.streaming,
    wasInterrupted: false,
    pendingPermission: null,
    hasConversation: true,
    suggestions: chat.suggestions,
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

// The composer's own api surface must stay the real module; only the
// quick-actions fetch is stubbed (the store opens an SSE stream otherwise).
vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getQuickActionsConfig: vi.fn().mockResolvedValue({ chips: [] }),
      setQuickActionsConfig: vi.fn(),
    },
  };
});

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    off: () => {},
  },
}));

// The quick-action store is config-driven; the REAL seed chips are used here
// so the quick-action strip actually renders. Without a rendered strip the
// ordering assertion below would be vacuous — the suggested-response row
// could be moved anywhere and the test would still pass.
vi.mock("../../lib/quickActions", async () => {
  const actual = await vi.importActual<typeof import("../../lib/quickActions")>("../../lib/quickActions");
  return {
    ...actual,
    useQuickActions: () => ({
      chips: actual.SEED_CHIPS as never,
      loading: false,
      error: null,
      revision: "r",
    }),
  };
});

describe("ChatInput suggested responses", () => {
  beforeEach(() => {
    chat.streaming = false;
    chat.suggestions = [];
    sendMessage.mockClear();
  });

  it("renders the proposed options above the quick-action strip", () => {
    chat.suggestions = ["Run the tests", "Open a PR"];
    render(<ChatInput sessionTabId="ses_1" />);

    const toolbar = screen.getByRole("toolbar", { name: "Suggested responses" });
    expect(toolbar).toBeInTheDocument();

    // The row must sit BEFORE the quick-action strip in the DOM: the user
    // asked for the suggestions to read as part of the quick actions. The
    // strip MUST be rendered for this assertion to mean anything, so its
    // presence is asserted first.
    const quick = screen.getByRole("toolbar", { name: "Quick actions" });
    expect(
      toolbar.compareDocumentPosition(quick) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("fills the composer when an option is clicked, without sending", () => {
    chat.suggestions = ["Run the tests"];
    render(<ChatInput sessionTabId="ses_1" />);

    const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
    expect(textarea.value).toBe("");

    fireEvent.click(screen.getByRole("button", { name: "Run the tests" }));

    expect(textarea.value).toBe("Run the tests");
    // A suggestion is a starting point the user may edit — it must never
    // start a turn on its own.
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("hides the row while a turn is streaming", () => {
    chat.suggestions = ["Run the tests"];
    chat.streaming = true;
    render(<ChatInput sessionTabId="ses_1" />);

    expect(
      screen.queryByRole("toolbar", { name: "Suggested responses" }),
    ).toBeNull();
  });

  it("renders no row when the agent proposed nothing", () => {
    render(<ChatInput sessionTabId="ses_1" />);
    expect(
      screen.queryByRole("toolbar", { name: "Suggested responses" }),
    ).toBeNull();
  });
});
