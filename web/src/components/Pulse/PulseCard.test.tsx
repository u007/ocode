import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { PulseCard } from "./PulseCard";
import { PULSE_TAIL_ENTRIES, type PulseTail, type PulseTailEntry } from "./usePulseTail";
import type { PulseRow, PulseStatus } from "../../api/types";

// Typed from the hook's own exported shape, not a hand-written literal: a
// hand-written copy silently omits new fields, and a mock that omits one reads
// as `undefined` in the component instead of failing here.
const mockTail = vi.fn<
  (sessionId: string, enabled: boolean, status: PulseStatus) => PulseTail
>(() => ({ entries: [], error: null, loading: false }));
vi.mock("./usePulseTail", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./usePulseTail")>();
  return {
    ...actual,
    usePulseTail: (...a: Parameters<typeof actual.usePulseTail>) => mockTail(...a),
  };
});

const mockFocus = vi.fn();
vi.mock("../../stores/pulseStore", () => ({
  setPulseFocus: (...a: unknown[]) => mockFocus(...a),
}));

const mockJump = vi.fn();
const mockJumpAsk = vi.fn();
vi.mock("../../lib/jumpToSession", () => ({
  useJumpToSession: () => mockJump,
  useJumpToPendingAsk: () => mockJumpAsk,
}));

/** Hover expansion is deliberately delayed; 150ms of fake time drives it. */
const EXPAND_DELAY_MS = 150;
/** Fixed clock so elapsed-time assertions are deterministic. */
const NOW = new Date("2026-09-28T12:00:00.000Z").getTime();

function makeRow(overrides: Partial<PulseRow> = {}): PulseRow {
  return {
    session_id: "ses_1",
    project_path: "/Users/james/www/ocode",
    title: "Fix the pulse dashboard",
    status: "running",
    current_task: { kind: "text", text: "writing the card" },
    todo: null,
    pending_ask: null,
    turn_started_at: new Date(NOW - 45_000).toISOString(),
    updated_at: new Date(NOW - 45_000).toISOString(),
    child_count: 0,
    ...overrides,
  };
}

const texts = (...lines: string[]): PulseTailEntry[] => lines.map((text) => ({ kind: "text", text }));

function cardButton(): HTMLElement {
  return screen.getByRole("button", { name: /Fix the pulse dashboard/ });
}

function overlay(): HTMLElement | null {
  return screen.queryByTestId("pulse-overlay");
}

/** Advance past the hover delay so an overlay is showing. */
async function expand() {
  fireEvent.pointerEnter(cardButton());
  await act(async () => {
    vi.advanceTimersByTime(EXPAND_DELAY_MS);
  });
}

beforeEach(() => {
  mockJump.mockReset();
  mockJumpAsk.mockReset();
  mockFocus.mockReset();
  mockTail.mockReset();
  mockTail.mockReturnValue({ entries: [], error: null, loading: false });
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("PulseCard structure", () => {
  it("is a listitem wrapping the header button plus a separate focus button", () => {
    const { container } = render(<PulseCard row={makeRow()} compact={false} />);

    const item = screen.getByRole("listitem");
    expect(item).toBe(container.firstChild);
    // The header is the click target; the focus control is a SIBLING of it, as
    // a button may not contain a button.
    const buttons = within(item).getAllByRole("button");
    expect(buttons).toHaveLength(3);
    expect(buttons.filter((b) => b.hasAttribute("aria-expanded"))).toEqual([cardButton()]);
    expect(cardButton().contains(screen.getByRole("button", { name: "Focus session" }))).toBe(false);
  });

  it("shows the project basename with the full path in the title attribute", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    const project = screen.getByText("ocode");
    expect(project).toHaveAttribute("title", "/Users/james/www/ocode");
  });

  it("falls back to the session id when the row has no title", () => {
    render(<PulseCard row={makeRow({ title: "" })} compact={false} />);

    expect(screen.getByText("ses_1")).toBeInTheDocument();
  });

  it("labels the elapsed time for running rows and ticks it live", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    const elapsed = screen.getByTestId("pulse-elapsed");
    expect(elapsed).toHaveTextContent("45s");

    act(() => {
      vi.advanceTimersByTime(5_000);
    });

    expect(screen.getByTestId("pulse-elapsed")).toHaveTextContent("50s");
  });

  it("falls back to recency when a running row has a zero turn_started_at", () => {
    render(
      <PulseCard
        row={makeRow({
          turn_started_at: "0001-01-01T00:00:00Z",
          updated_at: new Date(NOW - 120_000).toISOString(),
        })}
        compact={false}
      />,
    );

    expect(screen.getByTestId("pulse-elapsed")).toHaveTextContent("2m ago");
  });

  it("shows relative recency for a non-running row instead of a duration", () => {
    render(
      <PulseCard
        row={makeRow({
          status: "idle",
          turn_started_at: new Date(NOW - 3 * 3600_000).toISOString(),
          updated_at: new Date(NOW - 3 * 3600_000).toISOString(),
        })}
        compact={false}
      />,
    );

    expect(screen.getByTestId("pulse-elapsed")).toHaveTextContent("3h ago");
  });

  it("shows a child-count chip only when the row has children", () => {
    const { unmount } = render(<PulseCard row={makeRow()} compact={false} />);
    expect(screen.queryByTestId("pulse-children")).not.toBeInTheDocument();
    unmount();

    render(<PulseCard row={makeRow({ child_count: 3 })} compact={false} />);
    expect(screen.getByTestId("pulse-children")).toHaveTextContent("3");
  });
});

