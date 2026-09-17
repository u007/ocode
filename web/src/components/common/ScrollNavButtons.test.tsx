import { act, fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ScrollNavButtons from "./ScrollNavButtons";

function Harness() {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <div className="relative h-[300px]">
      <div ref={ref} data-testid="scroller" className="h-full overflow-y-auto" />
      <ScrollNavButtons scrollRef={ref} />
    </div>
  );
}

/** Give the scroller controllable metrics (jsdom has no layout engine). */
function makeScrollable(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  let top = 0;
  let height = scrollHeight;
  Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => height });
  Object.defineProperty(el, "clientHeight", { configurable: true, value: clientHeight });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      top = v;
    },
  });
  return {
    setTop: (v: number) => {
      top = v;
    },
    getTop: () => top,
    setScrollHeight: (h: number) => {
      height = h;
    },
  };
}

function scroller(): HTMLElement {
  return screen.getByTestId("scroller");
}

function scrollToTopButton() {
  return screen.queryByRole("button", { name: /scroll to top/i });
}

function scrollToBottomButton() {
  return screen.queryByRole("button", { name: /scroll to bottom/i });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ScrollNavButtons", () => {
  it("renders nothing when the content fits without scrolling", () => {
    render(<Harness />);
    makeScrollable(scroller(), 300, 300);
    fireEvent.scroll(scroller());

    expect(scrollToTopButton()).toBeNull();
    expect(scrollToBottomButton()).toBeNull();
  });

  it("offers only 'scroll to bottom' when parked at the top of a long surface", () => {
    render(<Harness />);
    makeScrollable(scroller(), 5000, 600);
    fireEvent.scroll(scroller());

    expect(scrollToTopButton()).toBeNull();
    expect(scrollToBottomButton()).toBeInTheDocument();
  });

  it("offers both buttons in the middle of a long surface", () => {
    render(<Harness />);
    const m = makeScrollable(scroller(), 5000, 600);
    m.setTop(2400);
    fireEvent.scroll(scroller());

    expect(scrollToTopButton()).toBeInTheDocument();
    expect(scrollToBottomButton()).toBeInTheDocument();
  });

  it("offers only 'scroll to top' when parked at the bottom", () => {
    render(<Harness />);
    const m = makeScrollable(scroller(), 5000, 600);
    m.setTop(4400); // scrollHeight - clientHeight
    fireEvent.scroll(scroller());

    expect(scrollToTopButton()).toBeInTheDocument();
    expect(scrollToBottomButton()).toBeNull();
  });

  it("jumps to the given edge when clicked", () => {
    render(<Harness />);
    const el = scroller();
    const m = makeScrollable(el, 5000, 600);
    const scrollTo = vi.fn();
    Object.defineProperty(el, "scrollTo", { configurable: true, value: scrollTo });
    m.setTop(2400);
    fireEvent.scroll(el);

    fireEvent.click(scrollToTopButton()!);
    expect(scrollTo).toHaveBeenCalledWith({ top: 0, behavior: "smooth" });

    fireEvent.click(scrollToBottomButton()!);
    expect(scrollTo).toHaveBeenCalledWith({ top: 5000, behavior: "smooth" });
  });

  it("falls back to assigning scrollTop when the element has no scrollTo", () => {
    render(<Harness />);
    const el = scroller();
    const m = makeScrollable(el, 5000, 600);
    Object.defineProperty(el, "scrollTo", { configurable: true, value: undefined });
    m.setTop(2400);
    fireEvent.scroll(el);

    fireEvent.click(scrollToTopButton()!);
    expect(m.getTop()).toBe(0);
  });

  it("re-evaluates when content grows without a scroll event", async () => {
    render(<Harness />);
    const el = scroller();
    makeScrollable(el, 600, 600); // fits at mount → no chrome
    fireEvent.scroll(el);
    expect(scrollToBottomButton()).toBeNull();

    // Streaming appends content and the element gets taller; the offset never
    // moved, so no scroll event fires. The subtree observer must catch it.
    makeScrollable(el, 5000, 600);
    el.appendChild(document.createElement("div"));
    await act(async () => {
      await Promise.resolve();
    });

    expect(scrollToBottomButton()).toBeInTheDocument();
  });
});
