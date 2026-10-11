/**
 * The seam between `useChat` and the composer: real store, real hook, real
 * ChatInput. Every other strip suite mocks `useChat`, so nothing else would
 * catch a renamed or dropped field on the way out of the hook — a live browser
 * probe is what surfaced that this path needed its own coverage.
 *
 * Deliberately does NOT mount ChatPanel: @tanstack/react-virtual needs the
 * layout shims that live in ChatPanel.test.tsx, and duplicating them here buys
 * nothing. The ChatPanel → store half of the contract is covered there.
 */
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, act, cleanup } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { ChatProvider, useChatDispatch, type ChatAction } from "../../stores/chatStore";
import { clearQueue } from "../../lib/tabQueue";
import { clearDraft } from "../../lib/tabDrafts";
import { clearCompaction } from "../../lib/compactionState";
import type { Message } from "../../api/types";

vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {} },
}));
vi.mock("../../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api/client")>()),
  api: {
    getChatVerbosityConfig: vi.fn(async () => ({
      preset: "full",
      overrides: {
        older_thinking: "preset",
        tool_calls: "preset",
        tool_output: "preset",
        activity_notices: "preset",
      },
    })),
  },
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
  remoteApiBase: () => "",
  ApiError: class extends Error {},
}));
vi.mock("../../stores/projectStore", () => ({
  findTabForSession: () => undefined,
  findProjectPathForTab: () => undefined,
  useProjectDispatch: () => vi.fn(),
  useProjectState: () => ({
    state: { projects: [], tabsByProject: {}, activeProject: null },
    dispatch: vi.fn(),
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const SESSION = "sess-strip-seam";

function mk(role: Message["role"], content: string): Message {
  return { role, content };
}

const PROMPTS: Message[] = [
  mk("user", "first thing I asked for"),
  mk("assistant", "done"),
  mk("user", "second thing I asked for"),
  mk("assistant", "also done"),
  mk("user", "third thing I asked for"),
  mk("assistant", "working"),
];

/** Hands the test the store's real dispatch so it can drive the same actions
 *  ChatPanel's scroll handler dispatches. */
function DispatchCapture({ onCapture }: { onCapture: (d: (a: ChatAction) => void) => void }) {
  const dispatch = useChatDispatch();
  onCapture(dispatch as (a: ChatAction) => void);
  return null;
}

const stripLines = () =>
  Array.from(screen.getByTestId("recent-inputs").querySelectorAll("li")).map((li) => li.getAttribute("title"));

function mount(messages: Message[]) {
  let dispatch: (a: ChatAction) => void = () => {};
  const utils = render(
    <ChatProvider>
      <DispatchCapture onCapture={(d) => (dispatch = d)} />
      <ChatInput sessionTabId={SESSION} isActive />
    </ChatProvider>,
  );
  act(() => {
    dispatch({ type: "SET_MESSAGES", sessionId: SESSION, messages });
  });
  return {
    ...utils,
    send: (a: ChatAction) => act(() => dispatch(a)),
  };
}

describe("useChat -> ChatInput seam for the recent-inputs strip", () => {
  afterEach(() => {
    cleanup();
    clearQueue(SESSION);
    clearDraft(SESSION);
    clearCompaction(SESSION);
  });

  it("renders the last 2 typed inputs once the store says the transcript is scrolled up", async () => {
    const { send } = mount(PROMPTS);
    // At the tail: the store flag is false, so nothing is reserved.
    expect(screen.queryByTestId("recent-inputs")).toBeNull();

    // Exactly what ChatPanel's scroll handler dispatches.
    await send({ type: "SET_TRANSCRIPT_SCROLLED_UP", sessionId: SESSION, scrolledUp: true });
    expect(screen.getByTestId("recent-inputs")).toBeInTheDocument();
    expect(stripLines()).toEqual(["second thing I asked for", "third thing I asked for"]);
  });

  it("hides the strip when the store says the reader is back at the tail", async () => {
    const { send } = mount(PROMPTS);
    await send({ type: "SET_TRANSCRIPT_SCROLLED_UP", sessionId: SESSION, scrolledUp: true });
    expect(screen.getByTestId("recent-inputs")).toBeInTheDocument();
    await send({ type: "SET_TRANSCRIPT_SCROLLED_UP", sessionId: SESSION, scrolledUp: false });
    expect(screen.queryByTestId("recent-inputs")).toBeNull();
  });

  it("excludes system-injected user-role rows the agent writes itself", async () => {
    const { send } = mount([
      mk("user", "the real question"),
      mk("assistant", "answering"),
      mk("user", "[advisor plan checkpoint] An advisor reviewed the changes you just made:"),
      mk("assistant", "continuing"),
      mk("user", "[ocode:event] out-of-band completion notice"),
      mk("user", "a follow-up I actually typed"),
    ]);
    await send({ type: "SET_TRANSCRIPT_SCROLLED_UP", sessionId: SESSION, scrolledUp: true });
    expect(stripLines()).toEqual(["the real question", "a follow-up I actually typed"]);
  });

  it("renders no strip when the loaded window holds no user message", async () => {
    // A long agent turn can push every prompt out of the client's tail window,
    // leaving only assistant/tool rows. The selector is right to return nothing;
    // the strip must not invent content.
    const { send } = mount([
      mk("assistant", "a"),
      mk("assistant", "b"),
      mk("tool", "tool output"),
      mk("assistant", "c"),
    ]);
    await send({ type: "SET_TRANSCRIPT_SCROLLED_UP", sessionId: SESSION, scrolledUp: true });
    expect(screen.queryByTestId("recent-inputs")).toBeNull();
  });
});
