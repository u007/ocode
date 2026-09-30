import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { usePulseViewState } from "./pulseViewState";
import {
  consumePendingJumpView,
  resolveViewOnProjectSwitch,
  type ActiveView,
  type FocusedKind,
} from "./viewPersistence";
import { type AppView } from "./pulseViewState";

/**
 * The Pulse card-jump view machine, end to end but without booting App.
 *
 * The bug these pin: a card click restored the view the user was in BEFORE the
 * dashboard, so the session tab opened invisibly behind Files/Git and that
 * foreign view was then persisted against the TARGET project.
 *
 * `switchProject` mirrors what App's project-switch layout effect does with the
 * hook's own ref, so the arm → consume → resolve sequence is exercised as one
 * flow rather than three disconnected units.
 */
function harness(initial?: Parameters<typeof usePulseViewState>[0]) {
  const view = renderHook(() => usePulseViewState(initial));
  /** Reproduce App's project-switch effect for `path`. */
  const switchProject = (
    path: string,
    saved: { view: ActiveView; focusedKind: FocusedKind } | null,
  ) => {
    // `act` returns a thenable, so the resolved view is captured in a variable
    // rather than returned from the callback.
    let resolved: { view: AppView; focusedKind: FocusedKind } | undefined;
    act(() => {
      const forced = consumePendingJumpView(view.result.current.pendingJumpRef, path);
      resolved = resolveViewOnProjectSwitch(saved, forced, path);
      view.result.current.setActiveView(resolved.view);
    });
    return resolved!;
  };
  return { view, switchProject };
}

const savedFiles = { view: "files", focusedKind: "chat" } as const;

describe("Pulse card jump", () => {
  it("lands on the chat surface even when the target project was left on Files", () => {
    const { view, switchProject } = harness();
    act(() => view.result.current.openPulse());
    expect(view.result.current.activeView).toBe("pulse");

    act(() => view.result.current.leavePulseFor("/target"));
    // The arm exists before the switch effect runs — this is the piece whose
    // absence let the target's saved view win.
    expect(view.result.current.pendingJumpRef.current).toEqual({
      path: "/target",
      view: "sessions",
    });

    const next = switchProject("/target", savedFiles);
    expect(next).toEqual({ view: "sessions", focusedKind: "chat" });
    expect(view.result.current.activeView).toBe("sessions");
  });

  it("a same-project jump leaves an arm that a LATER unrelated switch ignores", () => {
    const { view, switchProject } = harness();
    act(() => view.result.current.openPulse());
    act(() => view.result.current.leavePulseFor("/same"));

    // No switch runs (the project never changed), so the arm is still set…
    expect(view.result.current.pendingJumpRef.current).not.toBeNull();

    // …and an unrelated switch must restore ITS saved view, not the arm.
    expect(switchProject("/unrelated", savedFiles)).toEqual(savedFiles);
    expect(view.result.current.pendingJumpRef.current).toBeNull();
  });

  it("does not double-fire: a second switch after an honoured jump is normal", () => {
    const { view, switchProject } = harness();
    act(() => view.result.current.openPulse());
    act(() => view.result.current.leavePulseFor("/target"));

    expect(switchProject("/target", savedFiles)).toEqual({
      view: "sessions",
      focusedKind: "chat",
    });
    // A later switch back to the same project restores normally.
    expect(switchProject("/target", savedFiles)).toEqual(savedFiles);
  });
});

describe("Cmd+J toggle", () => {
  it("returns to the view the user was in, not the chat surface", () => {
    // The toggle is a different intent from a card click, and must keep its own
    // behaviour: it restores the pre-dashboard view and arms nothing.
    const { view } = harness();
    act(() => view.result.current.setActiveView("files"));
    act(() => view.result.current.openPulse());
    expect(view.result.current.activeView).toBe("pulse");
    expect(view.result.current.pendingJumpRef.current).toBeNull();

    act(() => view.result.current.togglePulse());
    expect(view.result.current.activeView).toBe("files");
  });

  it("does not re-enter itself when opened while already on the dashboard", () => {
    const { view } = harness();
    act(() => view.result.current.setActiveView("git"));
    act(() => view.result.current.openPulse());
    act(() => view.result.current.openPulse());
    act(() => view.result.current.togglePulse());
    expect(view.result.current.activeView).toBe("git");
  });
});
