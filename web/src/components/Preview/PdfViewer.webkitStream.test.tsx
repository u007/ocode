import { afterEach, describe, expect, it, vi } from "vitest";

// PdfViewer lazily pulls in pdf.js; stub it so importing the module stays cheap
// and deterministic. We only care about the module's platform shim, not pdf.js.
vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: { workerSrc: "" },
  Util: { transform: vi.fn() },
  getDocument: vi.fn(),
}));

type ShimmedProto = {
  [Symbol.asyncIterator]?: () => AsyncIteratorObject<unknown>;
};

const proto = ReadableStream.prototype as unknown as ShimmedProto;
const nativeAsyncIterator = proto[Symbol.asyncIterator];

afterEach(() => {
  if (nativeAsyncIterator === undefined) delete proto[Symbol.asyncIterator];
  else proto[Symbol.asyncIterator] = nativeAsyncIterator;
  vi.resetModules();
});

describe("PdfViewer module load", () => {
  it("installs the WebKit ReadableStream async-iterator shim before pdf.js runs", async () => {
    delete proto[Symbol.asyncIterator]; // WebKit has no async iteration on streams
    expect(proto[Symbol.asyncIterator]).toBeUndefined();

    await import("./PdfViewer");

    // pdf.js's getTextContent() does `for await (… of streamTextContent())`; the
    // module must have closed the WebKit gap by the time the viewer is usable.
    expect(typeof proto[Symbol.asyncIterator]).toBe("function");

    const chunks: string[] = [];
    const stream = new ReadableStream<string>({
      start(controller) {
        controller.enqueue("p.1");
        controller.close();
      },
    });
    for await (const chunk of stream) chunks.push(chunk);
    expect(chunks).toEqual(["p.1"]);
  });
});
