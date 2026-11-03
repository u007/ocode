// Navigation hints from the embedded-browser capture bundle (internal/browse/capture.js):
// a URL is posted only when it changes, and popstate/hashchange are classified as
// "traverse" so the store selects an existing history entry instead of rewriting one.
// The bundle is evaluated in the jsdom realm, like browseCapture.smoke.test.ts.
// Kept in its own file: each evaluation adds window popstate/hashchange listeners
// that no test can remove, so a second case here would see the first case's posts.
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import captureBundle from "../../../../internal/browse/capture.js?raw";

const DOC_PATH = "/b/side:chat:abc/https/www.yahoo.com/";

// Native methods, read before the bundle wraps them.
const pristinePushState = history.pushState;
const pristineReplaceState = history.replaceState;

describe("browse capture navigation hints", () => {
  let posts: Array<Record<string, unknown>>;

  beforeEach(() => {
    history.pushState = pristinePushState;
    history.replaceState = pristineReplaceState;
    posts = [];
    vi.spyOn(window.parent, "postMessage").mockImplementation((msg: unknown) => {
      posts.push(msg as Record<string, unknown>);
    });
    pristineReplaceState.call(history, {}, "", DOC_PATH);
    (window as unknown as { __OCODE_BROWSE__?: { stateKey: string; spaOrigin: string } }).__OCODE_BROWSE__ = {
      stateKey: "side:chat:abc",
      spaOrigin: window.location.origin,
    };
    (0, eval)(captureBundle);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    history.pushState = pristinePushState;
    history.replaceState = pristineReplaceState;
  });

  it("posts a URL only when it changes, and classifies popstate/hashchange as traverse", () => {
    history.pushState({}, "", DOC_PATH + "news");
    history.pushState({}, "", DOC_PATH + "news");
    history.replaceState({}, "", DOC_PATH + "news?x=1");
    history.replaceState({}, "", DOC_PATH + "news?x=1");
    // A traversal the page did not script: move the document without the hooks, then report it.
    pristinePushState.call(history, {}, "", DOC_PATH + "other");
    window.dispatchEvent(new PopStateEvent("popstate"));
    window.dispatchEvent(new PopStateEvent("popstate"));
    pristineReplaceState.call(history, {}, "", DOC_PATH + "other#frag");
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    const nav = posts
      .filter((m) => m.type === "ocode:browse:nav")
      .map((m) => [m.kind, m.url]);
    expect(nav).toEqual([
      ["push", "https://www.yahoo.com/news"],
      ["replace", "https://www.yahoo.com/news?x=1"],
      ["traverse", "https://www.yahoo.com/other"],
      ["traverse", "https://www.yahoo.com/other#frag"],
    ]);
  });
});