describe("PulseCard status glyphs", () => {
  it.each([
    ["needs_permission", "needs permission", "◆"],
    ["needs_question", "needs question", "◆"],
    ["running", "running", "●"],
    ["error", "error", "✕"],
    ["idle", "idle", "○"],
  ] as const)("renders %s as %s", (status, label, glyph) => {
    render(
      <PulseCard
        row={makeRow({
          status,
          pending_ask: status === "needs_permission" ? { kind: "permission", summary: "ask" } : null,
        })}
        compact={false}
      />,
    );

    const el = screen.getByLabelText(label);
    expect(el).toHaveTextContent(glyph);
  });

  it("pulses the running glyph but disables the animation under reduced motion", () => {
    render(<PulseCard row={makeRow({ status: "running" })} compact={false} />);

    const glyph = screen.getByLabelText("running");
    expect(glyph.className).toContain("animate-pulse");
    expect(glyph.className).toContain("motion-reduce:animate-none");
  });

  it("does not pulse a non-running glyph", () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);

    expect(screen.getByLabelText("idle").className).not.toContain("animate-pulse");
  });
});

describe("PulseCard task line", () => {
  it("prefers the pending ask summary for a needs-you row", () => {
    render(
      <PulseCard
        row={makeRow({
          status: "needs_permission",
          current_task: { kind: "text", text: "the current task" },
          pending_ask: { kind: "permission", summary: "run rm -rf build" },
        })}
        compact={false}
      />,
    );

    expect(screen.getByText("run rm -rf build")).toBeInTheDocument();
    expect(screen.queryByText("the current task")).not.toBeInTheDocument();
  });

  it("uses the current task when there is no pending ask", () => {
    render(
      <PulseCard
        row={makeRow({ current_task: { kind: "tool", text: "$ go test ./..." } })}
        compact={false}
      />,
    );

    expect(screen.getByText("$ go test ./...")).toBeInTheDocument();
  });

  it("omits the task line entirely when the row has neither", () => {
    render(<PulseCard row={makeRow({ current_task: null })} compact={false} />);

    expect(screen.queryByTestId("pulse-task")).not.toBeInTheDocument();
  });
});

describe("PulseCard todo progress", () => {
  it("renders a progress bar for the row's plan", () => {
    render(
      <PulseCard
        row={makeRow({
          todo: {
            done: 2,
            total: 4,
            current: "wire the overlay",
            items: [
              { text: "one", state: "done" },
              { text: "two", state: "in_progress" },
            ],
          },
        })}
        compact={false}
      />,
    );

    const bar = screen.getByRole("progressbar");
    expect(bar).toHaveAttribute("aria-valuenow", "50");
    // Radix's indicator carries the fill width; the aria value is the
    // accessible contract, and the visible bar must follow it.
    expect(bar.firstElementChild).toHaveStyle({ transform: "translateX(-50%)" });
    expect(screen.getByTestId("pulse-todo-count")).toHaveTextContent("2/4");
  });

  it("omits the progress bar when the row has no plan", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });
});

