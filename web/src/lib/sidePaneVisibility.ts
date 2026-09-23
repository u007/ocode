import type { SessionSubTabId } from "../stores/projectStore";
import type { FocusedKind } from "./viewPersistence";

export interface SidePaneVisibilityInput {
  activeView: string;
  /** Sub-tab of the ACTIVE session tab (undefined when no session tab exists). */
  activeSubTab: SessionSubTabId | undefined;
  focusedKind: FocusedKind;
  /**
   * True at the mobile breakpoint (≤767px). The side pane is a desktop
   * companion surface: on a phone it would be a fixed-width flex child
   * squeezed next to (and overflowing) the chat, so it is never rendered
   * there. Browser + preview stay reachable as tabs — the UnifiedTabBar
   * browser pills / "New browser tab" button and the session "Preview"
   * sub-tab — and preview activations are routed to the Preview sub-tab.
   */
  isMobile?: boolean;
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
 *
 * Mobile is an additional hard gate: the pane is desktop-only. On a phone the
 * chat column is the whole screen, and browser/preview live in their tabs.
 */
export function shouldRenderSidePane({ activeView, activeSubTab, focusedKind, isMobile }: SidePaneVisibilityInput): boolean {
  if (activeView !== "sessions") return false;
  // Phones never get the side pane (see isMobile doc above).
  if (isMobile) return false;
  // The browser *tab* has its own full-width surface; the side pane belongs to
  // chat/terminal focus only.
  if (focusedKind !== "chat" && focusedKind !== "terminal") return false;
  // Session sub-tabs: the pane is a chat companion. The terminal is a
  // focusedKind (project-level top tab), not a sub-tab, so its sub-tab value
  // is whatever the session tab last held — chat by default. Require chat.
  return activeSubTab === "chat";
}