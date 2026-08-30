import type { SessionSubTabId } from "../../stores/projectStore";
import type { FocusedKind } from "../../lib/viewPersistence";

export interface CoworkSidebarVisibilityInput {
  activeView: string;
  activeSubTab: SessionSubTabId | undefined;
  focusedKind: FocusedKind;
}

/**
 * Whether the right-hand "Cowork" sidebar should be mounted.
 *
 * It is shown only on the Sessions view when the active session tab is on the
 * Chat sub-tab — and it is deliberately hidden while the terminal is focused so
 * the right rail doesn't crowd the shell (same for the full-width browser tab).
 * Note this gate only controls mounting; the separate `coworkOpen` state
 * (toggled from the status bar) controls the panel's collapsed/expanded width
 * and is preserved across focus switches.
 */
export function shouldRenderCoworkSidebar({
  activeView,
  activeSubTab,
  focusedKind,
}: CoworkSidebarVisibilityInput): boolean {
  return activeView === "sessions" && activeSubTab === "chat" && focusedKind === "chat";
}