describe("PulseCard compact mode", () => {
  it("collapses to a single line without the task, progress, or child chip", () => {
    const { container } = render(
      <PulseCard
        row={makeRow({
          child_count: 2,
          todo: { done: 1, total: 2, current: "x", items: [{ text: "x", state: "in_progress" }] },
        })}
        compact
      />,
    );

    expect(screen.queryByTestId("pulse-task")).not.toBeInTheDocument();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByTestId("pulse-children")).not.toBeInTheDocument();
    // One line: project + title + elapsed all sit inside a single row.
    expect(container.querySelectorAll("[data-pulse-line]")).toHaveLength(1);
  });
});

describe("PulseCard streams a live row on the card itself", () => {
  // The dashboard's job is watching turns. Requiring a hover to see what a
  // running session is saying inverted that — and made the preview unreachable
  // on touch, which has no hover at all. These pin the new contract, including
  // that the stream is rendered ONCE (never in both the card and the overlay).

  const lines = ["streaming line a", "streaming line b"];

  beforeEach(() => {
    mockTail.mockReturnValue({ entries: texts(...lines), error: null, loading: false });
  });

  it("shows the running turn's text with no hover at all", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    const stream = screen.getByTestId("pulse-tail");
    expect(within(stream).getByText("streaming line a")).toBeInTheDocument();
    expect(overlay()).not.toBeInTheDocument();
  });

  it("keeps the tail hook enabled while collapsed, which is what makes it visible", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    expect(mockTail).toHaveBeenLastCalledWith("ses_1", true, "running");
  });

  it.each(["needs_permission", "needs_question"] as const)(
    "streams a %s row too, since a paused turn is still live output",
    (status) => {
      render(
        <PulseCard
          row={makeRow({ status, pending_ask: { kind: "permission", summary: "run rm -rf build" } })}
          compact={false}
        />,
      );

      expect(screen.getByTestId("pulse-tail")).toBeInTheDocument();
      expect(mockTail).toHaveBeenLastCalledWith("ses_1", true, status);
    },
  );

  it("does not repeat the stream in the hover overlay", async () => {
    render(
      <PulseCard
        row={makeRow({
          todo: { done: 1, total: 2, current: "x", items: [{ text: "x", state: "in_progress" }] },
        })}
        compact={false}
      />,
    );

    await expand();

    // Exactly one, not two: the card keeps the stream and the overlay keeps the
    // plan, which is the content the card body cannot show.
    expect(screen.getAllByTestId("pulse-tail")).toHaveLength(1);
    expect(within(overlay()!).getByTestId("pulse-todo-items")).toBeInTheDocument();
  });

  it("surfaces a fetch failure on the card rather than swallowing it", () => {
    mockTail.mockReturnValue({ entries: [], error: "session state failed: boom", loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    expect(screen.getByText(/session state failed: boom/)).toBeInTheDocument();
  });

  it("reserves the stream height so the grid does not jump as text arrives", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    // The floor sits on the card chrome div now, not on the header button.
    const chrome = cardButton().parentElement!;
    expect(chrome.className).toContain("min-h-[32rem]");
    expect(cardButton().className).not.toContain("min-h-[32rem]");
    // The FLOOR above is not the budget that keeps the height constant: min-h
    // only sets a minimum, and a flex-1 child of an auto-height column is sized
    // from its own content, so `flex-basis: 0%` caps nothing. The region's
    // ceiling is what pins the height; measured in headless Chromium against the
    // built CSS, an uncapped long wrapped stream grew the card to 2236px.
    const region = screen.getByTestId("pulse-stream-region");
    expect(region.className).toContain("max-h-[24rem]");
    expect(region.className).toContain("flex-1");
    expect(region.className).toContain("min-h-0");
    // The region SCROLLS instead of clipping, and no longer bottom-pins with
    // justify-end: scrolling (stick-to-bottom) replaces the old trick of letting
    // an un-shrinkable flex item overflow out of the top of an overflow-hidden
    // block. That is why the old assertions that a wrapping ENTRY carries no
    // min-h-0/shrink-0 are gone: the entry's shrink behaviour no longer decides
    // which text is visible.
    expect(region.className).toContain("overflow-y-auto");
    expect(region.className).not.toContain("overflow-hidden");
    expect(region.className).not.toContain("justify-end");
    // A wrapping entry must still never truncate, or a prose line is one clipped row.
    const entry = within(region).getByText("streaming line a").className;
    expect(entry).not.toContain("truncate");
  });

  it("wraps a streamed line that carries no newlines, so the card is multi-line", () => {
    // Model prose is one long line with no \n. Truncating per line is what left
    // a running card showing a single clipped line in seven lines of space.
    const prose = "Inspecting the permission matrix and the sandbox carve-outs in detail. ";
    mockTail.mockReturnValue({
      entries: texts(prose.repeat(4).trimEnd()),
      error: null,
      loading: false,
    });

    render(<PulseCard row={makeRow()} compact={false} />);

    const stream = screen.getByTestId("pulse-tail");
    expect(stream.className).toContain("whitespace-pre-wrap");
    expect(stream.className).toContain("break-words");
    // ONE DOM node: the extra lines are soft-wrapped by the layout, never
    // synthesised in JS, so there is nothing to re-render per delta beyond the
    // text itself and no reflow of the reserved box.
    const line = within(stream).getByText(prose.repeat(4).trimEnd());
    expect(line.className).not.toContain("truncate");
    expect(stream.querySelectorAll("div")).toHaveLength(1);
  });

  it("keeps the hover overlay one line per entry, since it has no height budget", async () => {
    const prose = "A settled turn's last assistant message, which may be one long line. ";
    mockTail.mockReturnValue({
      entries: texts(prose.repeat(3).trimEnd()),
      error: null,
      loading: false,
    });
    // Idle, so the preview genuinely lives in the overlay. Wrapping there would
    // turn a 3-entry preview into a full-page panel.
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);

    await expand();

    const line = within(overlay()!).getByText(prose.repeat(3).trimEnd());
    expect(line.className).toContain("truncate");
  });

  it("leaves an idle row hover-gated: nothing renders and nothing is fetched until hover", async () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);

    expect(screen.queryByTestId("pulse-tail")).not.toBeInTheDocument();
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", false, "idle");

    await expand();

    expect(screen.getByTestId("pulse-tail")).toBeInTheDocument();
  });

  it("never streams a compact card, even a running one", () => {
    render(<PulseCard row={makeRow()} compact />);

    expect(screen.queryByTestId("pulse-tail")).not.toBeInTheDocument();
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", false, "running");
    // No reserved height either: the one-line Recent row keeps its own box.
    expect(cardButton().className).not.toContain("min-h-[32rem]");
    expect(cardButton().parentElement!.className).not.toContain("min-h-[32rem]");
  });
});

