import type { Terminal } from "@xterm/xterm";

/**
 * Live xterm.js instances, keyed by terminal id. TerminalPanel registers on
 * mount and unregisters on cleanup — the registry itself does not affect
 * mounting/unmounting, it only observes it. Exists so the frontend memory
 * reporter (see lib/debug/frontendMemoryReporter.tsx) can attribute renderer
 * memory to terminal count/scrollback without threading state through React.
 */
const terminals = new Map<string, Terminal>();

export function registerTerminal(id: string, term: Terminal): void {
  terminals.set(id, term);
}

export function unregisterTerminal(id: string): void {
  terminals.delete(id);
}

export function terminalRegistrySnapshot(): { count: number; totalLines: number } {
  let totalLines = 0;
  for (const term of terminals.values()) {
    totalLines += term.buffer.active.length;
  }
  return { count: terminals.size, totalLines };
}
