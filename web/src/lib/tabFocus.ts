import { Store, useSelector } from "@tanstack/react-store";

/** Which half of the merged Sessions view a focus request reveals. */
export type TabFocusKind = "chat" | "terminal";

/**
 * A request from outside App's tree to reveal and focus an already-opened
 * (or just-opened) tab. The requester does not own `activeView`/`focusedKind`
 * — those are HomeApp state — so it queues the request here and the app shell
 * applies it.
 */
export interface TabFocusRequest {
  kind: TabFocusKind;
  /** The project that owns the tab. The request is applied only once this
   *  project is the active one, so a click on a non-active remote project's
   *  inventory (which also selects that project) still lands on the right tab
   *  instead of being applied against the outgoing project. */
  projectPath: string;
  host?: string;
  /** Terminal to activate; ignored for chat requests (the active session tab
   *  is already the one that was just opened/focused). */
  terminalId?: string;
}

interface TabFocusState {
  pending: TabFocusRequest | null;
}

/**
 * Module-level store (no provider) because the only writer is a sidebar
 * component far below — and outside — the component that owns the view state.
 */
export const tabFocusStore = new Store<TabFocusState>({ pending: null });

export const tabFocusActions = {
  /** Queue a reveal request. A later request replaces an unconsumed one — the
   *  user's most recent click is the only one that should win. */
  request(req: TabFocusRequest) {
    tabFocusStore.setState(() => ({ pending: req }));
  },
  /** Drop the pending request once the app shell has applied it. */
  clear() {
    tabFocusStore.setState((s) => (s.pending === null ? s : { ...s, pending: null }));
  },
};

export function useTabFocusRequest(): TabFocusRequest | null {
  return useSelector(tabFocusStore, (s) => s.pending);
}