describe("PulseCard activity feed and scrollable stream region", () => {
  it("renders a tool entry in the on-card stream with the tool styling", () => {
    mockTail.mockReturnValue({
      entries: [
        { kind: "text", text: "let me look" },
        { kind: "tool", text: "▸ bash ls -la ✓" },
      ],
      error: null,
      loading: false,
    });
    render(<PulseCard row={makeRow()} compact={false} />);

    const stream = screen.getByTestId("pulse-tail");
    const tool = within(stream).getByText("▸ bash ls -la ✓");
    expect(tool.className).toContain("font-mono");
    expect(tool.className).toContain("text-[11px]");
    expect(tool.className).toContain("text-muted-foreground");
    expect(tool.className).not.toContain("truncate");
    expect(within(stream).getByText("let me look")).toBeInTheDocument();
  });

  it("keeps a tool entry on one truncated line in the overlay", async () => {
    mockTail.mockReturnValue({
      entries: [{ kind: "tool", text: "▸ bash ls" }],
      error: null,
      loading: false,
    });
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);

    await expand();

    expect(within(overlay()!).getByText("▸ bash ls").className).toContain("truncate");
  });

  it("puts the stream region outside the header button", () => {
    mockTail.mockReturnValue({ entries: texts("streaming"), error: null, loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    const region = screen.getByTestId("pulse-stream-region");
    expect(within(cardButton()).queryByTestId("pulse-stream-region")).toBeNull();
    expect(cardButton().contains(region)).toBe(false);
    // Still one listitem, and the region lives inside it.
    expect(screen.getByRole("listitem").contains(region)).toBe(true);
  });

  it("has no stream region on a settled or compact card", () => {
    const { unmount } = render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);
    expect(screen.queryByTestId("pulse-stream-region")).not.toBeInTheDocument();
    unmount();

    render(<PulseCard row={makeRow()} compact />);
    expect(screen.queryByTestId("pulse-stream-region")).not.toBeInTheDocument();
  });

  it("still jumps from the header button and double-click opens the ask", () => {
    mockTail.mockReturnValue({ entries: texts("streaming"), error: null, loading: false });
    render(
      <PulseCard
        row={makeRow({ status: "needs_permission", pending_ask: { kind: "permission", summary: "ask" } })}
        compact={false}
      />,
    );

    fireEvent.click(cardButton());
    expect(mockJump).toHaveBeenCalledTimes(1);
    fireEvent.doubleClick(cardButton());
    expect(mockJumpAsk).toHaveBeenCalledTimes(1);
  });

  it("does not jump when the stream region is clicked, so text there can be selected", () => {
    mockTail.mockReturnValue({ entries: texts("streaming"), error: null, loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.click(screen.getByTestId("pulse-stream-region"));

    expect(mockJump).not.toHaveBeenCalled();
  });

  it("counts hovering the stream region as hovering the card", async () => {
    mockTail.mockReturnValue({ entries: texts("streaming"), error: null, loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.pointerEnter(screen.getByTestId("pulse-stream-region"));
    await act(async () => {
      vi.advanceTimersByTime(EXPAND_DELAY_MS);
    });
    expect(overlay()).toBeInTheDocument();

    fireEvent.pointerLeave(screen.getByTestId("pulse-stream-region"));
    expect(overlay()).not.toBeInTheDocument();
  });

  it("stops a wheel over the stream from reaching ancestors", () => {
    mockTail.mockReturnValue({ entries: texts("streaming"), error: null, loading: false });
    const onWheel = vi.fn();
    render(
      <div onWheel={onWheel}>
        <PulseCard row={makeRow()} compact={false} />
      </div>,
    );

    fireEvent.wheel(screen.getByTestId("pulse-stream-region"));

    expect(onWheel).not.toHaveBeenCalled();
  });

  describe("stick to bottom", () => {
    function layout(el: HTMLElement, m: { scrollHeight: number; clientHeight: number; scrollTop?: number }) {
      Object.defineProperty(el, "scrollHeight", { configurable: true, value: m.scrollHeight });
      Object.defineProperty(el, "clientHeight", { configurable: true, value: m.clientHeight });
      el.scrollTop = m.scrollTop ?? 0;
    }
    const row = makeRow();

    it("follows new entries while the user is at the bottom", () => {
      mockTail.mockReturnValue({ entries: texts("one"), error: null, loading: false });
      const { rerender } = render(<PulseCard row={row} compact={false} />);
      const region = screen.getByTestId("pulse-stream-region");
      layout(region, { scrollHeight: 500, clientHeight: 128 });

      mockTail.mockReturnValue({ entries: texts("one", "two"), error: null, loading: false });
      rerender(<PulseCard row={row} compact={false} />);

      expect(region.scrollTop).toBe(500);
    });

    it("leaves the scroll position alone once the user scrolled up", () => {
      mockTail.mockReturnValue({ entries: texts("one"), error: null, loading: false });
      const { rerender } = render(<PulseCard row={row} compact={false} />);
      const region = screen.getByTestId("pulse-stream-region");
      layout(region, { scrollHeight: 500, clientHeight: 128, scrollTop: 100 });
      fireEvent.scroll(region);

      mockTail.mockReturnValue({ entries: texts("one", "two"), error: null, loading: false });
      rerender(<PulseCard row={row} compact={false} />);

      expect(region.scrollTop).toBe(100);
    });

    it("resumes following when the user scrolls back to the bottom", () => {
      mockTail.mockReturnValue({ entries: texts("one"), error: null, loading: false });
      const { rerender } = render(<PulseCard row={row} compact={false} />);
      const region = screen.getByTestId("pulse-stream-region");
      layout(region, { scrollHeight: 500, clientHeight: 128, scrollTop: 100 });
      fireEvent.scroll(region);
      // 500 - 366 - 128 = 6 px from the bottom: inside the 8px tolerance.
      region.scrollTop = 366;
      fireEvent.scroll(region);

      mockTail.mockReturnValue({ entries: texts("one", "two"), error: null, loading: false });
      rerender(<PulseCard row={row} compact={false} />);

      expect(region.scrollTop).toBe(500);
    });
  });
});

describe("PulseCard overlay", () => {
  it("does not expand on hover before the 150ms delay elapses", async () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.pointerEnter(cardButton());
    await act(async () => {
      vi.advanceTimersByTime(EXPAND_DELAY_MS - 1);
    });

    expect(overlay()).not.toBeInTheDocument();
    expect(cardButton()).toHaveAttribute("aria-expanded", "false");
  });

  it("expands after the hover delay and enables the tail hook", async () => {
    // An IDLE row, deliberately: a running row is always subscribed because it
    // streams on the card face (see the streaming describe below), so hover is
    // only still the gate here. This keeps pinning that `expanded` reaches the
    // hook, which is what it did before live cards became always-on.
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);
    // The hook runs on every render; `expanded` is what tells it whether to
    // subscribe. Collapsed, it is passed false.
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", false, "idle");

    await expand();

    expect(overlay()).toBeInTheDocument();
    expect(cardButton()).toHaveAttribute("aria-expanded", "true");
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", true, "idle");
  });

  it("collapses on mouse leave", async () => {
    render(<PulseCard row={makeRow()} compact={false} />);
    await expand();
    expect(overlay()).toBeInTheDocument();

    fireEvent.pointerLeave(cardButton());

    expect(overlay()).not.toBeInTheDocument();
  });

  it("pins the panel open from the toggle, so mouse leave does not close it", async () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);
    const toggle = screen.getByRole("button", { name: "Keep details open" });
    expect(toggle).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(overlay()).toBeInTheDocument();
    expect(cardButton()).toHaveAttribute("aria-expanded", "true");
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", true, "idle");

    fireEvent.pointerLeave(cardButton());

    expect(overlay()).toBeInTheDocument();
    expect(cardButton()).toHaveAttribute("aria-expanded", "true");
  });

  it("unpins on a second toggle click and closes once the pointer has left", () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);
    const toggle = screen.getByRole("button", { name: "Keep details open" });
    fireEvent.click(toggle);
    fireEvent.pointerLeave(cardButton());
    expect(overlay()).toBeInTheDocument();

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-pressed", "false");
    expect(overlay()).not.toBeInTheDocument();
    expect(cardButton()).toHaveAttribute("aria-expanded", "false");
  });

  it("clears the pin on Escape", () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Keep details open" }));
    expect(overlay()).toBeInTheDocument();

    fireEvent.keyDown(cardButton(), { key: "Escape" });

    expect(overlay()).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Keep details open" })).toHaveAttribute("aria-pressed", "false");
  });

  it("expands immediately on keyboard focus, with no delay", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.focus(cardButton());

    // No timer advance and no act(): focus must not be gated on the hover
    // delay. If the delay were applied here, this assertion would fail.
    expect(overlay()).toBeInTheDocument();
  });

  it("collapses on Escape", async () => {
    render(<PulseCard row={makeRow()} compact={false} />);
    await expand();

    fireEvent.keyDown(cardButton(), { key: "Escape" });

    expect(overlay()).not.toBeInTheDocument();
  });

  it("collapses on blur", async () => {
    render(<PulseCard row={makeRow()} compact={false} />);
    await expand();

    fireEvent.blur(cardButton());

    expect(overlay()).not.toBeInTheDocument();
  });

  it("is absolutely positioned above the card so the card keeps its own box", async () => {
    const { container } = render(<PulseCard row={makeRow()} compact={false} />);
    await expand();

    const overlayEl = overlay()!;
    expect(overlayEl.className).toContain("absolute");
    // bottom-full: the overlay grows upward instead of downward off the list.
    expect(overlayEl.className).toContain("bottom-full");
    expect(container.querySelector("[role='listitem']")?.className).toContain("relative");
  });

  it("lists the full plan with a state mark per item", async () => {
    render(
      <PulseCard
        row={makeRow({
          todo: {
            done: 1,
            total: 3,
            current: "two",
            items: [
              { text: "first", state: "done" },
              { text: "second", state: "in_progress" },
              { text: "third", state: "pending" },
            ],
          },
        })}
        compact={false}
      />,
    );
    await expand();

    const list = screen.getByTestId("pulse-todo-items");
    expect(within(list).getByText("☑")).toBeInTheDocument();
    expect(within(list).getByText("▸")).toBeInTheDocument();
    expect(within(list).getByText("☐")).toBeInTheDocument();
    expect(within(list).getByText("first")).toBeInTheDocument();
    expect(within(list).getByText("second")).toBeInTheDocument();
    expect(within(list).getByText("third")).toBeInTheDocument();
  });

  it("shows the running tool text and the pending ask text in the overlay", async () => {
    render(
      <PulseCard
        row={makeRow({
          current_task: { kind: "tool", text: "$ go build ./..." },
          pending_ask: { kind: "question", summary: "which target?" },
        })}
        compact={false}
      />,
    );
    await expand();

    const overlayEl = overlay()!;
    expect(within(overlayEl).getByText("$ go build ./...")).toBeInTheDocument();
    expect(within(overlayEl).getByText("which target?")).toBeInTheDocument();
  });

  it("renders the tail entries the hook returned", async () => {
    const tailLines = Array.from({ length: PULSE_TAIL_ENTRIES }, (_, i) => `line ${i}`);
    mockTail.mockReturnValue({ entries: texts(...tailLines), error: null, loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    await expand();

    const tail = screen.getByTestId("pulse-tail");
    for (const line of tailLines) expect(within(tail).getByText(line)).toBeInTheDocument();
  });

  it("surfaces a tail fetch failure in the overlay instead of showing nothing", async () => {
    mockTail.mockReturnValue({ entries: [], error: "session state failed: boom", loading: false });
    // Idle, so the preview genuinely lives in the overlay. A live row puts it
    // on the card instead — pinned in the streaming describe.
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);

    await expand();

    expect(within(overlay()!).getByText(/session state failed: boom/)).toBeInTheDocument();
  });

  it("stops requesting the tail once collapsed", async () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);
    await expand();
    mockTail.mockClear();

    fireEvent.pointerLeave(cardButton());

    expect(mockTail).toHaveBeenLastCalledWith("ses_1", false, "idle");
  });
});

