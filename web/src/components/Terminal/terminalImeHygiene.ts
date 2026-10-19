/**
 * Keeps xterm's hidden input textarea empty between IME commits.
 *
 * xterm never clears that textarea after a composition or a keyCode-229 input;
 * it diffs `value` before/after instead (CompositionHelper). On Android Gboard
 * the retained text is autocorrect/prediction context: Gboard re-commits or
 * replaces earlier words, the diff sees a changed value, and the whole earlier
 * text is sent to the pty again ("did we have rate limit orotecdid we have
 * rage…"). With nothing left to replace, each commit is only the new text.
 *
 * The clear runs after xterm's own `setTimeout(0)` reads, and never while a
 * composition is in flight.
 */
const CLEAR_DELAY_MS = 50;

export function attachImeHygiene(textarea: HTMLTextAreaElement): () => void {
  let composing = false;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const scheduleClear = () => {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      if (!composing && textarea.value !== "") textarea.value = "";
    }, CLEAR_DELAY_MS);
  };
  const onCompositionStart = () => {
    composing = true;
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  };
  const onCompositionEnd = () => {
    composing = false;
    scheduleClear();
  };
  const onInput = () => {
    if (!composing) scheduleClear();
  };

  textarea.addEventListener("compositionstart", onCompositionStart);
  textarea.addEventListener("compositionend", onCompositionEnd);
  textarea.addEventListener("input", onInput);
  return () => {
    if (timer !== null) clearTimeout(timer);
    textarea.removeEventListener("compositionstart", onCompositionStart);
    textarea.removeEventListener("compositionend", onCompositionEnd);
    textarea.removeEventListener("input", onInput);
  };
}
