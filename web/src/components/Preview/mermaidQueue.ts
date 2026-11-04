// mermaid's config (theme, securityLevel) is one global that `initialize` sets,
// and `render` reads it. Every initialize+render pair goes through this queue, so
// a print render (strict, light theme) can never run between a viewer's
// initialize and its render, and its restore can never land mid-render.
//
// This module must not import mermaid: printPreview loads mermaid lazily.

let tail: Promise<unknown> = Promise.resolve();

/** Runs `task` once every earlier task has settled. A rejection reaches only its caller. */
export function runMermaid<T>(task: () => Promise<T>): Promise<T> {
  const run = tail.then(() => task());
  // intentionally not logged: the caller of `run` receives the rejection; the queue only has to keep going
  tail = run.then(
    () => undefined,
    () => undefined,
  );
  return run;
}
