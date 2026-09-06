import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, cleanup, waitFor, act } from "@testing-library/react";
import { useRef, StrictMode } from "react";
import { useWrappedOverflow } from "./useWrappedOverflow";

// jsdom has no real flex layout, so mock the geometry getters to read from
// data-* attributes set by the harness.
const orig = {
  offsetTop: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetTop"),
  offsetWidth: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth"),
  clientWidth: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientWidth"),
};

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "offsetTop", {
    configurable: true,
    get(this: HTMLElement) {
      return this.dataset?.top ? Number(this.dataset.top) : 0;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get(this: HTMLElement) {
      return this.dataset?.width ? Number(this.dataset.width) : 0;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get(this: HTMLElement) {
      return this.dataset?.clientWidth ? Number(this.dataset.clientWidth) : 0;
    },
  });
});

afterEach(() => {
  if (orig.offsetTop) Object.defineProperty(HTMLElement.prototype, "offsetTop", orig.offsetTop);
  if (orig.offsetWidth) Object.defineProperty(HTMLElement.prototype, "offsetWidth", orig.offsetWidth);
  if (orig.clientWidth) Object.defineProperty(HTMLElement.prototype, "clientWidth", orig.clientWidth);
  cleanup();
  vi.restoreAllMocks();
});

interface HarnessProps {
  keys: string[];
  activeKey: string | null;
  dragId: string | null;
  clientW: number;
  tops: number[];
  widths: number[];
}

function Harness({ keys, activeKey, dragId, clientW, tops, widths }: HarnessProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const { probe, visibleKeys, registerPillRef } = useWrappedOverflow({
    orderedKeys: keys,
    activeKey,
    activeDragId: dragId,
    contentSignature: "sig",
    containerRef,
    maxRows: 2,
    chipReservedPx: 40,
  });
  return (
    <div
      ref={containerRef}
      data-client-width={clientW}
      data-probe={probe ? "1" : "0"}
      data-visible={visibleKeys.join(",")}
    >
      {probe
        ? keys.map((k, i) => (
            <div key={k} ref={registerPillRef(k)} data-top={tops[i]} data-width={widths[i]}></div>
          ))
        : visibleKeys.map((k) => <div key={k}></div>)}
      {probe && <div data-chip></div>}
    </div>
  );
}

const BASE = {
  keys: ["a", "b", "c", "d"],
  tops: [0, 0, 24, 48],
  widths: [110, 110, 110, 110],
  clientW: 300,
};

describe("useWrappedOverflow", () => {
  it("probes then renders the visible-only split (measure → partition wiring)", async () => {
    const { container } = render(<Harness {...BASE} activeKey={null} dragId={null} />);
    await waitFor(() => expect(container.querySelector("[data-probe]")!.getAttribute("data-probe")).toBe("0"));
    // d sits on row 2 (bucket >= maxRows) → hidden; a,b visible row0; c visible row1.
    expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe("a,b,c");
  });

  it("re-probes on an activeKey-only change (no geometry change) and promotes it", async () => {
    const { container, rerender } = render(<Harness {...BASE} activeKey={null} dragId={null} />);
    await waitFor(() => expect(container.querySelector("[data-probe]")!.getAttribute("data-probe")).toBe("0"));
    expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe("a,b,c");

    rerender(<Harness {...BASE} activeKey="d" dragId={null} />);
    await waitFor(() => expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe("a,b,c,d"));
  });

  it("freezes the split during a dnd drag (activeDragId != null)", async () => {
    const { container, rerender } = render(<Harness {...BASE} activeKey={null} dragId={null} />);
    await waitFor(() => expect(container.querySelector("[data-probe]")!.getAttribute("data-probe")).toBe("0"));
    const before = container.querySelector("[data-visible]")!.getAttribute("data-visible");

    // Start dragging: split must stay frozen (not re-probe).
    rerender(<Harness {...BASE} activeKey={null} dragId="chat:a" />);
    await waitFor(() => expect(container.querySelector("[data-probe]")!.getAttribute("data-probe")).toBe("0"));
    expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe(before);

    // Drag end: re-probe lands a fresh (identical) split.
    rerender(<Harness {...BASE} activeKey={null} dragId={null} />);
    await waitFor(() => expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe(before));
  });

  it("re-measures on container resize via the window.resize fallback", async () => {
    const ro = (globalThis as { ResizeObserver?: unknown }).ResizeObserver;
    (globalThis as { ResizeObserver?: unknown }).ResizeObserver = undefined;
    try {
      const { container } = render(<Harness {...BASE} activeKey={null} dragId={null} />);
      await waitFor(() => expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe("a,b,c"));
      // Narrow the container to 90px → row capacity shrinks → more hidden (c,d).
      const box = container.querySelector("[data-client-width]") as HTMLElement;
      box.setAttribute("data-client-width", "90");
      await act(async () => {
        window.dispatchEvent(new Event("resize"));
      });
      await waitFor(() =>
        expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe("a,b")
      );
    } finally {
      (globalThis as { ResizeObserver?: unknown }).ResizeObserver = ro;
    }
  });

  it("is idempotent under StrictMode double-invoke", async () => {
    const { container } = render(
      <StrictMode>
        <Harness {...BASE} activeKey={null} dragId={null} />
      </StrictMode>
    );
    await waitFor(() => expect(container.querySelector("[data-probe]")!.getAttribute("data-probe")).toBe("0"));
    expect(container.querySelector("[data-visible]")!.getAttribute("data-visible")).toBe("a,b,c");
  });
});
