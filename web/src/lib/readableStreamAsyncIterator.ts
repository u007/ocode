/**
 * `ReadableStream` async-iteration shim for WebKit.
 *
 * WebKit (Safari, and therefore the WKWebView-backed ocode desktop shell) ships
 * `ReadableStream` but never implemented async iteration over it: there is no
 * `ReadableStream.prototype[Symbol.asyncIterator]` (nor `values()`). Chrome and
 * Firefox have had both for years.
 *
 * That gap is fatal for pdf.js v6: `PDFPageProxy.getTextContent()` assembles the
 * text layer with
 *
 *     for await (const chunk of this.streamTextContent()) { … }
 *
 * so on WebKit the call throws the notoriously opaque
 *
 *     TypeError: undefined is not a function (near '...e of t...')
 *
 * AFTER the page has already rasterised — the canvas paints, then the viewer
 * dies with "PDF failed: …". Installing the missing method (spec-shaped, backed
 * by `getReader()`) restores `for await … of` for every `ReadableStream`.
 *
 * The shim is feature-detected, so Chrome/Firefox and any engine that already
 * implements it are left completely untouched.
 *
 * Reference: https://bugs.webkit.org/show_bug.cgi?id=240634
 */
export function ensureReadableStreamAsyncIterator(): void {
  if (typeof ReadableStream === "undefined") return;

  // TypeScript's DOM libs (lib.dom.asynciterable) type this method as if every
  // engine implemented it — WebKit does not, so the runtime shape is what we
  // have to test for. Hence the cast through unknown.
  const proto = ReadableStream.prototype as unknown as {
    [Symbol.asyncIterator]?: () => AsyncIteratorObject<unknown>;
  };
  if (typeof proto[Symbol.asyncIterator] === "function") return;

  proto[Symbol.asyncIterator] = function (this: ReadableStream<unknown>): AsyncIteratorObject<unknown> {
    const reader = this.getReader();
    const iterator: AsyncIteratorObject<unknown> = {
      next: async () => {
        const { done, value } = await reader.read();
        return done ? { done: true, value: undefined } : { done: false, value };
      },
      // `for await` calls return() when the consumer breaks early; cancel the
      // reader so the underlying source is released like the native iterators.
      return: async (value?: unknown) => {
        await reader.cancel(value);
        return { done: true, value };
      },
      throw: async (error?: unknown) => {
        await reader.cancel(error);
        throw error;
      },
      [Symbol.asyncIterator]() {
        return iterator;
      },
    };
    return iterator;
  };
}
