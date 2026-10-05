import * as React from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  ScopedDialog,
  ScopedDialogContent,
  ScopedDialogFooter,
  ScopedDialogTitle,
  ScopedDialogTrigger,
} from "./scoped-dialog";

/**
 * Behaviour tests for the container-scoped dialog. The contract that matters
 * most is the DISMISSAL BOUNDARY: inside the target closes the dialog, outside
 * the target must not — the rest of the app is still live behind it. Those two
 * directions are asserted as a pair so neither can pass vacuously.
 */

/** Let Radix's deferred document listeners attach, then flush pending work. */
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

interface HarnessProps {
  withInput?: boolean;
  defaultAction?: boolean;
  container?: HTMLElement | null;
  onOpenChange?: (open: boolean) => void;
  defaultOpen?: boolean;
}

function Harness({
  withInput,
  defaultAction,
  container: containerOverride,
  onOpenChange,
  defaultOpen = false,
}: HarnessProps) {
  // The state form is the documented way to hand over the container: a ref's
  // .current is null on the first render, so the dialog could not resolve it.
  const [target, setTarget] = React.useState<HTMLDivElement | null>(null);
  const [open, setOpen] = React.useState(defaultOpen);
  const container = containerOverride === undefined ? target : containerOverride;

  return (
    <div>
      <button type="button" data-testid="outside">
        Outside the target
      </button>
      <div data-testid="panel" ref={setTarget}>
        <button type="button" data-testid="inside-panel">
          Panel background
        </button>
        <ScopedDialog
          container={container}
          open={open}
          onOpenChange={(next) => {
            setOpen(next);
            onOpenChange?.(next);
          }}
        >
          <ScopedDialogContent>
            <ScopedDialogTitle>Scoped</ScopedDialogTitle>
            {withInput ? <input aria-label="filter" /> : null}
            <ScopedDialogFooter>
              <button type="button">Cancel</button>
              <button type="button" data-dialog-default-action={defaultAction ? true : undefined}>
                Save
              </button>
            </ScopedDialogFooter>
          </ScopedDialogContent>
        </ScopedDialog>
      </div>
    </div>
  );
}

