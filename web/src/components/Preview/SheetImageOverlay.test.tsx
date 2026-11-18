import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import SheetImageOverlay, { DEFAULT_IMAGE_PX } from "./SheetImageOverlay";
import type { ViewportMetrics } from "./univerMount";
import type { WorkbookImage } from "./univerData";

const VIEW_W = 1000;
const VIEW_H = 600;

function makeMetrics(overrides: Partial<ViewportMetrics> = {}): ViewportMetrics {
  return {
    scrollX: 0,
    scrollY: 0,
    zoom: 1,
    rowHeaderWidth: 46,
    columnHeaderHeight: 20,
    rowStart: (row) => row * 40,
    colStart: (col) => col * 100,
    rowHeight: () => 40,
    colWidth: () => 100,
    ...overrides,
  };
}

function makeImage(overrides: Partial<WorkbookImage> = {}): WorkbookImage {
  return {
    sheetId: "sheet-1",
    sheetName: "Sheet1",
    dataUrl: "data:image/png;base64,AAAA",
    fromCol: 0,
    fromRow: 0,
    toCol: null,
    toRow: null,
    extWidth: null,
    extHeight: null,
    ...overrides,
  };
}

const subscribe = () => () => {};

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => VIEW_W });
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => VIEW_H });
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => setTimeout(() => cb(0), 0));
  vi.stubGlobal("cancelAnimationFrame", (id: number) => clearTimeout(id));
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(HTMLElement.prototype, "clientWidth");
  Reflect.deleteProperty(HTMLElement.prototype, "clientHeight");
});

