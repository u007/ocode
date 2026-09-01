// Smoke test for the embedded-browser capture bundle's runtime URL hooks.
// webpack/Next.js lazy chunks are injected as <script src> / <link href> at
// runtime — never present in the initial HTML the server-side rewrite sees —
// so the capture script must reroute those assignments onto the /b/ route.
// Without the hooks they resolve against the browse origin ROOT and 404
// (ChunkLoadError → blank SPA), the failure class reported by QA on
// Next.js sites (e.g. yahoo.com).
//
// The bundle is Go-embedded (internal/browse/capture.js); this test loads the
// real file so a broken hook cannot pass by having matching strings elsewhere.
// It is evaluated with an indirect eval in the jsdom realm (vitest's jsdom
// does not run appended <script> nodes, and the hooks must land on the same
// window/document the test drives).
import { describe, it, expect, beforeEach } from "vitest";
import captureBundle from "../../../../internal/browse/capture.js?raw";

const DOC_PATH = "/b/side:chat:abc/https/www.yahoo.com/";

interface BrowseConfig {
  __OCODE_BROWSE__?: { stateKey: string; spaOrigin: string };
}

function installCapture() {
  window.history.replaceState({}, "", DOC_PATH);
  (window as unknown as BrowseConfig).__OCODE_BROWSE__ = {
    stateKey: "side:chat:abc",
    spaOrigin: window.location.origin,
  };
  (0, eval)(captureBundle);
}

describe("browse capture runtime URL hooks", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/");
    document.head.innerHTML = "";
    delete (window as unknown as BrowseConfig).__OCODE_BROWSE__;
  });

  it("reroutes a webpack-style runtime script.src chunk onto the /b/ route", () => {
    installCapture();
    const origin = window.location.origin;
    const el = document.createElement("script");
    el.src = "/_nca/_next/static/chunks/47905.js?dpl=sha-a0d8c46";
    expect(el.src).toBe(
      `${origin}/b/side:chat:abc/https/www.yahoo.com/_nca/_next/static/chunks/47905.js?dpl=sha-a0d8c46`,
    );
  });

  it("reroutes runtime <link href> (preload / styles) through the /b/ route", () => {
    installCapture();
    const origin = window.location.origin;
    const el = document.createElement("link");
    el.href = "/_next/static/css/chunk.css";
    expect(el.href).toBe(`${origin}/b/side:chat:abc/https/www.yahoo.com/_next/static/css/chunk.css`);
  });

  it("reroutes setAttribute-based script src too", () => {
    installCapture();
    const origin = window.location.origin;
    const el = document.createElement("script");
    el.setAttribute("src", "/assets/chunk2.js");
    expect(el.getAttribute("src")).toBe(
      `${origin}/b/side:chat:abc/https/www.yahoo.com/assets/chunk2.js`,
    );
  });

  it("leaves bare-relative chunk URLs to the document's own resolution", () => {
    installCapture();
    const origin = window.location.origin;
    const el = document.createElement("script");
    el.src = "chunk3.js";
    // reroute passes bare-relative through unchanged; the browser then
    // resolves against the current /b/ document path, which stays in-route.
    expect(el.src).toBe(`${origin}/b/side:chat:abc/https/www.yahoo.com/chunk3.js`);
  });

  it("leaves already-routed absolute URLs untouched", () => {
    installCapture();
    const origin = window.location.origin;
    const el = document.createElement("script");
    el.src = `${origin}/b/side:chat:abc/https/www.yahoo.com/_next/static/chunks/keep.js`;
    expect(el.src).toBe(`${origin}/b/side:chat:abc/https/www.yahoo.com/_next/static/chunks/keep.js`);
  });
});