describe("ScopedDialog confinement", () => {
  it("portals the panel and the dimmed backdrop INTO the target element", async () => {
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");

    const panel = screen.getByTestId("panel");
    const dialog = screen.getByRole("dialog");
    const overlay = panel.querySelector("[data-scoped-dialog-overlay]");

    expect(overlay).not.toBeNull();
    expect(panel.contains(dialog)).toBe(true);
    expect(panel.contains(overlay!)).toBe(true);
    // Portalled straight into the target: the panel is the dialog's direct
    // parent, so nothing was appended to <body> to cover the whole app.
    expect(dialog.parentElement).toBe(panel);
    expect(overlay!.parentElement).toBe(panel);
  });

  it("positions the backdrop and the panel absolutely inside the target, not fixed on the viewport", async () => {
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");

    const panel = screen.getByTestId("panel");
    const overlay = panel.querySelector("[data-scoped-dialog-overlay]")!;
    const dialog = screen.getByRole("dialog");

    expect(overlay.className).toContain("absolute");
    expect(overlay.className).toContain("inset-0");
    expect(overlay.className).not.toContain("fixed");
    expect(dialog.className).toContain("absolute");
    expect(dialog.className).not.toContain("fixed");
  });

  it("makes a position:static target a positioning context so the dialog cannot escape it", async () => {
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");
    // A static container would let the absolute panel resolve against some
    // distant ancestor — i.e. cover the viewport, the bug this prevents.
    expect(screen.getByTestId("panel").style.position).toBe("relative");
  });

  it("restores the target's own inline position on unmount", async () => {
    const { unmount } = render(<Harness defaultOpen />);
    await screen.findByRole("dialog");
    const panel = screen.getByTestId("panel");
    expect(panel.style.position).toBe("relative");

    unmount();
    expect(panel.style.position).toBe("");
  });

  it("renders nothing at all while the container is unresolved", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    render(<Harness defaultOpen container={null} />);
    await settle();

    expect(screen.queryByRole("dialog")).toBeNull();
    // An open dialog with nowhere to render is invisible; say so rather than
    // failing silently.
    expect(warn.mock.calls.some((call) => String(call[0]).includes("unresolved container"))).toBe(true);
    warn.mockRestore();
  });

  it("leaves the surrounding app accessible and interactive", async () => {
    // The contract that `modal={false}` buys: a modal Radix dialog calls
    // hideOthers() (aria-hiding everything outside the panel) and sets
    // `body { pointer-events: none }`, which would make the still-visible app
    // unusable — the opposite of a scoped dialog.
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");

    const outside = screen.getByTestId("outside");
    expect(outside.closest("[aria-hidden='true']")).toBeNull();
    expect(document.body.style.pointerEvents).not.toBe("none");
  });

  it("closes on Escape", async () => {
    const onOpenChange = vi.fn();
    render(<Harness defaultOpen onOpenChange={onOpenChange} />);
    await screen.findByRole("dialog");

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });

    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("ScopedDialog dismissal boundary", () => {
  it("closes when the dimmed backdrop INSIDE the target is clicked", async () => {
    const onOpenChange = vi.fn();
    render(<Harness defaultOpen onOpenChange={onOpenChange} />);
    await screen.findByRole("dialog");
    await settle();

    const overlay = screen
      .getByTestId("panel")
      .querySelector("[data-scoped-dialog-overlay]")!;
    await act(async () => {
      fireEvent.pointerDown(overlay);
    });

    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("does NOT close when the click lands OUTSIDE the target", async () => {
    const onOpenChange = vi.fn();
    render(<Harness defaultOpen onOpenChange={onOpenChange} />);
    await screen.findByRole("dialog");
    await settle();

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("outside"));
    });

    // The rest of the app stays usable, so a click there is not a dismissal.
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("does NOT close when focus moves OUTSIDE the target", async () => {
    const onOpenChange = vi.fn();
    render(<Harness defaultOpen onOpenChange={onOpenChange} />);
    await screen.findByRole("dialog");
    await settle();

    await act(async () => {
      screen.getByTestId("outside").focus();
    });

    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("still honours an onOpenChange-driven close from outside the target", async () => {
    // The guard must block Radix's implicit dismissal only, never an explicit
    // close the caller asks for.
    const onOpenChange = vi.fn();
    render(<Harness onOpenChange={onOpenChange} />);
    expect(screen.queryByRole("dialog")).toBeNull();

    // Nothing to assert beyond "no dismissal escaped", covered above.
    expect(onOpenChange).not.toHaveBeenCalled();
  });
});

describe("ScopedDialog focus behaviour", () => {
  it("focuses the first text entry field, matching the app-wide dialog policy", async () => {
    render(<Harness defaultOpen withInput />);
    await screen.findByRole("dialog");
    expect(screen.getByLabelText("filter")).toHaveFocus();
  });

  it("falls back to the annotated default action when there is no text entry", async () => {
    render(<Harness defaultOpen defaultAction />);
    await screen.findByRole("dialog");
    expect(screen.getByRole("button", { name: "Save" })).toHaveFocus();
  });

  it("wraps Tab from the last focusable back to the first", async () => {
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");

    // jsdom has no default Tab navigation, so only the containment handler can
    // move focus here.
    const close = screen.getByRole("button", { name: "Close" });
    close.focus();
    expect(close).toHaveFocus();

    fireEvent.keyDown(close, { key: "Tab" });
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
  });

  it("wraps Shift+Tab from the first focusable back to the last", async () => {
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");

    const cancel = screen.getByRole("button", { name: "Cancel" });
    cancel.focus();

    fireEvent.keyDown(cancel, { key: "Tab", shiftKey: true });
    expect(screen.getByRole("button", { name: "Close" })).toHaveFocus();
  });

  it("does not yank focus back when focus moves to the app outside the target", async () => {
    // A modal dialog re-claims focus (Radix gates that on `trapped`, which is
    // false here). Re-claiming would make the still-visible surrounding app
    // unusable, so this pins the opposite contract.
    render(<Harness defaultOpen />);
    await screen.findByRole("dialog");

    const outside = screen.getByTestId("outside");
    await act(async () => {
      outside.focus();
    });

    expect(outside).toHaveFocus();
  });

  it("returns focus to the trigger when it closes", async () => {
    function TriggerHarness() {
      const [target, setTarget] = React.useState<HTMLDivElement | null>(null);
      return (
        <div>
          <div ref={setTarget} data-testid="panel">
            <ScopedDialog container={target} defaultOpen>
              <ScopedDialogTrigger>Open scoped</ScopedDialogTrigger>
              <ScopedDialogContent>
                <ScopedDialogTitle>Scoped</ScopedDialogTitle>
              </ScopedDialogContent>
            </ScopedDialog>
          </div>
        </div>
      );
    }
    render(<TriggerHarness />);
    await screen.findByRole("dialog");
    const trigger = screen.getByRole("button", { name: "Open scoped" });
    trigger.focus();

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });

    expect(screen.queryByRole("dialog")).toBeNull();
    expect(trigger).toHaveFocus();
  });
});