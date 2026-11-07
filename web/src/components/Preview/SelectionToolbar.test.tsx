import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

function Harness() {
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => "Test label");
  return (
    <div ref={ref} data-testid="container">
      <p>Highlight me</p>
      {sel && <SelectionToolbar sel={sel} path="notes.md" label="Test label" onDone={clear} />}
    </div>
  );
}

let frames: FrameRequestCallback[] = [];
function flushFrames() {
  const pending = frames;
  frames = [];
  act(() => {
    for (const cb of pending) cb(0);
  });
}

let rect = { top: 200, left: 400, width: 40 };
let collapsed = false;

class FakeResizeObserver {
  static instances: FakeResizeObserver[] = [];
  callback: () => void;
  observed: Element[] = [];
  disconnected = false;
  constructor(callback: () => void) {
    this.callback = callback;
    FakeResizeObserver.instances.push(this);
  }
  observe(el: Element) {
    this.observed.push(el);
  }
  unobserve() {}
  disconnect() {
    this.disconnected = true;
  }
}

function toolbarPosition() {
  const toolbar = screen.getByRole("toolbar", { name: "Selection actions" });
  return { top: toolbar.style.top, left: toolbar.style.left };
}

function selectHighlight() {
  fireEvent.mouseUp(screen.getByTestId("container"));
  flushFrames();
}

beforeEach(() => {
  frames = [];
  rect = { top: 200, left: 400, width: 40 };
  collapsed = false;
  FakeResizeObserver.instances = [];
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    frames.push(cb);
    return frames.length;
  });
  vi.stubGlobal("cancelAnimationFrame", vi.fn());

  vi.spyOn(window, "getSelection").mockImplementation(() => {
    const el = screen.getByTestId("container");
    return {
      get isCollapsed() {
        return collapsed;
      },
      anchorNode: el.firstElementChild,
      toString: () => "Highlight me",
      getRangeAt: () => ({ getBoundingClientRect: () => rect }),
    } as unknown as Selection;
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("usePreviewSelection toolbar re-anchoring", () => {
  it("moves the toolbar to the new highlight rect on window resize", () => {
    render(<Harness />);
    selectHighlight();
    expect(toolbarPosition()).toEqual({ top: "156px", left: "340px" });

    rect = { top: 300, left: 500, width: 40 };
    fireEvent(window, new Event("resize"));
    fireEvent(window, new Event("resize"));
    expect(frames).toHaveLength(1);
    flushFrames();

    expect(toolbarPosition()).toEqual({ top: "256px", left: "440px" });
  });

  it("removes the toolbar on resize when the selection has collapsed", () => {
    render(<Harness />);
    selectHighlight();
    expect(screen.queryByRole("toolbar")).not.toBeNull();

    collapsed = true;
    fireEvent(window, new Event("resize"));
    flushFrames();

    expect(screen.queryByRole("toolbar")).toBeNull();
  });

  it("re-anchors on a container resize observed by ResizeObserver", () => {
    render(<Harness />);
    selectHighlight();
    const observer = FakeResizeObserver.instances[FakeResizeObserver.instances.length - 1];
    expect(observer?.observed).toEqual([screen.getByTestId("container")]);

    rect = { top: 300, left: 500, width: 40 };
    act(() => observer?.callback());
    flushFrames();

    expect(toolbarPosition()).toEqual({ top: "256px", left: "440px" });
  });

  it("re-anchors on scroll in any nested container", () => {
    render(<Harness />);
    selectHighlight();

    rect = { top: 120, left: 400, width: 40 };
    fireEvent.scroll(window);
    flushFrames();

    expect(toolbarPosition()).toEqual({ top: "76px", left: "340px" });
  });

  it("does not crash when ResizeObserver is unavailable", () => {
    vi.stubGlobal("ResizeObserver", undefined);
    render(<Harness />);
    selectHighlight();

    rect = { top: 300, left: 500, width: 40 };
    fireEvent(window, new Event("resize"));
    flushFrames();

    expect(toolbarPosition()).toEqual({ top: "256px", left: "440px" });
  });

  it("binds no listeners while no highlight is showing", () => {
    const addSpy = vi.spyOn(window, "addEventListener");
    render(<Harness />);
    expect(addSpy.mock.calls.map((c) => c[0])).not.toContain("resize");
    expect(FakeResizeObserver.instances).toHaveLength(0);
  });
});
