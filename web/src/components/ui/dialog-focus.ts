/**
 * Shared initial-focus policy for dialog surfaces.
 *
 * Lives in its own module (not `dialog.tsx`) so every dialog flavour imports
 * ONE copy of the rules: `dialog.tsx` and `scoped-dialog.tsx` both depend on
 * it, and `dialog.tsx`'s export surface stays component-only (a non-component
 * export in a component module also defeats React Fast Refresh for that file).
 * A duplicated copy of these rules would drift silently, and then some dialog
 * surfaces would focus a different element than the rest of the app.
 */

/**
 * Selects text-entry fields only — checkboxes, radios, file/range/color
 * pickers and push-button inputs are not text entry, so they must not win
 * the initial focus (a dialog whose only "inputs" are a dropdown or a
 * checkbox falls through to the annotated default action instead).
 */
const TEXT_INPUT_SELECTOR = [
  'input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"]):not([type="button"]):not([type="submit"]):not([type="reset"]):not([type="image"]):not([type="file"]):not([type="range"]):not([type="color"])',
  "textarea",
  '[contenteditable="true"]',
  '[contenteditable=""]',
].join(", ");

/**
 * Initial-focus policy for every dialog in the app (web + desktop):
 * 1. the first text-entry field in DOM order (search / filter / prompt input,
 *    including cmdk's `[cmdk-input]`), else
 * 2. the button annotated `data-dialog-default-action` (the safe primary
 *    action, e.g. "Allow once" — never the destructive first button), else
 * 3. Radix's own default: the first tabbable element.
 * Returns true when a target was focused so the caller can preventDefault
 * Radix's "focus first tabbable" pass.
 */
export function focusDialogInitialTarget(content: HTMLElement | null): boolean {
  if (!content) return false;
  const inputs = content.querySelectorAll<HTMLElement>(TEXT_INPUT_SELECTOR);
  for (const el of Array.from(inputs)) {
    // focus() is a silent no-op on disabled/hidden elements; the
    // activeElement check moves on to the next candidate in that case.
    el.focus({ preventScroll: true });
    if (document.activeElement === el) return true;
  }
  const defaultAction = content.querySelector<HTMLElement>(
    "[data-dialog-default-action]:not([disabled])",
  );
  if (defaultAction) {
    defaultAction.focus({ preventScroll: true });
    if (document.activeElement === defaultAction) return true;
  }
  return false;
}