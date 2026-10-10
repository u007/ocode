import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import CompactionCancelButton from "./CompactionCancelButton";
import CommandActivityBar, { MOUNT_DELAY_MS } from "./CommandActivityBar";
import { api } from "../../api/client";
import * as actionErrors from "../../lib/actionErrors";
import { __resetSessionActivityForTests, setCommandActivity, setSkillActivity } from "../../lib/commandActivity";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  __resetSessionActivityForTests();
});

describe("CompactionCancelButton", () => {
  it("renders nothing without a session id", () => {
    const { container } = render(<CompactionCancelButton sessionId={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("cancels server-side, disables while in flight, and ignores a second click", async () => {
    let resolve!: (v: { cancelled: boolean }) => void;
    const spy = vi
      .spyOn(api, "cancelCompaction")
      .mockReturnValue(new Promise((r) => { resolve = r; }));

    render(<CompactionCancelButton sessionId="s1" host="devbox" />);
    const btn = screen.getByRole("button", { name: "Cancel compaction" });
    fireEvent.click(btn);

    // The host is threaded through, or a remote session's cancel would hit the
    // local server.
    expect(spy).toHaveBeenCalledWith("s1", "devbox");
    expect(btn).toBeDisabled();
    expect(btn).toHaveTextContent("Cancelling…");

    // A second click while the request is in flight is a no-op.
    fireEvent.click(btn);
    expect(spy).toHaveBeenCalledTimes(1);

    // On success the button deliberately stays disabled: the server's
    // compaction_done frame is what retires the bar (on every client).
    resolve({ cancelled: true });
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    expect(btn).toBeDisabled();
  });

  it("surfaces a failure as an action error and re-enables the button", async () => {
    vi.spyOn(api, "cancelCompaction").mockRejectedValue(new Error("boom"));
    const report = vi.spyOn(actionErrors, "reportActionError").mockImplementation(() => {});

    render(<CompactionCancelButton sessionId="s1" />);
    const btn = screen.getByRole("button", { name: "Cancel compaction" });
    fireEvent.click(btn);

    await waitFor(() => expect(report).toHaveBeenCalled());
    await waitFor(() => expect(btn).not.toBeDisabled());
  });
});

describe("CommandActivityBar compact cancel", () => {
  it("offers Cancel for a running /compact", () => {
    vi.useFakeTimers();
    setCommandActivity("s1", "/compact focus on auth");
    render(<CommandActivityBar sessionId="s1" host="devbox" />);
    act(() => { vi.advanceTimersByTime(MOUNT_DELAY_MS); });
    expect(screen.getByRole("button", { name: "Cancel compaction" })).toBeInTheDocument();
  });

  it("does not offer Cancel for a non-compact command", () => {
    vi.useFakeTimers();
    setCommandActivity("s1", "/recap");
    render(<CommandActivityBar sessionId="s1" />);
    act(() => { vi.advanceTimersByTime(MOUNT_DELAY_MS); });
    expect(screen.queryByRole("button", { name: "Cancel compaction" })).toBeNull();
  });

  it("does not offer Cancel for a skill load", () => {
    vi.useFakeTimers();
    setSkillActivity("s1", "git-commit-push");
    render(<CommandActivityBar sessionId="s1" />);
    act(() => { vi.advanceTimersByTime(MOUNT_DELAY_MS); });
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel compaction" })).toBeNull();
  });
});
