import { describe, expect, it } from "vitest";
import { shouldRenderReaderPreview, type ReaderModeInput } from "./readerMode";

const on: ReaderModeInput = {
  sideStateKey: "side:chat:s1",
  sidePanelKind: "chat",
  browserOpen: true,
  collapsed: false,
  reader: true,
  sidePaneVisible: true,
  isMobile: false,
};

describe("shouldRenderReaderPreview", () => {
  it("renders when every gate holds", () => {
    expect(shouldRenderReaderPreview(on)).toBe(true);
  });

  it("is off without reader mode set", () => {
    expect(shouldRenderReaderPreview({ ...on, reader: false })).toBe(false);
    expect(shouldRenderReaderPreview({ ...on, reader: undefined })).toBe(false);
  });

  it("is off with no focused surface", () => {
    expect(shouldRenderReaderPreview({ ...on, sideStateKey: null })).toBe(false);
  });

  it("is off for the terminal side panel", () => {
    expect(shouldRenderReaderPreview({ ...on, sidePanelKind: "term" })).toBe(false);
  });

  it("is off when the side pane is closed", () => {
    expect(shouldRenderReaderPreview({ ...on, browserOpen: false })).toBe(false);
  });

  it("is off when the side pane is collapsed", () => {
    expect(shouldRenderReaderPreview({ ...on, collapsed: true })).toBe(false);
  });

  it("is off on sub-tabs that hide the side pane", () => {
    expect(shouldRenderReaderPreview({ ...on, sidePaneVisible: false })).toBe(false);
  });

  it("is off on mobile", () => {
    expect(shouldRenderReaderPreview({ ...on, isMobile: true })).toBe(false);
  });
});
