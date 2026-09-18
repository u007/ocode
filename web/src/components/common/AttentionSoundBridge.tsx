import { useEffect, useRef } from "react";
import { useChatSelector, type ChatState } from "../../stores/chatStore";
import { playAlertSound } from "../Terminal/terminalAlertSound";

/** One open session tab, reduced to the fields the sound bridge needs. */
export interface AttentionTab {
  id: string;
  projectPath: string;
}

/** Attention-relevant flags for one session, in a fixed order. */
export interface AttentionFlags {
  turnActive: boolean;
  stalled: boolean;
  permission: boolean;
  question: boolean;
}

// `useChatSelector` compares with Object.is, so the selector must return a
// primitive that only changes on an attention-relevant edge — a fresh object
// would re-render on every streamed token. Encode the four booleans per
// session as a compact, key-sorted string.
function chatAttentionSignature(state: ChatState): string {
  let out = "";
  for (const id of Object.keys(state.sessions).sort()) {
    const slice = state.sessions[id];
    if (!slice) continue;
    out += `${id}:${slice.turnActive ? 1 : 0}${slice.turnStalled ? 1 : 0}${
      slice.pendingPermission ? 1 : 0
    }${slice.pendingQuestion ? 1 : 0}|`;
  }
  return out;
}

/** Inverse of `chatAttentionSignature` — exported for tests. */
export function parseAttentionSignature(signature: string): Map<string, AttentionFlags> {
  const map = new Map<string, AttentionFlags>();
  if (!signature) return map;
  for (const entry of signature.split("|")) {
    if (!entry) continue;
    const sep = entry.indexOf(":");
    if (sep < 0) continue;
    const flags = entry.slice(sep + 1);
    map.set(entry.slice(0, sep), {
      turnActive: flags[0] === "1",
      stalled: flags[1] === "1",
      permission: flags[2] === "1",
      question: flags[3] === "1",
    });
  }
  return map;
}

/**
 * Decides whether the move from `prev` to `next` should raise an alert sound.
 *
 * `isOpen` filters to sessions the user actually has open; `isFocused` reports
 * the session the user is currently looking at (chat sub-tab visible in the
 * active project). First sight of a session never alerts — only a rising
 * attention edge (turn finished, newly stalled, newly waiting on a
 * permission/question dialog) on a session we have already observed.
 */
export function attentionAlertEdge(
  prev: Map<string, AttentionFlags> | null,
  next: Map<string, AttentionFlags>,
  isOpen: (id: string) => boolean,
  isFocused: (id: string) => boolean,
): boolean {
  if (!prev) return false;
  for (const [id, cur] of next) {
    if (!isOpen(id) || isFocused(id)) continue;
    const before = prev.get(id);
    if (!before) continue;
    if (!before.permission && cur.permission) return true;
    if (!before.question && cur.question) return true;
    if (!before.stalled && cur.stalled) return true;
    if (before.turnActive && !cur.turnActive) return true;
  }
  return false;
}

interface AttentionSoundBridgeProps {
  /** Every open session tab across all projects. */
  tabs: ReadonlyArray<AttentionTab>;
  /** The active session tab of the active project, or null. */
  activeTabId: string | null;
  /** Path of the active project. */
  activeProjectPath: string;
  /** True when the merged Sessions view is showing the chat half. */
  chatVisible: boolean;
}

/**
 * Plays the shared alert sound when a session the user is NOT looking at needs
 * attention: its turn/loop finished, it stalled, or it started waiting on a
 * permission/question dialog. Covers background projects and background tabs
 * in the active project (e.g. while the user is on the terminal or files
 * view). Renders nothing.
 *
 * The sound obeys the same browser-side settings as the terminal bell
 * (enable toggle + optional custom file) — see `terminalAlertSound.ts`.
 */
export default function AttentionSoundBridge({
  tabs,
  activeTabId,
  activeProjectPath,
  chatVisible,
}: AttentionSoundBridgeProps) {
  // Read the latest routing/focus through refs so a tab/project switch does not
  // itself re-run the edge detector (it could not create an edge anyway) — only
  // an actual signature change does.
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;
  const focusRef = useRef({ activeTabId, activeProjectPath, chatVisible });
  focusRef.current = { activeTabId, activeProjectPath, chatVisible };

  const signature = useChatSelector(chatAttentionSignature);
  const prevRef = useRef<Map<string, AttentionFlags> | null>(null);

  useEffect(() => {
    const next = parseAttentionSignature(signature);
    const open = new Set(tabsRef.current.map((t) => t.id));
    const alert = attentionAlertEdge(
      prevRef.current,
      next,
      (id) => open.has(id),
      (id) => {
        const focus = focusRef.current;
        if (!focus.chatVisible || id !== focus.activeTabId) return false;
        const tab = tabsRef.current.find((t) => t.id === id);
        return !!tab && tab.projectPath === focus.activeProjectPath;
      },
    );
    prevRef.current = next;
    if (alert) playAlertSound();
  }, [signature]);

  return null;
}
