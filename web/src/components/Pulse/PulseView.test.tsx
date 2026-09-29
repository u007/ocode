import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { PulseView } from "./PulseView";
import { PulseJumpProvider } from "@/lib/jumpToSession";
import { usePulse } from "@/stores/pulseStore";
import type { PulseRow } from "@/api/types";

vi.mock("@/stores/pulseStore", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/stores/pulseStore")>();
  return { ...actual, usePulse: () => mockPulse() };
});

vi.mock("./usePulseTail", () => ({
  usePulseTail: () => ({ lines: [], error: null }),
}));

vi.mock("@/lib/jumpToSession", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/jumpToSession")>();
  return { ...actual, useJumpToSession: () => mockJump, useJumpToPendingAsk: () => mockJumpAsk };
});

function row(over: Partial<PulseRow> & { session_id: string }): PulseRow {
  return {
    project_path: "/proj/alpha",
    title: `t ${over.session_id}`,
    status: "idle",
    current_task: null,
    todo: null,
    pending_ask: null,
    turn_started_at: "",
    updated_at: "2020-01-01T00:00:00Z",
    child_count: 0,
    ...over,
  };
}

const mockJump = vi.fn();
const mockJumpAsk = vi.fn();

let overrides: Partial<ReturnType<typeof usePulse>> = {};
function mockPulse() {
  return {
    rows: [],
    scope: "live",
    nextCursor: null,
    hasMore: false,
    error: null,
    loading: false,
    counts: { running: 0, needsYou: 0 },
    setScope: vi.fn(),
    loadMore: vi.fn(),
    retry: vi.fn(),
    ...overrides,
  } as ReturnType<typeof usePulse>;
}

function mount() {
  return render(
    <PulseJumpProvider exitPulse={() => {}}>
      <PulseView />
    </PulseJumpProvider>,
  );
}

beforeEach(() => {
  overrides = {};
  mockJump.mockReset();
  mockJumpAsk.mockReset();
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("PulseView sections", () => {
  it("shows sections in order Needs you → Running → Recent", () => {
    overrides = {
      rows: [
        row({ session_id: "recent", status: "idle" }),
        row({ session_id: "run", status: "running" }),
        row({ session_id: "ask", status: "needs_permission" }),
      ],
    };
    mount();
    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings).toEqual(["Needs you (1)", "Running (1)", "Recent (1)"]);
  });

  it("hides a section with no rows and shows the count when it has some", () => {
    overrides = {
      rows: [
        row({ session_id: "run", status: "running" }),
        row({ session_id: "run2", status: "running" }),
      ],
    };
    mount();
    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings).toEqual(["Running (2)"]);
    expect(screen.queryByText(/Needs you/)).toBeNull();
  });

  it("routes error rows into Recent, not a separate section", () => {
    overrides = { rows: [row({ session_id: "e", status: "error" })] };
    mount();
    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings).toEqual(["Recent (1)"]);
  });
});

