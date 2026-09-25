// Mount point for the execCommand fallback's scratch textarea. It must live
// inside an open Radix modal's focus scope: an element appended to
// document.body sits outside the dialog's FocusScope, which bounces focus
// straight back into the dialog, so the scratch field is never the active
// selection and document.execCommand("copy") returns true while copying
// nothing. That is the "button says Copied but the clipboard is empty" bug.
function clipboardScratchHost(): HTMLElement {
  const active = document.activeElement;
  if (active instanceof HTMLElement && typeof active.closest === "function") {
    const dialog = active.closest('[role="dialog"]');
    if (dialog instanceof HTMLElement) return dialog;
  }
  return document.body;
}

// Best-effort clipboard write that works outside secure contexts (plain
// http://<lan-ip> origins, WKWebView/WebKitGTK shells) where
// navigator.clipboard is undefined or rejects. Returns true only when the
// text is known to have been written.
export async function copyTextToClipboard(text: string): Promise<boolean> {
  if (text === "") return false;
  try {
    if (
      typeof navigator !== "undefined" &&
      navigator.clipboard &&
      typeof navigator.clipboard.writeText === "function"
    ) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through to the execCommand fallback below.
  }
  // Synchronous fallback. execCommand copies the *active* selection, and it
  // returns true even when it copied nothing, so require that the scratch
  // textarea really took focus before believing it.
  const previousFocus = document.activeElement;
  let ta: HTMLTextAreaElement | null = null;
  try {
    ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    // Keep it out of view and out of layout.
    ta.style.position = "fixed";
    ta.style.top = "-9999px";
    ta.style.left = "-9999px";
    ta.style.opacity = "0";
    clipboardScratchHost().appendChild(ta);
    ta.focus({ preventScroll: true });
    ta.select();
    // iOS needs an explicit range.
    try {
      ta.setSelectionRange(0, ta.value.length);
    } catch {
      // Older WebKit can throw here; the select() above is the best effort.
    }
    if (document.activeElement === ta && document.execCommand("copy")) {
      return true;
    }
  } catch {
    // Fall through to the false return below.
  } finally {
    ta?.remove();
    if (previousFocus instanceof HTMLElement && previousFocus.isConnected) {
      try {
        previousFocus.focus({ preventScroll: true });
      } catch {
        // Focus restoration is best effort; never mask the copy result.
      }
    }
  }
  return false;
}
