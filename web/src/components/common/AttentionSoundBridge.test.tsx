import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { useEffect } from "react";
import { ChatProvider, useChatDispatch, type ChatAction } from "../../stores/chatStore";
import AttentionSoundBridge, {
  attentionAlertEdge,
  parseAttentionSignature,
  type AttentionFlags,
} from "./AttentionSoundBridge";
import { playAlertSound } from "../Terminal/terminalAlertSound";

vi.mock("../Terminal/terminalAlertSound", () => ({ playAlertSound: vi.fn() }));

// Dispatch handle captured from inside the ChatProvider tree.
let dispatch: (action: ChatAction) => void = () => {};

function Dispatcher() {
  const d = useChatDispatch();
  useEffect(() => {
    dispatch = d;
  }, [d]);
  return null;
}

function fire(action: ChatAction) {
  act(() => {
    dispatch(action);
  });
}

interface BridgeProps {
  tabs: { id: string; projectPath: string }[];
  activeTabId: string | null;
  activeProjectPath?: string;
  chatVisible?: boolean;
}

function renderBridge({
  tabs,
  activeTabId,
  activeProjectPath = "/proj",
  chatVisible = true,
}: BridgeProps) {
  return render(
    <ChatProvider>
      <Dispatcher />
      <AttentionSoundBridge
        tabs={tabs}
        activeTabId={activeTabId}
        activeProjectPath={activeProjectPath}
        chatVisible={chatVisible}
      />
    </ChatProvider>,
  );
}

const perm = { tool: "bash", request_id: "r1" };
const question = {
  request_id: "q1",
  questions: [{ header: "H", question: "?", options: [] }],
};

beforeEach(() => {
  vi.mocked(playAlertSound).mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("attentionAlertEdge", () => {
  const open = () => true;
  const never = () => false;
  const flags = (p: Partial<AttentionFlags>): AttentionFlags => ({
    turnActive: false,
    stalled: false,
    permission: false,
    question: false,
    ...p,
  });

  it("never alerts without a previous observation", () => {
    const next = new Map([["s1", flags({ permission: true })]]);
    expect(attentionAlertEdge(null, next, open, never)).toBe(false);
  });

  it("alerts on a rising permission / question / stall or a finished turn", () => {
    const prev = new Map([["s1", flags({ turnActive: true })]]);
    expect(attentionAlertEdge(prev, new Map([["s1", flags({ turnActive: true, permission: true })]]), open, never)).toBe(true);
    expect(attentionAlertEdge(prev, new Map([["s1", flags({ turnActive: true, question: true })]]), open, never)).toBe(true);
    expect(attentionAlertEdge(prev, new Map([["s1", flags({ turnActive: true, stalled: true })]]), open, never)).toBe(true);
    expect(attentionAlertEdge(prev, new Map([["s1", flags({})]]), open, never)).toBe(true);
  });

  it("stays quiet when the session is focused, unopened, or unchanged", () => {
    const prev = new Map([["s1", flags({ turnActive: true })]]);
    const next = new Map([["s1", flags({})]]);
    expect(attentionAlertEdge(prev, next, open, () => true)).toBe(false);
    expect(attentionAlertEdge(prev, next, () => false, never)).toBe(false);
    expect(attentionAlertEdge(prev, new Map([["s1", flags({ turnActive: true })]]), open, never)).toBe(false);
  });
});

describe("parseAttentionSignature", () => {
  it("round-trips the compact flag encoding", () => {
    const parsed = parseAttentionSignature("s1:1000|s2:0101|");
    expect(parsed.get("s1")).toEqual({ turnActive: true, stalled: false, permission: false, question: false });
    expect(parsed.get("s2")).toEqual({ turnActive: false, stalled: true, permission: false, question: true });
  });
});

describe("AttentionSoundBridge", () => {
  it("chimes when a background chat's turn finishes", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: "s2" });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    expect(playAlertSound).not.toHaveBeenCalled();
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: false });
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("stays silent for the chat the user is viewing", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: "s1" });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: false });
    expect(playAlertSound).not.toHaveBeenCalled();
  });

  it("stays silent for the active tab when the terminal half is showing", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: "s1", chatVisible: false });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: false });
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("chimes when a background chat starts waiting on permission", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: null });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    expect(playAlertSound).not.toHaveBeenCalled();
    fire({ type: "PERMISSION_REQUEST", sessionId: "s1", permission: perm });
    expect(playAlertSound).toHaveBeenCalledTimes(1);
    fire({ type: "PERMISSION_RESOLVED", sessionId: "s1", requestId: "r1" });
    fire({ type: "PERMISSION_REQUEST", sessionId: "s1", permission: { ...perm, request_id: "r2" } });
    expect(playAlertSound).toHaveBeenCalledTimes(2);
  });

  it("chimes when a background chat opens a question dialog", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: null });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    fire({ type: "QUESTION_REQUEST", sessionId: "s1", question });
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("chimes when a background chat stalls", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: null });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    fire({ type: "SET_TURN_STALLED", sessionId: "s1", stalled: true });
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("ignores sessions that are not open tabs", () => {
    renderBridge({ tabs: [], activeTabId: null });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: true });
    fire({ type: "SET_TURN_STATE", sessionId: "s1", turnActive: false });
    expect(playAlertSound).not.toHaveBeenCalled();
  });

  it("does not alert on the first sight of an already-waiting session", () => {
    renderBridge({ tabs: [{ id: "s1", projectPath: "/proj" }], activeTabId: null });
    fire({ type: "PERMISSION_REQUEST", sessionId: "s1", permission: perm });
    expect(playAlertSound).not.toHaveBeenCalled();
    fire({ type: "QUESTION_REQUEST", sessionId: "s1", question });
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });
});