describe("SheetImageOverlay", () => {
  it("anchors a fractional cell position and scales it by zoom", async () => {
    const metrics = makeMetrics({ zoom: 1.5, scrollX: 100, scrollY: 20 });
    const { container } = render(
      <SheetImageOverlay
        images={[makeImage({ fromCol: 2.5, fromRow: 3.25 })]}
        viewport={() => metrics}
        subscribe={subscribe}
      />,
    );

    const img = await waitFor(() => {
      const found = container.querySelector("img");
      expect(found).not.toBeNull();
      return found as HTMLImageElement;
    });
    // x = 46 + (2*100 + 0.5*100 - 100) * 1.5 = 271
    // y = 20 + (3*40 + 0.25*40 - 20) * 1.5 = 185
    expect(img.getAttribute("src")).toBe("data:image/png;base64,AAAA");
    expect(img.getAttribute("alt")).toBe("");
    expect(img.style.left).toBe("271px");
    expect(img.style.top).toBe("185px");
    expect(img.style.width).toBe(`${DEFAULT_IMAGE_PX.w * 1.5}px`);
    expect(img.style.height).toBe(`${DEFAULT_IMAGE_PX.h * 1.5}px`);
    expect(img.parentElement?.getAttribute("aria-hidden")).toBe("true");
    expect(img.parentElement?.className).toContain("pointer-events-none");
  });

  it("culls an image whose box is entirely outside the viewport", async () => {
    const { container } = render(
      <SheetImageOverlay
        images={[makeImage({ fromCol: 50, fromRow: 50 })]}
        viewport={() => makeMetrics()}
        subscribe={subscribe}
      />,
    );
    await waitFor(() => expect(container.querySelector("img")).toBeNull());
  });

  it("renders every image that lands inside the viewport", async () => {
    const images = [
      makeImage({ fromCol: 0, fromRow: 0, dataUrl: "data:image/png;base64,ONE" }),
      makeImage({ fromCol: 2, fromRow: 1, dataUrl: "data:image/png;base64,TWO" }),
      makeImage({ fromCol: 4, fromRow: 2, dataUrl: "data:image/png;base64,THREE" }),
    ];
    const { container } = render(
      <SheetImageOverlay images={images} viewport={() => makeMetrics()} subscribe={subscribe} />,
    );

    await waitFor(() => expect(container.querySelectorAll("img")).toHaveLength(3));
    expect([...container.querySelectorAll("img")].map((img) => img.getAttribute("src"))).toEqual(
      images.map((image) => image.dataUrl),
    );
  });

  it("recomputes after a subscribed scroll event", async () => {
    let current = makeMetrics({ scrollX: 0 });
    const listeners: (() => void)[] = [];
    const { container } = render(
      <SheetImageOverlay
        images={[makeImage({ fromCol: 1, fromRow: 1 })]}
        viewport={() => current}
        subscribe={(cb) => {
          listeners.push(cb);
          return () => {};
        }}
      />,
    );
    await waitFor(() => expect(container.querySelector("img")?.style.left).toBe("146px"));

    current = makeMetrics({ scrollX: 50 });
    listeners.forEach((cb) => cb());
    await waitFor(() => expect(container.querySelector("img")?.style.left).toBe("96px"));
  });

  it("sizes a two-cell anchored image from its br extent", async () => {
    const metrics = makeMetrics({ zoom: 1.5 });
    const { container } = render(
      <SheetImageOverlay
        images={[makeImage({ fromCol: 1, fromRow: 1, toCol: 3.5, toRow: 4.25 })]}
        viewport={() => metrics}
        subscribe={subscribe}
      />,
    );
    const img = await waitFor(() => {
      const found = container.querySelector("img");
      expect(found).not.toBeNull();
      return found as HTMLImageElement;
    });
    // width = (colPos(3.5) - colPos(1)) * 1.5 = (350 - 100) * 1.5
    // height = (rowPos(4.25) - rowPos(1)) * 1.5 = (170 - 40) * 1.5
    expect(img.style.left).toBe("196px");
    expect(img.style.top).toBe("80px");
    expect(img.style.width).toBe("375px");
    expect(img.style.height).toBe("195px");
  });

  it("sizes a one-cell anchored image from its ext, scaled by zoom", async () => {
    const metrics = makeMetrics({ zoom: 2 });
    const { container } = render(
      <SheetImageOverlay
        images={[makeImage({ extWidth: 240, extHeight: 90 })]}
        viewport={() => metrics}
        subscribe={subscribe}
      />,
    );
    const img = await waitFor(() => {
      const found = container.querySelector("img");
      expect(found).not.toBeNull();
      return found as HTMLImageElement;
    });
    expect(img.style.width).toBe("480px");
    expect(img.style.height).toBe("180px");
  });

  it("falls back to the default box when the anchor has no extent", async () => {
    const { container } = render(
      <SheetImageOverlay images={[makeImage()]} viewport={() => makeMetrics()} subscribe={subscribe} />,
    );
    const img = await waitFor(() => {
      const found = container.querySelector("img");
      expect(found).not.toBeNull();
      return found as HTMLImageElement;
    });
    expect(img.style.width).toBe(`${DEFAULT_IMAGE_PX.w}px`);
    expect(img.style.height).toBe(`${DEFAULT_IMAGE_PX.h}px`);
  });

  it("does not shrink an image that extends past the layer edge", async () => {
    // Starts at x = 46 + 800 = 846 inside a 1000px layer; a 300px box overruns it.
    const { container } = render(
      <SheetImageOverlay
        images={[makeImage({ fromCol: 8, fromRow: 0, extWidth: 300, extHeight: 100 })]}
        viewport={() => makeMetrics()}
        subscribe={subscribe}
      />,
    );
    const img = await waitFor(() => {
      const found = container.querySelector("img");
      expect(found).not.toBeNull();
      return found as HTMLImageElement;
    });
    expect(img.style.left).toBe("846px");
    expect(img.style.width).toBe("300px");
    expect(img.style.height).toBe("100px");
  });

  it("renders the signature at its real row-67 anchor when scrolled into view", async () => {
    // Real anchor from ~/Downloads/09nov-leave.xlsx (image2.png, "Picture 1").
    const signature = makeImage({
      fromCol: 2.9999936836786256,
      fromRow: 66.26736666666666,
      toCol: 3.9999936836786256,
      toRow: 72.99999285714286,
    });
    // Simulate the grid scrolled so row 60 is the first visible row.
    const metrics = makeMetrics({ rowStart: (row) => row * 40, scrollY: 60 * 40 });
    const { container } = render(
      <SheetImageOverlay images={[signature]} viewport={() => metrics} subscribe={subscribe} />,
    );
    const img = await waitFor(() => {
      const found = container.querySelector("img");
      expect(found).not.toBeNull();
      return found as HTMLImageElement;
    });
    // y = 20 + (66.267*40 - 2400) * 1 = 20 + 250.7 = 270.7 px (visible).
    expect(parseFloat(img.style.top)).toBeGreaterThan(0);
    expect(parseFloat(img.style.top)).toBeLessThan(VIEW_H);
    // Sized from the two-cell anchor: width = (4-3)*100 = 100, height = (73-66.27)*40 = 269.3.
    expect(img.style.width).toBe("100px");
    expect(parseFloat(img.style.height)).toBeGreaterThan(250);
  });


  it("uses the EMU extent for size, not the two-cell anchor", async () => {
    const image = makeImage({
      fromCol: 0, fromRow: 0, toCol: 4, toRow: 10,
      extWidth: 160, extHeight: 90,
    });
    const { container } = render(
      <SheetImageOverlay images={[image]} viewport={() => makeMetrics()} subscribe={subscribe} />,
    );
    const img = await waitFor(() => container.querySelector("img") as HTMLImageElement | null);
    expect(img).not.toBeNull();
    expect(img!.style.width).toBe("160px");
    expect(img!.style.height).toBe("90px");
  });

});
