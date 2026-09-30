import type { FocusedKind } from "./viewPersistence";

export interface LeaveTerminalViewInput {
  /**
   * How many terminals the project has LEFT after the close, as reported by
   * the store (`closeTerminal` / `TerminalTabsHandle.closeActiveTerminal`), or
   * `null` when the close removed nothing.
   *
   * This is deliberately the POST-close count and not something the caller
   * measured beforehand. A cross-client `terminal_tabs_changed` refetch can
   * land between a component's last render and the click that runs its
   * handler, so a pre-close snapshot taken at render time can disagree with
   * the store — and the direction that hurts is a snapshot that says "1" when
   * the store says "2": closing the visible terminal then looks like the last
   * one and hands the user back to the chat with a shell still running.
   * Reading the count from the store after the removal makes that mistake
   * unrepresentable rather than merely unlikely.
   */
  remaining: number | null;
  /** Which surface is focused in the active project. */
  focusedKind: FocusedKind;
}

/**
 * Whether closing a terminal should hand the user back to the chat.
 *
 * Closing the LAST terminal of a project leaves the terminal view with nothing
 * to render. Two reasons to fall back to the chat rather than sit on the empty
 * panel:
 *
 *  1. `focusedKind` is persisted per project (App.tsx → viewPersistence). Left
 *     alone, every launch would restore the terminal view onto an empty panel
 *     — the user closed the terminal precisely so it would be gone, and now
 *     they get an empty terminal screen instead of their chat.
 *  2. The empty panel is a dead end in the common case: the user closed the
 *     terminal to get it out of the way, not to stare at "No terminals open".
 *     They can click straight back into the terminal region (which now shows
 *     the empty state rather than silently spawning a shell).
 *
 * Deliberately NOT triggered by:
 *  - a close that left other terminals open (the view still has content);
 *  - a close that removed nothing (`remaining === null` — a repeated close of
 *    a terminal that is already gone);
 *  - the sidebar inventory's kill X (RemoteProjectStatus → the store's
 *    `killTerminal`) — the user is looking at the chat or another project, so
 *    yanking them to the chat of a project they did not close anything in
 *    would be a surprise. That path never calls this helper;
 *  - another client closing it (the shared `terminal_tabs_changed` refetch) —
 *    this window did not perform the gesture, and the empty state it lands in
 *    is correct and stable;
 *  - `focusedKind === "browser"` or `"chat"`, where the terminal view is not
 *    what the user is looking at anyway.
 *
 * The Processes sentinel counts as the terminal view: if the last terminal
 * closes while Processes is showing, the terminal region has nothing left to
 * offer, so the app returns to the chat. Processes has its own pill and one
 * click brings it back. (Closing from Processes is a no-op — the handle
 * short-circuits the sentinel — so in practice this covers closing the last
 * terminal from the tab strip while Processes has focus.)
 *
 * `remaining === 0` is exact on purpose rather than `<= 1`: `null` (nothing
 * was removed) and "1 left" are both genuine "there is still something here"
 * answers, and only a close that actually emptied the project should hand the
 * user away from it.
 */
export function shouldLeaveTerminalView(input: LeaveTerminalViewInput): boolean {
  return input.remaining === 0 && input.focusedKind === "terminal";
}
