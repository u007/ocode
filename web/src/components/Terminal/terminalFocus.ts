// Simple registry so the tab bar can focus an already-active xterm without
// threading refs through App -> TerminalTabs -> TerminalPanel.
const focusMap = new Map<string, () => void>();

export function registerTerminalFocus(id: string, fn: () => void): void {
  focusMap.set(id, fn);
}

export function unregisterTerminalFocus(id: string): void {
  focusMap.delete(id);
}

export function focusTerminalById(id: string): void {
  focusMap.get(id)?.();
}
