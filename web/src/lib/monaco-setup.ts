import { loader } from "@monaco-editor/react";
import * as monaco from "monaco-editor";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import jsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import cssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import htmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import tsWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";
import { disableMonacoWebKitClipboardWorkaround } from "./monacoClipboardPatch";

// Must run before the first editor mounts: Monaco installs a WebKit
// click/keydown clipboard workaround in the clipboard service's constructor.
// In the desktop WKWebView that fires a denied `navigator.clipboard.write` on
// every click and keystroke (console spam + input lag). See
// lib/monacoClipboardPatch.ts.
disableMonacoWebKitClipboardWorkaround();

// Workers are bundled locally via Vite's `?worker` imports so the editor runs
// fully offline in the ocode-desktop webview — no CDN fetch. Without this,
// Monaco falls back to running language services on the main thread (UI
// freezes) and warns "must define MonacoEnvironment.getWorker".
const monacoEnv: monaco.Environment = {
  getWorker(_workerId: string, label: string) {
    switch (label) {
      case "json":
        return new jsonWorker();
      case "css":
      case "scss":
      case "less":
        return new cssWorker();
      case "html":
      case "handlebars":
      case "razor":
        return new htmlWorker();
      case "typescript":
      case "javascript":
        return new tsWorker();
      default:
        return new editorWorker();
    }
  },
};
(self as unknown as { MonacoEnvironment: monaco.Environment }).MonacoEnvironment = monacoEnv;

// Use the locally bundled monaco instance instead of loading it from a CDN,
// which keeps the editor version in lockstep with the `monaco-editor` package.
loader.config({ monaco });

// Configure TypeScript/JavaScript language services for TSX/JSX.
// Without this, files like `ChatPanel.tsx` opened as `typescript` get no JSX
// tokenization and show spurious diagnostics ("Cannot use JSX unless --jsx").
// `allowNonTsExtensions` lets the TS worker handle .tsx/.jsx URIs, and `jsx`
// enables JSX parsing so the tokenizer recognises `<Tag>` syntax.
// `Preserve` (not `ReactJSX`): ReactJSX injects an implicit
// `import "react/jsx-runtime"` into any JSX-containing file, which the
// in-browser worker cannot resolve (no node_modules) and reports as TS2792
// on line 1. Preserve keeps JSX parsing/tokenization without that import.
monaco.languages.typescript.typescriptDefaults.setCompilerOptions({
  target: monaco.languages.typescript.ScriptTarget.Latest,
  module: monaco.languages.typescript.ModuleKind.ESNext,
  allowNonTsExtensions: true,
  allowJs: true,
  jsx: monaco.languages.typescript.JsxEmit.Preserve,
  jsxFactory: "React.createElement",
  reactNamespace: "React",
  esModuleInterop: true,
  allowSyntheticDefaultImports: true,
  strict: false,
  noEmit: true,
});
monaco.languages.typescript.javascriptDefaults.setCompilerOptions({
  target: monaco.languages.typescript.ScriptTarget.Latest,
  module: monaco.languages.typescript.ModuleKind.ESNext,
  allowNonTsExtensions: true,
  allowJs: true,
  jsx: monaco.languages.typescript.JsxEmit.Preserve,
});
monaco.languages.typescript.typescriptDefaults.setDiagnosticsOptions({
  noSemanticValidation: false,
  noSyntaxValidation: false,
});
monaco.languages.typescript.javascriptDefaults.setDiagnosticsOptions({
  noSemanticValidation: false,
  noSyntaxValidation: false,
});

