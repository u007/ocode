import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import OpenPathDialog from "./OpenPathDialog";

/**
 * The dialog is the entry point for opening a file OUTSIDE the project. It only
 * collects and validates the path — the caller grants the directory and opens
 * the file — so these tests pin the validation and the submit contract, not the
 * grant itself (that lives in useEditorTabs.outsideProject.test.ts).
 *
 * NOTE: this repo's web suite uses `fireEvent`, not `@testing-library/user-event`
 * (that package is not a dependency).
 */
describe("OpenPathDialog", () => {
  it("submits the trimmed absolute path", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const onOpenChange = vi.fn();
    render(
      <OpenPathDialog open onOpenChange={onOpenChange} onSubmit={onSubmit} projectPath="/proj" />,
    );

    fireEvent.change(screen.getByLabelText("File path"), {
      target: { value: "  /Users/x/notes.md  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("/Users/x/notes.md"));
    // A successful submit closes the dialog.
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("pre-fills the input from initialPath (a chat file link)", () => {
    render(
      <OpenPathDialog
        open
        onOpenChange={() => {}}
        onSubmit={vi.fn()}
        initialPath="/Users/x/Downloads/notes.md"
      />,
    );
    expect(screen.getByLabelText("File path")).toHaveValue("/Users/x/Downloads/notes.md");
  });

  it("rejects a relative path without calling onSubmit", async () => {
    const onSubmit = vi.fn();
    render(<OpenPathDialog open onOpenChange={() => {}} onSubmit={onSubmit} />);

    fireEvent.change(screen.getByLabelText("File path"), { target: { value: "notes.md" } });
    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/absolute path/i);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("rejects an empty path", async () => {
    const onSubmit = vi.fn();
    render(<OpenPathDialog open onOpenChange={() => {}} onSubmit={onSubmit} />);

    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/enter a path/i);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps the dialog open and shows the error when the grant fails", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("path could not be resolved"));
    const onOpenChange = vi.fn();
    render(
      <OpenPathDialog open onOpenChange={onOpenChange} onSubmit={onSubmit} projectPath="/proj" />,
    );

    fireEvent.change(screen.getByLabelText("File path"), { target: { value: "/nope/x.md" } });
    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/could not be resolved/);
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("submits on Enter", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<OpenPathDialog open onOpenChange={() => {}} onSubmit={onSubmit} />);

    fireEvent.change(screen.getByLabelText("File path"), { target: { value: "/etc/hosts" } });
    fireEvent.keyDown(screen.getByLabelText("File path"), { key: "Enter" });

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("/etc/hosts"));
  });

  it("says the path resolves on the remote host for a remote project", () => {
    render(
      <OpenPathDialog
        open
        onOpenChange={() => {}}
        onSubmit={vi.fn()}
        projectPath="/proj"
        projectHost="ci.local"
      />,
    );
    expect(screen.getByText(/resolved on the remote host/i)).toBeInTheDocument();
    expect(screen.getByText("ci.local")).toBeInTheDocument();
  });

  it("states that the grant is editor-only", () => {
    render(<OpenPathDialog open onOpenChange={() => {}} onSubmit={vi.fn()} projectPath="/proj" />);
    // The user must know the agent is NOT given the directory.
    expect(screen.getByText(/file editor only/i)).toBeInTheDocument();
    expect(screen.getByText(/agent is not given access/i)).toBeInTheDocument();
  });
});
