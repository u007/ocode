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
