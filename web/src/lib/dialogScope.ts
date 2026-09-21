import type { ActiveView, FocusedKind } from "./viewPersistence";
import type { SessionSubTabId } from "../stores/projectStore";

/**
 * Scope rule for dialogs that belong to a chat session.
 *
 * A chat-session-bound dialog — the permission ask, the `question` tool ask,
 * and any future session-scoped prompt — may only be mounted while that
 * session's chat surface is actually on screen. Mounting one off-surface
 * renders a full-screen Radix modal (`fixed inset-0` overlay + focus trap) over
 * a view the user is not working in, which blocks the entire app for a session
 * they cannot even see.
 *
 * The ask is not lost by hiding it: it stays in its per-session chat-store
 * slice, so returning to that session's Chat sub-tab re-opens the prompt. While
 * it is out of sight the project sidebar's Bell badge (`ProjectSidebar`
 * `pendingCount`) and the attention chime (`AttentionSoundBridge`) are the
 * signal that a session needs input.
 */
export interface SessionAskSurface {
  /** Top-level view currently on screen. */
  activeView: ActiveView;
  /** Which half of the merged Sessions view is showing. */
  focusedKind: FocusedKind;
  /** The active session tab's sub-tab, when a session tab is active. */
  activeSubTab?: SessionSubTabId | null;
}

/**
 * Whether a chat-session-bound dialog may be shown right now. True only for the
 * session's own Chat sub-tab on the Sessions view with the chat half focused.
 * Every other surface (another top-level view, the terminal/browser half, or a
 * non-chat sub-tab of the same session) must leave the app unblocked.
 */
export function sessionAskSurfaceVisible({
  activeView,
  focusedKind,
  activeSubTab,
}: SessionAskSurface): boolean {
  return activeView === "sessions" && focusedKind === "chat" && activeSubTab === "chat";
}
