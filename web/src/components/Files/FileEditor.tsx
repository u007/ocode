import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import type { editor } from "monaco-editor";
import { Loader2, Settings2 } from "lucide-react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import MonacoSettingsPanel from "./MonacoSettingsPanel";
import { loadEditorScroll, saveEditorScroll } from "./editorTabsPersistence";
import { parseDiffPatch, type DiffLine, type Hunk } from "../../lib/parseDiffPatch";

// Ensure Monaco is configured before any editor mounts.
import "../../lib/monaco-setup";

interface FileEditorProps {
  path: string;
  content: string;
  language?: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  onOpenSettings?: () => void;
  /** Active session ID for fetching change diffs. Decorations skip when omitted. */
  session?: string;
  /** Bumps when the file is saved so diff decorations refresh from the server. */
  diffVersion?: number;
  /** Called when the selection (non-collapsed range) changes in the editor. */
  onSelectionChange?: (sel: { startLine: number; endLine: number } | null) => void;
  /** Stable id to persist/restore the scroll position under. Skips when omitted. */
  persistKey?: string;
  /** The file changed on disk outside the app while this tab held unsaved
   *  edits — shows a warning banner with reload/dismiss actions. */
  externalChange?: boolean;
  onReloadFromDisk?: () => void;
  onDismissExternalChange?: () => void;
}

import { memo } from "react";

// Map file extensions to Monaco language identifiers.
function extensionToLanguage(filePath: string): string {
  const ext = filePath.split(".").pop()?.toLowerCase() || "";
  const langMap: Record<string, string> = {
    ts: "typescript",
    tsx: "typescript",
    js: "javascript",
    jsx: "javascript",
    go: "go",
    py: "python",
    rb: "ruby",
    rs: "rust",
    java: "java",
    kt: "kotlin",
    swift: "swift",
    c: "c",
    h: "c",
    cpp: "cpp",
    hpp: "cpp",
    css: "css",
    scss: "scss",
    less: "less",
    html: "html",
    json: "json",
    xml: "xml",
    yaml: "yaml",
    yml: "yaml",
    md: "markdown",
    sql: "sql",
    sh: "shell",
    bash: "shell",
    zsh: "shell",
    dockerfile: "dockerfile",
    toml: "plaintext",
    tf: "terraform",
    dart: "dart",
    vue: "html",
    svelte: "html",
    graphql: "graphql",
    gql: "graphql",
  };
  return langMap[ext] || "plaintext";
}

/**
 * Determine whether a hunk represents a modification (mix of del+add at the
 * same position) vs a pure addition (only add lines).
 */
function isModifiedHunk(hunk: Hunk): boolean {
  return hunk.lines.some((l: DiffLine) => l.type === "del") && hunk.lines.some((l: DiffLine) => l.type === "add");
}