describe("PulseCard jumps", () => {
  it("jumps to the session immediately on a single click, with no double-click wait", () => {
    const row = makeRow();
    render(<PulseCard row={row} compact={false} />);

    fireEvent.click(cardButton());

    expect(mockJump).toHaveBeenCalledTimes(1);
    expect(mockJump).toHaveBeenCalledWith({
      projectPath: "/Users/james/www/ocode",
      host: "",
      sessionId: "ses_1",
      title: "Fix the pulse dashboard",
    });
  });

  it("focuses the session on Enter instead of jumping", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    // fireEvent returns false when the handler called preventDefault, which is
    // what stops the browser's synthetic click from jumping anyway.
    const notPrevented = fireEvent.keyDown(cardButton(), { key: "Enter" });

    expect(notPrevented).toBe(false);
    expect(mockFocus).toHaveBeenCalledWith("ses_1");
    expect(mockJump).not.toHaveBeenCalled();
  });

  it("jumps on Shift+Enter, the keyboard spelling of the click", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.keyDown(cardButton(), { key: "Enter", shiftKey: true });

    expect(mockJump).toHaveBeenCalledTimes(1);
    expect(mockFocus).not.toHaveBeenCalled();
  });

  it.each([false, true])("focuses the session from the expand button (compact=%s) without jumping", (compact) => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={compact} />);

    fireEvent.click(screen.getByRole("button", { name: "Focus session" }));

    expect(mockFocus).toHaveBeenCalledWith("ses_1");
    expect(mockJump).not.toHaveBeenCalled();
  });

  it("still jumps when the title is clicked", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.click(screen.getByText("Fix the pulse dashboard"));

    expect(mockJump).toHaveBeenCalledTimes(1);
    expect(mockFocus).not.toHaveBeenCalled();
  });

  it("does not open the side pane on a single click", () => {
    render(<PulseCard row={makeRow({ pending_ask: { kind: "permission", summary: "ask" } })} compact={false} />);

    fireEvent.click(cardButton());

    expect(mockJumpAsk).not.toHaveBeenCalled();
  });

  it("adds the side-pane jump on double click, after the single click already jumped", () => {
    render(<PulseCard row={makeRow({ pending_ask: { kind: "permission", summary: "ask" } })} compact={false} />);
    const button = cardButton();

    // A real double click is click, click, dblclick.
    fireEvent.click(button);
    fireEvent.click(button);
    fireEvent.doubleClick(button);

    expect(mockJump).toHaveBeenCalledTimes(2);
    expect(mockJumpAsk).toHaveBeenCalledTimes(1);
    expect(mockJumpAsk).toHaveBeenCalledWith({
      projectPath: "/Users/james/www/ocode",
      host: "",
      sessionId: "ses_1",
      title: "Fix the pulse dashboard",
    });
  });

  it("does nothing extra on double click when there is no pending ask", () => {
    render(<PulseCard row={makeRow()} compact={false} />);
    const button = cardButton();

    fireEvent.click(button);
    fireEvent.click(button);
    fireEvent.doubleClick(button);

    expect(mockJump).toHaveBeenCalledTimes(2);
    expect(mockJumpAsk).not.toHaveBeenCalled();
  });
});