describe("PulseView toolbar", () => {
  it("filters by project basename, case-insensitively", async () => {
    overrides = {
      rows: [
        row({ session_id: "a", project_path: "/proj/alpha", title: "Fix the parser" }),
        row({ session_id: "b", project_path: "/proj/beta", title: "Write docs" }),
      ],
    };
    mount();
    fireEvent.change(screen.getByPlaceholderText(/filter/i), { target: { value: "BETA" } });
    // Assert on card COUNT: the surviving card's project label is "beta" and its
    // title is "Write docs", so a text query would not isolate the filter.
    await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(1));
  });

  it("matches the project BASENAME, not the whole path", async () => {
    // Neither "alpha" nor "beta" contains "proj", so a basename filter matches
    // nothing. A substring match on the FULL path would instead light up
    // "/proj/alpha" — one card, from a directory segment the user never typed.
    overrides = {
      rows: [
        row({ session_id: "a", project_path: "/proj/alpha" }),
        row({ session_id: "b", project_path: "/elsewhere/beta" }),
      ],
    };
    mount();
    fireEvent.change(screen.getByPlaceholderText(/filter/i), { target: { value: "proj" } });
    await waitFor(() => expect(screen.queryAllByRole("listitem")).toHaveLength(0));
    expect(screen.getByText(/no sessions match/i)).toBeTruthy();
  });

  it("filters by title, case-insensitively", async () => {
    overrides = {
      rows: [
        row({ session_id: "a", project_path: "/p/alpha", title: "Fix the parser" }),
        row({ session_id: "b", project_path: "/p/beta", title: "Write docs" }),
      ],
    };
    mount();
    fireEvent.change(screen.getByPlaceholderText(/filter/i), { target: { value: "PARSER" } });
    await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(1));
    expect(screen.getByText("Fix the parser")).toBeTruthy();
  });

  it("clearing the filter brings every row back", async () => {
    overrides = {
      // Distinct project paths: the filter also matches the project BASENAME,
      // so two rows sharing /proj/alpha could never be separated by a
      // project-name query (they'd both match "alpha").
      rows: [
        row({ session_id: "a", project_path: "/proj/alpha", title: "alpha" }),
        row({ session_id: "b", project_path: "/proj/beta", title: "beta" }),
      ],
    };
    mount();
    const input = screen.getByPlaceholderText(/filter/i);
    fireEvent.change(input, { target: { value: "alpha" } });
    await waitFor(() => expect(screen.queryByText("beta")).toBeNull());
    fireEvent.change(input, { target: { value: "" } });
    // Both the project label and the title read "beta", so assert on the card
    // count rather than a text query that matches two nodes.
    await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(2));
  });

  it("the Live|All toggle calls setScope and reflects the active scope", async () => {
    const setScope = vi.fn();
    overrides = { setScope };
    mount();
    fireEvent.click(screen.getByRole("button", { name: /all/i }));
    expect(setScope).toHaveBeenCalledWith("all");
  });

  it("marks the active scope for assistive tech", () => {
    overrides = { scope: "all" };
    mount();
    expect(screen.getByRole("button", { name: /^all/i }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: /^live/i }).getAttribute("aria-pressed")).toBe("false");
  });

  it("renders no loading spinner while a fetch is in flight", () => {
    // Rows are deliberately kept on screen during a refresh (the error banner
    // is non-destructive for the same reason), so a toolbar spinner was the
    // only thing that moved on every fetch. Rows also arrive over SSE, so it
    // blinked on unrelated live events and read as a stall. Asserted by
    // class, not testid: the point of the test is that NO element is there.
    overrides = { loading: true, rows: [row({ session_id: "a" })] };
    const { container } = mount();
    expect(container.querySelector(".animate-spin")).toBeNull();
  });

  it("shows a Load more button only when hasMore is true", async () => {
    const loadMore = vi.fn();
    overrides = { hasMore: true, loadMore, rows: [row({ session_id: "a" })] };
    mount();
    fireEvent.click(screen.getByRole("button", { name: /load more/i }));
    expect(loadMore).toHaveBeenCalledTimes(1);
  });

  it("hides Load more on the last page", () => {
    overrides = { hasMore: false, rows: [row({ session_id: "a" })] };
    mount();
    expect(screen.queryByRole("button", { name: /load more/i })).toBeNull();
  });
});

describe("PulseView empty and error states", () => {
  it("live scope with no rows says so and offers the All scope", async () => {
    const setScope = vi.fn();
    overrides = { rows: [], setScope };
    mount();
    expect(screen.getByText(/no live sessions/i)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /see recent sessions/i }));
    expect(setScope).toHaveBeenCalledWith("all");
  });

  it("shows an error banner with retry and does not render stale rows", async () => {
    const retry = vi.fn();
    overrides = { error: "server exploded", retry, rows: [] };
    mount();
    expect(screen.getByRole("alert").textContent).toContain("server exploded");
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("keeps rows visible alongside an error rather than blanking the dashboard", () => {
    overrides = { error: "refresh failed", rows: [row({ session_id: "a", title: "still here" })] };
    mount();
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText("still here")).toBeTruthy();
  });

  it("an empty filter result says the filter matched nothing", async () => {
    overrides = { rows: [row({ session_id: "a", title: "alpha" })] };
    mount();
    fireEvent.change(screen.getByPlaceholderText(/filter/i), { target: { value: "zzzz" } });
    await waitFor(() => expect(screen.getByText(/no sessions match/i)).toBeTruthy());
  });
});

describe("PulseView interaction", () => {
  it("clicking a card jumps to that session with host ''", async () => {
    overrides = { rows: [row({ session_id: "ses_x", title: "Jump me" })] };
    mount();
    fireEvent.click(screen.getByText("Jump me"));
    expect(mockJump).toHaveBeenCalledWith({
      projectPath: "/proj/alpha",
      host: "",
      sessionId: "ses_x",
      title: "Jump me",
    });
  });

  it("arrow keys move focus between cards in reading order", async () => {
    overrides = {
      rows: [
        row({ session_id: "ask", status: "needs_permission", title: "Needs" }),
        row({ session_id: "run", status: "running", title: "Runs" }),
      ],
    };
    mount();
    const needs = screen.getByText("Needs").closest("button")!;
    needs.focus();
    expect(document.activeElement).toBe(needs);
    fireEvent.keyDown(needs, { key: "ArrowDown" });
    expect(document.activeElement?.textContent ?? "").toContain("Runs");
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowUp" });
    expect(document.activeElement?.textContent ?? "").toContain("Needs");
  });
});
