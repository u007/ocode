/**
 * Fires `handler` when the OS/network signals the app is usable again: the
 * window `online` event and a `visibilitychange` to visible. Both often fire
 * together when a laptop wakes, so triggers are deduplicated within one second
 * — otherwise the event bus and terminal sockets would each reconnect twice.
 *
 * Returns an unsubscribe function.
 */
export function onWake(handler: () => void): () => void {
  let lastFired = 0;
  const fire = () => {
    const now = Date.now();
    if (now - lastFired < 1000) return;
    lastFired = now;
    handler();
  };
  const onOnline = () => fire();
  const onVisibility = () => {
    if (document.visibilityState === "visible") fire();
  };
  window.addEventListener("online", onOnline);
  document.addEventListener("visibilitychange", onVisibility);
  return () => {
    window.removeEventListener("online", onOnline);
    document.removeEventListener("visibilitychange", onVisibility);
  };
}