describe("PulseCard hover overlay is never empty", () => {
  // The overlay exists to answer "what is this session doing" in one glance.
  // Every test above asserted only that it APPEARS; none asserted it has
  // anything IN it, so a card could expand into a blank bordered panel and the
  // suite stayed green. The suite's own default row (kind "text", todo null, no
  // ask, empty tail) is exactly such a card.

  it("shows a text-kind current task, which the server emits for settled rows", async () => {
    // derivePulseTask's last fallback (internal/server/pulse_rows.go) returns
    // kind "text" — the last assistant line — for precisely the idle and error
    // rows that make up most of the dashboard. The overlay used to render
    // `current_task` only when kind was "tool", so the one line the server had
    // already computed for those rows was dropped on the floor.
    render(
      <PulseCard
        row={makeRow({ status: "idle", current_task: { kind: "text", text: "here is the summary" } })}
        compact={false}
      />,
    );
    await expand();

    expect(within(overlay()!).getByText("here is the summary")).toBeInTheDocument();
  });

  it("says so when the row has nothing at all to preview", async () => {
    // Reachable: a disk-only scope=all row carries todo: null, and a session
    // whose assistant only ever made tool calls has no last assistant line
    // either, so the server sends current_task: null.
    render(
      <PulseCard row={makeRow({ status: "idle", current_task: null, todo: null })} compact={false} />,
    );
    await expand();

    const overlayEl = overlay()!;
    expect(within(overlayEl).getByTestId("pulse-overlay-empty")).toHaveTextContent(
      /nothing to preview/i,
    );
  });

  it("shows a loading line while the tail is in flight, not the empty state", async () => {
    // Otherwise every hover flashes "nothing to preview" for the duration of
    // the fetch and then swaps it for real content.
    mockTail.mockReturnValue({ entries: [], error: null, loading: true });
    render(
      <PulseCard row={makeRow({ status: "idle", current_task: null, todo: null })} compact={false} />,
    );
    await expand();

    const overlayEl = overlay()!;
    expect(within(overlayEl).getByTestId("pulse-overlay-loading")).toBeInTheDocument();
    expect(within(overlayEl).queryByTestId("pulse-overlay-empty")).not.toBeInTheDocument();
  });

  it("keeps showing real content in preference to the loading line", async () => {
    mockTail.mockReturnValue({ entries: [], error: null, loading: true });
    render(
      <PulseCard
        row={makeRow({ status: "idle", current_task: { kind: "text", text: "already known" } })}
        compact={false}
      />,
    );
    await expand();

    const overlayEl = overlay()!;
    expect(within(overlayEl).getByText("already known")).toBeInTheDocument();
    expect(within(overlayEl).queryByTestId("pulse-overlay-loading")).not.toBeInTheDocument();
  });

  it("does not need the task line for a todo row, whose items already list it", async () => {
    // kind "todo" is covered by the items list, so it must not be rendered a
    // second time on top of it.
    render(
      <PulseCard
        row={makeRow({
          status: "idle",
          current_task: { kind: "todo", text: "second" },
          todo: {
            done: 0,
            total: 1,
            current: "second",
            items: [{ text: "second", state: "in_progress" }],
          },
        })}
        compact={false}
      />,
    );
    await expand();

    const items = within(overlay()!).getByTestId("pulse-todo-items");
    expect(within(items).getAllByText("second")).toHaveLength(1);
  });
});

describe("PulseCard settled multi-line", () => {
  it("renders a settled card with its header and no stream or height floor", () => {
    render(<PulseCard row={makeRow({ status: "idle" })} compact={false} />);

    expect(cardButton().textContent).toContain("Fix the pulse dashboard");
    expect(screen.queryByTestId("pulse-stream-region")).not.toBeInTheDocument();
    expect(cardButton().parentElement!.className).not.toContain("min-h-[32rem]");
  });
});
