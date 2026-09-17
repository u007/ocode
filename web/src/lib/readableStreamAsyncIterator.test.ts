import { afterEach, describe, expect, it } from "vitest";
import { ensureReadableStreamAsyncIterator } from "./readableStreamAsyncIterator";

// Loose view of the prototype: WebKit is missing the method even though the TS
// DOM libs declare it, so the tests delete/restore it behind a cast.
type ShimmedProto = {
  [Symbol.asyncIterator]?: () => AsyncIteratorObject<unknown>;
};

const proto = ReadableStream.prototype as unknown as ShimmedProto;
const nativeAsyncIterator = proto[Symbol.asyncIterator];

// Simulate WebKit by removing the method; always restore the environment after
// each case so other test files keep the real prototype.
function simulateWebKit(): void {
  delete proto[Symbol.asyncIterator];
}

afterEach(() => {
  if (nativeAsyncIterator === undefined) delete proto[Symbol.asyncIterator];
  else proto[Symbol.asyncIterator] = nativeAsyncIterator;
});

function streamOf(chunks: string[], onCancel?: () => void): ReadableStream<string> {
  return new ReadableStream<string>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk);
      controller.close();
    },
    cancel: onCancel,
  });
}

describe("ensureReadableStreamAsyncIterator", () => {
  it("installs the method WebKit omits so for await … of works again", async () => {
    simulateWebKit();
    // Without the shim the exact pdf.js-style loop fails (this is the bug: the
    // text layer threw "undefined is not a function (near '...e of t...')").
    const unshimmed: string[] = [];
    await expect(
      (async () => {
        for await (const chunk of streamOf(["a"])) unshimmed.push(chunk);
      })(),
    ).rejects.toBeInstanceOf(TypeError);

    ensureReadableStreamAsyncIterator();
    expect(typeof proto[Symbol.asyncIterator]).toBe("function");

    const got: string[] = [];
    for await (const chunk of streamOf(["a", "b", "c"])) got.push(chunk);
    expect(got).toEqual(["a", "b", "c"]);
  });

  it("leaves an engine that already implements it untouched", () => {
    ensureReadableStreamAsyncIterator();
    const after = proto[Symbol.asyncIterator];
    ensureReadableStreamAsyncIterator();
    expect(proto[Symbol.asyncIterator]).toBe(after);
    if (nativeAsyncIterator !== undefined) expect(after).toBe(nativeAsyncIterator);
  });

  it("releases the reader when the consumer breaks out of the loop", async () => {
    simulateWebKit();
    ensureReadableStreamAsyncIterator();
    let cancelled = false;
    for await (const _chunk of streamOf(["first", "second"], () => {
      cancelled = true;
    })) {
      break;
    }
    expect(cancelled).toBe(true);
  });

  it("propagates a stream error to the loop", async () => {
    simulateWebKit();
    ensureReadableStreamAsyncIterator();
    const failing = new ReadableStream<string>({
      start(controller) {
        controller.error(new Error("boom"));
      },
    });
    await expect(
      (async () => {
        for await (const _chunk of failing) {
          /* never reached */
        }
      })(),
    ).rejects.toThrow("boom");
  });
});
