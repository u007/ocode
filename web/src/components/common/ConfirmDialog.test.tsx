import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import ConfirmDialog from "./ConfirmDialog";

/**
 * The shared confirm is the app's only sanctioned way to gate a destructive
 * action (native `window.confirm` silently returns false in the Wails/WKWebView
 * desktop webview). Its contract is what every call site depends on:
 *  - Cancel/Escape/overlay never runs the action.
 *  - Confirm runs it exactly once.
 *  - A REJECTED action shows the error and KEEPS the dialog open, so a failed
 *    write is never reported as a success.
 *  - The safe action is the default-focused one (repo dialog focus policy).
 */
function renderConfirm(overrides: Partial<React.ComponentProps<typeof ConfirmDialog>> = {}) {
  const onConfirm = vi.fn().mockResolvedValue(undefined);
  const onCancel = vi.fn();
  const utils = render(
    <ConfirmDialog
      open
      title="Delete the thing?"
      description="This removes it from the list."
      confirmLabel="Delete"
      onConfirm={onConfirm}
      onCancel={onCancel}
      {...overrides}
    />,
  );
  return { ...utils, onConfirm, onCancel };
}

function dialog() {
  return screen.getByRole("dialog");
}

function confirmButton(name = "Delete") {
  return within(dialog()).getByRole("button", { name });
}

describe("ConfirmDialog", () => {
  it("renders the title and description", () => {
    renderConfirm();
    expect(screen.getByText("Delete the thing?")).toBeDefined();
    expect(screen.getByText("This removes it from the list.")).toBeDefined();
    expect(confirmButton()).toBeDefined();
  });

  it("does not run the action on Cancel", () => {
    const { onConfirm, onCancel } = renderConfirm();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalled();
  });

  it("does not run the action when the dialog is dismissed with Escape", () => {
    const { onConfirm, onCancel } = renderConfirm();
    fireEvent.keyDown(dialog(), { key: "Escape" });
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalled();
  });

  it("runs the action exactly once on confirm", async () => {
    const { onConfirm, onCancel } = renderConfirm();
    fireEvent.click(confirmButton());
    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1));
    // The call site closes the dialog itself; the shared component must not
    // assume it, so onCancel is not fired behind its back.
    expect(onCancel).not.toHaveBeenCalled();
  });

  it("shows the error and stays open when the action rejects", async () => {
    const onConfirm = vi.fn().mockRejectedValue(new Error("server said 404"));
    renderConfirm({ onConfirm });
    fireEvent.click(confirmButton());

    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("server said 404"));
    // Still open: a rejected write must not look like a completed one.
    expect(dialog()).toBeDefined();
    // And the buttons are usable again so the user can retry or cancel.
    expect(confirmButton()).not.toBeDisabled();
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).not.toBeDisabled();
  });

  it("clears a previous error when the action is retried", async () => {
    const onConfirm = vi
      .fn()
      .mockRejectedValueOnce(new Error("server said 404"))
      .mockResolvedValueOnce(undefined);
    renderConfirm({ onConfirm });

    fireEvent.click(confirmButton());
    await waitFor(() => expect(screen.getByRole("alert")).toBeDefined());

    fireEvent.click(confirmButton());
    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows a pending label and disables both buttons while the action runs", async () => {
    let release!: () => void;
    const onConfirm = vi.fn(
      () => new Promise<void>((resolve) => {
        release = resolve;
      }),
    );
    renderConfirm({ onConfirm, confirmLabel: "Delete", pendingLabel: "Deleting…" });

    fireEvent.click(confirmButton());
    await waitFor(() => expect(screen.getByText("Deleting…")).toBeDefined());
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).toBeDisabled();
    // A second click cannot fire the action twice.
    fireEvent.click(screen.getByText("Deleting…"));
    expect(onConfirm).toHaveBeenCalledTimes(1);

    release();
    await waitFor(() => expect(screen.queryByText("Deleting…")).toBeNull());
    // Success does not close the dialog — the call site owns that, so it can
    // decide what "done" looks like (close, or close-and-navigate).
    expect(dialog()).toBeDefined();
  });

  it("focuses Cancel, the safe action, not the destructive button", () => {
    renderConfirm();
    // Repo dialog focus policy: [data-dialog-default-action] receives initial
    // focus, so Enter on a freshly opened confirm cancels instead of deleting.
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).toHaveFocus();
  });

  it("renders nothing when closed", () => {
    renderConfirm({ open: false });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
