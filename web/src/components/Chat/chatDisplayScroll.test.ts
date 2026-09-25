import { describe, expect, it, vi } from "vitest";
import {
  captureChatDisplayAnchor,
  restoreChatDisplayAnchor,
} from "./chatDisplayScroll";

describe("chat display scroll anchoring", () => {
  it("captures the pixel distance inside the first visible row", () => {
    const anchor = captureChatDisplayAnchor(137, 4, (_index) => [40, "auto"] as const);
    expect(anchor).toEqual({ index: 4, distance: 97 });
  });

  it("restores the same row and in-row offset after remeasurement", () => {
    const scrollElement = { scrollTop: 0 };
    const virtualizer = {
      scrollToIndex: vi.fn(),
      getOffsetForIndex: vi.fn(() => [62, "auto"] as const),
    };
    restoreChatDisplayAnchor(scrollElement, { index: 4, distance: 97 }, virtualizer);
    expect(virtualizer.scrollToIndex).toHaveBeenCalledWith(4, { align: "start" });
    expect(scrollElement.scrollTop).toBe(159);
  });

  it("leaves scrollTop unchanged when the virtualizer cannot resolve the offset", () => {
    const scrollElement = { scrollTop: 12 };
    const virtualizer = {
      scrollToIndex: vi.fn(),
      getOffsetForIndex: vi.fn(() => undefined),
    };
    restoreChatDisplayAnchor(scrollElement, { index: 2, distance: 4 }, virtualizer);
    expect(virtualizer.scrollToIndex).toHaveBeenCalledWith(2, { align: "start" });
    expect(scrollElement.scrollTop).toBe(12);
  });
});
