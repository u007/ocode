import { describe, it, expect } from "vitest";
import { browseOriginOf, isTrustedBrowseMessage } from "./browseOrigin";

const TAB = "http://127.0.0.1:40001";
const OTHER_TAB = "http://127.0.0.1:40002";
const SPA = "http://localhost:4096";

function frameWindow(): Window {
  return { name: "frame" } as unknown as Window;
}

describe("browseOriginOf", () => {
  it("returns the scheme, host and port, dropping path and query", () => {
    expect(browseOriginOf("http://127.0.0.1:40001/app/dashboard?__grant=abc#x")).toBe(TAB);
  });

  it("returns null for an unparseable URL", () => {
    expect(browseOriginOf("not a url")).toBeNull();
    expect(browseOriginOf("")).toBeNull();
  });

  it("returns null for an opaque origin", () => {
    expect(browseOriginOf("data:text/html,hi")).toBeNull();
    expect(browseOriginOf("blob:null/1234")).toBeNull();
  });
});

describe("isTrustedBrowseMessage", () => {
  it("accepts a message from the tab origin whose source is the iframe window", () => {
    const frame = frameWindow();
    const gate = { origin: TAB, frame };
    expect(isTrustedBrowseMessage({ origin: TAB, source: frame }, gate)).toBe(true);
  });

  it("rejects a message from another tab's origin", () => {
    const frame = frameWindow();
    expect(isTrustedBrowseMessage({ origin: OTHER_TAB, source: frame }, { origin: TAB, frame })).toBe(false);
  });

  it("rejects a message from the SPA's own origin", () => {
    const frame = frameWindow();
    expect(isTrustedBrowseMessage({ origin: SPA, source: frame }, { origin: TAB, frame })).toBe(false);
  });

  it("rejects a null (opaque) origin even when the source matches", () => {
    const frame = frameWindow();
    expect(isTrustedBrowseMessage({ origin: "null", source: frame }, { origin: "null", frame })).toBe(false);
    expect(isTrustedBrowseMessage({ origin: "null", source: frame }, { origin: TAB, frame })).toBe(false);
  });

  it("rejects a message from the right origin but a different window", () => {
    const frame = frameWindow();
    expect(isTrustedBrowseMessage({ origin: TAB, source: frameWindow() }, { origin: TAB, frame })).toBe(false);
    expect(isTrustedBrowseMessage({ origin: TAB, source: null }, { origin: TAB, frame })).toBe(false);
  });

  it("rejects everything before the first load and when no iframe is mounted", () => {
    const frame = frameWindow();
    expect(isTrustedBrowseMessage({ origin: TAB, source: frame }, { origin: null, frame })).toBe(false);
    expect(isTrustedBrowseMessage({ origin: TAB, source: frame }, { origin: TAB, frame: null })).toBe(false);
  });
});
