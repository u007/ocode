import { describe, it, expect, beforeEach } from "vitest";
import { rekeySidePaneState, sideChatKey, sideTermKey } from "./sidePaneState";
import { browserStore, browserActions } from "./browserStore";
import { loadSidebarPreviewState, saveSidebarPreviewState } from "../components/Preview/sidebarPreviewState";

beforeEach(() => {
  browserStore.setState(() => ({ byKey: {} }));
  window.localStorage.clear();
});

describe("rekeySidePaneState", () => {
  it("builds the side surface keys", () => {
    expect(sideChatKey("ses_1")).toBe("side:chat:ses_1");
    expect(sideTermKey("term-9")).toBe("side:term:term-9");
  });
  it("moves both the live browser surface and the persisted preview to the new session id", () => {
    const oldKey = sideChatKey("new-1");
    const newKey = sideChatKey("ses_real");
    browserActions.open(oldKey);
    browserActions.navigate(oldKey, "https://a.com");
    saveSidebarPreviewState(oldKey, { surface: "preview", path: "a.md", kind: "markdown" });

    rekeySidePaneState("new-1", "ses_real");

    expect(browserStore.state.byKey[oldKey]).toBeUndefined();
    expect(browserStore.state.byKey[newKey]?.url).toBe("https://a.com");
    expect(loadSidebarPreviewState(oldKey)).toBeNull();
    expect(loadSidebarPreviewState(newKey)?.path).toBe("a.md");
  });

  it("also moves the terminal pane key, which embeds the same session id", () => {
    browserActions.open(sideTermKey("new-1"));
    saveSidebarPreviewState(sideTermKey("new-1"), { surface: "preview", path: "t.md", kind: "markdown" });
    rekeySidePaneState("new-1", "ses_real");
    expect(browserStore.state.byKey[sideTermKey("new-1")]).toBeUndefined();
    expect(browserStore.state.byKey[sideTermKey("ses_real")]).toBeTruthy();
    expect(loadSidebarPreviewState(sideTermKey("ses_real"))?.path).toBe("t.md");
  });

  it("no-ops for empty or identical ids", () => {
    const key = sideChatKey("s1");
    browserActions.open(key);
    rekeySidePaneState("s1", "s1");
    rekeySidePaneState("", "s2");
    rekeySidePaneState(null, "s2");
    expect(browserStore.state.byKey[key]).toBeTruthy();
    expect(browserStore.state.byKey[sideChatKey("s2")]).toBeUndefined();
  });
});