function FileEditorImpl({
  path,
  content,
  language,
  onChange,
  readOnly = false,
  onOpenSettings,
  session,
  diffVersion,
  onSelectionChange,
  persistKey,
  externalChange = false,
  onReloadFromDisk,
  onDismissExternalChange,
}: FileEditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  // Store the Monaco API object so it's available in effects that can't reach
  // the onMount closure.
  const monacoRef = useRef<typeof import("monaco-editor") | null>(null);
  const [persistedSettings, setPersistedSettings] = useState<editor.IStandaloneEditorConstructionOptions | null>(null);
  const [persistedTheme, setPersistedTheme] = useState<string>("ocode-dark");
  // When the parent doesn't provide onOpenSettings (the current default), the
  // Settings button opens the Monaco settings/extensions panel in a dialog.
  const [settingsOpen, setSettingsOpen] = useState(false);
  const lang = language || extensionToLanguage(path);

  // Refs for diff decoration cleanup
  const decorationIdsRef = useRef<string[]>([]);
  const viewZoneIdsRef = useRef<string[]>([]);

  // ── Stable callback refs ──
  // The parent recreates onChange / onSelectionChange on every render (App.tsx
  // inlines `(v) => handleEditorChange(id, v)`). If we pass those identities
  // straight through to Monaco, the @monaco-editor/react wrapper tears down
  // and re-creates its onDidChangeModelContent listener on every keystroke.
  // That shows up as a stutter when typing over a selection (selection
  // replacement fires content + selection events in quick succession). Keep a
  // stable caller that forwards through a ref.
  const onChangeRef = useRef(onChange);
  const onSelectionChangeRef = useRef(onSelectionChange);
  // Last model value this component forwarded to the parent via onChange.
  const lastEmittedRef = useRef<string>(content);
  // Latest incoming content, readable from mount/async callbacks.
  const contentRef = useRef(content);
  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);
  useEffect(() => {
    onSelectionChangeRef.current = onSelectionChange;
  }, [onSelectionChange]);

  const stableOnChange = useCallback((val: string | undefined) => {
    // Remember what WE emitted so the external-content sync effect can tell
    // "parent echoed our own keystroke back" (skip) from "parent has new
    // content from disk/draft/reload" (apply). Without this, any momentary
    // prop/model divergence makes @monaco-editor/react's controlled sync fire
    // a full-document executeEdits — visually reverting the in-flight
    // keystroke and slamming the caret (the "first keypress is eaten" /
    // "cursor jumps while typing" bug class).
    lastEmittedRef.current = val || "";
    onChangeRef.current?.(val || "");
  }, []);

  // ── Memoized editor options ──
  // Building this object inline makes it a new reference every render, which
  // drives Monaco's `updateOptions` and contributes to jank on the typing
  // hot path. Memoize so options only change when an actual setting does.
  const editorOptions = useMemo<editor.IStandaloneEditorConstructionOptions>(
    () => ({
      readOnly,
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'SF Mono', 'Menlo', monospace",
      scrollBeyondLastLine: false,
      insertSpaces: true,
      renderWhitespace: "selection",
      bracketPairColorization: { enabled: true },
      autoClosingBrackets: "always",
      autoClosingQuotes: "always",
      formatOnPaste: true,
      smoothScrolling: true,
      cursorBlinking: "smooth",
      // Smooth caret animation renders the caret ~30-60ms BEHIND the insertion
      // point; during fast typing it visibly trails the text and appears to
      // land in the wrong place (user-reported "cursor lags / mispositions
      // while typing"). Keep blinking smooth, drop the position animation.
      cursorSmoothCaretAnimation: "off",
      padding: { top: 8 },
      suggest: {
        showKeywords: true,
        showSnippets: true,
      },
      multiCursorModifier: "alt",
      selectionClipboard: true,
      // Persisted settings override defaults — intentionally spread last so a
      // user's saved fontSize/tabSize etc. win without recreating the object
      // when `persistedSettings` hasn't changed.
      ...persistedSettings,
    }),
    [readOnly, persistedSettings],
  );

  // Load persisted Monaco settings on mount
  useEffect(() => {
    api
      .getMonacoSettings()
      .then((s) => {
        setPersistedTheme(s.theme);
        setPersistedSettings({
          fontSize: s.font_size,
          tabSize: s.tab_size,
          wordWrap: s.word_wrap ? "on" : "off",
          minimap: { enabled: s.minimap },
          lineNumbers: s.line_numbers ? "on" : "off",
        });
      })
      .catch(() => {
        // Use defaults
        setPersistedTheme("ocode-dark");
      });
  }, []);

  const scrollSaveTimer = useRef<number | null>(null);

  // Keep contentRef in sync for mount-time and async readers.
  useEffect(() => {
    contentRef.current = content;
  }, [content]);

  // ── External content sync (uncontrolled editor) ──
  // The editor is UNCONTROLLED: it owns its model while the user types, and
  // this effect applies only GENUINE external content changes (reload from
  // disk, draft restore, clean-tab disk follow). A change that merely echoes
  // what we emitted via onChange is skipped — feeding user edits back through
  // the value prop is what made @monaco-editor/react's controlled sync fire a
  // full-document executeEdits on any divergence, eating keystrokes over a
  // selection and yanking the caret.
  useEffect(() => {
    const ed = editorRef.current;
    const model = ed?.getModel();
    if (!ed || !model) return;
    const incoming = content;
    if (incoming === model.getValue()) return;
    if (incoming === lastEmittedRef.current) return;

    lastEmittedRef.current = incoming;
    const sel = ed.getSelection();
    model.pushEditOperations(
      sel ? [sel] : [],
      [{ range: model.getFullModelRange(), text: incoming }],
      () => null,
    );
    // Restore the caret/selection (clamped to the new text automatically).
    if (sel) {
      try {
        ed.setSelection(sel);
      } catch {
        // Selection no longer valid in the new text — Monaco already clamped.
      }
    }
  }, [content]);

  // ── Selection tracking ──
  // Wired inside handleEditorMount (where the editor instance is guaranteed
  // non-null) and forwarded through a ref so we never tear down the Monaco
  // listener on parent renders. The previous empty-deps effect read
  // editorRef.current during the first commit — always null, because Monaco
  // is created asynchronously by @monaco-editor/loader AFTER effects run — so
  // the subscription silently never attached.
  const wireSelectionTracking = useCallback((ed: editor.IStandaloneCodeEditor) => {
    ed.onDidChangeCursorSelection((e) => {
      const cb = onSelectionChangeRef.current;
      if (!cb) return;
      const sel = e.selection;
      if (sel.startLineNumber === sel.endLineNumber && sel.startColumn === sel.endColumn) {
        cb(null);
      } else {
        cb({
          startLine: sel.startLineNumber,
          endLine: sel.endColumn === 1 ? sel.endLineNumber - 1 : sel.endLineNumber,
        });
      }
    });
  }, []);

  const handleEditorMount: OnMount = useCallback((ed, monaco) => {
    editorRef.current = ed;
    monacoRef.current = monaco;

    // Selection tracking (see wireSelectionTracking above).
    wireSelectionTracking(ed);

    // Content may have arrived between the first render and this async mount
    // (loader resolution). Reconcile once so the model never starts stale.
    const model = ed.getModel();
    if (model && contentRef.current !== model.getValue()) {
      lastEmittedRef.current = contentRef.current;
      model.pushEditOperations(
        [],
        [{ range: model.getFullModelRange(), text: contentRef.current }],
        () => null,
      );
    }

    // Restore the persisted scroll position and keep it saved (debounced) as
    // the user scrolls, so reopening this file lands where they left off.
    if (persistKey) {
      const savedTop = loadEditorScroll(persistKey);
      if (savedTop > 0) ed.setScrollTop(savedTop);
      ed.onDidScrollChange(() => {
        if (scrollSaveTimer.current !== null) window.clearTimeout(scrollSaveTimer.current);
        scrollSaveTimer.current = window.setTimeout(() => {
          saveEditorScroll(persistKey, ed.getScrollTop());
        }, 300);
      });
    }

    // Match the shadcn dark theme
    monaco.editor.defineTheme("ocode-dark", {
      base: "vs-dark",
      inherit: true,
      rules: [],
      colors: {
        "editor.background": "#09090b", // zinc-950
        "editor.foreground": "#e4e4e7", // zinc-200
        "editorCursor.foreground": "#e4e4e7",
        "editor.lineHighlightBackground": "#27272a1a", // zinc-800 at 10%
        "editorLineNumber.foreground": "#52525b", // zinc-600
        "editorLineNumber.activeForeground": "#a1a1aa", // zinc-400
        "editor.selectionBackground": "#264f78", // blue-800
        "editor.inactiveSelectionBackground": "#264f7840",
        "editor.selectionHighlightBackground": "#3a3d410a",
        "editorBracketMatch.background": "#3a3d4150",
        "editorBracketMatch.border": "#3a3d41",
        "editorGutter.background": "#09090b",
        "editorWidget.background": "#18181b", // zinc-900
        "editorWidget.border": "#27272a", // zinc-800
        "input.background": "#18181b",
        "input.border": "#27272a",
        "input.foreground": "#e4e4e7",
        "list.activeSelectionBackground": "#27272a",
        "list.hoverBackground": "#27272a50",
        "editorSuggestWidget.background": "#18181b",
        "editorSuggestWidget.border": "#27272a",
        "editorSuggestWidget.selectedBackground": "#27272a",
        "editorHoverWidget.background": "#18181b",
        "editorHoverWidget.border": "#27272a",
        "scrollbar.shadow": "#00000000",
        "scrollbarSlider.background": "#52525b60",
        "scrollbarSlider.hoverBackground": "#52525b90",
        "scrollbarSlider.activeBackground": "#52525b",
        "minimap.background": "#09090b",
      },
    });

    monaco.editor.setTheme("ocode-dark");
  }, [persistKey, wireSelectionTracking]);

  // ── Inline diff decorations ──
  // Fetch the change diff whenever path or session changes, parse it, and apply
  // deltaDecorations (for added/modified lines) and view zones (for deleted lines).
  useEffect(() => {
    const ed = editorRef.current;
    const monaco = monacoRef.current;
    if (!ed || !monaco || !session) return;

    let cancelled = false;

    // Clear previous decorations/zones
    if (decorationIdsRef.current.length > 0) {
      ed.deltaDecorations(decorationIdsRef.current, []);
      decorationIdsRef.current = [];
    }
    // View zones must be removed one at a time
    for (const zoneId of viewZoneIdsRef.current) {
      ed.changeViewZones((accessor) => {
        accessor.removeZone(zoneId);
      });
    }
    viewZoneIdsRef.current = [];

    api
      .getChangeDiff(session, path)
      .then((res) => {
        if (cancelled) return;
        const hunks = parseDiffPatch(res.patch);
        if (hunks.length === 0) return;

        const decorations: editor.IModelDeltaDecoration[] = [];
        const zoneDefs: { afterLine: number; lines: { text: string }[] }[] = [];

        for (const hunk of hunks) {
          const modified = isModifiedHunk(hunk);
          const delRunLines: string[] = [];
          let currentNewLine = hunk.newStart;

          const flushDeletionRun = () => {
            if (delRunLines.length === 0) return;
            zoneDefs.push({
              afterLine: Math.max(currentNewLine - 1, 1),
              lines: delRunLines.map((t: string) => ({ text: t })),
            });
            delRunLines.length = 0;
          };

          for (const line of hunk.lines) {
            if (line.type === "add") {
              // If this add is replacing one or more deleted lines, emit the
              // deleted block before the added text so the visual order matches
              // the diff hunk.
              flushDeletionRun();

              // Added/modified line — highlight with decoration
              const className = modified
                ? "diff-line-modified"
                : "diff-line-added";
              const gutter = modified ? "diff-gutter-modified" : "diff-gutter-added";
              decorations.push({
                range: new monaco.Range(currentNewLine, 1, currentNewLine, 1),
                options: {
                  isWholeLine: true,
                  className,
                  linesDecorationsClassName: gutter,
                  minimap: {
                    color: modified ? "#eab308" : "#22c55e",
                    position: monaco.editor.MinimapPosition.Gutter,
                  },
                },
              });
              currentNewLine++;
            } else if (line.type === "del") {
              delRunLines.push(line.text);
            } else {
              // Context line — flush any pending deletion run as a view zone
              // above THIS line (since it's the line after the deletion).
              flushDeletionRun();
              currentNewLine++;
            }
          }

          // Flush trailing deletion run at end of hunk
          flushDeletionRun();
        }

        // Apply decorations
        decorationIdsRef.current = ed.deltaDecorations([], decorations);

        // Apply view zones
        ed.changeViewZones((accessor) => {
          for (const zd of zoneDefs) {
            const domNode = document.createElement("div");
            domNode.className = "monaco-deleted-block";
            domNode.style.cssText = [
              "background: rgba(127, 17, 17, 0.15);",
              "border-left: 3px solid rgba(239, 68, 68, 0.6);",
              "padding: 2px 8px;",
              "font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'SF Mono', Menlo, monospace;",
              "font-size: 12px;",
              "color: rgba(239, 68, 68, 0.7);",
              "display: flex;",
              "align-items: flex-start;",
              "gap: 6px;",
              "user-select: text;",
            ].join(" ");

            // Copy button
            const copyBtn = document.createElement("button");
            copyBtn.innerHTML = [
              '<svg width="12" height="12" viewBox="0 0 24 24" fill="none"',
              '  stroke="currentColor" stroke-width="2"',
              '  stroke-linecap="round" stroke-linejoin="round">',
              '  <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/>',
              '  <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
              "</svg>",
            ].join("\n");
            copyBtn.title = "Copy deleted text";
            copyBtn.style.cssText = [
              "background: none;",
              "border: none;",
              "cursor: pointer;",
              "color: rgba(239, 68, 68, 0.5);",
              "padding: 0;",
              "flex-shrink: 0;",
              "margin-top: 2px;",
            ].join(" ");
            copyBtn.onmouseover = () => { copyBtn.style.color = "rgba(239, 68, 68, 0.9)"; };
            copyBtn.onmouseout = () => { copyBtn.style.color = "rgba(239, 68, 68, 0.5)"; };
            copyBtn.onclick = (e: MouseEvent) => {
              e.stopPropagation();
              const text = zd.lines.map((l) => l.text).join("\n");
              navigator.clipboard.writeText(text).catch(console.error);
            };

            const textSpan = document.createElement("span");
            textSpan.style.cssText = "white-space: pre-wrap;";
            textSpan.textContent = zd.lines.map((l) => l.text).join("\n");

            domNode.appendChild(copyBtn);
            domNode.appendChild(textSpan);

            const id = accessor.addZone({
              afterLineNumber: Math.max(zd.afterLine, 1),
              heightInLines: zd.lines.length,
              domNode,
            });
            viewZoneIdsRef.current.push(id);
          }
        });
      })
      .catch(() => {
        // 404 or network error — no diff for this file, skip silently.
        // This is the common case (most files have no uncommitted changes).
      });

    return () => {
      cancelled = true;
    };
  }, [path, session, diffVersion]);

  // Reset editor ref when path changes
  useEffect(() => {
    return () => {
      editorRef.current = null;
      monacoRef.current = null;
    };
  }, [path]);

  return (
    <div className="h-full flex flex-col overflow-hidden">
      {/* Editor header */}
      <div className="flex items-center justify-between px-4 py-1.5 border-b border-border bg-muted/30 text-xs text-muted-foreground">
        <span className="font-mono">{path}</span>
        <div className="flex items-center gap-2">
          {readOnly && (
            <span className="text-muted-foreground/60 italic">read-only</span>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 gap-1 text-muted-foreground hover:text-foreground"
            onClick={() => (onOpenSettings ? onOpenSettings() : setSettingsOpen(true))}
            title="Editor settings & extensions"
          >
            <Settings2 className="w-3.5 h-3.5" />
            <span className="text-xs">Settings</span>
          </Button>
        </div>
      </div>

      {/* External-change conflict banner */}
      {externalChange && (
        <div className="flex items-center justify-between gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-1.5 text-xs text-amber-500">
          <span>This file was modified or deleted outside ocode. Your unsaved edits are kept; saving will overwrite the on-disk version.</span>
          <div className="flex shrink-0 items-center gap-1">
            {onReloadFromDisk && (
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs text-amber-500 hover:text-amber-400"
                onClick={onReloadFromDisk}
                title="Discard your edits and load the on-disk content"
              >
                Reload from disk
              </Button>
            )}
            {onDismissExternalChange && (
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground"
                onClick={onDismissExternalChange}
                title="Keep editing your version"
              >
                Dismiss
              </Button>
            )}
          </div>
        </div>
      )}

      {/* Editor settings & extensions panel */}
      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent className="max-w-md h-[70vh] p-0 overflow-hidden gap-0 !flex flex-col">
          <DialogHeader className="px-4 py-2 border-b border-border">
            <DialogTitle className="text-sm">Editor Settings</DialogTitle>
          </DialogHeader>
          <div className="flex-1 min-h-0 overflow-hidden">
            <MonacoSettingsPanel />
          </div>
        </DialogContent>
      </Dialog>

      {/* Monaco editor */}
      <div className="flex-1 overflow-hidden">
        <Editor
          key={path}
          language={lang}
          defaultValue={content}
          onChange={stableOnChange}
          onMount={handleEditorMount}
          loading={
            <div className="flex items-center justify-center h-full">
              <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
            </div>
          }
          options={editorOptions}
          theme={persistedTheme}
        />
      </div>
    </div>
  );
}

// Memoized outer wrapper — inactive tabs skip re-render when another tab's
// content changes. Without this every keystroke re-renders *every* FileEditor
// (App maps over editorTabs) and each runs `editor.getValue()` in the
// Monaco wrapper, multiplying work by tab count. The lag is most visible
// when replacing a selection because that fires both a content and a
// selection event back-to-back.
function arePropsEqual(prev: FileEditorProps, next: FileEditorProps): boolean {
  return (
    prev.path === next.path &&
    prev.content === next.content &&
    prev.language === next.language &&
    prev.readOnly === next.readOnly &&
    prev.session === next.session &&
    prev.diffVersion === next.diffVersion &&
    prev.persistKey === next.persistKey &&
    prev.externalChange === next.externalChange &&
    // Callbacks are forwarded through refs inside the component (stable
    // `stableOnChange` / selection ref), so their identity is intentionally
    // ignored here — the parent inlines `(v) => handleEditorChange(id, v)`
    // on every render and that would otherwise defeat memoization.
    prev.onOpenSettings === next.onOpenSettings
  );
}

export default memo(FileEditorImpl, arePropsEqual);
