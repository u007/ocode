// Side-pane state follows the chat session it accompanies. A chat tab's id is
// not stable for its whole life: a brand-new tab starts as `new-<ts>` and is
// rekeyed to its real `ses_...` id on the first message, and `/reset-id`
// rekeys an existing session again. The side pane's state lives under the tab
// id (`side:chat:<id>` / `side:term:<id>`), so it must be moved along with the
// other per-tab stores (drafts, queue, input history) or the pane detaches and
// closes.
//
// This composes the two stores that hold side-pane state:
//   - browserStore (live browser surface: URL, history, scroll, console)
//   - sidebarPreviewState (persisted previewed file/surface/page)

import { browserActions, type StateKey } from "./browserStore";
import { rekeySidebarPreviewState } from "../components/Preview/sidebarPreviewState";

/** The side surface key for a chat session tab. */
export function sideChatKey(sessionId: string): StateKey {
  return `side:chat:${sessionId}`;
}

/** The side surface key for a focused terminal tab. */
export function sideTermKey(terminalId: string): StateKey {
  return `side:term:${terminalId}`;
}

/** Move a chat session's side-pane state (browser surface + previewed file)
 *  from one session id to another. No-op for empty/identical ids.
 *
 *  Both the chat pane key (`side:chat:<id>`) and the terminal pane key
 *  (`side:term:<id>`) embed the active session-tab id — `App.tsx` builds the
 *  terminal pane's key from `activeTabId` too (the terminal is project-level,
 *  so it has no tab id of its own) — so both must move. */
export function rekeySidePaneState(oldId: string | null | undefined, newId: string | null | undefined): void {
  if (!oldId || !newId || oldId === newId) return;
  for (const [from, to] of [
    [sideChatKey(oldId), sideChatKey(newId)],
    [sideTermKey(oldId), sideTermKey(newId)],
  ] as const) {
    browserActions.rekey(from, to);
    rekeySidebarPreviewState(from, to);
  }
}
