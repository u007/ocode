import { useState, useRef, useCallback, useEffect } from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import { Loader2, Settings2 } from "lucide-react";
import type { editor } from "monaco-editor";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import MonacoSettingsPanel from "./MonacoSettingsPanel";

// Ensure Monaco is configured before any editor mounts.
import "../../lib/monaco-setup";

interface FileEditorProps {
  path: string;
  content: string;
  language?: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  onOpenSettings?: () => void;
}

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
    yaml: "yaml",
    yml: "yaml",
    xml: "xml",
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

export default function FileEditor({
  path,
  content,
  language,
  onChange,
  readOnly = false,
  onOpenSettings,
}: FileEditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const [persistedSettings, setPersistedSettings] = useState<editor.IStandaloneEditorConstructionOptions | null>(null);
  const [persistedTheme, setPersistedTheme] = useState<string>("ocode-dark");
  // When the parent doesn't provide onOpenSettings (the current default), the
  // Settings button opens the Monaco settings/extensions panel in a dialog.
  const [settingsOpen, setSettingsOpen] = useState(false);
  const lang = language || extensionToLanguage(path);

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

  const handleEditorMount: OnMount = useCallback((editor, monaco) => {
    editorRef.current = editor;

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
  }, []);

  // Update content when the file path changes
  useEffect(() => {
    return () => {
      editorRef.current = null;
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

      {/* Editor settings & extensions panel (opened by the Settings button when
          the parent doesn't handle onOpenSettings itself). */}
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
          value={content}
          onChange={(val) => onChange?.(val || "")}
          onMount={handleEditorMount}
          loading={
            <div className="flex items-center justify-center h-full">
              <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
            </div>
          }
          options={{
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
            cursorSmoothCaretAnimation: "on",
            padding: { top: 8 },
            suggest: {
              showKeywords: true,
              showSnippets: true,
            },
            multiCursorModifier: "alt",
            selectionClipboard: true,
            // Persisted settings override defaults
            ...persistedSettings,
          }}
          theme={persistedTheme}
        />
      </div>
    </div>
  );
}
