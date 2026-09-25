import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Mock } from "vitest";
import { getActionError, resetActionErrors } from "./actionErrors";
import {
  __resetDraftGuardForTests,
  draftPersistenceBlocked,
  draftPersistenceMessage,
  installQuitBlockedListener,
  noteDraftPersistFailure,
  noteDraftPersistPending,
  noteDraftPersistSuccess,
  reconcileDraftGuard,
} from "./editorDraftGuard";

let invoke: Mock<(m: string) => void>;

beforeEach(() => {
  __resetDraftGuardForTests();
  resetActionErrors();
  invoke = vi.fn<(m: string) => void>();
  (window as unknown as { _wails?: { invoke: (m: string) => void } })._wails = { invoke };
});

afterEach(() => {
  delete (window as unknown as { _wails?: unknown })._wails;
  __resetDraftGuardForTests();
  resetActionErrors();
});

describe("editorDraftGuard", () => {
  it("reports a failure, blocks quit, and notifies native once", () => {
    noteDraftPersistFailure("editor-/a/big.txt", "/a/big.txt");
    expect(draftPersistenceBlocked()).toBe(true);
    expect(getActionError()?.message).toContain("big.txt");
    expect(invoke).toHaveBeenCalledTimes(1);
    expect(invoke.mock.calls[0][0]).toMatch(/^ocode:quit-guard:blocked:/);

    // A second failing tab must not re-notify native (already blocked)...
    noteDraftPersistFailure("editor-/a/other.txt", "/a/other.txt");
    expect(invoke).toHaveBeenCalledTimes(1);
    // ...but the toast reflects both files.
    expect(getActionError()?.message).toContain("big.txt");
    expect(getActionError()?.message).toContain("other.txt");
  });

  it("clears the block only after every failing tab recovers", () => {
    noteDraftPersistFailure("t1", "/a/one.txt");
    noteDraftPersistFailure("t2", "/a/two.txt");

    noteDraftPersistSuccess("t1");
    expect(draftPersistenceBlocked()).toBe(true);
    expect(invoke).toHaveBeenCalledTimes(1); // still blocked; no clear yet

    noteDraftPersistSuccess("t2");
    expect(draftPersistenceBlocked()).toBe(false);
    expect(invoke).toHaveBeenCalledTimes(2);
    expect(invoke.mock.calls[1][0]).toBe("ocode:quit-guard:clear");
  });

  it("reconcile drops entries for tabs that are no longer dirty, then clears", () => {
    noteDraftPersistFailure("t1", "/a/one.txt");
    noteDraftPersistFailure("t2", "/a/two.txt");

    reconcileDraftGuard(["t2"]);
    expect(draftPersistenceBlocked()).toBe(true);

    reconcileDraftGuard([]);
    expect(draftPersistenceBlocked()).toBe(false);
    expect(invoke.mock.calls[invoke.mock.calls.length - 1]?.[0]).toBe("ocode:quit-guard:clear");
  });

  it("does not touch native when nothing was blocked", () => {
    reconcileDraftGuard([]);
    noteDraftPersistSuccess("never-failed");
    expect(invoke).not.toHaveBeenCalled();
  });

  it("still reports the toast in a plain browser (no bridge)", () => {
    delete (window as unknown as { _wails?: unknown })._wails;
    expect(() => noteDraftPersistFailure("t1", "/a/one.txt")).not.toThrow();
    expect(draftPersistenceBlocked()).toBe(true);
    expect(getActionError()?.message).toContain("one.txt");
  });

  it("re-raises the reason when the desktop shell reports a refused quit", () => {
    const uninstall = installQuitBlockedListener();
    try {
      window.dispatchEvent(new Event("ocode:quit-blocked"));
      expect(getActionError()?.message).toContain("won't quit");

      resetActionErrors();
      noteDraftPersistFailure("t1", "/a/one.txt");
      resetActionErrors();
      window.dispatchEvent(new Event("ocode:quit-blocked"));
      expect(getActionError()?.message).toContain("one.txt");
    } finally {
      uninstall();
    }
  });

  it("draftPersistenceMessage has a generic fallback with no tracked failure", () => {
    expect(draftPersistenceMessage()).toContain("won't quit");
  });

  // The guard must mean "edits are in memory and not written yet", not just
  // "a write failed" — otherwise quitting inside the write debounce silently
  // drops the unpersisted tail.
  it("blocks quit while edits are still in memory, without raising a failure toast", () => {
    noteDraftPersistPending("t1", "/a/one.txt");
    expect(draftPersistenceBlocked()).toBe(true);
    // Nothing has FAILED yet, so the sticky error toast must stay quiet —
    // popping it on every keystroke would be noise.
    expect(getActionError()).toBeNull();
    expect(invoke).toHaveBeenCalledWith(expect.stringMatching(/^ocode:quit-guard:blocked:/));

    noteDraftPersistSuccess("t1"); // debounced write landed
    expect(draftPersistenceBlocked()).toBe(false);
    expect(invoke).toHaveBeenCalledWith("ocode:quit-guard:clear");
  });

  it("stays blocked while a pending edit sits alongside a recovered failure", () => {
    noteDraftPersistFailure("t1", "/a/one.txt");
    noteDraftPersistPending("t2", "/a/two.txt");
    noteDraftPersistSuccess("t1");
    expect(draftPersistenceBlocked()).toBe(true);
    noteDraftPersistSuccess("t2");
    expect(draftPersistenceBlocked()).toBe(false);
  });

  it("reconcile drops pending entries for tabs that are no longer dirty", () => {
    noteDraftPersistPending("t1", "/a/one.txt");
    reconcileDraftGuard([]);
    expect(draftPersistenceBlocked()).toBe(false);
    expect(invoke.mock.calls[1][0]).toBe("ocode:quit-guard:clear");
  });

  it("describes a pending (not failed) quit block differently", () => {
    noteDraftPersistPending("t1", "/a/one.txt");
    const msg = draftPersistenceMessage();
    expect(msg).toContain("one.txt");
    expect(msg).not.toContain("storage is full");
  });
});
