import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "./dialog";
import { Button } from "./button";
import { Input } from "./input";

// Regression tests for the shared dialog initial-focus policy in
// ui/dialog.tsx: on open, focus (1) the first text-entry field, else (2) the
// `data-dialog-default-action` button, else (3) Radix's default first
// tabbable element. Drives every dialog surface (web + desktop) through the
// single DialogContent choke point.

function Harness({
  defaultAction,
  withInput,
}: {
  defaultAction?: boolean;
  withInput?: boolean;
}) {
  return (
    <Dialog open>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Test dialog</DialogTitle>
        </DialogHeader>
        {withInput ? <Input placeholder="filter" /> : null}
        <DialogFooter>
          <Button variant="outline" onClick={() => {}}>
            Cancel
          </Button>
          <Button onClick={() => {}} data-dialog-default-action={defaultAction ? true : undefined}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

describe("dialog initial focus policy", () => {
  it("focuses the first text input when one exists (even after other buttons)", () => {
    render(<Harness withInput />);
    const input = screen.getByPlaceholderText("filter");
    expect(input).toHaveFocus();
  });

  it("focuses the data-dialog-default-action button when there is no input", () => {
    render(<Harness defaultAction />);
    expect(screen.getByRole("button", { name: "Save" })).toHaveFocus();
  });

  it("falls back to Radix's first-tabbable default without input or annotation", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
  });
});