import { useCallback, useRef, useState } from "react";
import { type ActiveView } from "./viewPersistence";
import { type PendingJumpView } from "./viewPersistence";

/** The merged Sessions tab plus the cross-project Pulse dashboard. */
export type AppView = ActiveView | "pulse";

export interface PulseViewState {
  activeView: AppView;
  /** The raw setter, for the TopTabs change handler and the restore effects. */
  setActiveView: (view: AppView) => void;
  /** Arm + land on the chat surface when a Pulse card is clicked. */
  leavePulseFor: (targetPath: string) => void;
  openPulse: () => void;
  togglePulse: () => void;
  /**
   * The jump arm currently outstanding, for the project-switch effect to
   * consume (see `consumePendingJumpView`). Exposed so the arm/clear lifecycle
   * is testable without rendering all of App.
   */
  pendingJumpRef: { current: PendingJumpView | null };
}

/**
 * Pulse dashboard view transitions.
 *
 * Three intents share one `activeView`, and conflating any two of them is what
 * put a user on Files after clicking a session card:
 *
 *  - `openPulse` / `togglePulse` are the Cmd+J (and menu) intents. They are a
 *    TOGGLE: leaving returns to whatever was showing before the dashboard, so
 *    `previousViewRef` belongs to them alone.
 *  - `leavePulseFor` is a card click, which means "open this session". It
 *    always lands on the chat surface, and it arms `pendingJumpRef` because the
 *    project-switch effect runs on the commit AFTER `selectProject` resolves
 *    and would otherwise restore the target project's saved view over it.
 *
 * Extracted from App so the whole machine — including the arm — is testable
 * without booting the application shell.
 */
export function usePulseViewState(initial: AppView = "sessions"): PulseViewState {
  const [activeView, setActiveView] = useState<AppView>(initial);
  // What makes Cmd+J a toggle rather than a jump to a hard-coded default.
  const previousViewRef = useRef<AppView>("sessions");
  const pendingJumpRef = useRef<PendingJumpView | null>(null);

  const openPulse = useCallback(() => {
    previousViewRef.current = activeView === "pulse" ? previousViewRef.current : activeView;
    setActiveView("pulse");
  }, [activeView]);

  const togglePulse = useCallback(() => {
    if (activeView === "pulse") {
      setActiveView(previousViewRef.current);
    } else {
      previousViewRef.current = activeView;
      setActiveView("pulse");
    }
  }, [activeView]);

  const leavePulseFor = useCallback((targetPath: string) => {
    previousViewRef.current = "sessions";
    // Keyed on the project being LANDED on, so a leftover arm from a
    // same-project jump cannot dictate a later, unrelated switch.
    pendingJumpRef.current = { path: targetPath, view: "sessions" };
    setActiveView("sessions");
  }, []);

  return { activeView, setActiveView, openPulse, togglePulse, leavePulseFor, pendingJumpRef };
}
