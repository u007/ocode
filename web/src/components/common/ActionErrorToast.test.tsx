import { describe, expect, it, vi, afterEach } from "vitest";
import { render, cleanup, screen, fireEvent, act } from "@testing-library/react";
import ActionErrorToast from "./ActionErrorToast";
import {
  describeActionError,
  dismissActionError,
  getActionError,
  reportActionError,
  reportActionErrorMessage,
  resetActionErrors,
  useActionError,
} from "../../lib/actionErrors";
import { ApiError } from "../../api/client";

// The reported gap: a sidebar toggle / model pick that failed (e.g. a 404 from
// a remote session whose write hit the wrong server) only reached
// console.error, so the UI gave no feedback at all. These tests pin the
// user-visible contract: a failed action raises a sticky, dismissible banner
// that names the action and surfaces the HTTP status.
afterEach(() => {
  cleanup();
  resetActionErrors();
});

describe("actionErrors store", () => {
  it("formats an ApiError with the action name and HTTP status", () => {
    expect(describeActionError(new ApiError("session not found", 404), "Toggling the advisor")).toBe(
      "Toggling the advisor failed: session not found (HTTP 404)",
    );
  });

  it("formats a plain Error and an unknown throw without a status", () => {
    expect(describeActionError(new Error("boom"), "Saving")).toBe("Saving failed: boom");
    expect(describeActionError("nope", "Saving")).toBe("Saving failed");
  });

  it("holds only the most recent error (toast semantics) and clears on dismiss", () => {
    reportActionError(new ApiError("first", 500), "A");
    expect(getActionError()?.message).toBe("A failed: first (HTTP 500)");
    reportActionError(new ApiError("second", 404), "B");
    expect(getActionError()?.message).toBe("B failed: second (HTTP 404)");
    dismissActionError();
    expect(getActionError()).toBeNull();
  });

  it("notifies subscribers so a mounted toast re-renders", () => {
    // useActionError subscribes through useSyncExternalStore; this asserts the
    // notification path end to end (report → store emit → re-render → dismiss).
    render(<ActionErrorToast />);
    expect(screen.queryByTestId("action-error-toast")).toBeNull();

    act(() => reportActionErrorMessage("Something failed"));
    expect(screen.getByTestId("action-error-toast")).toHaveTextContent("Something failed");

    act(() => dismissActionError());
    expect(screen.queryByTestId("action-error-toast")).toBeNull();
  });
});

describe("ActionErrorToast", () => {
  it("renders the error with role=alert and dismisses on the close button", () => {
    render(<ActionErrorToast />);
    act(() => reportActionError(new ApiError("session not found", 404), "Toggling the advisor"));

    const toast = screen.getByRole("alert");
    expect(toast).toHaveTextContent("Toggling the advisor failed: session not found (HTTP 404)");

    fireEvent.click(screen.getByLabelText("Dismiss error"));
    expect(screen.queryByTestId("action-error-toast")).toBeNull();
  });

  it("dismisses on Escape", () => {
    render(<ActionErrorToast />);
    act(() => reportActionErrorMessage("nope"));
    expect(screen.getByTestId("action-error-toast")).toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("action-error-toast")).toBeNull();
  });

  it("stays visible until dismissed (no auto-hide timer)", () => {
    vi.useFakeTimers();
    try {
      render(<ActionErrorToast />);
      act(() => reportActionErrorMessage("sticky"));
      act(() => {
        vi.advanceTimersByTime(60_000);
      });
      expect(screen.getByTestId("action-error-toast")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("useActionError", () => {
  it("is null when no error has been reported", () => {
    const Probe = () => {
      const e = useActionError();
      return <span>{e ? e.message : "none"}</span>;
    };
    render(<Probe />);
    expect(screen.getByText("none")).toBeInTheDocument();
  });
});
