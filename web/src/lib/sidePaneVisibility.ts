import type { SessionSubTabId } from "../stores/projectStore";
import type { FocusedKind } from "./viewPersistence";

export interface SidePaneVisibilityInput {
  activeView: string;
  /** Sub-tab of the ACTIVE session tab (undefined when no session tab exists). */
  activeSubTab: SessionSubTabId | undefined;
  focusedKind: FocusedKind;
}

/**
 * Whether the right-hand "Browser / Preview" side pane may be shown.
 *
 * The pane accompanies a CHAT session (or the terminal). It must NOT render
 * while the active session tab shows one of the non-chat sub-tabs (Agents,
 * Changes, Logs, Status, Preview) — those panels are full-width content and
 * the pane's file preview is a chat-companion surface. The pane's own Preview
 * sub-tab already IS a preview surface, so the side pane is redundant there.
 *
 * Note this only controls RENDERING (like shouldRenderCoworkSidebar); the
 * open/collapsed state lives in browserStore under the side: stateKey and is
 * preserved, so returning to the chat sub-tab restores the pane as it was.
 * The full-width browser *tab* (focusedKind "browser") never gets it either.
 */
export function shouldRenderSidePane({ activeView, activeSubTab, focusedKind }: SidePaneVisibilityInput): boolean {
  if (activeView !== "sessions") return false;
  // The browser *tab* has its own full-width surface; the side pane belongs to
  // chat/terminal focus only.
  if (focusedKind !== "chat" && focusedKind !== "terminal") return false;
  // Session sub-tabs: the pane is a chat companion. The terminal is a
  // focusedKind (project-level top tab), not a sub-tab, so its sub-tab value
  // is whatever the session tab last held — chat by default. Require chat.
  return activeSubTab === "chat";
}