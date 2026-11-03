import { act, renderHook } from "@testing-library/react";
import type React from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useResizableSidebar } from "./useResizableSidebar";

const KEY = "test.sidebar_width.v1";

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.removeItem(KEY);
});

function renderPane(side?: "left" | "right") {
  return renderHook(() =>
    useResizableSidebar({ storageKey: KEY, defaultWidth: 300, minWidth: 100, maxWidth: 800, side }),
  );
}

// Drives the real drag path: pointerdown on the handle, then window pointermove.
function dragBy(result: { current: ReturnType<typeof useResizableSidebar> }, dx: number) {
  const startX = 500;
  const down = {
    clientX: startX,
    pointerId: 1,
    target: document.createElement("div"),
    preventDefault: () => {},
    stopPropagation: () => {},
  } as unknown as React.PointerEvent;
  act(() => result.current.onPointerDown(down));
  act(() => {
    window.dispatchEvent(new MouseEvent("pointermove", { clientX: startX + dx }));
  });
  act(() => {
    window.dispatchEvent(new MouseEvent("pointerup"));
  });
}

describe("useResizableSidebar drag direction", () => {
  it("left pane (default) grows when the handle moves right", () => {
    const { result } = renderPane();
    dragBy(result, 50);
    expect(result.current.width).toBe(350);
  });

  it("left pane shrinks when the handle moves left", () => {
    const { result } = renderPane("left");
    dragBy(result, -50);
    expect(result.current.width).toBe(250);
  });

  it("right pane grows when the handle moves left", () => {
    const { result } = renderPane("right");
    dragBy(result, -50);
    expect(result.current.width).toBe(350);
  });

  it("right pane shrinks when the handle moves right", () => {
    const { result } = renderPane("right");
    dragBy(result, 50);
    expect(result.current.width).toBe(250);
  });

  it("clamps a right pane dragged past its maximum", () => {
    const { result } = renderPane("right");
    dragBy(result, -1000);
    expect(result.current.width).toBe(800);
  });
});
