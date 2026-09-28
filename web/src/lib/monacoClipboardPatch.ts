import { BrowserClipboardService } from "monaco-editor/esm/vs/platform/clipboard/browser/clipboardService.js";

/**
 * Neutralize Monaco's WebKit clipboard workaround.
 *
 * Why: when Monaco's `BrowserClipboardService` is constructed in a WebKit
 * context (`isSafari || isWebkitWebView` — the Wails desktop shell's WKWebView
 * has a UA without "Safari", so `isWebkitWebView` is true), it installs
 * `installWebKitWriteTextWorkaround()`. That registers `click` and `keydown`
 * listeners on the layout service's main container — which for standalone
 * Monaco is `document.body` — and each event calls
 * `navigator.clipboard.write([new ClipboardItem({"text/plain": <pending promise>})])`.
 *
 * In the desktop WKWebView that write is denied, so **every click and every
 * keystroke** (including typing in the chat input) spawns a rejected
 * `NotAllowedError` promise and a pasteboard IPC round-trip, which spams the
 * console and stalls the UI. See `docs/gotchas/monaco-webkit-clipboard-workaround.md`.
 *
 * Called unconditionally (not gated on `isDesktopShell()`): the workaround is
 * only meaningful when an async clipboard write is issued from outside a user
 * gesture, which ocode never does. Plain Safari therefore loses nothing either.
 *
 * ocode never uses Monaco's async clipboard API: its own copy paths go through
 * `lib/clipboard.ts` / `writeClipboardText` (`navigator.clipboard.writeText`
 * with an `execCommand` fallback). Removing the workaround leaves Monaco's
 * normal path intact — `writeText` falls through to `writeText` + textarea
 * fallback (`clipboardService.js`), which works on a real user gesture.
 */
export function disableMonacoWebKitClipboardWorkaround(): void {
  BrowserClipboardService.prototype.installWebKitWriteTextWorkaround = () => {};
}
