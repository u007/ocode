import { useEffect } from "react";

/**
 * PulseShellSignal — lets the desktop shell open the Pulse dashboard.
 *
 * Transport note (this is the load-bearing part): the desktop webview loads a
 * plain `http://127.0.0.1:PORT` URL served by ocode's own embed.FS-backed HTTP
 * server, NOT the `wails://` scheme. Wails injects `window._wails` via that
 * scheme handler, so `window.EmitEvent` / `window.wails.Events` are
 * **structurally unavailable** here — a no-op, not a timing issue. The shell
 * therefore uses `window.ExecJS` to dispatch a plain DOM CustomEvent, exactly
 * as it already does for `ocode:open-settings` (see buildAppMenu in
 * cmd/ocode-desktop/main.go). This component is the receiving end.
 *
 * In a plain browser the event is simply never dispatched, so this is inert.
 */
export const PULSE_SHELL_EVENT = "ocode:open-pulse";

export function PulseShellSignal({
  onOpen,
  onFocus,
}: {
  onOpen: () => void;
  /** Raise the window. Called after onOpen. */
  onFocus: () => void;
}) {
  useEffect(() => {
    const handler = () => {
      onOpen();
      onFocus();
    };
    window.addEventListener(PULSE_SHELL_EVENT, handler);
    return () => window.removeEventListener(PULSE_SHELL_EVENT, handler);
  }, [onOpen, onFocus]);

  return null;
}
