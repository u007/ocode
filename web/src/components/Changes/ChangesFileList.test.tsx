import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ChangesFileList from "./ChangesFileList";
import type { FileChange } from "@/api/types";

function change(overrides: Partial<FileChange>): FileChange {
  return {
    originalPath: "/project/src/a.ts",
    status: "modified",
    firstBackupPath: "",
    undoable: true,
    undoAllTcId: "",
    changeCount: 1,
    authors: [],
    createdAt: "",
    updatedAt: "",
    lastBashCommand: "",
    lastBashExitCode: 0,
    ...overrides,
  };
}

const noop = () => {};

describe("ChangesFileList open file", () => {
  it("opens the changed file by its absolute path", () => {
    const onOpenFile = vi.fn();
    render(
      <ChangesFileList
        files={[change({ originalPath: "/project/src/a.ts" })]}
        selectedPath={null}
        onSelect={noop}
        onOpenFile={onOpenFile}
        onUndoFile={noop}
        onUndoBlock={noop}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "open file" }));
    expect(onOpenFile).toHaveBeenCalledWith("/project/src/a.ts");
  });

  it("hides open file for a deleted file, which no longer exists to open", () => {
    render(
      <ChangesFileList
        files={[change({ status: "deleted" })]}
        selectedPath={null}
        onSelect={noop}
        onOpenFile={noop}
        onUndoFile={noop}
        onUndoBlock={noop}
      />,
    );
    expect(screen.queryByRole("button", { name: "open file" })).toBeNull();
  });
});
