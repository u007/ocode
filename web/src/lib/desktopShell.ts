/**
 * True when running inside the ocode desktop shell's webview. The Wails
 * runtime core is injected into every webview page; a plain browser tab never
 * has it.
 */
export function isDesktopShell() {
  return (
    typeof window !== "undefined" &&
    typeof (window as unknown as { _wails?: unknown })._wails !== "undefined"
  );
}
