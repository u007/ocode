import { useCallback, useContext, useEffect, useRef } from "react";
import type { editor, IRange } from "monaco-editor";
import {
  VIEWER_SELECTION_MAX,
  ViewerReportContext,
  registerViewerController,
  useViewerReport,
  type ViewerController,
  type ViewerFindCommand,
  type ViewerScrollTarget,
  type ViewerSelectTarget,
} from "../../lib/viewerContext";

/** Find session kept between agent find commands: the query and the 1-based current match. */
interface FindSession {
  query: string;
  current: number;
}

type ReportFn = ReturnType<typeof useViewerReport>;

/** The mounted editor and its model, or an error: a controller call before mount fails loudly. */
function mountedEditor(edRef: { current: editor.IStandaloneCodeEditor | null }): {
  ed: editor.IStandaloneCodeEditor;
  model: editor.ITextModel;
} {
  const ed = edRef.current;
  const model = ed?.getModel();
  if (!ed || !model) throw new Error("editor not ready");
  return { ed, model };
}

function scrollTo(ed: editor.IStandaloneCodeEditor, model: editor.ITextModel, t: ViewerScrollTarget): boolean {
  if (t.line !== undefined) {
    const count = model.getLineCount();
    if (!Number.isInteger(t.line) || t.line < 1 || t.line > count) {
      throw new Error(`line ${t.line} out of range 1-${count}`);
    }
    ed.revealLineInCenter(t.line);
    return true;
  }
  if (t.direction !== undefined) {
    // Scroll by most of a screen, so the reader keeps some context.
    const step = 0.8 * ed.getLayoutInfo().height * (t.direction === "down" ? 1 : -1);
    ed.setScrollTop(ed.getScrollTop() + step);
    return true;
  }
  return false;
}

function selectAndReveal(ed: editor.IStandaloneCodeEditor, range: IRange): void {
  ed.setSelection(range);
  ed.revealRangeInCenter(range);
}

function selectTo(ed: editor.IStandaloneCodeEditor, model: editor.ITextModel, t: ViewerSelectTarget): boolean {
  if (t.lines !== undefined) {
    const { start, end } = t.lines;
    const count = model.getLineCount();
    if (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end > count || start > end) {
      throw new Error(`lines ${start}-${end} out of range 1-${count}`);
    }
    selectAndReveal(ed, {
      startLineNumber: start,
      startColumn: 1,
      endLineNumber: end,
      endColumn: model.getLineMaxColumn(end),
    });
    return true;
  }
  if (t.text !== undefined) {
    const [first] = model.findMatches(t.text, false, false, false, null, false, 1);
    if (!first) throw new Error("text not found");
    selectAndReveal(ed, first.range);
    return true;
  }
  return false;
}

/**
 * The in-editor find bar. Matches are recomputed on every step, so a stored
 * range never goes stale after an edit; only the query and the current index
 * are kept.
 */
function runFind(
  ed: editor.IStandaloneCodeEditor,
  model: editor.ITextModel,
  findRef: { current: FindSession | null },
  report: ReportFn,
  cmd: ViewerFindCommand,
): boolean {
  if (cmd.op === "close") {
    findRef.current = null;
    report({ find: null });
    return true;
  }
  if (cmd.op === "open") {
    if (cmd.query === "") throw new Error("find query is empty");
    findRef.current = { query: cmd.query, current: 0 };
  }
  const session = findRef.current;
  if (session === null) throw new Error("find not open");

  const matches = model.findMatches(session.query, false, false, false, null, false);
  const count = matches.length;
  let current = 0;
  if (count > 0) {
    if (cmd.op === "open") current = 1;
    else if (cmd.op === "next") current = session.current < 1 || session.current >= count ? 1 : session.current + 1;
    else current = session.current <= 1 ? count : session.current - 1;
    selectAndReveal(ed, matches[current - 1].range);
  }
  session.current = current;
  report({ find: { query: session.query, count, current } });
  return true;
}

/**
 * Monaco's agent-control surface. Refs, not closures over state, so one
 * registration serves every render of the mounted editor.
 */
function buildController(
  edRef: { current: editor.IStandaloneCodeEditor | null },
  findRef: { current: FindSession | null },
  reportRef: { current: ReportFn },
): ViewerController {
  return {
    scroll: (t) => {
      const { ed, model } = mountedEditor(edRef);
      return scrollTo(ed, model, t);
    },
    select: (t) => {
      const { ed, model } = mountedEditor(edRef);
      return selectTo(ed, model, t);
    },
    find: (cmd) => {
      const { ed, model } = mountedEditor(edRef);
      return runFind(ed, model, findRef, reportRef.current, cmd);
    },
  };
}

/**
 * Reports what a Monaco-backed text viewer shows (selection lines and text,
 * visible span) and registers the agent-control controller for the session.
 *
 * `enabled` gates everything: when false the hook neither reports nor
 * registers, so a sibling viewer (MarkdownViewer's rendered mode) keeps its
 * own controller. `useViewerController` has no such gate, which is why this
 * registers through `registerViewerController` directly.
 */
export function useMonacoViewerReport(
  path: string,
  enabled = true,
): {
  onEditorMount: (ed: editor.IStandaloneCodeEditor) => void;
  onSelectionChange: (sel: { startLine: number; endLine: number } | null) => void;
  onViewportChange: (view: { start: number; end: number; text: string }) => void;
} {
  const report = useViewerReport(path);
  const key = useContext(ViewerReportContext);
  const edRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const findRef = useRef<FindSession | null>(null);
  // Read by the stable callbacks below, so their identity never changes.
  const reportRef = useRef(report);
  reportRef.current = report;
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;

  useEffect(() => {
    if (!key || !enabled) return;
    return registerViewerController(key, buildController(edRef, findRef, reportRef));
  }, [key, path, enabled]);

  useEffect(
    () => () => {
      edRef.current = null;
      findRef.current = null;
    },
    [],
  );

  const onEditorMount = useCallback((ed: editor.IStandaloneCodeEditor) => {
    edRef.current = ed;
  }, []);

  const onSelectionChange = useCallback((sel: { startLine: number; endLine: number } | null) => {
    if (!enabledRef.current) return;
    if (!sel) {
      reportRef.current({ location: { lines: undefined }, selection: null });
      return;
    }
    const model = edRef.current?.getModel();
    // intentionally not reported: no mounted model, so there is no text to read
    if (!model) return;
    // Whole lines, as the reader sees them: the selection is line-granular in the report.
    const text = model.getValueInRange({
      startLineNumber: sel.startLine,
      startColumn: 1,
      endLineNumber: sel.endLine,
      endColumn: model.getLineMaxColumn(sel.endLine),
    });
    reportRef.current({
      location: { lines: { start: sel.startLine, end: sel.endLine } },
      selection: text.slice(0, VIEWER_SELECTION_MAX),
    });
  }, []);

  const onViewportChange = useCallback((view: { start: number; end: number; text: string }) => {
    if (!enabledRef.current) return;
    reportRef.current({
      location: { visibleLines: { start: view.start, end: view.end } },
      visibleText: view.text,
    });
  }, []);

  return { onEditorMount, onSelectionChange, onViewportChange };
}
