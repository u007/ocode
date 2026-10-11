import type { ActiveView, FocusedKind } from "./viewPersistence";
import type { SessionSubTabId } from "../stores/projectStore";

/**
 * Scope rule for dialogs that belong to a chat session.
 *
 * A chat-session-bound dialog — the permission ask, the `question` tool ask,
 * and any future session-scoped prompt — may only be mounted while that
 * session's chat surface is actually on screen.
 *
 * This gate is still REQUIRED even though those two dialogs are now CONFINED to
 * the session's chat surface (`ui/scoped-dialog.tsx` portals them into the
 * active chat tab's element, so an ask no longer covers the project list or any
 * other session). Off-surface that same panel is `display:none`
 * (`ui/tabs.tsx` `data-[state=inactive]:hidden`), so confining alone would
 * render the ask INVISIBLY — worse than blocking, because the user gets no
 * signal at all. The two rules are complementary: this one decides WHETHER an
 * ask may mount; confinement decides WHERE it renders once it may.
 *
 * The ask is not lost by hiding it: it stays in its per-session chat-store
 * slice, so returning to that session's Chat sub-tab re-opens the prompt. While
 * it is out of sight the project sidebar's Bell badge (`ProjectSidebar`
 * `pendingCount`) and the attention chime (`AttentionSoundBridge`) are the
 * signal that a session needs input.
 */
export interface SessionAskSurface {
  /** Top-level view currently on screen. */
  /**
   * The App's full view set, including the global "pulse" dashboard, which is
   * NOT a member of the persisted per-project `ActiveView`. Accepting it here
   * is what keeps this predicate total: the dashboard is simply another
   * surface where a chat ask must not be shown.
   */
  activeView: ActiveView | "pulse";
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
