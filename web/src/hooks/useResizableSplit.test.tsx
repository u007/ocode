import { act, render, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useResizableSplit } from "./useResizableSplit";

const SPLIT_KEY = "test.split_ratio.v1";

// jsdom lacks PointerEvent; provide a minimal one so pointer events carry
// clientY/clientX through React's synthetic-event system.
class PointerEventPolyfill extends Event {
  clientX = 0;
  clientY = 0;
  pointerId = 0;
  constructor(type: string, init: { clientX?: number; clientY?: number; pointerId?: number; bubbles?: boolean } = {}) {
    super(type, { bubbles: init.bubbles ?? true, cancelable: true });
    this.clientX = init.clientX ?? 0;
    this.clientY = init.clientY ?? 0;
    this.pointerId = init.pointerId ?? 0;
  }
}
Object.defineProperty(window, "PointerEvent", { value: PointerEventPolyfill, writable: true, configurable: true });
(globalThis as unknown as { PointerEvent?: typeof PointerEventPolyfill }).PointerEvent = PointerEventPolyfill;

beforeEach(() => {
  window.localStorage.clear();
  vi.useFakeTimers();
});

afterEach(() => {
  window.localStorage.removeItem(SPLIT_KEY);
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function renderVerticalSplit(props: {
  storageKey?: string;
  defaultRatio?: number;
  minRatio?: number;
  maxRatio?: number;
} = {}) {
  return renderHook(() =>
    useResizableSplit({
      storageKey: props.storageKey ?? SPLIT_KEY,
      orientation: "vertical",
      defaultRatio: props.defaultRatio ?? 0.6,
      minRatio: props.minRatio ?? 0.2,
      maxRatio: props.maxRatio ?? 0.85,
    }),
  );
}

describe("useResizableSplit vertical", () => {
  it("defaults to the provided defaultRatio when nothing is stored", () => {
    const { result } = renderVerticalSplit({ defaultRatio: 0.6 });
    expect(result.current.ratio).toBe(0.6);
    expect(result.current.orientation).toBe("vertical");
  });

  it("loads a stored ratio from localStorage", () => {
    window.localStorage.setItem(SPLIT_KEY, "0.75");
    const { result } = renderVerticalSplit();
    expect(result.current.ratio).toBe(0.75);
  });

  it("ignores a stored ratio below minRatio", () => {
    window.localStorage.setItem(SPLIT_KEY, "0.05");
    const { result } = renderVerticalSplit({ minRatio: 0.2 });
    expect(result.current.ratio).toBe(0.6); // default
  });

  it("ignores a stored ratio above maxRatio", () => {
    window.localStorage.setItem(SPLIT_KEY, "0.95");
    const { result } = renderVerticalSplit({ maxRatio: 0.85 });
    expect(result.current.ratio).toBe(0.6); // default
  });

  it("ignores a non-numeric stored value", () => {
    window.localStorage.setItem(SPLIT_KEY, "not-a-number");
    const { result } = renderVerticalSplit();
    expect(result.current.ratio).toBe(0.6); // default
  });

  it("persists the ratio to localStorage on change", () => {
    const { result } = renderVerticalSplit();
    act(() => {
      result.current.onKeyDown({
        key: "ArrowDown",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    expect(result.current.ratio).toBeGreaterThan(0.6);
    expect(window.localStorage.getItem(SPLIT_KEY)).toBe(String(result.current.ratio));
  });

  it("clamps keyboard increase at maxRatio", () => {
    window.localStorage.setItem(SPLIT_KEY, "0.84");
    const { result } = renderVerticalSplit({ maxRatio: 0.85 });
    act(() => {
      result.current.onKeyDown({
        key: "ArrowDown",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    expect(result.current.ratio).toBe(0.85);
  });

  it("clamps keyboard decrease at minRatio", () => {
    window.localStorage.setItem(SPLIT_KEY, "0.21");
    const { result } = renderVerticalSplit({ minRatio: 0.2 });
    act(() => {
      result.current.onKeyDown({
        key: "ArrowUp",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    expect(result.current.ratio).toBe(0.2);
  });

  it("ArrowUp decreases and ArrowDown increases the list ratio", () => {
    const { result } = renderVerticalSplit();
    const start = result.current.ratio;
    act(() => {
      result.current.onKeyDown({
        key: "ArrowUp",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    expect(result.current.ratio).toBe(start - 0.02);
    act(() => {
      result.current.onKeyDown({
        key: "ArrowDown",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    act(() => {
      result.current.onKeyDown({
        key: "ArrowDown",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    expect(result.current.ratio).toBe(start + 0.02);
  });

  it("ignores non-arrow keys", () => {
    const { result } = renderVerticalSplit();
    const start = result.current.ratio;
    act(() => {
      result.current.onKeyDown({
        key: "Enter",
        preventDefault: vi.fn(),
      } as unknown as React.KeyboardEvent);
    });
    expect(result.current.ratio).toBe(start);
  });

  it("resetToDefault restores the default ratio", () => {
    window.localStorage.setItem(SPLIT_KEY, "0.9");
    const { result } = renderVerticalSplit({ defaultRatio: 0.6 });
    expect(result.current.ratio).toBe(0.6);
    act(() => {
      result.current.resetToDefault();
    });
    expect(result.current.ratio).toBe(0.6);
  });

  // Mock getBoundingClientRect for all elements so drag tests have a definite
  // container size (jsdom returns zero rects).
  const mockRect = (height: number) =>
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
      width: 0,
      height,
      top: 0,
      left: 0,
      right: 0,
      bottom: height,
      x: 0,
      y: 0,
      toJSON: () => {},
    } as DOMRect);

  it("drag updates ratio based on pointer movement within the container", () => {
    mockRect(400);
    let latestRatio = 0.6;
    function Harness() {
      const split = useResizableSplit({
        storageKey: SPLIT_KEY,
        orientation: "vertical",
        defaultRatio: 0.6,
        minRatio: 0.2,
        maxRatio: 0.85,
      });
      latestRatio = split.ratio;
      return (
        <div
          ref={split.containerRef}
          style={{ height: 400 }}
          onPointerDown={split.onPointerDown}
          onKeyDown={split.onKeyDown}
        >
          <div data-testid="divider" />
        </div>
      );
    }
    const { container } = render(<Harness />);
    const divider = container.querySelector("[data-testid='divider']")!;

    // Press at the top (clientY=0) then drag down 100px: divider moves down,
    // so the top (list) pane grows: ratio rises 0.6 -> 0.6 + 100/400 = 0.85.
    act(() => {
      divider.dispatchEvent(new PointerEvent("pointerdown", { clientY: 50, bubbles: true, pointerId: 1 }));
    });
    expect(latestRatio).toBe(0.6);
    act(() => {
      // Drag down 100px: divider moves down, top pane grows 0.6 -> 0.85.
      window.dispatchEvent(new PointerEvent("pointermove", { clientY: 150, bubbles: true, pointerId: 1 }));
    });
    expect(latestRatio).toBeCloseTo(0.85, 2);

    act(() => {
      window.dispatchEvent(new PointerEvent("pointerup", { pointerId: 1 }));
    });
    expect(window.localStorage.getItem(SPLIT_KEY)).toBe(String(latestRatio));
  });

  it("drag clamps at maxRatio", () => {
    mockRect(400);
    let latestRatio = 0.6;
    function Harness() {
      const split = useResizableSplit({
        storageKey: SPLIT_KEY,
        orientation: "vertical",
        defaultRatio: 0.6,
        minRatio: 0.2,
        maxRatio: 0.85,
      });
      latestRatio = split.ratio;
      return (
        <div
          ref={split.containerRef}
          style={{ height: 400 }}
          onPointerDown={split.onPointerDown}
        >
          <div data-testid="divider" />
        </div>
      );
    }
    const { container } = render(<Harness />);
    const divider = container.querySelector("[data-testid='divider']")!;

    act(() => {
      divider.dispatchEvent(new PointerEvent("pointerdown", { clientY: 50, bubbles: true, pointerId: 1 }));
    });
    // Drag down 300px: 0.6 + 300/400 = 1.35, clamped to maxRatio 0.85.
    act(() => {
      window.dispatchEvent(new PointerEvent("pointermove", { clientY: 350, bubbles: true, pointerId: 1 }));
    });
    expect(latestRatio).toBe(0.85);
  });

  it("drag clamps at minRatio", () => {
    mockRect(400);
    let latestRatio = 0.6;
    function Harness() {
      const split = useResizableSplit({
        storageKey: SPLIT_KEY,
        orientation: "vertical",
        defaultRatio: 0.6,
        minRatio: 0.2,
        maxRatio: 0.85,
      });
      latestRatio = split.ratio;
      return (
        <div
          ref={split.containerRef}
          style={{ height: 400 }}
          onPointerDown={split.onPointerDown}
        >
          <div data-testid="divider" />
        </div>
      );
    }
    const { container } = render(<Harness />);
    const divider = container.querySelector("[data-testid='divider']")!;

    act(() => {
      divider.dispatchEvent(new PointerEvent("pointerdown", { clientY: 250, bubbles: true, pointerId: 1 }));
    });
    // Drag up 300px (to clientY=-50): 0.6 - 300/400 = -0.15, clamped to minRatio 0.2.
    act(() => {
      window.dispatchEvent(new PointerEvent("pointermove", { clientY: -50, bubbles: true, pointerId: 1 }));
    });
    expect(latestRatio).toBe(0.2);
  });
});
