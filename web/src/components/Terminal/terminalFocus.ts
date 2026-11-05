import type { Terminal } from "ghostty-web";

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

/** Focus the terminal's hidden input textarea, never the contenteditable host. ghostty-web's
 *  `Terminal.focus()` focuses the host; a phone soft keyboard then types into the host, whose
 *  `beforeinput` ghostty-web cancels instead of forwarding (only the textarea's reaches the pty),
 *  so the keyboard is up and nothing is typed. Hardware keys bubble to the host either way. */
export function focusTerminalInput(term: Pick<Terminal, "textarea">): void {
  const textarea = term.textarea;
  // ghostty-web creates the textarea in open() and removes it in dispose(), so a missing
  // textarea is the normal pre-open or disposed state. Old term.focus() was a no-op there too.
  if (!textarea) return;
  if (!(textarea instanceof HTMLTextAreaElement)) {
    throw new Error("focusTerminalInput: terminal input is not an HTMLTextAreaElement");
  }
  textarea.focus();
}
