type WailsWindow = Window & {
  _wails?: {
    invoke?: (message: string) => void;
  };
};

// The SPA is served by ocode's HTTP server rather than Wails' asset server,
// so the full Wails runtime does not post the ready message for us. The
// desktop shell calls this until its minimal bridge has been injected.
export function notifyWailsRuntimeReady(target: Window = window): boolean {
  const invoke = (target as WailsWindow)._wails?.invoke;
  if (!invoke) {
    return false;
  }
  invoke("wails:runtime:ready");
  return true;
}

/**
 * Send a raw message to the desktop shell. Messages that do not start with
 * "wails:" reach application.Options.RawMessageHandler on the Go side; this is
 * how the SPA talks to native code without loading the full runtime module.
 *
 * Returns false in a plain browser (no bridge), so callers can no-op.
 */
export function invokeWails(message: string, target: Window = window): boolean {
  const invoke = (target as WailsWindow)._wails?.invoke;
  if (!invoke) {
    return false;
  }
  invoke(message);
  return true;
}

/**
 * Ask the desktop shell to raise and focus its window.
 *
 * Used when the user opens the Pulse dashboard from the tray or the app menu
 * while ocode is in the background: the page already switched views, but
 * without this the new view appears behind whatever app is in front, which
 * reads as "nothing happened". Returns false in a plain browser.
 */
export function focusDesktopWindow(target: Window = window): boolean {
  return invokeWails("ocode:focus-window", target);
}
