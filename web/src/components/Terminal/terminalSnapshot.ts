const INPUT_QUIET_MS = 200;

/**
 * Coalesces parsed-buffer generations, not queued writes: xterm parses writes
 * asynchronously. Call markDirty from onWriteParsed/onResize and after explicit
 * synchronous clear/reset operations. Periodic work waits for input to settle;
 * flush is the lifecycle escape hatch (pagehide/unmount/socket close).
 */
export function createTerminalSnapshot(save: () => void) {
  let generation = 1;
  let savedGeneration = 0;
  let cancelPending: (() => void) | null = null;
  let quietUntil = 0;
  let selecting = false;
  let disposed = false;

  const cancel = () => {
    cancelPending?.();
    cancelPending = null;
  };
  const saveDirty = () => {
    if (disposed || savedGeneration === generation) return;
    const capturedGeneration = generation;
    save();
    // A mutation during save belongs to the next snapshot. A thrown save must
    // not acknowledge this generation, so a later attempt can retry it.
    savedGeneration = capturedGeneration;
  };
  const run = () => {
    cancelPending = null;
    if (disposed || savedGeneration === generation) return;
    if (selecting || Date.now() < quietUntil) {
      const timer = setTimeout(schedule, Math.max(INPUT_QUIET_MS, quietUntil - Date.now()));
      cancelPending = () => clearTimeout(timer);
      return;
    }
    saveDirty();
  };
  const schedule = () => {
    // A retry timer has fired; clear it before scheduling a new idle callback.
    cancel();
    if (disposed || savedGeneration === generation) return;
    if (typeof requestIdleCallback === "function") {
      const idle = requestIdleCallback(run, { timeout: 5000 });
      cancelPending = () => cancelIdleCallback(idle);
    } else {
      const timer = setTimeout(run, 0);
      cancelPending = () => clearTimeout(timer);
    }
  };

  return {
    markDirty() {
      if (!disposed) generation++;
    },
    input() {
      quietUntil = Date.now() + INPUT_QUIET_MS;
    },
    beginSelection() {
      selecting = true;
    },
    endSelection() {
      selecting = false;
      quietUntil = Date.now() + INPUT_QUIET_MS;
    },
    schedule() {
      if (cancelPending === null) schedule();
    },
    flush() {
      cancel();
      saveDirty();
    },
    dispose() {
      disposed = true;
      cancel();
    },
  };
}
