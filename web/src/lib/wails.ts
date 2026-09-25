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
