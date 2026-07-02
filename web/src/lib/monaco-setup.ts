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

export { loader, monaco };
