import { Ghostty } from "ghostty-web";

/**
 * The one libghostty-vt wasm instance every terminal shares (each panel, each Pulse screen
 * read), loaded on first use. Upstream ghostty-web 0.4.0 left recycled heap memory unzeroed,
 * so a terminal created after another was freed read the old one's cells as garbage; the
 * vendored u007/ghostty-web fork zero-initializes page buffers (verified 20/20 Pulse parity
 * fixtures on one shared instance, 2026-10-28), so sharing is safe. The instance is passed to
 * `Terminal` as `options.ghostty` and to the headless `createTerminal`; ghostty-web's own
 * `init()` keeps its instance private, so we cannot use it. A failed load is not cached:
 * the error reaches every caller and the next call retries.
 */
let shared: Promise<Ghostty> | null = null;

export function loadGhostty(): Promise<Ghostty> {
  if (!shared) {
    shared = Ghostty.load().catch((err: unknown) => {
      shared = null;
      throw err;
    });
  }
  return shared;
}
