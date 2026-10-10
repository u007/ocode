import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CommandActivityBar, { MOUNT_DELAY_MS } from "./CommandActivityBar";
import {
  __resetSessionActivityForTests,
  clearSessionActivity,
  setCommandActivity,
  setSkillActivity,
} from "../../lib/commandActivity";

beforeEach(() => {
  __resetSessionActivityForTests();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

/** Advance past the mount delay and flush React so the bar can appear. */
function passMountDelay(ms = MOUNT_DELAY_MS) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

describe("CommandActivityBar", () => {
  it("renders nothing when nothing is running", () => {
    const { container } = render(<CommandActivityBar sessionId="s1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for an unknown session", () => {
    setCommandActivity("other", "/recap");
    const { container } = render(<CommandActivityBar sessionId="s1" />);
    expect(container).toBeEmptyDOMElement();
  });

  // The 400ms hold is what keeps instant commands (/yolo, /effort) from
  // flashing a bar. Without this assertion the delay could be removed and
  // every other test here would still pass.
  it("holds the bar back until the work has lasted long enough to be worth showing", () => {
    setCommandActivity("s1", "/recap");
    const { container } = render(<CommandActivityBar sessionId="s1" />);

    act(() => {
      vi.advanceTimersByTime(MOUNT_DELAY_MS - 1);
    });
    expect(container).toBeEmptyDOMElement();

    passMountDelay(1);
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("labels a blocking command with an elapsed counter", () => {
    setCommandActivity("s1", "/recap");
    render(<CommandActivityBar sessionId="s1" />);
    passMountDelay();

    expect(screen.getByRole("status")).toHaveTextContent("Running /recap");
  });

  // Casing matches the transcript tool-block header (Skill "name"), so the two
// surfaces read identically.
  it("labels the skill the model loaded", () => {
    setSkillActivity("s1", "git-commit-push");
    render(<CommandActivityBar sessionId="s1" />);
    passMountDelay();

    expect(screen.getByRole("status")).toHaveTextContent('Running Skill "git-commit-push"');
  });

  it("ticks the elapsed counter once a second", () => {
    setCommandActivity("s1", "/recap");
    render(<CommandActivityBar sessionId="s1" />);
    passMountDelay();

    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByRole("status")).toHaveTextContent("3s elapsed");
  });

  it("disappears when the activity clears", () => {
    setCommandActivity("s1", "/recap");
    const { container } = render(<CommandActivityBar sessionId="s1" />);
    passMountDelay();
    expect(container).not.toBeEmptyDOMElement();

    act(() => {
      clearSessionActivity("s1");
    });
    expect(container).toBeEmptyDOMElement();
  });

  // A pending mount timer must not resurrect the bar after the work ended.
  it("does not reveal a bar whose activity was cleared before the delay elapsed", () => {
    setCommandActivity("s1", "/recap");
    const { container } = render(<CommandActivityBar sessionId="s1" />);

    act(() => {
      vi.advanceTimersByTime(MOUNT_DELAY_MS - 100);
      clearSessionActivity("s1");
    });
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(container).toBeEmptyDOMElement();
  });

  // Switching tabs re-keys the component's sessionId; a timer armed for the
  // previous session must not fire and paint a bar for the new one.
  it("does not leak a pending timer across a session switch", () => {
    setCommandActivity("s1", "/recap");
    const { container, rerender } = render(<CommandActivityBar sessionId="s1" />);

    act(() => {
      vi.advanceTimersByTime(MOUNT_DELAY_MS - 100);
    });
    rerender(<CommandActivityBar sessionId="s2" />);

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the label on one clamped row so long names cannot grow the composer", () => {
    const longName = "a".repeat(300);
    setSkillActivity("s1", longName);
    const { container } = render(<CommandActivityBar sessionId="s1" />);
    passMountDelay();

    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(container.querySelector(".truncate")).not.toBeNull();
  });
});