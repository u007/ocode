import { describe, expect, it } from "vitest";
import { SseParser, readSse } from "./sse";

describe("SseParser", () => {
  it("parses events split across chunks, CRLF, comments and multi-line data", () => {
    const p = new SseParser();
    const out = [
      ...p.feed(": connected\n\nevent: envelope\nda"),
      ...p.feed('ta: {"a":1}\n\n'),
      ...p.feed("event: x\r\ndata: l1\r\ndata: l2\r\n\r\n"),
      ...p.feed("data: no-event-name\n\n"),
    ];
    expect(out).toEqual([
      { event: "envelope", data: '{"a":1}', id: undefined },
      { event: "x", data: "l1\nl2", id: undefined },
      { event: "message", data: "no-event-name", id: undefined },
    ]);
  });
  it("does not split a CRLF that straddles two chunks", () => {
    const p = new SseParser();
    expect([...p.feed("event: a\r"), ...p.feed("\ndata: 1\r\n\r\n")]).toEqual([{ event: "a", data: "1", id: undefined }]);
  });
  it("readSse streams events as they arrive", async () => {
    const enc = new TextEncoder();
    const body = new ReadableStream<Uint8Array>({
      start(c) { c.enqueue(enc.encode("event: a\ndata: 1\n\nevent: b\nda")); c.enqueue(enc.encode("ta: 2\n\n")); c.close(); },
    });
    const got: string[] = [];
    await readSse(body, (e) => got.push(`${e.event}:${e.data}`));
    expect(got).toEqual(["a:1", "b:2"]);
  });
});
