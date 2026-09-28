import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { PulseCard } from "./PulseCard";
import { PULSE_TAIL_LINES, type PulseTail } from "./usePulseTail";
import type { PulseRow, PulseStatus } from "../../api/types";

// Typed from the hook's own exported shape, not a hand-written literal: a
// hand-written copy silently omits new fields, and a mock that omits one reads
// as `undefined` in the component instead of failing here.
const mockTail = vi.fn<
  (sessionId: string, enabled: boolean, status: PulseStatus) => PulseTail
>(() => ({ lines: [], error: null, loading: false }));
vi.mock("./usePulseTail", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./usePulseTail")>();
  return {
    ...actual,
    usePulseTail: (...a: Parameters<typeof actual.usePulseTail>) => mockTail(...a),
  };
});

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
  mockTail.mockReset();
  mockTail.mockReturnValue({ lines: [], error: null, loading: false });
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("PulseCard structure", () => {
  it("is a listitem wrapping one button that is the whole click target", () => {
    const { container } = render(<PulseCard row={makeRow()} compact={false} />);

    const item = screen.getByRole("listitem");
    expect(item).toBe(container.firstChild);
    expect(within(item).getAllByRole("button")).toHaveLength(1);
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
    render(<PulseCard row={makeRow()} compact={false} />);
    // The hook runs on every render; `expanded` is what tells it whether to
    // subscribe. Collapsed, it is always passed false.
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", false, "running");

    await expand();

    expect(overlay()).toBeInTheDocument();
    expect(cardButton()).toHaveAttribute("aria-expanded", "true");
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", true, "running");
  });

  it("collapses on mouse leave", async () => {
    render(<PulseCard row={makeRow()} compact={false} />);
    await expand();
    expect(overlay()).toBeInTheDocument();

    fireEvent.pointerLeave(cardButton());

    expect(overlay()).not.toBeInTheDocument();
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

  it("renders the tail lines the hook returned", async () => {
    const tailLines = Array.from({ length: PULSE_TAIL_LINES }, (_, i) => `line ${i}`);
    mockTail.mockReturnValue({ lines: tailLines, error: null, loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    await expand();

    const tail = screen.getByTestId("pulse-tail");
    for (const line of tailLines) expect(within(tail).getByText(line)).toBeInTheDocument();
  });

  it("surfaces a tail fetch failure in the overlay instead of showing nothing", async () => {
    mockTail.mockReturnValue({ lines: [], error: "session state failed: boom", loading: false });
    render(<PulseCard row={makeRow()} compact={false} />);

    await expand();

    expect(within(overlay()!).getByText(/session state failed: boom/)).toBeInTheDocument();
  });

  it("stops requesting the tail once collapsed", async () => {
    render(<PulseCard row={makeRow()} compact={false} />);
    await expand();
    mockTail.mockClear();

    fireEvent.pointerLeave(cardButton());

    expect(mockTail).toHaveBeenLastCalledWith("ses_1", false, "running");
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

  it("jumps on Enter as well as on click", () => {
    render(<PulseCard row={makeRow()} compact={false} />);

    fireEvent.keyDown(cardButton(), { key: "Enter" });

    expect(mockJump).toHaveBeenCalledTimes(1);
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
    mockTail.mockReturnValue({ lines: [], error: null, loading: true });
    render(
      <PulseCard row={makeRow({ status: "idle", current_task: null, todo: null })} compact={false} />,
    );
    await expand();

    const overlayEl = overlay()!;
    expect(within(overlayEl).getByTestId("pulse-overlay-loading")).toBeInTheDocument();
    expect(within(overlayEl).queryByTestId("pulse-overlay-empty")).not.toBeInTheDocument();
  });

  it("keeps showing real content in preference to the loading line", async () => {
    mockTail.mockReturnValue({ lines: [], error: null, loading: true });
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
