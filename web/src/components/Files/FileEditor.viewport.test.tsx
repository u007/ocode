// FileEditor's viewport and mount hooks. The Monaco stub calls `onMount` with a
// fake editor, so the tests can move the visible range and fire scroll and
// content events. Monaco itself never runs here.
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => {
  type Range = { startLineNumber: number; endLineNumber: number };
  const scrollListeners = new Set<() => void>();
  const contentListeners = new Set<() => void>();
  const state = {
    lines: [] as string[],
    visible: [] as Range[],
  };
  const model = {
    uri: { toString: () => "file:///fixture.txt" },
    getValue: () => state.lines.join("\n"),
    getLineCount: () => state.lines.length,
    getLineMaxColumn: (n: number) => state.lines[n - 1].length + 1,
    getValueInRange: (r: Range) => state.lines.slice(r.startLineNumber - 1, r.endLineNumber).join("\n"),
    getFullModelRange: () => ({ startLineNumber: 1, startColumn: 1, endLineNumber: state.lines.length, endColumn: 1 }),
    pushEditOperations: vi.fn(),
  };
  const ed = {
    getModel: () => model,
    getVisibleRanges: () => state.visible,
    onDidScrollChange: vi.fn((cb: () => void) => {
      scrollListeners.add(cb);
      return { dispose: () => scrollListeners.delete(cb) };
    }),
    onDidChangeModelContent: vi.fn((cb: () => void) => {
      contentListeners.add(cb);
      return { dispose: () => contentListeners.delete(cb) };
    }),
    onDidChangeCursorSelection: vi.fn(() => ({ dispose: () => {} })),
    getDomNode: () => null,
    focus: vi.fn(),
  };
  const monaco = {
    editor: {
      defineTheme: vi.fn(),
      setTheme: vi.fn(),
    },
  };
  const onEditorMount = vi.fn();
  return { scrollListeners, contentListeners, state, ed, monaco, onEditorMount };
});

vi.mock("@monaco-editor/react", async () => {
  const React = await import("react");
  return {
    default: (props: { onMount?: (ed: unknown, monaco: unknown) => void }) => {
      React.useEffect(() => {
        props.onMount?.(h.ed, h.monaco);
      }, []);
      return <div data-testid="monaco-stub" />;
    },
  };
});
vi.mock("../../lib/monaco-setup", () => ({}));
vi.mock("../../api/client", () => ({
  api: {
    getMonacoSettings: vi.fn().mockResolvedValue({}),
    getAppearanceSettings: vi.fn().mockResolvedValue({}),
    getAppearanceTheme: vi.fn().mockResolvedValue({}),
  },
}));
vi.mock("../../lib/monacoTheme", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/monacoTheme")>()),
  applyMonacoTheme: vi.fn(),
}));
vi.mock("../../lib/editorLinks", () => ({
  ensureEditorLinks: vi.fn(),
  setEditorModelInfo: vi.fn(),
  clearEditorModelInfo: vi.fn(),
}));

import FileEditor from "./FileEditor";

/** The most recent argument passed to a view callback. */
function lastView(fn: ReturnType<typeof vi.fn>): { start: number; end: number; text: string } {
  const last = fn.mock.calls[fn.mock.calls.length - 1];
  if (!last) throw new Error("no viewport reported");
  return last[0];
}

function linesOf(n: number, width = 10): string[] {
  return Array.from({ length: n }, (_, i) => `line ${i + 1}`.padEnd(width, " "));
}

beforeEach(() => {
  h.state.lines = linesOf(200);
  h.state.visible = [{ startLineNumber: 1, endLineNumber: 40 }];
  h.scrollListeners.clear();
  h.contentListeners.clear();
  h.onEditorMount.mockClear();
  h.ed.onDidScrollChange.mockClear();
  h.ed.onDidChangeModelContent.mockClear();
});

afterEach(() => {
  cleanup();
});

describe("FileEditor onEditorMount", () => {
  it("hands the live editor to the parent once Monaco has mounted", async () => {
    render(<FileEditor path="a.txt" content="x" onEditorMount={h.onEditorMount} />);
    await waitFor(() => expect(h.onEditorMount).toHaveBeenCalledWith(h.ed));
  });
});

describe("FileEditor onViewportChange", () => {
  it("reports the first to last visible line and their text after mount", async () => {
    const onViewportChange = vi.fn();
    render(<FileEditor path="a.txt" content="x" onViewportChange={onViewportChange} />);
    await waitFor(() => expect(onViewportChange).toHaveBeenCalled(), { timeout: 1000 });
    const view = lastView(onViewportChange);
    expect(view.start).toBe(1);
    expect(view.end).toBe(40);
    expect(view.text.startsWith("line 1")).toBe(true);
    expect(view.text).toContain("line 40");
    expect(view.text).not.toContain("line 41");
  });

  it("reports the new span when the editor scrolls", async () => {
    const onViewportChange = vi.fn();
    render(<FileEditor path="a.txt" content="x" onViewportChange={onViewportChange} />);
    await waitFor(() => expect(onViewportChange).toHaveBeenCalled(), { timeout: 1000 });

    h.state.visible = [{ startLineNumber: 50, endLineNumber: 90 }];
    act(() => h.scrollListeners.forEach((cb) => cb()));
    await waitFor(() => expect(lastView(onViewportChange)).toMatchObject({ start: 50, end: 90 }), {
      timeout: 1000,
    });
  });

  it("reports again when the content of the model changes", async () => {
    const onViewportChange = vi.fn();
    render(<FileEditor path="a.txt" content="x" onViewportChange={onViewportChange} />);
    await waitFor(() => expect(onViewportChange).toHaveBeenCalled(), { timeout: 1000 });
    const before = onViewportChange.mock.calls.length;

    h.state.lines = ["replaced"];
    h.state.visible = [{ startLineNumber: 1, endLineNumber: 1 }];
    act(() => h.contentListeners.forEach((cb) => cb()));
    await waitFor(() => expect(onViewportChange.mock.calls.length).toBeGreaterThan(before), { timeout: 1000 });
    expect(lastView(onViewportChange).text).toBe("replaced");
  });

  it("caps the visible text at 1200 characters", async () => {
    h.state.lines = ["x".repeat(2000)];
    h.state.visible = [{ startLineNumber: 1, endLineNumber: 1 }];
    const onViewportChange = vi.fn();
    render(<FileEditor path="a.txt" content="x" onViewportChange={onViewportChange} />);
    await waitFor(() => expect(onViewportChange).toHaveBeenCalled(), { timeout: 1000 });
    expect(lastView(onViewportChange).text).toHaveLength(1200);
  });

  it("wires no scroll or content listeners when the prop is absent", async () => {
    render(<FileEditor path="a.txt" content="x" />);
    await waitFor(() => expect(h.ed.onDidChangeModelContent).not.toHaveBeenCalled());
    expect(h.ed.onDidScrollChange).not.toHaveBeenCalled();
  });

  it("disposes its listeners on unmount", async () => {
    const onViewportChange = vi.fn();
    const view = render(<FileEditor path="a.txt" content="x" onViewportChange={onViewportChange} />);
    await waitFor(() => expect(h.scrollListeners.size).toBe(1));
    expect(h.contentListeners.size).toBe(1);
    view.unmount();
    expect(h.scrollListeners.size).toBe(0);
    expect(h.contentListeners.size).toBe(0);
  });
});