export { loader, monaco };
// Register a Monarch grammar for Makefiles. Monaco ships no bundled `makefile`
// language (only an editor-settings override), so without this the id resolves
// to plaintext and there is no highlighting.
monaco.languages.register({ id: "makefile" });
monaco.languages.setLanguageConfiguration("makefile", {
  comments: { lineComment: "#" },
  brackets: [
    ["{", "}"],
    ["[", "]"],
    ["(", ")"],
  ],
  autoClosingPairs: [
    { open: "{", close: "}" },
    { open: "[", close: "]" },
    { open: "(", close: ")" },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
});
monaco.languages.setMonarchTokensProvider("makefile", {
  tokenizer: {
    root: [
      // A tab at line start makes a recipe line; its body is tokenized by the
      // `recipe` state, which pops at end of line.
      [/^\t/, "", "@recipe"],
      // Comments.
      [/#.*$/, "comment"],
      // Special targets (`.PHONY: ...`): only the name is a keyword; the
      // colon and prerequisites flow through the rules below.
      [
        /^[ ]*\.(?:PHONY|SUFFIXES|DEFAULT|PRECIOUS|INTERMEDIATE|SECONDARY|DELETE_ON_ERROR|IGNORE|LOW_RESOLUTION_TIME|SILENT|EXPORT_ALL_VARIABLES|NOTPARALLEL|ONESHELL|POSIX)(?=[ \t]*::?(?![:=]))/,
        "keyword",
      ],
      // `else ifeq` and friends: both conditional words are keywords.
      [/^([ ]*else)(\s+)(ifeq|ifneq|ifdef|ifndef)\b/, ["keyword", "", "keyword"]],
      // Assignment name after a prefix word (`export FOO=`, `override FOO =`,
      // `define FOO =`).
      [
        /^([ ]*(?:export|unexport|override|define))([ \t]+)([A-Za-z_][A-Za-z0-9_.-]*)(?=[ \t]*(?::{1,3}|[?+!])?=)/,
        ["keyword", "", "variable"],
      ],
      // Directive words only; the arguments of the line are tokenized normally.
      [
        /^[ ]*(?:-include|sinclude|include|vpath|export|unexport|override|define|endef|ifdef|ifndef|ifeq|ifneq|else|endif)\b/,
        "keyword",
      ],
      // Variable assignments (VAR =, :=, ::=, :::=, ?=, +=, !=). Anchored at
      // line start so words inside values (`-DDEBUG=1`) are not variables.
      [
        /^[ ]*[A-Za-z_][A-Za-z0-9_.-]*(?=[ \t]*(?::{1,3}|[?+!])?=)/,
        "variable",
      ],
      // Targets: names before a single colon. The lookahead rejects `:=`,
      // `::=` and `:::=`, so `FOO := a:b` and `$(VAR):=x` are not targets.
      [
        /^[ ]*[^\s#:=]+(?:[ \t]+[^\s#:=]+)*(?=[ \t]*::?(?![:=]))/,
        "entity.name.function",
      ],
      // Escaped dollar: not a reference, so `$$(HOME)` stays plain text.
      [/\$\$/, ""],
      // Variable/function references and automatic variables.
      [/\$\([^\)]*\)/, "variable"],
      [/\$\{[^\}]*\}/, "variable"],
      [/\$[@<^?*%+|]/, "variable"],
      [/"(?:[^"\\]|\\.)*"/, "string"],
      [/'(?:[^'\\]|\\.)*'/, "string"],
      // Assignment operators, longest first.
      [/:::=|::=|:=|\?=|\+=|!=|=/, "operator"],
    ],
    recipe: [
      // Monarch never tries rules at the end of a non-empty line, so a
      // pop-at-`$` rule would leak the state into the next line. Instead the
      // state ends when the next line starts without a tab (or is empty).
      [/^(?!\t)/, "", "@pop"],
      [/^\t/, ""],
      [/#.*$/, "comment"],
      [/\$\$/, "string"],
      [/\$\([^\)]*\)/, "variable"],
      [/\$\{[^\}]*\}/, "variable"],
      [/\$[@<^?*%+|]/, "variable"],
      [/[^#$]+/, "string"],
      [/\$/, "string"],
    ],
  },
});
