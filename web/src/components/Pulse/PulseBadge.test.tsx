import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { PulseBadge } from "./PulseBadge";
import { usePulse } from "@/stores/pulseStore";

// The store fallback returns running=1 / needsYou=4, so a test that omits
// countsOverride and still sees a badge proves the store was read.
vi.mock("@/stores/pulseStore", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/stores/pulseStore")>();
  return { ...actual, usePulse: () => mockPulse({ running: 1, needsYou: 4 }) };
});

function mockPulse(counts: { running: number; needsYou: number }) {
  return {
    rows: [],
    scope: "live",
    nextCursor: null,
    hasMore: false,
    error: null,
    loading: false,
    counts,
    setScope: vi.fn(),
    loadMore: vi.fn(),
    retry: vi.fn(),
  } as unknown as ReturnType<typeof usePulse>;
}

beforeEach(() => {});

describe("PulseBadge", () => {
  it("renders nothing when both counts are zero", () => {
    // A permanent "0 · 0" pill would train the user to ignore the one
    // surface whose whole purpose is "something needs you".
    const { container } = render(
      <PulseBadge onClick={() => {}} countsOverride={{ running: 0, needsYou: 0 }} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("shows running and needs-you counts with the documented glyphs", () => {
    render(<PulseBadge onClick={() => {}} countsOverride={{ running: 2, needsYou: 1 }} />);
    expect(screen.getByRole("button").textContent).toBe("● 2 · ◆ 1");
  });

  it("shows only the non-zero half", () => {
    render(<PulseBadge onClick={() => {}} countsOverride={{ running: 3, needsYou: 0 }} />);
    expect(screen.getByRole("button").textContent).toBe("● 3");
  });

  it("spells the counts in its aria-label", () => {
    // The glyphs carry no meaning to a screen reader; the label must.
    render(<PulseBadge onClick={() => {}} countsOverride={{ running: 2, needsYou: 1 }} />);
    expect(screen.getByRole("button").getAttribute("aria-label")).toBe(
      "Open Pulse: 1 session needs you, 2 running",
    );
  });

  it("uses the singular for one session", () => {
    render(<PulseBadge onClick={() => {}} countsOverride={{ running: 1, needsYou: 1 }} />);
    expect(screen.getByRole("button").getAttribute("aria-label")).toBe(
      "Open Pulse: 1 session needs you, 1 running",
    );
  });

  it("calls onClick when pressed", async () => {
    const onClick = vi.fn();
    render(<PulseBadge onClick={onClick} countsOverride={{ running: 1, needsYou: 0 }} />);
    await fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("falls back to the store when no override is given", () => {
    render(<PulseBadge onClick={() => {}} />);
    expect(screen.getByRole("button").textContent).toBe("● 1 · ◆ 4");
  });
});
