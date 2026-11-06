import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { collectVisibleText, throttleTrailing } from "./viewerDom";

// jsdom returns zero rects, so each element gets an explicit vertical span.
function place(el: HTMLElement, top: number, bottom: number): HTMLElement {
  el.getBoundingClientRect = () => ({ top, bottom, left: 0, right: 0, width: 0, height: bottom - top, x: 0, y: top, toJSON: () => ({}) }) as DOMRect;
  return el;
}

function scrollerWith(html: string): HTMLElement {
  const scroller = document.createElement("div");
  scroller.innerHTML = html;
  place(scroller, 0, 500);
  return scroller;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("collectVisibleText", () => {
  it("returns visible blocks in document order and skips those outside the viewport", () => {
    const s = scrollerWith("<p>one</p><p>two</p><ul><li>three</li></ul><p>offscreen</p>");
    const [p1, p2, ul, li, off] = [...s.querySelectorAll<HTMLElement>("p, ul, li")];
    place(p1, 0, 50);
    place(p2, 60, 100);
    place(ul, 110, 130);
    place(li, 110, 130);
    place(off, 600, 620);
    expect(collectVisibleText(s, 1200).text).toBe("one\ntwo\nthree");
  });

  it("does not repeat a block already carried by an included ancestor", () => {
    const s = scrollerWith("<ul><li>Item <p>Alpha</p></li></ul>");
    const li = s.querySelector<HTMLElement>("li")!;
    const p = s.querySelector<HTMLElement>("p")!;
    place(li, 0, 60);
    place(p, 20, 40);
    expect(collectVisibleText(s, 1200).text).toBe("Item Alpha");
  });

  it("stops once the text exceeds the cap", () => {
    const s = scrollerWith("<p>aaaaaaaaaa</p><p>bbbbbbbbbb</p><p>cccccccccc</p>");
    const ps = [...s.querySelectorAll<HTMLElement>("p")];
    place(ps[0], 0, 50);
    place(ps[1], 60, 100);
    place(ps[2], 110, 150);
    const result = collectVisibleText(s, 15);
    expect(result.text.length).toBeLessThanOrEqual(15);
    expect(result.text).not.toContain("c");
  });

  it("reports the last heading above the viewport top", () => {
    const s = scrollerWith("<h1>Top</h1><h2>Mid</h2><p>body</p>");
    const [h1, h2, p] = [...s.querySelectorAll<HTMLElement>("h1, h2, p")];
    place(h1, -200, -150);
    place(h2, -100, -50);
    place(p, 20, 60);
    expect(collectVisibleText(s, 1200).heading).toBe("Mid");
  });

  it("falls back to the first heading inside the viewport when none is above", () => {
    const s = scrollerWith("<h2>Inside</h2><h3>Later</h3>");
    const [h2, h3] = [...s.querySelectorAll<HTMLElement>("h2, h3")];
    place(h2, 100, 130);
    place(h3, 300, 320);
    expect(collectVisibleText(s, 1200).heading).toBe("Inside");
  });

  it("reports no heading when the document has none", () => {
    const s = scrollerWith("<p>plain</p>");
    place(s.querySelector<HTMLElement>("p")!, 0, 40);
    expect(collectVisibleText(s, 1200).heading).toBeNull();
  });
});

describe("throttleTrailing", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("runs once with the latest arguments after the window", () => {
    const fn = vi.fn((_n: number) => {});
    const throttled = throttleTrailing(fn, 250);
    throttled(1);
    throttled(2);
    throttled(3);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(250);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenCalledWith(3);
  });

  it("drops a pending call on cancel", () => {
    const fn = vi.fn((_n: number) => {});
    const throttled = throttleTrailing(fn, 250);
    throttled(1);
    throttled.cancel();
    vi.advanceTimersByTime(500);
    expect(fn).not.toHaveBeenCalled();
  });
});
