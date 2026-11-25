import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import URLOpenDialog from "./URLOpenDialog";

describe("URLOpenDialog", () => {
  it("offers embedded and external destinations without opening on mount", () => {
    const onOpenInTab = vi.fn();
    const onOpenExternal = vi.fn();
    const onCancel = vi.fn();
    render(
      <URLOpenDialog
        open
        url="https://example.com/docs"
        canOpenInTab
        onOpenInTab={onOpenInTab}
        onOpenExternal={onOpenExternal}
        onCancel={onCancel}
      />,
    );

    expect(screen.getByText("https://example.com/docs")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Open in browser tab" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Open in external browser" }),
    ).toBeEnabled();
    expect(onOpenInTab).not.toHaveBeenCalled();
    expect(onOpenExternal).not.toHaveBeenCalled();
  });

  it("runs only the selected destination", () => {
    const onOpenInTab = vi.fn();
    const onOpenExternal = vi.fn();
    const onCancel = vi.fn();
    render(
      <URLOpenDialog
        open
        url="https://example.com/docs"
        canOpenInTab
        onOpenInTab={onOpenInTab}
        onOpenExternal={onOpenExternal}
        onCancel={onCancel}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Open in external browser" }),
    );
    expect(onOpenExternal).toHaveBeenCalledTimes(1);
    expect(onOpenInTab).not.toHaveBeenCalled();
    expect(onCancel).not.toHaveBeenCalled();
  });

  it("cancels without opening", () => {
    const onOpenInTab = vi.fn();
    const onOpenExternal = vi.fn();
    const onCancel = vi.fn();
    render(
      <URLOpenDialog
        open
        url="https://example.com/docs"
        canOpenInTab
        onOpenInTab={onOpenInTab}
        onOpenExternal={onOpenExternal}
        onCancel={onCancel}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onOpenInTab).not.toHaveBeenCalled();
    expect(onOpenExternal).not.toHaveBeenCalled();
  });

  it("disables the embedded destination when no project is active", () => {
    render(
      <URLOpenDialog
        open
        url="https://example.com/docs"
        canOpenInTab={false}
        onOpenInTab={vi.fn()}
        onOpenExternal={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Open in browser tab" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Open in external browser" }),
    ).toBeEnabled();
  });
});
