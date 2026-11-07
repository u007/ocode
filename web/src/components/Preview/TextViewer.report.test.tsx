import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  onSelectionChange: null as null | ((sel: { startLine: number; endLine: number } | null) => void),
  onEditorMount: null as null | ((ed: unknown) => void),
}));

vi.mock("../../api/client", () => ({
  api: { getFileContent: vi.fn().mockResolvedValue({ content: "line1\nline2\nline3", is_binary: false }) },
}));
vi.mock("../Files/FileEditor", () => ({
  default: (props: {
    onSelectionChange?: (sel: { startLine: number; endLine: number } | null) => void;
    onEditorMount?: (ed: unknown) => void;
  }) => {
    mocks.onSelectionChange = props.onSelectionChange ?? null;
    mocks.onEditorMount = props.onEditorMount ?? null;
    return null;
  },
}));

import TextViewer from "./TextViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

const LINES = ["line1", "line2", "line3"];
const fakeEditor = {
  getModel: () => ({
    getLineMaxColumn: (n: number) => LINES[n - 1].length + 1,
    getValueInRange: (r: { startLineNumber: number; endLineNumber: number }) =>
      LINES.slice(r.startLineNumber - 1, r.endLineNumber).join("\n"),
  }),
};

afterEach(() => {
  cleanup();
  resetViewerStates();
});

describe("TextViewer report", () => {
  it("reports the selected lines, and clears them when the selection collapses", async () => {
    setViewerDoc("s1", { path: "/notes.txt", kind: "text" });
    render(
      <ViewerReportContext.Provider value="s1">
        <TextViewer path="/notes.txt" />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(mocks.onSelectionChange).not.toBeNull());
    // The selection text is read from the mounted editor's model, so mount a fake one.
    mocks.onEditorMount?.(fakeEditor);

    mocks.onSelectionChange?.({ startLine: 2, endLine: 3 });
    expect(readViewerState("s1")?.location.lines).toEqual({ start: 2, end: 3 });
    expect(readViewerState("s1")?.selection).toBe("line2\nline3");

    mocks.onSelectionChange?.(null);
    expect(readViewerState("s1")?.location.lines).toBeUndefined();
    expect(readViewerState("s1")?.selection).toBeNull();
  });
});
