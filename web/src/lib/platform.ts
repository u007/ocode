/**
 * True on Apple platforms. Used only to print the right modifier in a shortcut
 * label (⌘ versus Ctrl); behaviour never branches on it, because the bindings
 * accept both Cmd and Ctrl.
 */
export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return false;
  return /Mac|iPhone|iPad|iPod/.test(navigator.userAgent);
}
