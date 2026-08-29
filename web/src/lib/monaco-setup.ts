import { loader } from "@monaco-editor/react";
import * as monaco from "monaco-editor";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import jsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import cssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import htmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import tsWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";

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
// enables JSX emit so the tokenizer recognises `<Tag>` syntax.
monaco.languages.typescript.typescriptDefaults.setCompilerOptions({
  target: monaco.languages.typescript.ScriptTarget.Latest,
  module: monaco.languages.typescript.ModuleKind.ESNext,
  allowNonTsExtensions: true,
  allowJs: true,
  jsx: monaco.languages.typescript.JsxEmit.ReactJSX,
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
  jsx: monaco.languages.typescript.JsxEmit.ReactJSX,
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
