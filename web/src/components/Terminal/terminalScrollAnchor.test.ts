import { describe, expect, it, vi } from "vitest";
import { writePreservingScrollback } from "./terminalScrollAnchor";

type BufferType = "normal" | "alternate";

interface FakeState {
  viewportY: number;
  scrollback: number;
}

/**
 * A fake terminal that models the two library facts the helper compensates for:
 * `write` resets a scrolled-back viewport to 0 (ghostty-web 0.4.0's
 * `writeInternal`) and appends scrollback lines.
 */
function makeTerm(opts: {
  viewportY?: number;
  scrollback?: number;
  type?: BufferType;
  afterWrite?: (state: FakeState) => void;
}) {
  const state: FakeState = { viewportY: opts.viewportY ?? 0, scrollback: opts.scrollback ?? 0 };
  const term = {
    write: vi.fn(() => {
      if (state.viewportY !== 0) state.viewportY = 0;
      opts.afterWrite?.(state);
    }),
    getViewportY: vi.fn(() => state.viewportY),
    getScrollbackLength: vi.fn(() => state.scrollback),
    scrollToLine: vi.fn((line: number) => {
      state.viewportY = Math.max(0, Math.min(state.scrollback, line));
    }),
    buffer: { active: { type: opts.type ?? "normal" } },
  };
  return { term, state, write: term.write as unknown as ReturnType<typeof vi.fn> };
}

function write(term: ReturnType<typeof makeTerm>["term"], text = "chunk") {
  writePreservingScrollback(term as unknown as Parameters<typeof writePreservingScrollback>[0], text);
}

describe("writePreservingScrollback", () => {
  it("follows output while the viewport is at the bottom", () => {
    const { term, state, write: writeSpy } = makeTerm({
      viewportY: 0,
      scrollback: 30,
      afterWrite: (s) => {
        s.scrollback += 1;
      },
    });

    write(term, "streamed\r\n");

    expect(writeSpy).toHaveBeenCalledWith("streamed\r\n");
    expect(term.scrollToLine).not.toHaveBeenCalled();
    expect(state.viewportY).toBe(0);
  });

  it("keeps a scrolled-back reader on the same content when output grows scrollback", () => {
    const { term, state } = makeTerm({
      viewportY: 5,
      scrollback: 30,
      afterWrite: (s) => {
        s.scrollback += 2;
      },
    });

    write(term);

    // 5 lines above the bottom + 2 new scrollback lines = the same text on screen.
    expect(term.scrollToLine).toHaveBeenCalledWith(7);
    expect(state.viewportY).toBe(7);
  });

  it("restores the offset even when the write adds no scrollback (a spinner redrawing a line)", () => {
    const { term, state } = makeTerm({ viewportY: 5, scrollback: 30 });

    write(term, "\rdrawing");

    expect(term.scrollToLine).toHaveBeenCalledWith(5);
    expect(state.viewportY).toBe(5);
  });

  it("leaves the alternate screen alone — it has no scrollback", () => {
    const { term, state } = makeTerm({
      viewportY: 5,
      scrollback: 30,
      type: "alternate",
      afterWrite: (s) => {
        s.scrollback += 1;
      },
    });

    write(term);

    expect(term.scrollToLine).not.toHaveBeenCalled();
    // The library's own reset still applies; the helper does not fight it there.
    expect(state.viewportY).toBe(0);
  });

  it("clamps when the write drops scrollback (clear / RIS)", () => {
    const { term, state } = makeTerm({
      viewportY: 5,
      scrollback: 3,
    });

    write(term, "\x1bc");

    expect(term.scrollToLine).toHaveBeenCalledWith(5);
    expect(state.viewportY).toBe(3);
  });
});
